package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/filesystem/blob"
	"github.com/pocketbase/pocketbase/tools/types"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"
)

// AuthContext is the caller identity carried through to storage operations.
// UserID binds signed URLs and supplies audit metadata; it does not impose
// automatic file ownership. Application functions remain responsible for
// deciding who may request a URL or delete a StorageID.
type AuthContext struct {
	IsAuthenticated bool
	TokenIdentifier string
	// UserID is retained for source compatibility with existing embedders. New
	// PBVex request paths always use TokenIdentifier.
	UserID string
}

func (a AuthContext) identifier() string {
	if a.TokenIdentifier != "" {
		return a.TokenIdentifier
	}
	return a.UserID
}

// Service is the application layer for file storage.
type Service struct {
	app    core.App
	repo   *Repo
	config Config
	kr     *keyring

	cleanupStop     chan struct{}
	cleanupDone     chan struct{}
	cleanupStopOnce sync.Once
	cleanupOnce     sync.Once
	cleanupMutex    sync.Mutex

	// persistHook is a test-only hook invoked at the start of persistStagedBlob.
	// When non-nil it can block to simulate a slow backend persist.
	persistHook func()

	thumbPending singleflight.Group
	thumbSem     *semaphore.Weighted
}

// NewService creates a new storage service.
func NewService(app core.App, repo *Repo, config Config) (*Service, error) {
	cfg, err := NormalizeConfig(config)
	if err != nil {
		return nil, fmt.Errorf("invalid storage config: %w", err)
	}
	return &Service{
		app:      app,
		repo:     repo,
		config:   cfg,
		kr:       newKeyring(app, cfg),
		thumbSem: semaphore.NewWeighted(2),
	}, nil
}

// WarmActive pre-loads signing keys and any other runtime state.
func (s *Service) WarmActive() error {
	if err := s.repo.BackfillPublicTokens(schema.WithInternalContext(context.Background()), s.app); err != nil {
		return fmt.Errorf("backfill storage public tokens: %w", err)
	}
	return s.kr.LoadOrCreate(schema.WithInternalContext(context.Background()))
}

// GenerateUploadURL returns a short-lived, single-use URL for uploading a file.
func (s *Service) GenerateUploadURL(ctx context.Context, auth AuthContext) (string, error) {
	return s.generateUploadURL(ctx, auth, nil)
}

// GenerateImageUploadURL creates an upload URL bound to a schema image policy.
func (s *Service) GenerateImageUploadURL(ctx context.Context, auth AuthContext, policy ImagePolicy) (string, error) {
	if err := validateImagePolicy(&policy); err != nil {
		return "", err
	}
	return s.generateUploadURL(ctx, auth, &policy)
}

func (s *Service) generateUploadURL(ctx context.Context, auth AuthContext, policy *ImagePolicy) (string, error) {
	storageID, err := GenerateStorageID()
	if err != nil {
		return "", fmt.Errorf("generate upload url: %w", err)
	}
	token, err := GenerateToken()
	if err != nil {
		return "", fmt.Errorf("generate upload url: %w", err)
	}

	tokenHash := HashToken(token)
	maxSize := s.config.MaxFileSize
	if s.config.DefaultTokenMaxSize > 0 && s.config.DefaultTokenMaxSize < maxSize {
		maxSize = s.config.DefaultTokenMaxSize
	}

	createdBy := ""
	if auth.IsAuthenticated {
		createdBy = auth.identifier()
	}

	rec := TokenRecord{
		TokenHash:    tokenHash,
		StorageID:    storageID,
		ExpiresAt:    time.Now().UTC().Add(s.config.DefaultUploadTTL),
		CreatedBy:    createdBy,
		MaxSize:      maxSize,
		AllowedTypes: s.config.AllowedContentTypes,
	}
	if policy != nil {
		rec.AllowedTypes = policy.MimeTypes
		rec.Policy = policy
	}

	app := s.appFor(ctx)
	if _, err := s.repo.CreateToken(schema.WithInternalContext(ctx), app, rec); err != nil {
		return "", err
	}

	return s.uploadURL(token), nil
}

// GetMetadata returns persisted metadata for a storage object.
func (s *Service) GetMetadata(ctx context.Context, storageID string) (map[string]any, error) {
	if err := ValidateStorageID(storageID); err != nil {
		return nil, nil
	}
	record, err := s.repo.GetFile(schema.WithInternalContext(ctx), s.appFor(ctx), storageID)
	if err != nil {
		if errors.Is(err, ErrStorageNotFound) {
			return nil, nil
		}
		return nil, err
	}
	result := map[string]any{
		"storageId":   storageID,
		"kind":        "file",
		"createdBy":   record.GetString(schema.FieldStorageCreatedBy),
		"filename":    record.GetString(schema.FieldStorageFilename),
		"contentType": record.GetString(schema.FieldStorageContentType),
		"size":        record.GetInt(schema.FieldStorageSize),
		"sha256":      record.GetString(schema.FieldStorageSha256),
		"extension":   fileExtension(record.GetString(schema.FieldStorageFilename), record.GetString(schema.FieldStorageContentType)),
	}
	if metadata, err := imageMetadataFromRecord(record); err != nil {
		return nil, err
	} else if metadata != nil {
		result["kind"] = metadata.Kind
		result["extension"] = metadata.Extension
		result["width"] = metadata.Width
		result["height"] = metadata.Height
		result["thumbs"] = metadata.Thumbs
	}
	return result, nil
}

func fileExtension(filename, contentType string) string {
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(filename)), ".")
	if extension != "" && len(extension) <= 16 {
		return extension
	}
	if extensions, err := mime.ExtensionsByType(contentType); err == nil && len(extensions) > 0 {
		return strings.TrimPrefix(extensions[0], ".")
	}
	return ""
}

// GetURL returns a signed short-lived download URL for the storage ID, or an empty string if missing/deleted.
func (s *Service) GetURL(ctx context.Context, storageID string, auth AuthContext) (string, error) {
	return s.getURL(ctx, storageID, auth, false)
}

// GetCapabilityURL returns a signed short-lived bearer URL that does not require caller authentication.
func (s *Service) GetCapabilityURL(ctx context.Context, storageID string) (string, error) {
	return s.getURL(ctx, storageID, AuthContext{}, true)
}

// GetPublicURL returns the stable public bearer URL for a stored file.
func (s *Service) GetPublicURL(ctx context.Context, storageID string) (string, error) {
	if err := ValidateStorageID(storageID); err != nil {
		return "", nil
	}
	record, err := s.repo.GetFile(schema.WithInternalContext(ctx), s.appFor(ctx), storageID)
	if err != nil {
		if errors.Is(err, ErrStorageNotFound) {
			return "", nil
		}
		return "", err
	}
	token := record.GetString(schema.FieldStoragePublicToken)
	if err := validatePublicToken(token); err != nil {
		return "", fmt.Errorf("stored public token: %w", err)
	}
	base := strings.TrimRight(s.config.BaseURL, "/")
	if base == "" {
		base = strings.TrimRight(s.app.Settings().Meta.AppURL, "/")
	}
	return base + s.config.BasePath + "/public/" + token + "/blob.bin", nil
}

func (s *Service) getURL(ctx context.Context, storageID string, auth AuthContext, capability bool) (string, error) {
	if err := ValidateStorageID(storageID); err != nil {
		return "", nil
	}
	app := s.appFor(ctx)
	if _, err := s.repo.GetFile(schema.WithInternalContext(ctx), app, storageID); err != nil {
		if errors.Is(err, ErrStorageNotFound) {
			return "", nil
		}
		return "", err
	}
	return s.signURL(ctx, storageID, auth, 0, capability)
}

// Delete removes a stored file and its metadata.
// When called inside a transaction, it marks the file as deleting and schedules
// the irreversible blob deletion in TxInfo.OnComplete after successful commit.
func (s *Service) Delete(ctx context.Context, storageID string) error {
	if err := ValidateStorageID(storageID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidStorageID, err)
	}

	app := s.appFor(ctx)
	if app.IsTransactional() {
		return s.deleteInApp(ctx, app, storageID)
	}

	return s.app.RunInTransaction(func(txApp core.App) error {
		return s.deleteInApp(ctx, txApp, storageID)
	})
}

func (s *Service) deleteInApp(ctx context.Context, app core.App, storageID string) error {
	record, err := s.repo.MarkFileStatus(schema.WithInternalContext(ctx), app, storageID, statusDeleting)
	if err != nil {
		return err
	}
	fileKey := record.GetString(schema.FieldStorageFileKey)

	if app.TxInfo() == nil {
		return s.deleteBlob(record, fileKey)
	}

	app.TxInfo().OnComplete(func(txErr error) error {
		if txErr != nil {
			return nil
		}
		// Best-effort post-commit delete: a failure leaves the record in the
		// deleting state for the cleanup worker to retry, so do not surface it
		// to the caller (the transaction already succeeded).
		if err := s.deleteBlob(record, fileKey); err != nil {
			s.app.Logger().Warn("storage post-commit delete failed; cleanup will retry",
				"storageId", storageID, "error", err)
		}
		return nil
	})
	return nil
}

// deleteBlob removes the blob and marks the metadata deleted. It returns an
// error when either step fails so that callers (notably the cleanup worker) can
// report accurate recovery metrics and retry. A missing blob is treated as
// success.
func (s *Service) deleteBlob(record *core.Record, fileKey string) error {
	if err := s.deleteFilePrefix(s.app, strings.TrimRight(path.Dir(fileKey), "/")+"/"); err != nil {
		return fmt.Errorf("delete storage blob: %w", err)
	}
	record.Set(schema.FieldStorageStatus, statusDeleted)
	record.Set(schema.FieldStorageDeletedAt, types.NowDateTime())
	record.Set("updated", types.NowDateTime())
	if err := s.app.SaveWithContext(schema.WithInternalContext(context.Background()), record); err != nil {
		return fmt.Errorf("mark storage file deleted: %w", err)
	}
	return nil
}

func (s *Service) deleteFilePrefix(app core.App, prefix string) error {
	if prefix == "" || prefix == "." || prefix == "/" {
		return fmt.Errorf("refusing to delete invalid storage prefix")
	}
	fs, err := app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fs.Close()
	for _, err := range fs.DeletePrefix(prefix) {
		if err != nil && !errors.Is(err, filesystem.ErrNotFound) {
			return err
		}
	}
	return nil
}

// Upload streams and persists a file from an upload token.
// The commit creates a staged file record, then OnComplete moves the staged blob
// to the final key and marks the file active. If the transaction fails, the claim
// is released and the staged blob is removed.
func (s *Service) Upload(ctx context.Context, token string, body io.Reader, contentType, filename string, headerSize int64) (string, error) {
	if token == "" {
		return "", &UploadError{Code: ErrorCodeBadRequest, Message: "missing upload token"}
	}

	tokenHash := HashToken(token)
	attempt, err := GenerateAttempt()
	if err != nil {
		return "", &UploadError{Code: ErrorCodeInternal, Message: "failed to prepare upload", Err: err}
	}

	app := s.appFor(ctx)

	claimExpiry := s.claimExpiry()
	tokenRec, err := s.repo.ClaimToken(schema.WithInternalContext(ctx), app, tokenHash, attempt, claimExpiry)
	if err != nil {
		// Only a genuine CAS miss (ErrTokenClaimFailed) can be safely
		// classified as expired/consumed/pending. Any other failure is a
		// database or context error that must propagate; release any claim that
		// may have been acquired before the read failed so the token is
		// retryable without waiting for the claim lease to expire.
		if errors.Is(err, ErrTokenClaimFailed) {
			return "", s.classifyTokenFailure(ctx, app, tokenHash)
		}
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		return "", &UploadError{Code: ErrorCodeInternal, Message: "failed to claim upload token", Err: err}
	}

	if tokenRec.GetString(schema.FieldTokenClaim) != attempt {
		return "", &UploadError{Code: ErrorCodeUnauthorized, Message: "invalid upload token"}
	}

	if exp := tokenRec.GetDateTime(schema.FieldTokenExpiresAt); exp.Time().Before(time.Now().UTC()) {
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		return "", &UploadError{Code: ErrorCodeUploadExpired, Message: "upload token expired"}
	}

	maxSize := int64(tokenRec.GetInt(schema.FieldTokenMaxSize))
	if maxSize <= 0 || maxSize > s.config.MaxFileSize {
		maxSize = s.config.MaxFileSize
	}

	storageID := tokenRec.GetString(schema.FieldTokenStorageID)
	stageKey := s.stageFileKey(storageID, attempt)
	finalKey := s.fileKey(storageID)

	filename, err = s.normalizeFilename(filename)
	if err != nil {
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		return "", &UploadError{Code: ErrorCodeBadRequest, Message: err.Error(), Err: err}
	}

	imagePolicy, err := imagePolicyFromRecord(tokenRec, schema.FieldTokenPolicy)
	if err != nil {
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		return "", &UploadError{Code: ErrorCodeInternal, Message: "invalid upload policy", Err: err}
	}
	if imagePolicy == nil {
		if err := s.validateContentType(contentType, tokenRec.GetString(schema.FieldTokenAllowedTypes)); err != nil {
			_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
			return "", err
		}
	}

	createdBy := tokenRec.GetString(schema.FieldTokenCreatedBy)
	reservationContentType := contentType
	if imagePolicy != nil {
		reservationContentType = "application/octet-stream"
	}

	// Reserve capacity BEFORE reading the request body. The durable "uploading"
	// record counts toward MaxFiles and is reclaimed by the cleanup worker if the
	// process exits before the stage blob is committed. Because it is not
	// "staged", cleanup never finalizes it ahead of its blob.
	if _, err := s.reserveUpload(ctx, app, storageID, stageKey, filename, reservationContentType, createdBy, attempt); err != nil {
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		var uploadErr *UploadError
		if errors.As(err, &uploadErr) {
			s.app.Logger().Info("storage upload rejected", "storageId", storageID, "contentType", contentType, "code", string(uploadErr.Code))
		}
		return "", err
	}

	// Renew the reservation lease while staging and backend persistence are
	// active so cleanup cannot reclaim a live upload even if it outlives a single
	// lease interval. The renewer's lifetime follows the actual staging/persist
	// operation — it stops only after that operation has returned (not merely on
	// request-context cancellation, because a blocked reader or slow backend can
	// keep running after ctx.Done) — and is joined before commit/release so no
	// renewal goroutine outlives the upload.
	stopRenewer := s.startUploadLeaseRenewer(storageID, attempt)

	// Read the client stream into a bounded local temp file (computing the
	// digest). Capacity is already reserved, so a cap-full request never buffers
	// a body.
	tmpPath, sha, size, err := s.stageToTemp(body, filename, maxSize, headerSize)
	if err != nil {
		stopRenewer()
		_ = s.releaseReservation(storageID, attempt)
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		return "", err
	}
	defer os.Remove(tmpPath)

	var metadata any
	if imagePolicy != nil {
		if err := s.thumbSem.Acquire(ctx, 1); err != nil {
			stopRenewer()
			_ = s.releaseReservation(storageID, attempt)
			_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
			return "", &UploadError{Code: ErrorCodeInternal, Message: "image inspection cancelled", Err: err}
		}
		imageMetadata, detectedContentType, inspectErr := inspectImage(tmpPath, imagePolicy)
		s.thumbSem.Release(1)
		if inspectErr != nil {
			stopRenewer()
			_ = s.releaseReservation(storageID, attempt)
			_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
			return "", inspectErr
		}
		metadata = imageMetadata
		contentType = detectedContentType
	}

	// Persist the staged blob to the storage backend (now capacity-reserved).
	persistErr := s.persistStagedBlob(tmpPath, stageKey, filename)
	stopRenewer()
	if persistErr != nil {
		_ = s.releaseReservation(storageID, attempt)
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		s.app.Logger().Error("storage upload persist failed", "storageId", storageID, "error", persistErr)
		return "", persistErr
	}
	if storedType, err := s.storedContentType(stageKey); err != nil {
		_ = s.deleteFile(app, stageKey)
		_ = s.releaseReservation(storageID, attempt)
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		return "", &UploadError{Code: ErrorCodeInternal, Message: "failed to inspect stored upload", Err: err}
	} else {
		contentType = storedType
	}
	if imagePolicy == nil {
		if err := s.validateContentType(contentType, tokenRec.GetString(schema.FieldTokenAllowedTypes)); err != nil {
			_ = s.deleteFile(app, stageKey)
			_ = s.releaseReservation(storageID, attempt)
			_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
			return "", err
		}
	}

	// Commit: atomically consume the token and CAS-transition the reservation
	// from uploading to staged with the finalized metadata. A CAS failure here
	// means the reservation was reclaimed or the claim was lost during the
	// (potentially slow) staging; classify/handle precisely rather than 500.
	if err := s.commitUpload(ctx, app, tokenHash, attempt, storageID, sha, size, stageKey, contentType, metadata); err != nil {
		_ = s.deleteFile(app, stageKey)
		_ = s.releaseReservation(storageID, attempt)
		if errors.Is(err, ErrTokenClaimFailed) {
			return "", s.classifyTokenFailure(ctx, app, tokenHash)
		}
		if errors.Is(err, ErrReservationLost) {
			s.app.Logger().Warn("storage upload reservation lost during commit", "storageId", storageID)
			return "", &UploadError{Code: ErrorCodeInternal, Message: "upload reservation expired", Err: err}
		}
		_ = s.repo.ReleaseClaim(schema.WithInternalContext(ctx), app, tokenHash, attempt)
		s.app.Logger().Error("storage upload commit failed", "storageId", storageID, "error", err)
		return "", &UploadError{Code: ErrorCodeInternal, Message: "failed to commit upload", Err: err}
	}

	// Finalize: move the staged blob to its final key and mark the record
	// active. Re-fetch the record fresh because the commit CAS updated its
	// metadata directly in the database. Idempotent and recoverable by the
	// cleanup worker on crash.
	staged, err := s.repo.GetFileByIDAnyStatus(schema.WithInternalContext(ctx), s.app, storageID)
	if err != nil {
		return "", &UploadError{Code: ErrorCodeInternal, Message: "failed to load staged file", Err: err}
	}
	if err := s.finalizeUpload(staged, stageKey, finalKey); err != nil {
		s.app.Logger().Error("storage upload finalize failed; cleanup will recover", "storageId", storageID, "error", err)
		return "", &UploadError{Code: ErrorCodeInternal, Message: "failed to finalize upload", Err: err}
	}

	s.app.Logger().Info("storage upload complete", "storageId", storageID, "size", size, "contentType", contentType, "createdBy", createdBy)
	if s.config.MaxFiles > 0 {
		if used, err := s.repo.GetActiveFilesCount(schema.WithInternalContext(ctx), s.app); err == nil {
			if float64(used) >= float64(s.config.MaxFiles)*0.8 {
				s.app.Logger().Warn("storage approaching file cap", "used", used, "cap", s.config.MaxFiles)
			}
		}
	}

	return storageID, nil
}

func (s *Service) storedContentType(fileKey string) (string, error) {
	fs, err := s.app.NewFilesystem()
	if err != nil {
		return "", err
	}
	defer fs.Close()
	attrs, err := fs.Attributes(fileKey)
	if err != nil {
		return "", err
	}
	mediaType, _, err := mime.ParseMediaType(attrs.ContentType)
	if err != nil {
		return "", err
	}
	return mediaType, nil
}

// reserveUpload atomically reserves upload capacity before the request body is
// read: it enforces the MaxFiles cap and creates a durable "uploading" record
// that consumes a capacity slot. The record carries the attempt identity as
// owner and a renewable leaseUntil so cleanup can only reclaim it once the
// upload stops renewing. The token is not consumed yet so that body staging
// failures remain retryable.
func (s *Service) reserveUpload(ctx context.Context, app core.App, storageID, fileKey, filename, contentType, createdBy, owner string) (*core.Record, error) {
	publicToken, err := GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate public token: %w", err)
	}
	rec := FileRecord{
		StorageID:   storageID,
		ContentType: contentType,
		FileKey:     fileKey,
		Filename:    filename,
		CreatedBy:   createdBy,
		Status:      statusUploading,
		Owner:       owner,
		LeaseUntil:  time.Now().UTC().Add(s.leaseInterval()),
		PublicToken: publicToken,
	}
	var reserved *core.Record
	txErr := s.app.RunInTransaction(func(txApp core.App) error {
		if s.config.MaxFiles > 0 {
			count, err := s.repo.GetActiveFilesCount(schema.WithInternalContext(ctx), txApp)
			if err != nil {
				return err
			}
			if count >= s.config.MaxFiles {
				s.app.Logger().Warn("storage file cap reached", "cap", s.config.MaxFiles, "active", count)
				return &UploadError{Code: ErrorCodeStorageFull, Message: "storage file cap reached"}
			}
		}
		created, err := s.repo.CreateFile(schema.WithInternalContext(ctx), txApp, rec)
		if err != nil {
			return err
		}
		reserved = created
		return nil
	})
	if txErr != nil {
		var uploadErr *UploadError
		if errors.As(txErr, &uploadErr) {
			return nil, uploadErr
		}
		return nil, &UploadError{Code: ErrorCodeInternal, Message: "failed to reserve upload", Err: txErr}
	}
	return reserved, nil
}

// commitUpload atomically consumes the single-use token and CAS-transitions the
// reservation from uploading to staged with the finalized metadata. The
// ownership/status CAS means it cannot transition a record that cleanup
// reclaimed or another owner took; it surfaces ErrReservationLost in that case.
// A token CAS failure (ErrTokenClaimFailed) surfaces for the caller to classify.
func (s *Service) commitUpload(ctx context.Context, app core.App, tokenHash, attempt, storageID, sha string, size int64, fileKey, contentType string, metadata any) error {
	return s.app.RunInTransaction(func(txApp core.App) error {
		if err := s.repo.ConsumeToken(schema.WithInternalContext(ctx), txApp, tokenHash, attempt); err != nil {
			return err
		}
		return s.repo.TransitionUploadingToStaged(schema.WithInternalContext(ctx), txApp, storageID, attempt, sha, size, fileKey, contentType, metadata)
	})
}

// releaseReservation removes an uploading reservation owned by owner so its
// capacity slot and storage id are freed for retry. The ownership/status CAS
// makes it idempotent and unable to delete a record that was already reclaimed
// or committed by another path.
func (s *Service) releaseReservation(storageID, owner string) error {
	return s.repo.ReleaseReservation(schema.WithInternalContext(context.Background()), s.app, storageID, owner)
}

// startUploadLeaseRenewer launches a goroutine that periodically renews the
// uploading reservation lease while staging/persistence are active. It returns a
// stop function that signals the loop to exit and blocks until the goroutine has
// returned, ensuring the renewal goroutine is joined before commit/release. The
// renewer's lifetime is tied to the staging/persist operation (via stop), NOT to
// the request context, because a blocked reader or slow backend can keep running
// after request cancellation and must remain protected until the operation truly
// finishes.
func (s *Service) startUploadLeaseRenewer(storageID, owner string) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	var once sync.Once
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.renewUploadLeaseLoop(storageID, owner, done)
	}()
	return func() {
		once.Do(func() { close(done) })
		wg.Wait()
	}
}

// renewUploadLeaseLoop periodically renews the uploading reservation lease. It
// stops when done is closed and is a no-op (CAS affects zero rows) if the
// reservation was already reclaimed or transitioned. The cadence is always
// strictly positive so a tiny leaseInterval cannot panic NewTicker.
func (s *Service) renewUploadLeaseLoop(storageID, owner string, done chan struct{}) {
	interval := s.leaseInterval()
	cadence := interval / 2
	if cadence < time.Nanosecond {
		cadence = time.Nanosecond
	}
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			until := time.Now().UTC().Add(interval)
			if err := s.repo.RenewUploadLease(schema.WithInternalContext(context.Background()), s.app, storageID, owner, until); err != nil && !errors.Is(err, ErrReservationLost) {
				s.app.Logger().Warn("storage upload lease renewal failed", "storageId", storageID, "error", err)
			}
		}
	}
}

// leaseInterval returns the uploading reservation lease window. It is renewed
// at half this interval while an upload is active.
func (s *Service) leaseInterval() time.Duration {
	if s.config.UploadLeaseInterval > 0 {
		return s.config.UploadLeaseInterval
	}
	return 30 * time.Second
}

// classifyTokenFailure inspects a token whose CAS claim genuinely missed
// (ErrTokenClaimFailed) and returns a precise error: consumed, expired, or
// still in-use by another attempt. Database/context read failures are
// propagated as internal errors rather than masked as an invalid token.
func (s *Service) classifyTokenFailure(ctx context.Context, app core.App, tokenHash string) error {
	rec, err := s.repo.GetTokenByHashAnyState(schema.WithInternalContext(ctx), app, tokenHash)
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			// No record at all: do not reveal whether the token ever existed.
			return &UploadError{Code: ErrorCodeUnauthorized, Message: "invalid upload token"}
		}
		return &UploadError{Code: ErrorCodeInternal, Message: "failed to validate upload token", Err: err}
	}
	if rec.GetBool(schema.FieldTokenConsumed) {
		return &UploadError{Code: ErrorCodeUploadConsumed, Message: "upload token already consumed"}
	}
	if exp := rec.GetDateTime(schema.FieldTokenExpiresAt); exp.Time().Before(time.Now().UTC()) {
		return &UploadError{Code: ErrorCodeUploadExpired, Message: "upload token expired"}
	}
	if claim := rec.GetString(schema.FieldTokenClaim); claim != "" {
		return &UploadError{Code: ErrorCodeUploadPending, Message: "upload token is in use"}
	}
	return &UploadError{Code: ErrorCodeUnauthorized, Message: "invalid upload token"}
}

// finalizeUpload moves a staged blob to its final key and marks the record active.
// It is idempotent: it will recover a partially completed move by the cleanup worker.
func (s *Service) finalizeUpload(record *core.Record, stageKey, finalKey string) error {
	fs, err := s.app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fs.Close()

	finalExists, err := fs.Exists(finalKey)
	if err != nil {
		return err
	}

	if !finalExists {
		if err := fs.Copy(stageKey, finalKey); err != nil {
			if !errors.Is(err, filesystem.ErrNotFound) {
				return err
			}
			// Both the staged and final blobs are missing: the upload data is
			// irrecoverable. Mark the record deleted so it is observable and
			// releases its capacity reservation instead of silently leaking
			// forever in the staged state.
			return s.markFileLost(record)
		}
	}

	if err := fs.Delete(stageKey); err != nil {
		// Best effort; if the stage is gone that is fine.
		if !errors.Is(err, filesystem.ErrNotFound) {
			s.app.Logger().Warn("failed to delete staged file", "error", err)
		}
	}

	record.Set(schema.FieldStorageFileKey, finalKey)
	record.Set(schema.FieldStorageStatus, statusActive)
	record.Set("updated", types.NowDateTime())
	if err := s.app.SaveWithContext(schema.WithInternalContext(context.Background()), record); err != nil {
		return fmt.Errorf("failed to activate file: %w", err)
	}
	return nil
}

// markFileLost transitions a record whose blobs are both missing to the deleted
// state and returns a distinct error so the upload path surfaces the data loss.
func (s *Service) markFileLost(record *core.Record) error {
	storageID := record.GetString(schema.FieldStorageID)
	s.app.Logger().Error("storage file data lost", "storageId", storageID, "reason", "missing staged and final blobs")
	record.Set(schema.FieldStorageStatus, statusDeleted)
	record.Set(schema.FieldStorageDeletedAt, types.NowDateTime())
	record.Set("updated", types.NowDateTime())
	if err := s.app.SaveWithContext(schema.WithInternalContext(context.Background()), record); err != nil {
		return fmt.Errorf("failed to mark lost storage file deleted: %w", err)
	}
	return ErrStorageDataLost
}

// Download serves a stored file for GET/HEAD requests.
func (s *Service) Download(w http.ResponseWriter, r *http.Request, storageID string, auth AuthContext) error {
	if err := ValidateStorageID(storageID); err != nil {
		return &UploadError{Code: ErrorCodeNotFound, Message: "file not found"}
	}

	if err := s.verifySignedURL(storageID, r.URL, auth); err != nil {
		if errors.Is(err, ErrURLExpired) {
			return &UploadError{Code: ErrorCodeUnauthorized, Message: "signed url expired", Err: err}
		}
		if errors.Is(err, ErrURLForbidden) {
			return &UploadError{Code: ErrorCodeForbidden, Message: "signed url does not match caller", Err: err}
		}
		return &UploadError{Code: ErrorCodeForbidden, Message: "signed url invalid", Err: err}
	}

	record, err := s.repo.GetFile(schema.WithInternalContext(r.Context()), s.app, storageID)
	if err != nil {
		if errors.Is(err, ErrStorageNotFound) {
			return &UploadError{Code: ErrorCodeNotFound, Message: "file not found"}
		}
		return err
	}

	return s.serveDownload(w, r, record, s.cacheControlHeader(r.URL))
}

// DownloadPublic serves a stable public storage URL without caller authentication.
func (s *Service) DownloadPublic(w http.ResponseWriter, r *http.Request, token string) error {
	if err := validatePublicToken(token); err != nil {
		return &UploadError{Code: ErrorCodeNotFound, Message: "file not found"}
	}
	record, err := s.repo.GetFileByPublicToken(schema.WithInternalContext(r.Context()), s.app, token)
	if err != nil {
		if errors.Is(err, ErrStorageNotFound) {
			return &UploadError{Code: ErrorCodeNotFound, Message: "file not found"}
		}
		return err
	}
	seconds := int64(s.config.PublicCacheTTL / time.Second)
	cacheControl := fmt.Sprintf("public, max-age=%d, s-maxage=%d, must-revalidate, stale-if-error=0", seconds, seconds)
	return s.serveDownload(w, r, record, cacheControl)
}

func (s *Service) serveDownload(w http.ResponseWriter, r *http.Request, record *core.Record, cacheControl string) error {
	storageID := record.GetString(schema.FieldStorageID)
	fileKey := record.GetString(schema.FieldStorageFileKey)
	filename := record.GetString(schema.FieldStorageFilename)
	if filename == "" {
		filename = storageID
	}

	fs, err := s.app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fs.Close()
	thumb, _, err := requestedThumb(record, r)
	if err != nil {
		return err
	}
	if thumb != "" {
		fileKey, err = s.ensureThumb(r.Context(), fs, storageID, fileKey, thumb)
		if err != nil {
			return &UploadError{Code: ErrorCodeInternal, Message: "failed to create image thumb", Err: err}
		}
	}

	attrs, err := fs.Attributes(fileKey)
	if err != nil {
		if errors.Is(err, filesystem.ErrNotFound) {
			return &UploadError{Code: ErrorCodeNotFound, Message: "file not found"}
		}
		return err
	}

	sha := record.GetString(schema.FieldStorageSha256)
	if sha != "" && thumb == "" {
		shaBytes, _ := hex.DecodeString(sha)
		if len(shaBytes) > 0 {
			w.Header().Set("Digest", "sha-256="+base64.StdEncoding.EncodeToString(shaBytes))
		}
	}

	etagValue := sha
	if thumb != "" {
		etagValue = fmt.Sprintf("%x", sha256.Sum256([]byte(sha+":"+thumb)))
	}
	etag := fmt.Sprintf("%q", etagValue)
	modTime := attrs.ModTime
	if modTime.IsZero() {
		modTime = record.GetDateTime("created").Time()
	}

	contentType := s.contentTypeHeader(record, attrs)
	if thumb != "" {
		contentType = attrs.ContentType
	}
	disposition := s.contentDispositionHeader(filename, contentType)

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Content-Disposition", disposition)

	cw := &countingResponseWriter{ResponseWriter: w}

	// Conditional HEAD/GET: evaluate preconditions from headers alone so that
	// 304/412 responses never open the (potentially remote) body stream. HEAD is
	// subject to the same preconditions as GET per RFC 7232.
	if code := evaluatePreconditions(r, etag, modTime); code != 0 {
		cw.WriteHeader(code)
		s.app.Logger().Info("storage download served", "storageId", storageID, "method", r.Method, "status", cw.status, "bytes", cw.bytes)
		return nil
	}

	// HEAD never needs the body: serve from metadata only (avoids opening the
	// S3/local body stream).
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(attrs.Size, 10))
		cw.WriteHeader(http.StatusOK)
		s.app.Logger().Info("storage download served", "storageId", storageID, "method", r.Method, "status", cw.status, "bytes", cw.bytes)
		return nil
	}

	br, err := fs.GetReader(fileKey)
	if err != nil {
		if errors.Is(err, filesystem.ErrNotFound) {
			return &UploadError{Code: ErrorCodeNotFound, Message: "file not found"}
		}
		return err
	}
	defer br.Close()

	http.ServeContent(cw, r, filename, modTime, br)
	s.app.Logger().Info("storage download served", "storageId", storageID, "method", r.Method, "status", cw.status, "bytes", cw.bytes)
	return nil
}

// evaluatePreconditions implements the RFC 7232 §6 precedence for the
// conditional headers that suppress the body. It returns 0 when the request
// should proceed, http.StatusNotModified (304), or
// http.StatusPreconditionFailed (412). Per RFC 7232 §6:
//   - If-Unmodified-Since is evaluated only when If-Match is absent.
//   - If-Modified-Since is evaluated only when If-None-Match is absent.
//   - If-Match uses the strong comparison function (a weak entity-tag never matches).
//
// If-Range is intentionally not handled here because it does not suppress the
// body. modtime is truncated to whole seconds before comparison, matching the
// resolution of HTTP-date header values.
func evaluatePreconditions(r *http.Request, etag string, modtime time.Time) int {
	trunc := modtime.Truncate(time.Second)
	if v := r.Header.Get("If-Match"); v != "" {
		if !strongEtagMatch(v, etag) {
			return http.StatusPreconditionFailed
		}
	} else if v := r.Header.Get("If-Unmodified-Since"); v != "" {
		if t, err := http.ParseTime(v); err == nil && trunc.After(t) {
			return http.StatusPreconditionFailed
		}
	}
	if v := r.Header.Get("If-None-Match"); v != "" {
		if v == "*" || weakEtagMatch(v, etag) {
			return http.StatusNotModified
		}
	} else if v := r.Header.Get("If-Modified-Since"); v != "" {
		if t, err := http.ParseTime(v); err == nil && !trunc.After(t) {
			return http.StatusNotModified
		}
	}
	return 0
}

// strongEtagMatch implements the strong comparison function for If-Match: "*"
// matches any representation; otherwise the listed entity-tags must equal the
// strong representation etag exactly. A weak (W/) tag in the list never matches.
func strongEtagMatch(list, etag string) bool {
	for _, p := range splitEtags(list) {
		if p == "*" {
			return true
		}
		if strings.HasPrefix(p, "W/") {
			continue
		}
		if p == etag {
			return true
		}
	}
	return false
}

// weakEtagMatch implements the weak comparison function for If-None-Match: "*"
// matches any representation; otherwise tags match ignoring the W/ prefix.
func weakEtagMatch(list, etag string) bool {
	for _, p := range splitEtags(list) {
		if p == "*" {
			return true
		}
		p = strings.TrimPrefix(p, "W/")
		if p == etag {
			return true
		}
	}
	return false
}

// splitEtags parses a comma-separated entity-tag list, trimming surrounding
// whitespace from each entry.
func splitEtags(list string) []string {
	parts := strings.Split(list, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// countingResponseWriter captures the response status and delivered byte count
// for observability without altering response semantics.
type countingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *countingResponseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (s *Service) cacheControlHeader(u *url.URL) string {
	q := u.Query()
	exp, _ := parseInt(q.Get("exp"))
	remaining := exp - time.Now().UTC().Unix()
	if remaining <= 0 {
		return "no-cache, no-store"
	}
	if q.Get("bnd") != "capability" && q.Get("sub") != "" {
		return fmt.Sprintf("private, max-age=%d", remaining)
	}
	return fmt.Sprintf("public, max-age=%d", remaining)
}

// streamToFile reads a bounded stream into a temporary local file and uploads it to the filesystem.
func (s *Service) streamToFile(r io.Reader, fileKey, filename string, maxSize int64, headerSize int64) (sha256hex string, size int64, err error) {
	tmpPath, sha256hex, size, err := s.stageToTemp(r, filename, maxSize, headerSize)
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmpPath)
	if err := s.persistStagedBlob(tmpPath, fileKey, filename); err != nil {
		return "", 0, err
	}
	return sha256hex, size, nil
}

// stageToTemp reads a bounded upload stream into a local temp file, enforcing
// the size limit and computing the SHA-256 digest. It does NOT touch the storage
// backend, so capacity can be reserved before the expensive staged-blob upload.
// The returned path refers to a closed file the caller must remove.
func (s *Service) stageToTemp(r io.Reader, filename string, maxSize, headerSize int64) (tmpPath, sha256hex string, size int64, err error) {
	if headerSize > 0 && headerSize > maxSize {
		return "", "", 0, &UploadError{Code: ErrorCodeUploadTooLarge, Message: ErrUploadTooLarge.Error()}
	}

	sizeLimit := maxSize
	if headerSize > 0 && headerSize < sizeLimit {
		sizeLimit = headerSize
	}

	tmp, err := os.CreateTemp("", "pbvex-upload-*")
	if err != nil {
		return "", "", 0, &UploadError{Code: ErrorCodeInternal, Message: "failed to stage upload", Err: err}
	}
	tmpPath = tmp.Name()
	defer tmp.Close()

	lr := io.LimitReader(r, sizeLimit+1)
	n, err := io.Copy(tmp, lr)
	if err != nil {
		return "", "", 0, &UploadError{Code: ErrorCodeInternal, Message: "failed to read upload stream", Err: err}
	}
	if n == 0 {
		return "", "", 0, &UploadError{Code: ErrorCodeBadRequest, Message: "empty upload"}
	}
	if n > maxSize {
		return "", "", 0, &UploadError{Code: ErrorCodeUploadTooLarge, Message: ErrUploadTooLarge.Error()}
	}
	if headerSize > 0 && n != headerSize {
		return "", "", 0, &UploadError{Code: ErrorCodeBadRequest, Message: "upload size mismatch"}
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", "", 0, &UploadError{Code: ErrorCodeInternal, Message: "failed to stage upload", Err: err}
	}

	h := sha256.New()
	if _, err := io.Copy(h, tmp); err != nil {
		return "", "", 0, &UploadError{Code: ErrorCodeInternal, Message: "failed to hash upload", Err: err}
	}

	return tmpPath, hex.EncodeToString(h.Sum(nil)), n, nil
}

// persistStagedBlob uploads an already-hashed local temp file to the storage
// backend at fileKey (the staged object key).
func (s *Service) persistStagedBlob(tmpPath, fileKey, filename string) error {
	if s.persistHook != nil {
		s.persistHook()
	}
	fs, err := s.app.NewFilesystem()
	if err != nil {
		return &UploadError{Code: ErrorCodeInternal, Message: "failed to open filesystem", Err: err}
	}
	defer fs.Close()

	file, err := filesystem.NewFileFromPath(tmpPath)
	if err != nil {
		return &UploadError{Code: ErrorCodeInternal, Message: "failed to stage upload", Err: err}
	}
	file.OriginalName = filename

	if err := fs.UploadFile(file, fileKey); err != nil {
		return &UploadError{Code: ErrorCodeInternal, Message: "failed to persist file", Err: err}
	}
	return nil
}

func (s *Service) deleteFile(app core.App, fileKey string) error {
	if fileKey == "" {
		return nil
	}
	if app == nil {
		app = s.app
	}
	fs, err := app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fs.Close()
	if err := fs.Delete(fileKey); err != nil {
		if errors.Is(err, filesystem.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func (s *Service) validateContentType(contentType, allowedCSV string) error {
	if contentType == "" {
		return &UploadError{Code: ErrorCodeInvalidContent, Message: "missing Content-Type"}
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return &UploadError{Code: ErrorCodeInvalidContent, Message: ErrMalformedContentType.Error(), Err: err}
	}

	allowed := s.config.AllowedContentTypes
	if allowedCSV != "" {
		allowed = strings.Split(allowedCSV, ",")
	}
	if len(allowed) == 0 {
		return nil
	}

	if matchesContentType(mt, allowed) {
		return nil
	}
	return &UploadError{Code: ErrorCodeInvalidContent, Message: ErrContentTypeNotAllowed.Error()}
}

func matchesContentType(mt string, allowed []string) bool {
	for _, p := range allowed {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "*" {
			return true
		}
		if strings.HasSuffix(p, "/*") {
			prefix := strings.TrimSuffix(p, "/*")
			if strings.HasPrefix(mt, prefix+"/") {
				return true
			}
			continue
		}
		if strings.EqualFold(mt, p) {
			return true
		}
	}
	return false
}

func (s *Service) contentDispositionHeader(filename, contentType string) string {
	// Sanitize the filename to a safe ASCII token for the legacy filename
	// parameter, and provide an RFC 5987 filename* when the original contains
	// non-ASCII or control characters.
	safe := strings.Map(func(r rune) rune {
		if r >= 0x20 && r < 0x7f && r != '"' && r != '\\' && r != ';' && r != 0x7f {
			return r
		}
		return '_'
	}, filename)
	if safe == "" {
		safe = "upload"
	}

	disp := "inline"
	if isActiveContentType(contentType) {
		// Active content (HTML/SVG) must not be executed inline in the same
		// origin. Force attachment as the default safe policy.
		disp = "attachment"
	}

	if safe == filename {
		return fmt.Sprintf("%s; filename=%q", disp, safe)
	}
	return fmt.Sprintf("%s; filename=%q; filename*=UTF-8''%s", disp, safe, rfc5987Encode(filename))
}

// rfc5987Encode percent-encodes a filename for the RFC 5987 ext-value grammar,
// keeping only the attr-char set (ALPHA / DIGIT / "!#$&+-.^_`|~") unencoded and
// encoding each byte of every other rune as %XX. Unlike url.PathEscape it does
// not leave sub-delims such as ":" ";" "=" "?" "@" unencoded.
func rfc5987Encode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isAttrChar(r) {
			b.WriteRune(r)
			continue
		}
		for _, bt := range []byte(string(r)) {
			fmt.Fprintf(&b, "%%%02X", bt)
		}
	}
	return b.String()
}

func isAttrChar(r rune) bool {
	switch {
	case r >= '0' && r <= '9':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	}
	return strings.ContainsRune("!#$&+-.^_`|~", r)
}

func isActiveContentType(contentType string) bool {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch media {
	case "text/html", "image/svg+xml", "application/xhtml+xml":
		return true
	}
	return false
}

func (s *Service) normalizeFilename(name string) (string, error) {
	if name == "" {
		return "upload", nil
	}
	name = strings.ReplaceAll(name, "\x00", "")
	if !utf8.ValidString(name) {
		return "", ErrInvalidFilename
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", ErrInvalidFilename
	}
	name = path.Base(name)
	// Truncate by whole runes so a multi-byte UTF-8 sequence is never split,
	// keeping the persisted filename and any RFC 5987 encoding well-formed.
	for len(name) > 255 {
		_, sz := utf8.DecodeLastRuneInString(name)
		if sz <= 0 {
			break
		}
		name = name[:len(name)-sz]
	}
	if name == "" || name == "." || name == ".." {
		return "", ErrInvalidFilename
	}
	return name, nil
}

func (s *Service) contentTypeHeader(record *core.Record, attrs *blob.Attributes) string {
	ct := record.GetString(schema.FieldStorageContentType)
	if ct != "" {
		return ct
	}
	if attrs != nil && attrs.ContentType != "" {
		return attrs.ContentType
	}
	return "application/octet-stream"
}

func (s *Service) uploadURL(token string) string {
	base := s.config.BaseURL
	if base == "" {
		base = s.app.Settings().Meta.AppURL
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return s.config.BasePath + "/upload/" + url.PathEscape(token)
	}
	return base + s.config.BasePath + "/upload/" + url.PathEscape(token)
}

func (s *Service) fileKey(storageID string) string {
	return fmt.Sprintf("%s/%s/blob", strings.Trim(s.config.FileStoragePrefix, "/"), storageID)
}

func (s *Service) stageFileKey(storageID, attempt string) string {
	return fmt.Sprintf("%s/%s/_stage/%s/blob", strings.Trim(s.config.FileStoragePrefix, "/"), storageID, attempt)
}

func (s *Service) claimExpiry() time.Time {
	return time.Now().UTC().Add(s.config.DefaultClaimTTL)
}

func (s *Service) appFor(ctx context.Context) core.App {
	if app, ok := AppFromContext(ctx); ok && app != nil {
		return app
	}
	return s.app
}
