package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Repo provides persistence access for storage metadata and tokens.
type Repo struct{}

// NewRepo creates a new storage repository.
func NewRepo() *Repo { return &Repo{} }

// CreateFile creates a storage metadata record.
func (r *Repo) CreateFile(ctx context.Context, app core.App, record FileRecord) (*core.Record, error) {
	col, err := app.FindCollectionByNameOrId(schema.CollectionStorageFiles)
	if err != nil {
		return nil, err
	}

	rec := core.NewRecord(col)
	rec.Set(schema.FieldStorageID, record.StorageID)
	rec.Set(schema.FieldStorageSha256, record.Sha256)
	rec.Set(schema.FieldStorageSize, record.Size)
	rec.Set(schema.FieldStorageContentType, record.ContentType)
	rec.Set(schema.FieldStorageFileKey, record.FileKey)
	rec.Set(schema.FieldStorageFilename, record.Filename)
	rec.Set(schema.FieldStorageStatus, record.Status)
	rec.Set(schema.FieldStorageCreatedBy, record.CreatedBy)
	rec.Set(schema.FieldStorageOwner, record.Owner)
	rec.Set(schema.FieldStoragePublicToken, record.PublicToken)
	if record.Metadata != nil {
		rec.Set(schema.FieldStorageMetadata, record.Metadata)
	}
	if !record.LeaseUntil.IsZero() {
		leaseDt, err := types.ParseDateTime(record.LeaseUntil.UTC())
		if err != nil {
			return nil, fmt.Errorf("invalid lease until: %w", err)
		}
		rec.Set(schema.FieldStorageLeaseUntil, leaseDt)
	}
	now := types.NowDateTime()
	rec.Set("created", now)
	rec.Set("updated", now)

	if err := app.SaveWithContext(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// GetFileByPublicToken returns an active storage file for its stable public token.
func (r *Repo) GetFileByPublicToken(ctx context.Context, app core.App, token string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		schema.CollectionStorageFiles,
		fmt.Sprintf("%s = {:token} && %s = {:status}", schema.FieldStoragePublicToken, schema.FieldStorageStatus),
		dbx.Params{"token": token, schema.FieldStorageStatus: statusActive},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStorageNotFound
		}
		return nil, err
	}
	return record, nil
}

// BackfillPublicTokens gives files created by older releases stable public tokens.
func (r *Repo) BackfillPublicTokens(ctx context.Context, app core.App) error {
	records := []*core.Record{}
	if err := app.RecordQuery(schema.CollectionStorageFiles).
		AndWhere(dbx.HashExp{schema.FieldStoragePublicToken: ""}).
		All(&records); err != nil {
		return err
	}
	for _, record := range records {
		token, err := GenerateToken()
		if err != nil {
			return err
		}
		record.Set(schema.FieldStoragePublicToken, token)
		if err := app.SaveWithContext(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

// GetFile returns a non-deleted storage file metadata record by storageId.
func (r *Repo) GetFile(ctx context.Context, app core.App, storageID string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		schema.CollectionStorageFiles,
		fmt.Sprintf("%s = {:storageId} && %s = {:status}", schema.FieldStorageID, schema.FieldStorageStatus),
		dbx.Params{schema.FieldStorageID: storageID, schema.FieldStorageStatus: statusActive},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStorageNotFound
		}
		return nil, err
	}
	return record, nil
}

// GetFileByKey returns a non-deleted file by its backend file key.
func (r *Repo) GetFileByKey(ctx context.Context, app core.App, fileKey string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		schema.CollectionStorageFiles,
		fmt.Sprintf("%s = {:fileKey} && %s = {:status}", schema.FieldStorageFileKey, schema.FieldStorageStatus),
		dbx.Params{schema.FieldStorageFileKey: fileKey, schema.FieldStorageStatus: statusActive},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStorageNotFound
		}
		return nil, err
	}
	return record, nil
}

// MarkFileStatus updates the status of a file record and returns its file key.
func (r *Repo) MarkFileStatus(ctx context.Context, app core.App, storageID, status string) (*core.Record, error) {
	record, err := r.GetFile(ctx, app, storageID)
	if err != nil {
		return nil, err
	}
	record.Set(schema.FieldStorageStatus, status)
	if status == statusDeleted {
		record.Set(schema.FieldStorageDeletedAt, types.NowDateTime())
	}
	record.Set("updated", types.NowDateTime())
	if err := app.SaveWithContext(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

// HardDeleteFile removes a file metadata record.
func (r *Repo) HardDeleteFile(ctx context.Context, app core.App, storageID string) (string, error) {
	record, err := app.FindFirstRecordByFilter(
		schema.CollectionStorageFiles,
		fmt.Sprintf("%s = {:storageId}", schema.FieldStorageID),
		dbx.Params{schema.FieldStorageID: storageID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrStorageNotFound
		}
		return "", err
	}
	fileKey := record.GetString(schema.FieldStorageFileKey)
	if err := app.DeleteWithContext(ctx, record); err != nil {
		return "", err
	}
	return fileKey, nil
}

// CreateToken stores a new upload token. TokenHash is the digest that is persisted.
func (r *Repo) CreateToken(ctx context.Context, app core.App, token TokenRecord) (*core.Record, error) {
	col, err := app.FindCollectionByNameOrId(schema.CollectionStorageTokens)
	if err != nil {
		return nil, err
	}

	rec := core.NewRecord(col)
	rec.Set(schema.FieldToken, token.TokenHash)
	rec.Set(schema.FieldTokenStorageID, token.StorageID)
	expires, err := types.ParseDateTime(token.ExpiresAt.UTC())
	if err != nil {
		return nil, err
	}
	rec.Set(schema.FieldTokenExpiresAt, expires)
	rec.Set(schema.FieldTokenConsumed, false)
	rec.Set(schema.FieldTokenCreatedBy, token.CreatedBy)
	rec.Set(schema.FieldTokenMaxSize, token.MaxSize)
	rec.Set(schema.FieldTokenAllowedTypes, strings.Join(token.AllowedTypes, ","))
	rec.Set(schema.FieldTokenFilename, token.Filename)
	if token.Policy != nil {
		rec.Set(schema.FieldTokenPolicy, token.Policy)
	}
	now := types.NowDateTime()
	rec.Set("created", now)
	rec.Set("updated", now)

	if err := app.SaveWithContext(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// GetTokenByHash returns a non-consumed token by its digest.
func (r *Repo) GetTokenByHash(ctx context.Context, app core.App, tokenHash string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		schema.CollectionStorageTokens,
		fmt.Sprintf("%s = {:token} && %s = {:consumed}", schema.FieldToken, schema.FieldTokenConsumed),
		dbx.Params{schema.FieldToken: tokenHash, schema.FieldTokenConsumed: false},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	return record, nil
}

// GetTokenByHashAnyState returns a token by its digest regardless of consumed state.
// It is used to classify a failed claim into expired, consumed, or in-use so that
// callers can surface a precise error instead of a generic rejection.
func (r *Repo) GetTokenByHashAnyState(ctx context.Context, app core.App, tokenHash string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		schema.CollectionStorageTokens,
		fmt.Sprintf("%s = {:token}", schema.FieldToken),
		dbx.Params{schema.FieldToken: tokenHash},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	return record, nil
}

// ClaimToken atomically CAS a token from unclaimed to claimed by attempt.
func (r *Repo) ClaimToken(ctx context.Context, app core.App, tokenHash, claim string, claimExpiresAt time.Time) (*core.Record, error) {
	now := time.Now().UTC()
	expires, err := types.ParseDateTime(claimExpiresAt.UTC())
	if err != nil {
		return nil, err
	}
	nowDt, err := types.ParseDateTime(now)
	if err != nil {
		return nil, err
	}
	updated, err := types.ParseDateTime(now)
	if err != nil {
		return nil, err
	}

	res, err := app.DB().NewQuery(fmt.Sprintf(
		"UPDATE %s SET %s = {:claim}, %s = {:claimExpiresAt}, %s = {:updated} WHERE %s = {:tokenHash} AND %s = false AND (%s = '' OR %s < {:now}) AND %s > {:now}",
		schema.CollectionStorageTokens,
		schema.FieldTokenClaim,
		schema.FieldTokenClaimExpiresAt,
		"updated",
		schema.FieldToken,
		schema.FieldTokenConsumed,
		schema.FieldTokenClaim,
		schema.FieldTokenClaimExpiresAt,
		schema.FieldTokenExpiresAt,
	)).Bind(dbx.Params{
		"claim":          claim,
		"claimExpiresAt": expires,
		"updated":        updated,
		"tokenHash":      tokenHash,
		"now":            nowDt,
	}).WithContext(ctx).Execute()
	if err != nil {
		return nil, fmt.Errorf("claim token: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("claim token rows affected: %w", err)
	}
	if affected == 0 {
		return nil, ErrTokenClaimFailed
	}

	return r.GetTokenByHash(ctx, app, tokenHash)
}

// ReleaseClaim clears a claim for an attempt if the token is not consumed.
func (r *Repo) ReleaseClaim(ctx context.Context, app core.App, tokenHash, claim string) error {
	res, err := app.DB().NewQuery(fmt.Sprintf(
		"UPDATE %s SET %s = '' WHERE %s = {:tokenHash} AND %s = {:claim} AND %s = false",
		schema.CollectionStorageTokens,
		schema.FieldTokenClaim,
		schema.FieldToken,
		schema.FieldTokenClaim,
		schema.FieldTokenConsumed,
	)).Bind(dbx.Params{
		"tokenHash": tokenHash,
		"claim":     claim,
	}).WithContext(ctx).Execute()
	if err != nil {
		return fmt.Errorf("release token claim: %w", err)
	}
	_, err = res.RowsAffected()
	if err != nil {
		return fmt.Errorf("release token claim rows affected: %w", err)
	}
	return nil
}

// ConsumeToken atomically consumes a token only if the claim matches.
func (r *Repo) ConsumeToken(ctx context.Context, app core.App, tokenHash, claim string) error {
	now := types.NowDateTime()
	res, err := app.DB().NewQuery(fmt.Sprintf(
		"UPDATE %s SET %s = true, %s = '', %s = {:updated} WHERE %s = {:tokenHash} AND %s = {:claim} AND %s = false",
		schema.CollectionStorageTokens,
		schema.FieldTokenConsumed,
		schema.FieldTokenClaim,
		"updated",
		schema.FieldToken,
		schema.FieldTokenClaim,
		schema.FieldTokenConsumed,
	)).Bind(dbx.Params{
		"updated":   now,
		"tokenHash": tokenHash,
		"claim":     claim,
	}).WithContext(ctx).Execute()
	if err != nil {
		return fmt.Errorf("consume token: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("consume token rows affected: %w", err)
	}
	if affected == 0 {
		return ErrTokenClaimFailed
	}
	return nil
}

// DeleteExpiredTokens removes tokens whose expiry has passed.
func (r *Repo) DeleteExpiredTokens(ctx context.Context, app core.App, before time.Time) (int64, error) {
	beforeDt, err := types.ParseDateTime(before.UTC())
	if err != nil {
		return 0, err
	}
	records := []*core.Record{}
	err = app.RecordQuery(schema.CollectionStorageTokens).
		AndWhere(dbx.NewExp(fmt.Sprintf("%s < {:before}", schema.FieldTokenExpiresAt), dbx.Params{"before": beforeDt})).
		All(&records)
	if err != nil {
		return 0, err
	}
	for _, rec := range records {
		if err := app.DeleteWithContext(ctx, rec); err != nil {
			return 0, err
		}
	}
	return int64(len(records)), nil
}

// GetActiveFilesCount returns the number of file records that consume storage
// capacity (uploading, staged, active, or deleting). Deleted records are not counted.
func (r *Repo) GetActiveFilesCount(ctx context.Context, app core.App) (int64, error) {
	var count int64
	err := app.RecordQuery(schema.CollectionStorageFiles).
		AndWhere(dbx.NewExp(
			fmt.Sprintf("%s IN ('%s', '%s', '%s', '%s')", schema.FieldStorageStatus, statusUploading, statusActive, statusStaged, statusDeleting),
		)).
		WithContext(ctx).
		Select("COUNT(*)").
		Row(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// RenewUploadLease atomically extends the lease on an uploading reservation
// owned by owner. It is a CAS: the update only applies while the record is
// still uploading and owned by owner, so it cannot clobber a record that
// cleanup reclaimed or commit transitioned. It returns ErrReservationLost when
// the reservation no longer matches.
func (r *Repo) RenewUploadLease(ctx context.Context, app core.App, storageID, owner string, until time.Time) error {
	untilDt, err := types.ParseDateTime(until.UTC())
	if err != nil {
		return fmt.Errorf("invalid lease until: %w", err)
	}
	res, err := app.DB().NewQuery(fmt.Sprintf(
		"UPDATE %s SET %s = {:until}, updated = {:updated} WHERE %s = {:storageId} AND %s = {:uploading} AND %s = {:owner}",
		schema.CollectionStorageFiles,
		schema.FieldStorageLeaseUntil,
		schema.FieldStorageID,
		schema.FieldStorageStatus,
		schema.FieldStorageOwner,
	)).Bind(dbx.Params{
		"until":     untilDt,
		"updated":   types.NowDateTime(),
		"storageId": storageID,
		"uploading": statusUploading,
		"owner":     owner,
	}).WithContext(ctx).Execute()
	if err != nil {
		return fmt.Errorf("renew upload lease: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("renew upload lease rows affected: %w", err)
	}
	if affected == 0 {
		return ErrReservationLost
	}
	return nil
}

// TransitionUploadingToStaged atomically moves a reservation from uploading to
// staged with the finalized metadata. The CAS (status=uploading AND owner)
// ensures it cannot transition a record that cleanup reclaimed or another owner
// took. Returns ErrReservationLost when the reservation no longer matches.
func (r *Repo) TransitionUploadingToStaged(ctx context.Context, app core.App, storageID, owner, sha string, size int64, fileKey, contentType string, metadata any) error {
	sizeDt := size
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode storage metadata: %w", err)
	}
	res, err := app.DB().NewQuery(fmt.Sprintf(
		"UPDATE %s SET %s = {:sha}, %s = {:size}, %s = {:fileKey}, %s = {:contentType}, %s = {:metadata}, %s = {:staged}, updated = {:updated} WHERE %s = {:storageId} AND %s = {:uploading} AND %s = {:owner}",
		schema.CollectionStorageFiles,
		schema.FieldStorageSha256,
		schema.FieldStorageSize,
		schema.FieldStorageFileKey,
		schema.FieldStorageContentType,
		schema.FieldStorageMetadata,
		schema.FieldStorageStatus,
		schema.FieldStorageID,
		schema.FieldStorageStatus,
		schema.FieldStorageOwner,
	)).Bind(dbx.Params{
		"sha":         sha,
		"size":        sizeDt,
		"fileKey":     fileKey,
		"contentType": contentType,
		"metadata":    string(metadataJSON),
		"staged":      statusStaged,
		"updated":     types.NowDateTime(),
		"storageId":   storageID,
		"uploading":   statusUploading,
		"owner":       owner,
	}).WithContext(ctx).Execute()
	if err != nil {
		return fmt.Errorf("transition upload to staged: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("transition upload to staged rows affected: %w", err)
	}
	if affected == 0 {
		return ErrReservationLost
	}
	return nil
}

// ReleaseReservation hard-deletes an uploading reservation owned by owner. The
// CAS guard makes it idempotent and safe against a record already reclaimed or
// committed by another path.
func (r *Repo) ReleaseReservation(ctx context.Context, app core.App, storageID, owner string) error {
	_, err := app.DB().NewQuery(fmt.Sprintf(
		"DELETE FROM %s WHERE %s = {:storageId} AND %s = {:uploading} AND %s = {:owner}",
		schema.CollectionStorageFiles,
		schema.FieldStorageID,
		schema.FieldStorageStatus,
		schema.FieldStorageOwner,
	)).Bind(dbx.Params{
		"storageId": storageID,
		"uploading": statusUploading,
		"owner":     owner,
	}).WithContext(ctx).Execute()
	if err != nil {
		return fmt.Errorf("release reservation: %w", err)
	}
	return nil
}

// DeleteUploadingIfLeaseExpired atomically hard-deletes an uploading reservation
// only if its lease has expired (leaseUntil < before) AND it is still owned by
// the snapshotted owner. The atomic status+owner+lease guard means a concurrent
// renewal that extended the lease, or an owner takeover, causes this to affect
// zero rows, so cleanup cannot reclaim an actively-renewed or re-owned upload.
// Returns true when the reservation was reclaimed.
func (r *Repo) DeleteUploadingIfLeaseExpired(ctx context.Context, app core.App, id, owner string, before time.Time) (bool, error) {
	beforeDt, err := types.ParseDateTime(before.UTC())
	if err != nil {
		return false, fmt.Errorf("invalid lease before: %w", err)
	}
	res, err := app.DB().NewQuery(fmt.Sprintf(
		"DELETE FROM %s WHERE id = {:id} AND %s = {:uploading} AND %s = {:owner} AND (%s = '' OR %s < {:before})",
		schema.CollectionStorageFiles,
		schema.FieldStorageStatus,
		schema.FieldStorageOwner,
		schema.FieldStorageLeaseUntil,
		schema.FieldStorageLeaseUntil,
	)).Bind(dbx.Params{
		"id":        id,
		"uploading": statusUploading,
		"owner":     owner,
		"before":    beforeDt,
	}).WithContext(ctx).Execute()
	if err != nil {
		return false, fmt.Errorf("delete expired uploading: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete expired uploading rows affected: %w", err)
	}
	return affected > 0, nil
}

// GetFilesByStatus returns all file records with the given status.
func (r *Repo) GetFilesByStatus(ctx context.Context, app core.App, status string) ([]*core.Record, error) {
	records := []*core.Record{}
	err := app.RecordQuery(schema.CollectionStorageFiles).
		AndWhere(dbx.NewExp(fmt.Sprintf("%s = {:status}", schema.FieldStorageStatus), dbx.Params{"status": status})).
		All(&records)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// GetFileByIDAnyStatus returns a file record by storage ID regardless of status.
func (r *Repo) GetFileByIDAnyStatus(ctx context.Context, app core.App, storageID string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		schema.CollectionStorageFiles,
		fmt.Sprintf("%s = {:storageId}", schema.FieldStorageID),
		dbx.Params{schema.FieldStorageID: storageID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStorageNotFound
		}
		return nil, err
	}
	return record, nil
}

// GetTokenByStorageID returns any token for the given storage ID.
func (r *Repo) GetTokenByStorageID(ctx context.Context, app core.App, storageID string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		schema.CollectionStorageTokens,
		fmt.Sprintf("%s = {:storageId}", schema.FieldTokenStorageID),
		dbx.Params{schema.FieldTokenStorageID: storageID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	return record, nil
}

// TokenRecord is the domain model for an upload token.
type TokenRecord struct {
	TokenHash    string
	StorageID    string
	ExpiresAt    time.Time
	CreatedBy    string
	MaxSize      int64
	AllowedTypes []string
	Filename     string
	Policy       any
}

// FileRecord is the domain model for a stored file.
type FileRecord struct {
	StorageID   string
	Sha256      string
	Size        int64
	ContentType string
	FileKey     string
	Filename    string
	CreatedBy   string
	Status      string
	Owner       string
	LeaseUntil  time.Time
	PublicToken string
	Metadata    any
}

const (
	statusUploading = "uploading"
	statusActive    = "active"
	statusStaged    = "staged"
	statusDeleting  = "deleting"
	statusDeleted   = "deleted"
)
