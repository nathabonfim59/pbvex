package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
)

// QuotaPurpose classifies the storage write a byte reservation covers.
type QuotaPurpose string

const (
	// QuotaPurposeUpload covers writing original upload bytes.
	QuotaPurposeUpload QuotaPurpose = "upload"
	// QuotaPurposeVariant covers writing a derived image variant.
	QuotaPurposeVariant QuotaPurpose = "variant"
)

// QuotaReservationRequest describes a worst-case byte reservation for one
// storage write attempt. OpID is the idempotent identity of the attempt:
// every retry of the same attempt must reuse the same OpID, while a new
// attempt needs a new OpID.
type QuotaReservationRequest struct {
	OpID      string
	Purpose   QuotaPurpose
	StorageID string
	// Key is the object key or prefix the write targets when known.
	Key string
	// WorstCase is the upper bound of bytes the write may store. It must
	// be reserved before any bytes reach the storage backend.
	WorstCase int64
}

// QuotaReservation is a host-acknowledged byte reservation. A reservation
// must end in exactly one of Settle (bytes were written; actual usage) or
// Release (the write definitively did not persist). When the outcome is
// unknown, neither may be called: the provider keeps the reservation for
// reconciliation instead of guessing.
type QuotaReservation interface {
	ID() string
	// Settle transitions the reservation to the actual stored byte count.
	Settle(ctx context.Context, actualBytes int64) error
	// Release returns reserved capacity because no bytes persisted.
	Release(ctx context.Context) error
}

// QuotaCreditRequest reports bytes actually freed by a confirmed deletion.
// OpID is the idempotent identity of the deletion report.
type QuotaCreditRequest struct {
	OpID      string
	StorageID string
	Key       string
	Bytes     int64
}

// QuotaObserver is the provider-neutral boundary between the storage
// service and a host-owned quota service. A nil observer means standalone
// mode: no byte accounting is performed and existing semantics are kept.
//
// The observer is authoritative. Reserve decisions must be atomic at the
// host before acknowledging an allow, and usage must never live in the
// tenant database, so a tenant restore cannot reset consumed quota.
type QuotaObserver interface {
	// Reserve is called before any bytes are written. Returning an error
	// must abort the write: an unavailable quota service fails closed.
	Reserve(ctx context.Context, req QuotaReservationRequest) (QuotaReservation, error)
	// Credit reports confirmed freed bytes after a deletion.
	Credit(ctx context.Context, req QuotaCreditRequest) error
}

var (
	// ErrQuotaDenied is returned by an observer when the provider denied a
	// reservation (for example the tenant is over its byte quota).
	ErrQuotaDenied = errors.New("storage quota denied")
	// ErrQuotaUnavailable is returned when the quota service could not be
	// reached or answered invalidly. Storage writes fail closed.
	ErrQuotaUnavailable = errors.New("storage quota unavailable")
)

// SetQuotaObserver attaches the host quota observer. It must be called
// before the service is used for uploads or downloads (before Start in the
// standard wiring) and is intended to be called at most once. A nil
// observer keeps standalone semantics.
func (s *Service) SetQuotaObserver(observer QuotaObserver) {
	s.quota = observer
}

// hasQuota reports whether a quota observer is attached.
func (s *Service) hasQuota() bool { return s.quota != nil }

// quotaReserve reserves worst-case bytes through the observer, or returns a
// no-op reservation when no observer is attached. Observer errors are
// sanitized to the quota sentinels here — the single choke point for all
// call sites — so raw provider or custom observer error text can never
// propagate into errors or logs.
func (s *Service) quotaReserve(ctx context.Context, req QuotaReservationRequest) (QuotaReservation, error) {
	if s.quota == nil {
		return noopQuotaReservation{}, nil
	}
	res, err := s.quota.Reserve(ctx, req)
	switch {
	case err == nil:
		if res == nil {
			return nil, ErrQuotaUnavailable
		}
		return res, nil
	case errors.Is(err, ErrQuotaDenied):
		return nil, ErrQuotaDenied
	default:
		return nil, ErrQuotaUnavailable
	}
}

// quotaSettle reports the actual stored bytes for a reservation. It is
// best-effort: settlement runs on a cancellation-independent context with
// bounded retries so a caller timeout does not lose the report, and a
// failure never rolls back the committed write.
func (s *Service) quotaSettle(res QuotaReservation, actualBytes int64) {
	if res == nil || isNoopReservation(res) || actualBytes < 0 {
		return
	}
	ctx, cancel := quotaSettlementContext()
	defer cancel()
	var err error
	for attempt := 0; attempt < quotaSettlementAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(quotaSettlementBackoff)
		}
		if err = res.Settle(ctx, actualBytes); err == nil {
			return
		}
		if errors.Is(err, ErrQuotaDenied) {
			break
		}
	}
	s.app.Logger().Warn("storage quota settlement failed; provider reconciliation required",
		"reservationId", res.ID(), "bytes", actualBytes, "classification", classifyQuotaError(err))
}

// quotaRelease returns a reservation because the write definitively did not
// persist. It is best-effort with the same bounded, cancellation-independent
// delivery as settlement.
func (s *Service) quotaRelease(res QuotaReservation) {
	if res == nil || isNoopReservation(res) {
		return
	}
	ctx, cancel := quotaSettlementContext()
	defer cancel()
	var err error
	for attempt := 0; attempt < quotaSettlementAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(quotaSettlementBackoff)
		}
		if err = res.Release(ctx); err == nil {
			return
		}
		if errors.Is(err, ErrQuotaDenied) {
			break
		}
	}
	s.app.Logger().Warn("storage quota release failed; provider reconciliation required",
		"reservationId", res.ID(), "classification", classifyQuotaError(err))
}

// classifyQuotaError maps an observer or transport error onto a fixed
// classification label for logging. Provider and observer errors are never
// logged verbatim: they may carry implementation detail or secrets, and
// only the sanitized classification crosses the log stream.
func classifyQuotaError(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, ErrQuotaDenied):
		return "denied"
	case errors.Is(err, ErrQuotaUnavailable):
		return "unavailable"
	default:
		return "unavailable"
	}
}

// quotaCredit reports confirmed freed bytes for a deletion. It is
// best-effort with bounded retries; a lost report is reconciled by the
// provider rather than blocking or reversing the committed deletion.
func (s *Service) quotaCredit(req QuotaCreditRequest) {
	if s.quota == nil || req.Bytes <= 0 {
		return
	}
	ctx, cancel := quotaSettlementContext()
	defer cancel()
	var err error
	for attempt := 0; attempt < quotaSettlementAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(quotaSettlementBackoff)
		}
		if err = s.quota.Credit(ctx, req); err == nil {
			return
		}
	}
	s.app.Logger().Warn("storage quota credit failed; provider reconciliation required",
		"opId", req.OpID, "storageId", req.StorageID, "bytes", req.Bytes, "classification", classifyQuotaError(err))
}

// releaseReservationIfGone releases the reservation only when the object
// that may have been written is confirmed absent. An existing or unreadable
// object leaves the reservation unsettled for provider reconciliation.
// It reports whether the reservation was released.
func (s *Service) releaseReservationIfGone(app core.App, key string, res QuotaReservation) bool {
	if res == nil || isNoopReservation(res) || key == "" {
		return false
	}
	exists, err := objectExists(app, key)
	if err != nil || exists {
		if err != nil {
			s.app.Logger().Warn("storage quota reservation kept for unreadable object",
				"reservationId", res.ID(), "classification", classifyQuotaError(err))
		} else {
			s.app.Logger().Warn("storage quota reservation kept for uncertain write",
				"reservationId", res.ID())
		}
		return false
	}
	s.quotaRelease(res)
	return true
}

// quotaSettlementContext returns a bounded, caller-cancellation-independent
// context for settlement reports, wrapped in the internal-context marker
// used by background storage work. The cancel function is returned so the
// short-lived helper always releases timer resources deterministically.
func quotaSettlementContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(schema.WithInternalContext(context.Background()), quotaSettlementTimeout)
}

const (
	quotaSettlementAttempts = 3
	quotaSettlementBackoff  = 50 * time.Millisecond
	quotaSettlementTimeout  = 10 * time.Second
)

// noopQuotaReservation is used when no observer is attached.
type noopQuotaReservation struct{}

func (noopQuotaReservation) ID() string                          { return "" }
func (noopQuotaReservation) Settle(context.Context, int64) error { return nil }
func (noopQuotaReservation) Release(context.Context) error       { return nil }

func isNoopReservation(res QuotaReservation) bool {
	_, ok := res.(noopQuotaReservation)
	return ok
}

// newQuotaOpID generates a fresh operation identity for one reservation or
// deletion report. Callers must reuse the returned value across the bounded
// retries of the same operation so retries preserve identity.
func newQuotaOpID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// reserveQuotaForVariant reserves bytes for one derived image variant
// before the persistent object is written. The exact encoded size is known
// because the variant is generated into a bounded local temporary
// filesystem first; the reservation therefore equals the bytes that will be
// stored, and the persistent write cannot exceed it. It returns a nil
// reservation in standalone mode. Denials map to the typed storage-full
// upload error; unavailability fails closed as an internal upload error. In
// both cases the caller must abort before persisting anything.
func (s *Service) reserveQuotaForVariant(ctx context.Context, storageID, thumbKey string, exactBytes int64) (QuotaReservation, error) {
	if !s.hasQuota() {
		return nil, nil
	}
	res, err := s.quotaReserve(ctx, QuotaReservationRequest{
		OpID:      newQuotaOpID("variant"),
		Purpose:   QuotaPurposeVariant,
		StorageID: storageID,
		Key:       thumbKey,
		WorstCase: exactBytes,
	})
	if err == nil {
		return res, nil
	}
	if errors.Is(err, ErrQuotaDenied) {
		s.app.Logger().Info("storage variant denied by quota",
			"storageId", storageID, "bytes", exactBytes)
		return nil, &UploadError{Code: ErrorCodeStorageFull, Message: "storage quota exhausted", Err: ErrQuotaDenied}
	}
	return nil, &UploadError{Code: ErrorCodeInternal, Message: "storage quota check failed", Err: ErrQuotaUnavailable}
}

// objectExists reports whether an object key exists in the app filesystem.
func objectExists(app core.App, key string) (bool, error) {
	fs, err := app.NewFilesystem()
	if err != nil {
		return false, err
	}
	defer fs.Close()
	return fs.Exists(key)
}

// sumPrefixBytes returns the stored byte total for every object under an
// object prefix. Directory markers are ignored. A listing failure surfaces
// as an error so callers can skip crediting instead of under-reporting.
func sumPrefixBytes(app core.App, prefix string) (int64, error) {
	if prefix == "" {
		return 0, errors.New("empty object prefix")
	}
	fs, err := app.NewFilesystem()
	if err != nil {
		return 0, err
	}
	defer fs.Close()
	objects, err := fs.List(prefix)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, obj := range objects {
		if obj == nil || obj.IsDir {
			continue
		}
		total += obj.Size
	}
	return total, nil
}

// quotaDeleteOpID builds the stable deletion-report identity for a PBVex
// storage id (a storage id is deleted at most once).
func quotaDeleteOpID(storageID string) string {
	return "delete-" + storageID
}

// reserveQuotaForUpload reserves the worst-case upload bytes at the quota
// service before the request body is staged. It returns a nil reservation
// in standalone mode. Denials map to the typed storage-full upload error;
// unavailability fails closed as an internal upload error. In both cases
// the caller must abort the upload before writing.
func (s *Service) reserveQuotaForUpload(ctx context.Context, storageID, stageKey, filename string, maxSize int64) (QuotaReservation, error) {
	if !s.hasQuota() {
		return nil, nil
	}
	res, err := s.quotaReserve(ctx, QuotaReservationRequest{
		OpID:      newQuotaOpID("upload"),
		Purpose:   QuotaPurposeUpload,
		StorageID: storageID,
		Key:       stageKey,
		// maxSize is already the effective per-attempt staging cap.
		WorstCase: maxSize,
	})
	if err == nil {
		return res, nil
	}
	if errors.Is(err, ErrQuotaDenied) {
		s.app.Logger().Info("storage upload denied by quota",
			"storageId", storageID, "filename", filename, "worstCase", maxSize)
		return nil, &UploadError{Code: ErrorCodeStorageFull, Message: "storage quota exhausted", Err: ErrQuotaDenied}
	}
	return nil, &UploadError{Code: ErrorCodeInternal, Message: "storage quota check failed", Err: err}
}

// quotaKeepForUncertainWrite logs that a possibly-partial write leaves its
// reservation unsettled. The provider reconciles such reservations; the
// bytes are never silently released on an uncertain outcome.
func (s *Service) quotaKeepForUncertainWrite(res QuotaReservation, key string) {
	if res == nil || isNoopReservation(res) {
		return
	}
	s.app.Logger().Warn("storage quota reservation kept for uncertain write",
		"reservationId", res.ID(), "key", key)
}

// quotaPrefixFor returns the object prefix that holds a PBVex object and
// its derived variants for a file key ("prefix/<id>/blob" or the stage key).
func quotaPrefixFor(fileKey string) string {
	trimmed := strings.TrimSuffix(fileKey, "/blob")
	if idx := strings.Index(trimmed, "/_stage/"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	return strings.TrimRight(trimmed, "/") + "/"
}
