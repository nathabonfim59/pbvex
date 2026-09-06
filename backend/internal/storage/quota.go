package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register decoders for image.DecodeConfig
	_ "image/png"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
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
// no-op reservation when no observer is attached.
func (s *Service) quotaReserve(ctx context.Context, req QuotaReservationRequest) (QuotaReservation, error) {
	if s.quota == nil {
		return noopQuotaReservation{}, nil
	}
	res, err := s.quota.Reserve(ctx, req)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("quota observer returned nil reservation: %w", ErrQuotaUnavailable)
	}
	return res, nil
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
		"reservationId", res.ID(), "bytes", actualBytes, "error", err)
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
		"reservationId", res.ID(), "error", err)
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
		"opId", req.OpID, "storageId", req.StorageID, "bytes", req.Bytes, "error", err)
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
				"reservationId", res.ID(), "error", err)
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

// worstCaseThumbBytes returns a conservative upper bound for a generated
// image variant stored by filesystem.CreateThumb. Variants are re-encoded
// in the original's format (JPEG or PNG). Decoded RGBA pixels (4 bytes per
// pixel) plus a 6.25% encoder and header overhead safely bound both
// encoders at the validated variant dimensions.
func worstCaseThumbBytes(selector string, originalWidth, originalHeight int) int64 {
	width, height, ok := effectiveThumbDimensions(selector, originalWidth, originalHeight)
	if !ok || width <= 0 || height <= 0 {
		return 0
	}
	raw := int64(width) * int64(height) * 4
	return raw + raw/16 + 4096
}

// effectiveThumbDimensions mirrors validateThumbForImage: it derives the
// final variant pixel size from the selector and the original aspect ratio.
func effectiveThumbDimensions(selector string, originalWidth, originalHeight int) (int, int, bool) {
	match := filesystem.ThumbSizeRegex.FindStringSubmatch(selector)
	if len(match) == 0 || originalWidth <= 0 || originalHeight <= 0 {
		return 0, 0, false
	}
	width, widthErr := strconv.Atoi(match[1])
	height, heightErr := strconv.Atoi(match[2])
	if widthErr != nil || heightErr != nil {
		return 0, 0, false
	}
	if width == 0 {
		width = int((int64(originalWidth)*int64(height) + int64(originalHeight) - 1) / int64(originalHeight))
	} else if height == 0 {
		height = int((int64(originalHeight)*int64(width) + int64(originalWidth) - 1) / int64(originalWidth))
	}
	return width, height, width > 0 && height > 0
}

// imageDimensions reads the pixel size of a stored image without decoding
// the full body. It is used to bound variant reservations on paths without
// persisted metadata. The caller must pass a fresh reader.
func imageDimensions(r io.Reader) (int, int, bool) {
	if r == nil {
		return 0, 0, false
	}
	config, _, err := image.DecodeConfig(r)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
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

// reserveQuotaForVariant reserves the worst-case bytes for one derived image
// variant before generation starts. It returns a nil reservation in
// standalone mode. The bound derives from the persisted image metadata; a
// record without usable metadata cannot be bounded and fails closed.
func (s *Service) reserveQuotaForVariant(ctx context.Context, storageID, thumbKey, selector string) (QuotaReservation, error) {
	if !s.hasQuota() {
		return nil, nil
	}
	record, err := s.repo.GetFile(schema.WithInternalContext(ctx), s.app, storageID)
	if err != nil {
		return nil, err
	}
	meta, err := imageMetadataFromRecord(record)
	if err != nil || meta == nil {
		return nil, &UploadError{Code: ErrorCodeInternal, Message: "variant size cannot be bounded", Err: err}
	}
	worstCase := worstCaseThumbBytes(selector, meta.Width, meta.Height)
	if worstCase <= 0 {
		return nil, &UploadError{Code: ErrorCodeBadRequest, Message: "invalid image thumb"}
	}
	res, err := s.quotaReserve(ctx, QuotaReservationRequest{
		OpID:      newQuotaOpID("variant"),
		Purpose:   QuotaPurposeVariant,
		StorageID: storageID,
		Key:       thumbKey,
		WorstCase: worstCase,
	})
	if err == nil {
		return res, nil
	}
	if errors.Is(err, ErrQuotaDenied) {
		s.app.Logger().Info("storage variant denied by quota",
			"storageId", storageID, "thumb", selector, "worstCase", worstCase)
		return nil, &UploadError{Code: ErrorCodeStorageFull, Message: "storage quota exhausted", Err: ErrQuotaDenied}
	}
	return nil, &UploadError{Code: ErrorCodeInternal, Message: "storage quota check failed", Err: err}
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

// quotaSettledBytesFrom reads the actual size of a stored object for
// settlement. A read failure returns -1 so callers keep the reservation
// unsettled rather than reporting a wrong value.
func quotaSettledBytesFrom(app core.App, key string) int64 {
	fs, err := app.NewFilesystem()
	if err != nil {
		return -1
	}
	defer fs.Close()
	attrs, err := fs.Attributes(key)
	if err != nil || attrs == nil || attrs.Size < 0 {
		return -1
	}
	return attrs.Size
}
