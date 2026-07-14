package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// Start begins the background cleanup worker. It is safe to call multiple times.
func (s *Service) Start() error {
	s.cleanupMutex.Lock()
	defer s.cleanupMutex.Unlock()

	if s.cleanupStop != nil {
		return nil
	}
	if err := s.kr.LoadOrCreate(schema.WithInternalContext(context.Background())); err != nil {
		return err
	}
	s.cleanupStop = make(chan struct{})
	s.cleanupDone = make(chan struct{})
	s.cleanupStopOnce = sync.Once{}

	go s.cleanupLoop()
	return nil
}

// Stop halts the background cleanup worker and waits for the current pass to finish.
func (s *Service) Stop() error {
	s.cleanupMutex.Lock()
	stop := s.cleanupStop
	done := s.cleanupDone
	if stop == nil {
		s.cleanupMutex.Unlock()
		return nil
	}
	s.cleanupStopOnce.Do(func() { close(stop) })
	s.cleanupMutex.Unlock()
	<-done

	s.cleanupMutex.Lock()
	if s.cleanupStop == stop {
		s.cleanupStop = nil
		s.cleanupDone = nil
	}
	s.cleanupMutex.Unlock()
	return nil
}

func (s *Service) cleanupLoop() {
	defer close(s.cleanupDone)

	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.cleanupStop:
			return
		case <-ticker.C:
			s.runCleanup()
		}
	}
}

func (s *Service) runCleanup() {
	defer func() {
		if r := recover(); r != nil {
			s.app.Logger().Error("storage cleanup worker panic", "recover", r)
		}
	}()

	ctx := schema.WithInternalContext(context.Background())
	s.runCleanupWithLogger(ctx)
}

// runCleanupWithLogger executes one cleanup pass and emits structured metrics
// for backlog, recovery, and reclamation outcomes.
func (s *Service) runCleanupWithLogger(ctx context.Context) {
	stagedBacklog, _ := s.repo.GetFilesByStatus(ctx, s.app, statusStaged)
	deletingBacklog, _ := s.repo.GetFilesByStatus(ctx, s.app, statusDeleting)
	uploadingBacklog, _ := s.repo.GetFilesByStatus(ctx, s.app, statusUploading)
	if len(stagedBacklog) > 0 || len(deletingBacklog) > 0 || len(uploadingBacklog) > 0 {
		s.app.Logger().Info("storage cleanup backlog", "uploading", len(uploadingBacklog), "staged", len(stagedBacklog), "deleting", len(deletingBacklog))
	}

	recoveredUploading, err := s.recoverUploading(ctx)
	if err != nil {
		s.app.Logger().Warn("storage cleanup recover uploading failed", "error", err)
	}
	recoveredStaged, err := s.recoverStaged(ctx)
	if err != nil {
		s.app.Logger().Warn("storage cleanup recover staged failed", "error", err)
	}
	recoveredDeleting, err := s.recoverDeleting(ctx)
	if err != nil {
		s.app.Logger().Warn("storage cleanup recover deleting failed", "error", err)
	}
	expiredTokens, err := s.repo.DeleteExpiredTokens(ctx, s.app, time.Now().UTC())
	if err != nil {
		s.app.Logger().Warn("storage cleanup expired tokens failed", "error", err)
	}
	orphanBlobs, err := s.cleanupOrphanBlobs(ctx)
	if err != nil {
		s.app.Logger().Warn("storage cleanup orphan blobs failed", "error", err)
	}
	if _, err := s.kr.Current(ctx); err != nil {
		s.app.Logger().Warn("storage signing key rotation failed", "error", err)
	}
	prunedKeys, err := s.kr.Prune(ctx)
	if err != nil {
		s.app.Logger().Warn("storage cleanup keyring prune failed", "error", err)
	}

	s.app.Logger().Info("storage cleanup complete",
		"recoveredUploading", recoveredUploading,
		"recoveredStaged", recoveredStaged,
		"recoveredDeleting", recoveredDeleting,
		"expiredTokens", expiredTokens,
		"orphanBlobs", orphanBlobs,
		"prunedKeys", prunedKeys,
	)
}

// RunCleanup executes a single cleanup pass synchronously. Useful for tests.
func (s *Service) RunCleanup() error {
	s.cleanupOnce.Do(func() {})
	ctx := schema.WithInternalContext(context.Background())

	if _, err := s.recoverUploading(ctx); err != nil {
		return err
	}
	if _, err := s.recoverStaged(ctx); err != nil {
		return err
	}
	if _, err := s.recoverDeleting(ctx); err != nil {
		return err
	}
	if _, err := s.repo.DeleteExpiredTokens(ctx, s.app, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := s.cleanupOrphanBlobs(ctx); err != nil {
		return err
	}
	if _, err := s.kr.Current(ctx); err != nil {
		return err
	}
	if _, err := s.kr.Prune(ctx); err != nil {
		return err
	}
	return nil
}

// recoverUploading reclaims capacity from durable "uploading" reservations
// whose lease has expired without renewal. An uploading record holds a capacity
// slot before its stage blob exists and must never be finalized by cleanup. The
// reclamation is an atomic status+lease CAS (DeleteUploadingIfLeaseExpired): a
// concurrent renewal that extended the lease causes the delete to affect zero
// rows, so cleanup can never reclaim an upload that is still actively renewing.
func (s *Service) recoverUploading(ctx context.Context) (int, error) {
	records, err := s.repo.GetFilesByStatus(ctx, s.app, statusUploading)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	reclaimed := 0
	for _, rec := range records {
		leaseUntil := rec.GetDateTime(schema.FieldStorageLeaseUntil).Time()
		if leaseUntil.IsZero() {
			leaseUntil = rec.GetDateTime("created").Time()
		}
		if leaseUntil.After(now) {
			continue
		}
		storageID := rec.GetString(schema.FieldStorageID)
		// Atomic reclaim: only succeeds if the record is still uploading, still
		// owned by the snapshotted owner, and the lease is still expired (a
		// racing renewal or owner takeover would cause zero rows affected).
		owner := rec.GetString(schema.FieldStorageOwner)
		gone, err := s.repo.DeleteUploadingIfLeaseExpired(ctx, s.app, rec.Id, owner, now)
		if err != nil {
			s.app.Logger().Warn("failed to reclaim uploading reservation", "storageId", storageID, "error", err)
			continue
		}
		if !gone {
			// Leased was renewed between the list and the delete; leave it.
			continue
		}
		// Best-effort removal of any partially-written stage blob.
		if key := rec.GetString(schema.FieldStorageFileKey); key != "" {
			if err := s.deleteFile(s.app, key); err != nil {
				s.app.Logger().Warn("failed to delete abandoned stage blob", "storageId", storageID, "error", err)
			}
		}
		s.app.Logger().Info("storage uploading reservation reclaimed", "storageId", storageID)
		reclaimed++
	}
	return reclaimed, nil
}

func (s *Service) recoverStaged(ctx context.Context) (int, error) {
	records, err := s.repo.GetFilesByStatus(ctx, s.app, statusStaged)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, rec := range records {
		stageKey := rec.GetString(schema.FieldStorageFileKey)
		storageID := rec.GetString(schema.FieldStorageID)
		finalKey := s.fileKey(storageID)
		if err := s.finalizeUpload(rec, stageKey, finalKey); err != nil {
			s.app.Logger().Warn("failed to recover staged file", "storageId", storageID, "error", err)
			continue
		}
		recovered++
	}
	return recovered, nil
}

func (s *Service) recoverDeleting(ctx context.Context) (int, error) {
	records, err := s.repo.GetFilesByStatus(ctx, s.app, statusDeleting)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, rec := range records {
		fileKey := rec.GetString(schema.FieldStorageFileKey)
		if err := s.deleteBlob(rec, fileKey); err != nil {
			s.app.Logger().Warn("failed to recover deleting file", "storageId", rec.GetString(schema.FieldStorageID), "error", err)
			continue
		}
		recovered++
	}
	return recovered, nil
}

// cleanupOrphanBlobs removes filesystem objects that are no longer needed.
// This covers three recovery cases: (1) a blob whose metadata was never
// committed, (2) a final blob whose metadata was deleted, and (3) a stale
// stage blob left behind when finalizeUpload's Copy succeeded but its
// best-effort stage Delete failed. It returns the number of blobs removed.
func (s *Service) cleanupOrphanBlobs(ctx context.Context) (int, error) {
	fs, err := s.app.NewFilesystem()
	if err != nil {
		return 0, err
	}
	defer fs.Close()

	prefix := strings.Trim(s.config.FileStoragePrefix, "/")
	objects, err := fs.List(prefix + "/")
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, obj := range objects {
		key := obj.Key
		storageID, isStage := parseStorageKey(prefix, key)
		if storageID == "" {
			continue
		}

		rec, recErr := s.repo.GetFileByIDAnyStatus(ctx, s.app, storageID)
		if recErr != nil && !errors.Is(recErr, ErrStorageNotFound) {
			s.app.Logger().Warn("failed to check file record", "error", recErr)
			continue
		}
		hasRecord := !errors.Is(recErr, ErrStorageNotFound)
		status := ""
		if hasRecord {
			status = rec.GetString(schema.FieldStorageStatus)
		}

		if isStage {
			// A stage blob is required while a record is uploading (persist may
			// still be in flight) or staged (finalize pending) or deleting. Once
			// the record is active, the finalize copy completed and any leftover
			// stage blob is a stale leak that must be reclaimed here.
			if hasRecord && (status == statusUploading || status == statusStaged || status == statusDeleting) {
				continue
			}
			// No record means the stage is orphaned unless a token is still
			// pending (the upload may not have committed its record yet).
			if !hasRecord {
				keep, err := s.hasActiveToken(ctx, storageID)
				if err != nil {
					s.app.Logger().Warn("failed to check token record", "error", err)
					continue
				}
				if keep {
					continue
				}
			}
		} else {
			// Final blob: keep it while any record or pending token exists.
			if hasRecord {
				continue
			}
			keep, err := s.hasActiveToken(ctx, storageID)
			if err != nil {
				s.app.Logger().Warn("failed to check token record", "error", err)
				continue
			}
			if keep {
				continue
			}
		}

		if err := fs.Delete(key); err != nil && !errors.Is(err, filesystem.ErrNotFound) {
			s.app.Logger().Warn("failed to delete orphan blob", "key", "<redacted>", "error", err)
			continue
		}
		removed++
	}

	return removed, nil
}

func (s *Service) hasActiveToken(ctx context.Context, storageID string) (bool, error) {
	_, err := s.repo.GetTokenByStorageID(ctx, s.app, storageID)
	if errors.Is(err, ErrTokenNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// parseStorageKey extracts a storage ID from a file key and reports whether it is a stage key.
// Expected forms: "prefix/<storageId>/blob" and "prefix/<storageId>/_stage/<attempt>/blob".
func parseStorageKey(prefix, key string) (storageID string, isStage bool) {
	key = strings.TrimPrefix(key, prefix+"/")
	parts := strings.Split(key, "/")
	if len(parts) == 2 && parts[1] == "blob" {
		return parts[0], false
	}
	if len(parts) == 4 && parts[1] == "_stage" && parts[3] == "blob" {
		return parts[0], true
	}
	return "", false
}
