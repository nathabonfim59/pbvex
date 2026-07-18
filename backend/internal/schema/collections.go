package schema

import (
	"github.com/pocketbase/pocketbase/core"
)

// Bundles are stored as decoded JavaScript. It must accommodate the protocol
// default maxUploadBytes, not the former implementation-specific 16 MiB cap.
const maxBundleChars = 64 << 20

func deploymentsCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionDeployments)
	col.System = true

	col.Fields.Add(&core.DateField{
		Name:   "created",
		System: true,
		Hidden: true,
	})
	col.Fields.Add(&core.DateField{
		Name:   "updated",
		System: true,
		Hidden: true,
	})
	col.Fields.Add(&core.DateField{
		Name:   FieldActivatedAt,
		System: true,
		Hidden: true,
	})

	col.Fields.Add(&core.TextField{
		Name:     FieldDeploymentID,
		System:   true,
		Required: true,
		Max:      1024,
	})
	col.Fields.Add(&core.JSONField{
		Name:     FieldManifest,
		System:   true,
		Required: true,
		MaxSize:  1 << 20,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldBundleHash,
		System:   true,
		Required: true,
		Max:      64,
	})
	col.Fields.Add(&core.NumberField{
		Name:     FieldBundleSize,
		System:   true,
		Required: true,
		OnlyInt:  true,
		Min:      floatPtr(0),
		Max:      floatPtr(1 << 30),
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldBundle,
		System:   true,
		Required: true,
		Hidden:   true,
		Max:      maxBundleChars,
	})
	col.Fields.Add(&core.BoolField{
		Name:   FieldActive,
		System: true,
	})
	col.Fields.Add(&core.NumberField{
		Name:    FieldPinCount,
		System:  true,
		Hidden:  true,
		OnlyInt: true,
		Min:     floatPtr(0),
	})

	col.AddIndex("idx_pbvex_deployments_deploymentId", true, "deploymentId", "")
	return col
}

func functionsCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionFunctions)
	col.System = true

	col.Fields.Add(&core.TextField{
		Name:     FieldDeploymentID,
		System:   true,
		Required: true,
		Max:      1024,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldModulePath,
		System:   true,
		Required: true,
		Max:      4096,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldExportName,
		System:   true,
		Required: true,
		Max:      1024,
	})
	col.Fields.Add(&core.JSONField{
		Name:     FieldFunctionType,
		System:   true,
		Required: true,
		MaxSize:  1 << 20,
	})
	col.Fields.Add(&core.TextField{
		Name:   FieldHttpAction,
		System: true,
		Max:    16,
	})
	col.Fields.Add(&core.TextField{
		Name:   FieldVisibility,
		System: true,
		Max:    16,
	})
	col.Fields.Add(&core.JSONField{
		Name:    FieldArgs,
		System:  true,
		MaxSize: 1 << 20,
	})
	col.Fields.Add(&core.JSONField{
		Name:    FieldReturns,
		System:  true,
		MaxSize: 1 << 20,
	})

	col.AddIndex("idx_pbvex_functions_deployment_exportName", true, "deploymentId, exportName", "")
	return col
}

func schemaStateCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionSchemaState)
	col.System = true

	col.Fields.Add(&core.TextField{
		Name:     FieldKey,
		System:   true,
		Required: true,
		Max:      64,
	})
	col.Fields.Add(&core.TextField{
		Name:   FieldActiveID,
		System: true,
		Max:    1024,
	})
	col.Fields.Add(&core.TextField{
		Name:   FieldPreviousID,
		System: true,
		Max:    1024,
	})
	col.Fields.Add(&core.TextField{Name: FieldCursorSecret, System: true, Hidden: true, Required: true, Max: 128})
	col.Fields.Add(&core.TextField{Name: FieldCursorPreviousSecret, System: true, Hidden: true, Max: 128})
	col.Fields.Add(&core.NumberField{Name: FieldCursorKeyID, System: true, Required: true, OnlyInt: true, Min: floatPtr(1)})
	col.Fields.Add(&core.TextField{Name: FieldIDSecret, System: true, Hidden: true, Required: true, Max: 128})
	col.Fields.Add(&core.NumberField{Name: FieldIDKeyID, System: true, Required: true, OnlyInt: true, Min: floatPtr(1)})
	col.Fields.Add(&core.TextField{Name: FieldLegacyIDSecret, System: true, Hidden: true, Required: true, Max: 128})

	col.AddIndex("idx_pbvex_schemaState_key", true, "key", "")
	return col
}

func migrationHistoryCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionMigrationHistory)
	col.System = true
	col.Fields.Add(&core.TextField{Name: FieldMigrationID, System: true, Required: true, Max: 128})
	col.Fields.Add(&core.TextField{Name: FieldChecksum, System: true, Required: true, Max: 64})
	col.Fields.Add(&core.TextField{Name: FieldSourceHash, System: true, Required: true, Max: 64})
	col.Fields.Add(&core.TextField{Name: FieldTargetHash, System: true, Required: true, Max: 64})
	col.Fields.Add(&core.TextField{Name: FieldDeploymentID, System: true, Required: true, Max: 1024})
	col.Fields.Add(&core.TextField{Name: FieldDirection, System: true, Required: true, Max: 4})
	col.Fields.Add(&core.DateField{Name: FieldAppliedAt, System: true, Required: true, Hidden: true})
	col.AddIndex("idx_pbvex_migration_history_id", false, FieldMigrationID, "")
	return col
}

func jobsCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionJobs)
	col.System = true

	col.Fields.Add(&core.DateField{
		Name:   FieldCreated,
		System: true,
		Hidden: true,
	})
	col.Fields.Add(&core.DateField{
		Name:   FieldUpdated,
		System: true,
		Hidden: true,
	})

	col.Fields.Add(&core.TextField{
		Name:     FieldDeploymentID,
		System:   true,
		Required: true,
		Max:      1024,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldType,
		System:   true,
		Required: true,
		Max:      4096,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldStatus,
		System:   true,
		Required: true,
		Max:      32,
	})
	col.Fields.Add(&core.JSONField{
		Name:    FieldPayload,
		System:  true,
		MaxSize: 1 << 20,
	})
	col.Fields.Add(&core.JSONField{
		Name:    FieldResult,
		System:  true,
		MaxSize: 1 << 20,
	})
	col.Fields.Add(&core.TextField{
		Name:   FieldError,
		System: true,
		Max:    5000,
	})
	col.Fields.Add(&core.DateField{
		Name:   FieldStarted,
		System: true,
	})
	col.Fields.Add(&core.DateField{
		Name:   FieldFinished,
		System: true,
	})
	col.Fields.Add(&core.DateField{
		Name:     FieldScheduledAt,
		System:   true,
		Required: true,
	})
	col.Fields.Add(&core.TextField{
		Name:   FieldLease,
		System: true,
		Max:    1024,
	})
	col.Fields.Add(&core.DateField{
		Name:   FieldLeaseExpiresAt,
		System: true,
	})
	col.Fields.Add(&core.NumberField{
		Name:    FieldAttempts,
		System:  true,
		OnlyInt: true,
		Min:     floatPtr(0),
		Max:     floatPtr(1000),
	})

	col.Fields.Add(&core.JSONField{
		Name:    FieldMetadata,
		System:  true,
		MaxSize: 1 << 20,
	})

	col.AddIndex("idx_pbvex_jobs_status_scheduled", false, "status, scheduledAt", "")
	col.AddIndex("idx_pbvex_jobs_lease_expiry", false, "status, leaseExpiresAt", "")
	return col
}

func componentsCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionComponents)
	col.System = true

	col.Fields.Add(&core.TextField{
		Name:     FieldDeploymentID,
		System:   true,
		Required: true,
		Max:      1024,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldName,
		System:   true,
		Required: true,
		Max:      128,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldType,
		System:   true,
		Required: true,
		// 32 mount segments, each bounded by the 1024-byte identifier limit,
		// plus separators. Every graph accepted by deploy validation must fit.
		Max: 32*1024 + 31,
	})
	col.Fields.Add(&core.JSONField{
		Name:    FieldMetadata,
		System:  true,
		MaxSize: 1 << 20,
	})

	col.AddIndex("idx_pbvex_components_namespace", true, "name", "")
	return col
}

func storageFilesCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionStorageFiles)
	col.System = true

	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.DateField{Name: "updated", System: true, Hidden: true})

	col.Fields.Add(&core.TextField{
		Name:     FieldStorageID,
		System:   true,
		Required: true,
		Max:      128,
	})
	col.Fields.Add(&core.TextField{
		Name:   FieldStorageSha256,
		System: true,
		Max:    64,
	})
	col.Fields.Add(&core.NumberField{
		Name:    FieldStorageSize,
		System:  true,
		OnlyInt: true,
		Min:     floatPtr(0),
		Max:     floatPtr(1 << 40),
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldStorageContentType,
		System:   true,
		Required: true,
		Max:      128,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldStorageFileKey,
		System:   true,
		Required: true,
		Max:      512,
	})
	col.Fields.Add(&core.TextField{
		Name:   FieldStorageFilename,
		System: true,
		Max:    255,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldStorageStatus,
		System:   true,
		Required: true,
		Max:      16,
	})
	col.Fields.Add(&core.DateField{Name: FieldStorageDeletedAt, System: true, Hidden: true})
	col.Fields.Add(&core.TextField{Name: FieldStorageCreatedBy, System: true, Max: 128})
	// leaseOwner/leaseUntil form the durable uploading reservation contract:
	// the owner is the upload attempt identity, and leaseUntil is renewed while
	// the upload is active so cleanup can only reclaim abandoned reservations.
	col.Fields.Add(&core.TextField{Name: FieldStorageOwner, System: true, Max: 128, Hidden: true})
	col.Fields.Add(&core.DateField{Name: FieldStorageLeaseUntil, System: true, Hidden: true})

	col.AddIndex("idx_pbvex_storage_files_storageId", true, "storageId", "")
	col.AddIndex("idx_pbvex_storage_files_fileKey", true, "fileKey", "")
	return col
}

func storageTokensCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionStorageTokens)
	col.System = true

	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.DateField{Name: "updated", System: true, Hidden: true})

	col.Fields.Add(&core.TextField{
		Name:     FieldToken,
		System:   true,
		Required: true,
		Max:      128,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldTokenStorageID,
		System:   true,
		Required: true,
		Max:      128,
	})
	col.Fields.Add(&core.DateField{
		Name:     FieldTokenExpiresAt,
		System:   true,
		Required: true,
	})
	col.Fields.Add(&core.BoolField{
		Name:     FieldTokenConsumed,
		System:   true,
		Required: false,
	})
	col.Fields.Add(&core.TextField{Name: FieldTokenCreatedBy, System: true, Max: 128})
	col.Fields.Add(&core.NumberField{
		Name:    FieldTokenMaxSize,
		System:  true,
		OnlyInt: true,
		Min:     floatPtr(0),
		Max:     floatPtr(1 << 40),
	})
	col.Fields.Add(&core.TextField{Name: FieldTokenAllowedTypes, System: true, Max: 2048})
	col.Fields.Add(&core.TextField{Name: FieldTokenFilename, System: true, Max: 255})

	col.Fields.Add(&core.TextField{Name: FieldTokenClaim, System: true, Max: 64})
	col.Fields.Add(&core.DateField{Name: FieldTokenClaimExpiresAt, System: true, Hidden: true})

	col.AddIndex("idx_pbvex_storage_tokens_token", true, "token", "")
	col.AddIndex("idx_pbvex_storage_tokens_expires", false, "expiresAt", "")
	col.AddIndex("idx_pbvex_storage_tokens_claim", false, "claim", "")
	return col
}

func storageKeyringCollection() *core.Collection {
	col := core.NewBaseCollection(CollectionStorageKeyring)
	col.System = true

	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.DateField{Name: "updated", System: true, Hidden: true})

	col.Fields.Add(&core.TextField{
		Name:     FieldKeyringKeyID,
		System:   true,
		Required: true,
		Max:      64,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldKeyringKey,
		System:   true,
		Required: true,
		Hidden:   true,
		Max:      512,
	})
	col.Fields.Add(&core.TextField{
		Name:     FieldKeyringPurpose,
		System:   true,
		Required: true,
		Max:      32,
	})
	col.Fields.Add(&core.DateField{
		Name:     FieldKeyringCreatedAt,
		System:   true,
		Required: true,
	})
	col.Fields.Add(&core.DateField{
		Name:     FieldKeyringExpiresAt,
		System:   true,
		Required: true,
	})

	col.AddIndex("idx_pbvex_storage_keyring_keyId", true, "keyId", "")
	col.AddIndex("idx_pbvex_storage_keyring_expires", false, "expiresAt", "")
	return col
}

func floatPtr(v float64) *float64 {
	return &v
}
