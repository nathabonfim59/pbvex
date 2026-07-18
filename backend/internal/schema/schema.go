// Package schema manages the PBVex reserved system collections and bootstrap state.
package schema

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	CollectionDeployments      = "_pbvex_deployments"
	CollectionFunctions        = "_pbvex_functions"
	CollectionSchemaState      = "_pbvex_schemaState"
	CollectionJobs             = "_pbvex_jobs"
	CollectionComponents       = "_pbvex_components"
	CollectionStorageFiles     = "_pbvex_storage_files"
	CollectionStorageTokens    = "_pbvex_storage_tokens"
	CollectionStorageKeyring   = "_pbvex_storage_keyring"
	CollectionMigrationHistory = "_pbvex_migration_history"

	StateKeyActive = "active"

	FieldManifest             = "manifest"
	FieldBundleHash           = "bundle_hash"
	FieldBundleSize           = "bundle_size"
	FieldBundle               = "bundle"
	FieldActive               = "active"
	FieldPinCount             = "pinCount"
	FieldActivatedAt          = "activatedAt"
	FieldName                 = "name"
	FieldVersion              = "version"
	FieldProtocol             = "protocol"
	FieldFunctions            = "functions"
	FieldKey                  = "key"
	FieldActiveID             = "activeDeploymentId"
	FieldPreviousID           = "previousDeploymentId"
	FieldCursorSecret         = "cursorSecret"
	FieldCursorPreviousSecret = "cursorPreviousSecret"
	FieldCursorKeyID          = "cursorKeyId"
	// ID signing is deliberately independent from the rotating cursor key.
	// Cursor rotations may expire pagination tokens, but must never invalidate
	// a durable document capability or a persisted v.id reference.
	FieldIDSecret = "idSecret"
	FieldIDKeyID  = "idKeyId"
	// FieldLegacyIDSecret is a durable migration verifier for capabilities
	// emitted before ids received their own signing root.  It is copied once
	// from the pre-v1 cursor root and deliberately never follows cursor
	// rotation: old document ids and persisted v.id references must not expire
	// merely because pagination keys rotate.
	FieldLegacyIDSecret = "legacyIdSecret"
	FieldDeploymentID   = "deploymentId"
	FieldModulePath     = "modulePath"
	FieldExportName     = "exportName"
	FieldFunctionType   = "type"
	FieldHttpAction     = "httpAction"
	FieldVisibility     = "visibility"
	FieldArgs           = "args"
	FieldReturns        = "returns"
	FieldStatus         = "status"
	FieldPayload        = "payload"
	FieldType           = "type"
	FieldResult         = "result"
	FieldError          = "error"
	FieldStarted        = "started"
	FieldFinished       = "finished"
	FieldMetadata       = "metadata"
	FieldScheduledAt    = "scheduledAt"
	FieldLease          = "lease"
	FieldLeaseExpiresAt = "leaseExpiresAt"
	FieldAttempts       = "attempts"
	FieldCreated        = "created"
	FieldUpdated        = "updated"
	FieldMigrationID    = "migrationId"
	FieldChecksum       = "checksum"
	FieldSourceHash     = "sourceSchemaHash"
	FieldTargetHash     = "targetSchemaHash"
	FieldDirection      = "direction"
	FieldAppliedAt      = "appliedAt"

	FieldStorageID          = "storageId"
	FieldStorageSha256      = "sha256"
	FieldStorageSize        = "size"
	FieldStorageContentType = "contentType"
	FieldStorageFileKey     = "fileKey"
	FieldStorageFilename    = "filename"
	FieldStorageStatus      = "status"
	FieldStorageDeletedAt   = "deletedAt"
	FieldStorageCreatedBy   = "createdBy"
	FieldStorageOwner       = "leaseOwner"
	FieldStorageLeaseUntil  = "leaseUntil"

	FieldToken               = "token"
	FieldTokenStorageID      = "storageId"
	FieldTokenExpiresAt      = "expiresAt"
	FieldTokenConsumed       = "consumed"
	FieldTokenCreatedBy      = "createdBy"
	FieldTokenMaxSize        = "maxSize"
	FieldTokenAllowedTypes   = "allowedTypes"
	FieldTokenFilename       = "filename"
	FieldTokenClaim          = "claim"
	FieldTokenClaimExpiresAt = "claimExpiresAt"

	FieldKeyringKeyID     = "keyId"
	FieldKeyringKey       = "key"
	FieldKeyringPurpose   = "purpose"
	FieldKeyringCreatedAt = "createdAt"
	FieldKeyringExpiresAt = "expiresAt"
)

var collectionNames = []string{
	CollectionDeployments,
	CollectionFunctions,
	CollectionSchemaState,
	CollectionJobs,
	CollectionComponents,
	CollectionStorageFiles,
	CollectionStorageTokens,
	CollectionStorageKeyring,
	CollectionMigrationHistory,
}

type internalKey struct{}

// InternalContextKey is used to mark PBVex service DB writes as internal.
var InternalContextKey = internalKey{}

// Bootstrap ensures all PBVex system collections exist and creates the active state seed.
func Bootstrap(app core.App) error {
	ctx := internalCtx()
	for _, name := range collectionNames {
		if err := ensureCollection(app, name); err != nil {
			return fmt.Errorf("pbvex schema bootstrap %q: %w", name, err)
		}
	}

	if err := migratePinCount(ctx, app); err != nil {
		return fmt.Errorf("pbvex schema bootstrap pinCount: %w", err)
	}

	if err := ensureActiveState(ctx, app); err != nil {
		return fmt.Errorf("pbvex schema bootstrap state: %w", err)
	}

	return nil
}

func migratePinCount(ctx context.Context, app core.App) error {
	// Reconcile the pin counter from the jobs table. Deployments with
	// outstanding jobs get pinCount = job count; deployments with no jobs
	// but pinCount > 0 (leaked from a prior bug) are reset to 0.
	rows := []struct {
		DeploymentID string `db:"deploymentId"`
		Count        int    `db:"cnt"`
	}{}
	err := app.DB().
		NewQuery("SELECT deploymentId, COUNT(*) AS cnt FROM " + CollectionJobs + " GROUP BY deploymentId").
		All(&rows)
	if err != nil {
		return err
	}
	jobCounts := make(map[string]int, len(rows))
	for _, r := range rows {
		jobCounts[r.DeploymentID] = r.Count
	}

	allDeployments := []*core.Record{}
	if err := app.RecordQuery(CollectionDeployments).All(&allDeployments); err != nil {
		return err
	}
	for _, rec := range allDeployments {
		deploymentID := rec.GetString(FieldDeploymentID)
		expected := jobCounts[deploymentID]
		if rec.GetInt(FieldPinCount) == expected {
			continue
		}
		rec.Set(FieldPinCount, expected)
		if err := app.SaveWithContext(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

func ensureCollection(app core.App, name string) error {
	existing, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		// v1 builds used unprefixed system names. Rename in place so records,
		// indexes and PocketBase IDs survive the reserved-name transition.
		if legacy, legacyErr := app.FindCollectionByNameOrId(strings.TrimPrefix(name, "_")); legacyErr == nil {
			legacy.Name = name
			if err := app.Save(legacy); err != nil {
				return err
			}
			return mergeCollection(app, legacy, name)
		}
		var col *core.Collection
		switch name {
		case CollectionDeployments:
			col = deploymentsCollection()
		case CollectionFunctions:
			col = functionsCollection()
		case CollectionSchemaState:
			col = schemaStateCollection()
		case CollectionJobs:
			col = jobsCollection()
		case CollectionComponents:
			col = componentsCollection()
		case CollectionStorageFiles:
			col = storageFilesCollection()
		case CollectionStorageTokens:
			col = storageTokensCollection()
		case CollectionStorageKeyring:
			col = storageKeyringCollection()
		case CollectionMigrationHistory:
			col = migrationHistoryCollection()
		default:
			return fmt.Errorf("unknown pbvex collection %q", name)
		}

		if err := app.Save(col); err != nil {
			return err
		}
		return nil
	}

	return mergeCollection(app, existing, name)
}

func mergeCollection(app core.App, existing *core.Collection, name string) error {
	desired := func() *core.Collection {
		switch name {
		case CollectionDeployments:
			return deploymentsCollection()
		case CollectionFunctions:
			return functionsCollection()
		case CollectionSchemaState:
			return schemaStateCollection()
		case CollectionJobs:
			return jobsCollection()
		case CollectionComponents:
			return componentsCollection()
		case CollectionStorageFiles:
			return storageFilesCollection()
		case CollectionStorageTokens:
			return storageTokensCollection()
		case CollectionStorageKeyring:
			return storageKeyringCollection()
		case CollectionMigrationHistory:
			return migrationHistoryCollection()
		}
		return nil
	}()

	if desired == nil {
		return fmt.Errorf("unknown pbvex collection %q", name)
	}
	if existing.Type != desired.Type {
		return fmt.Errorf("existing collection %q has incompatible type", name)
	}
	if existing.System != desired.System {
		return fmt.Errorf("existing collection %q has incompatible system flag", name)
	}
	if !sameRule(existing.ListRule, desired.ListRule) || !sameRule(existing.ViewRule, desired.ViewRule) || !sameRule(existing.CreateRule, desired.CreateRule) || !sameRule(existing.UpdateRule, desired.UpdateRule) || !sameRule(existing.DeleteRule, desired.DeleteRule) {
		return fmt.Errorf("existing collection %q has incompatible access rules", name)
	}

	for _, desiredField := range desired.Fields {
		existingField := existing.Fields.GetByName(desiredField.GetName())
		if existingField == nil {
			existing.Fields.Add(desiredField)
			continue
		}
		if existingField.Type() != desiredField.Type() {
			return fmt.Errorf(
				"existing collection %q field %q has incompatible type %s (expected %s)",
				name, desiredField.GetName(), existingField.Type(), desiredField.Type(),
			)
		}
		existingFingerprint, err := fieldFingerprint(existingField)
		if err != nil {
			return fmt.Errorf("existing collection %q field %q fingerprint: %w", name, desiredField.GetName(), err)
		}
		desiredFingerprint, err := fieldFingerprint(desiredField)
		if err != nil {
			return fmt.Errorf("desired collection %q field %q fingerprint: %w", name, desiredField.GetName(), err)
		}
		if existingFingerprint != desiredFingerprint {
			return fmt.Errorf("existing collection %q field %q has incompatible options", name, desiredField.GetName())
		}
	}
	if len(existing.Fields) != len(desired.Fields) {
		return fmt.Errorf("existing collection %q has unexpected fields", name)
	}
	if !sameIndexes(existing.Indexes, desired.Indexes) {
		return fmt.Errorf("existing collection %q has incompatible indexes", name)
	}

	return app.Save(existing)
}

func sameRule(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func fieldFingerprint(field core.Field) (string, error) {
	b, err := json.Marshal(field)
	if err != nil {
		return "", err
	}
	var o map[string]any
	if err := json.Unmarshal(b, &o); err != nil {
		return "", err
	}
	delete(o, "id")
	delete(o, "name")
	normalized, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}
func sameIndexes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	return slices.Equal(aa, bb)
}

func ensureActiveState(ctx context.Context, app core.App) error {
	existing, err := app.FindFirstRecordByFilter(CollectionSchemaState, fmt.Sprintf("%s = {:key}", FieldKey), dbx.Params{FieldKey: StateKeyActive})
	if err == nil {
		if existing.GetString(FieldCursorSecret) == "" {
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				return err
			}
			existing.Set(FieldCursorSecret, base64.RawURLEncoding.EncodeToString(secret))
		}
		if existing.GetInt(FieldCursorKeyID) <= 0 {
			existing.Set(FieldCursorKeyID, 1)
		}
		if existing.GetString(FieldIDSecret) == "" {
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				return err
			}
			existing.Set(FieldIDSecret, base64.RawURLEncoding.EncodeToString(secret))
		}
		if existing.GetInt(FieldIDKeyID) <= 0 {
			existing.Set(FieldIDKeyID, 1)
		}
		// Older installations signed document ids with the cursor secret. Keep
		// that root as a one-time, durable compatibility verifier before any
		// subsequent cursor rotation can discard it. New ids use FieldIDSecret,
		// so this is a bounded migration reader rather than an ever-growing key
		// history.
		if existing.GetString(FieldLegacyIDSecret) == "" {
			existing.Set(FieldLegacyIDSecret, existing.GetString(FieldCursorSecret))
		}
		if existing.IsNew() {
			return nil
		}
		if existing.GetString(FieldCursorSecret) == "" {
			return fmt.Errorf("cursor secret unavailable")
		}
		if err := app.SaveWithContext(ctx, existing); err != nil {
			return err
		}
		return nil
	}

	col, err := app.FindCollectionByNameOrId(CollectionSchemaState)
	if err != nil {
		return err
	}

	record := core.NewRecord(col)
	record.Set(FieldKey, StateKeyActive)
	record.Set(FieldActiveID, "")
	record.Set(FieldPreviousID, "")
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	record.Set(FieldCursorSecret, base64.RawURLEncoding.EncodeToString(secret))
	record.Set(FieldCursorPreviousSecret, "")
	record.Set(FieldCursorKeyID, 1)
	idSecret := make([]byte, 32)
	if _, err := rand.Read(idSecret); err != nil {
		return err
	}
	record.Set(FieldIDSecret, base64.RawURLEncoding.EncodeToString(idSecret))
	record.Set(FieldIDKeyID, 1)
	// A newly bootstrapped store has no cursor-signed ids yet, but persisting
	// the anchor from day one makes the state layout restart-stable and lets a
	// restored pre-root state be migrated without changing ID verification.
	record.Set(FieldLegacyIDSecret, record.GetString(FieldCursorSecret))
	return app.SaveWithContext(ctx, record)
}

// IsReservedCollection reports whether the name belongs to the PBVex system.
func IsReservedCollection(name string) bool {
	if strings.HasPrefix(strings.ToLower(name), "pbvex_cmp_") {
		return true
	}
	for _, n := range collectionNames {
		if strings.EqualFold(name, n) || strings.EqualFold(name, strings.TrimPrefix(n, "_")) {
			return true
		}
	}
	return false
}

// IsBackingCollection reports whether a collection has the PBVex backing
// storage fingerprint. It deliberately recognizes the internal fields even
// if an operator has drifted a rule/hidden flag: those fields must never become
// a raw PocketBase API escape hatch while activation is rejecting that drift.
// A non-PBVex collection which deliberately adopts this private ABI is also
// protected, which is the safe failure mode for reserved storage names.
func IsBackingCollection(c *core.Collection) bool {
	if c == nil || c.Type != core.CollectionTypeBase || c.System {
		return false
	}
	data := c.Fields.GetByName("_pbvex_data")
	order := c.Fields.GetByName(DocumentOrderField)
	return data != nil && order != nil && data.Type() == core.FieldTypeJSON && order.Type() == core.FieldTypeJSON
}

func internalCtx() context.Context {
	return context.WithValue(context.Background(), InternalContextKey, true)
}

// WithInternalContext returns a context marked for PBVex internal writes.
func WithInternalContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, InternalContextKey, true)
}

type appContextKey struct{}

// AppContextKey carries the PocketBase app through request contexts.
var AppContextKey = appContextKey{}

// WithApp returns a context carrying the given PocketBase app and marked as internal.
func WithApp(ctx context.Context, app core.App) context.Context {
	ctx = context.WithValue(ctx, AppContextKey, app)
	return context.WithValue(ctx, InternalContextKey, true)
}

// AppFromContext returns the PocketBase app stored in the context.
func AppFromContext(ctx context.Context) (core.App, bool) {
	app, ok := ctx.Value(AppContextKey).(core.App)
	return app, ok
}
