package deploy

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/dbutils"
	"github.com/pocketbase/pocketbase/tools/types"
)

// RuntimeInvoker is the interface required by the deployment service for runtime operations.
// RuntimeInvoker is intentionally structural at the Service boundary. PBVex
// supports the original narrow embedder contract and the richer authenticated
// runtime without forcing existing embedders to change method signatures.
type RuntimeInvoker any

// Config controls deployment service behavior.
type Config struct {
	HistoryLimit int
	PoolSize     int
}

// DefaultConfig returns the default deployment configuration.
func DefaultConfig() Config {
	return Config{
		HistoryLimit: 10,
		PoolSize:     5,
	}
}

// Invalidator is notified when the active deployment changes so that realtime
// subscriptions can be re-evaluated.
type Invalidator interface {
	// InvalidateAll notifies active subscriptions to re-run their queries
	// without dropping the connection (used for record mutations).
	InvalidateAll()
	// ReconnectAll closes all active subscription connections so clients
	// reconnect and re-negotiate limits with the newly active deployment.
	// Used on activation/rollback where config (maxReturnValueBytes etc.)
	// may differ from the pinned snapshot.
	ReconnectAll()
}

// ActivationObserver is notified after a new active deployment commits and
// when the persisted active deployment is warmed during bootstrap.
type ActivationObserver interface {
	ActiveDeploymentChanged(deploymentID string, manifest DeploymentManifest)
}

// Service is the application layer for deployments.
type Service struct {
	app                core.App
	repo               *Repo
	invoker            RuntimeInvoker
	config             Config
	invalidator        Invalidator
	activationObserver ActivationObserver
}

const (
	maxSchemaMigrationRows  = 10_000
	maxSchemaMigrationBytes = 64 << 20
	// One record at a time keeps the pre-normalized input, normalized document
	// and order projection bounded independently of a hostile document/default
	// expansion. The total work budget below still prevents an unbounded
	// activation migration.
	schemaMigrationBatch = 1
)

func backingCollection(name string) *core.Collection {
	c := core.NewBaseCollection(name)
	c.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	// The backing JSON is an implementation detail. It is hidden in addition
	// to the collection's locked rules so an accidental rule drift cannot turn
	// it into a raw writable PocketBase API surface that bypasses ctx.db.
	c.Fields.Add(&core.JSONField{Name: "_pbvex_data", Required: true, Hidden: true, MaxSize: 1 << 20})
	c.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	return c
}

// validateBackingCollection treats the three PBVex-owned fields as a storage
// ABI.  A matching field name alone is not enough: changing hidden/system,
// required or maximum-size options can make documents observable or make an
// activation silently truncate data.
func validateBackingCollection(c *core.Collection, name string) error {
	if c == nil {
		return fmt.Errorf("schema drift: collection is missing")
	}
	if c.Name != name {
		return fmt.Errorf("schema drift: collection name is %q", c.Name)
	}
	if c.Type != core.CollectionTypeBase {
		return fmt.Errorf("schema drift: collection type %q is incompatible with PBVex table storage", c.Type)
	}
	if c.System {
		return fmt.Errorf("schema drift: collection is system-owned")
	}
	// A nil PocketBase API rule means no client-side access. Empty/non-nil
	// rules are public and any filter rule could expose a mutable raw backing
	// document, so all five must remain locked.
	if c.ListRule != nil || c.ViewRule != nil || c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil {
		return fmt.Errorf("schema drift: collection API rules are not locked")
	}
	want := backingCollection(name)
	if len(c.Fields) != len(want.Fields) {
		return fmt.Errorf("schema drift: collection fields do not match PBVex table storage")
	}
	for _, desired := range want.Fields {
		actual := c.Fields.GetByName(desired.GetName())
		if actual == nil || actual.Type() != desired.Type() {
			return fmt.Errorf("schema drift: backing field %q is missing or incompatible", desired.GetName())
		}
		actualFingerprint, err := backingFieldFingerprint(actual)
		if err != nil {
			return fmt.Errorf("schema drift: backing field %q is unreadable", desired.GetName())
		}
		desiredFingerprint, err := backingFieldFingerprint(desired)
		if err != nil || actualFingerprint != desiredFingerprint {
			return fmt.Errorf("schema drift: backing field %q options are incompatible", desired.GetName())
		}
	}
	// Field names are part of the physical ABI too. The cardinality check
	// above makes this loop reject a collection with an injected raw field even
	// if all PBVex-owned fields happen to match.
	for _, actual := range c.Fields {
		if want.Fields.GetByName(actual.GetName()) == nil {
			return fmt.Errorf("schema drift: unexpected backing field %q", actual.GetName())
		}
	}
	return nil
}

func backingFieldFingerprint(field core.Field) (string, error) {
	b, err := json.Marshal(field)
	if err != nil {
		return "", err
	}
	var value map[string]any
	if err := json.Unmarshal(b, &value); err != nil {
		return "", err
	}
	delete(value, "id")
	delete(value, "name")
	return CanonicalJSON(value)
}

// NewService creates a new deployment service.
func NewService(app core.App, repo *Repo, invoker RuntimeInvoker, config Config) *Service {
	if config.HistoryLimit <= 0 {
		config.HistoryLimit = DefaultConfig().HistoryLimit
	}
	if config.PoolSize <= 0 {
		config.PoolSize = DefaultConfig().PoolSize
	}
	return &Service{
		app:     app,
		repo:    repo,
		invoker: invoker,
		config:  config,
	}
}

func verifyRuntime(invoker any, ctx context.Context, deploymentID, bundle string, descriptors []FunctionDescriptor, migrations []MigrationDescriptor) error {
	if v, ok := invoker.(interface {
		VerifyDeployment(context.Context, string, string, []FunctionDescriptor, []MigrationDescriptor) error
	}); ok {
		return v.VerifyDeployment(ctx, deploymentID, bundle, descriptors, migrations)
	}
	if len(migrations) != 0 {
		return fmt.Errorf("runtime migration verifier is not configured")
	}
	v, ok := invoker.(interface {
		Verify(context.Context, string, string, []FunctionDescriptor) error
	})
	if !ok {
		return fmt.Errorf("runtime verifier is not configured")
	}
	return v.Verify(ctx, deploymentID, bundle, descriptors)
}

func compileRuntime(invoker any, deploymentID, bundle string, descriptors []FunctionDescriptor, migrations []MigrationDescriptor, cfg DeploymentConfig) error {
	if v, ok := invoker.(interface {
		CompileDeployment(string, string, []FunctionDescriptor, []MigrationDescriptor, ...DeploymentConfig) error
	}); ok {
		return v.CompileDeployment(deploymentID, bundle, descriptors, migrations, cfg)
	}
	if len(migrations) != 0 {
		return fmt.Errorf("runtime migration compiler is not configured")
	}
	switch v := invoker.(type) {
	case interface {
		Compile(string, string, []FunctionDescriptor, ...DeploymentConfig) error
	}:
		return v.Compile(deploymentID, bundle, descriptors, cfg)
	case interface {
		Compile(string, string, []FunctionDescriptor, DeploymentConfig) error
	}:
		return v.Compile(deploymentID, bundle, descriptors, cfg)
	case interface {
		Compile(string, string, []FunctionDescriptor) error
	}:
		return v.Compile(deploymentID, bundle, descriptors)
	default:
		return fmt.Errorf("runtime compiler is not configured")
	}
}

func invokeRuntime(invoker any, ctx context.Context, deploymentID, functionName string, args any) (any, error) {
	metadata := auth.InvocationMetadataFromContext(ctx)
	switch v := invoker.(type) {
	case interface {
		Invoke(context.Context, string, string, any, ...any) (any, error)
	}:
		return v.Invoke(ctx, deploymentID, functionName, args)
	case interface {
		Invoke(context.Context, string, string, any) (any, error)
	}:
		return v.Invoke(ctx, deploymentID, functionName, args)
	case interface {
		Invoke(context.Context, string, string, any, *auth.UserIdentity, string) (any, error)
	}:
		return v.Invoke(ctx, deploymentID, functionName, args, metadata.Identity, metadata.RequestID)
	default:
		return nil, fmt.Errorf("runtime invoker is not configured")
	}
}

func invokeDatabaseRuntime(invoker any, ctx context.Context, deploymentID, functionName string, args any, app core.App, manifest DeploymentManifest) (any, bool, error) {
	metadata := auth.InvocationMetadataFromContext(ctx)
	switch v := invoker.(type) {
	case interface {
		InvokeWithDatabase(context.Context, string, string, any, ...any) (any, error)
	}:
		result, err := v.InvokeWithDatabase(ctx, deploymentID, functionName, args, app, manifest)
		return result, true, err
	case interface {
		InvokeWithDatabase(context.Context, string, string, any, core.App, DeploymentManifest) (any, error)
	}:
		result, err := v.InvokeWithDatabase(ctx, deploymentID, functionName, args, app, manifest)
		return result, true, err
	case interface {
		InvokeWithDatabase(context.Context, string, string, any, *auth.UserIdentity, string, core.App, DeploymentManifest) (any, error)
	}:
		result, err := v.InvokeWithDatabase(ctx, deploymentID, functionName, args, metadata.Identity, metadata.RequestID, app, manifest)
		return result, true, err
	default:
		return nil, false, nil
	}
}

// SetInvalidator sets the invalidator that is notified on activation/rollback.
func (s *Service) SetInvalidator(inv Invalidator) {
	s.invalidator = inv
}

// SetActivationObserver installs the active-deployment lifecycle observer.
func (s *Service) SetActivationObserver(observer ActivationObserver) {
	s.activationObserver = observer
}

// Upload validates, stores, and prepares a new deployment.
func (s *Service) Upload(raw any) (*DeploymentUploadResponse, error) {
	return s.UploadContext(context.Background(), raw)
}

// UploadContext is the request-aware form used by the HTTP API. The legacy
// Upload method remains for embedders, but lifecycle work must otherwise keep
// the caller's cancellation/deadline all the way through verification and DB
// writes.
func (s *Service) UploadContext(ctx context.Context, raw any) (*DeploymentUploadResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, bundleBytes, err := ValidateUploadRequest(raw)
	if err != nil {
		return nil, err
	}

	manifest := req.Manifest
	if req.Size > NormalizeConfig(manifest.Config).MaxUploadBytes {
		return nil, fmt.Errorf("%w: bundle exceeds configured maxUploadBytes", ErrInvalidBundle)
	}
	bundleJS := string(bundleBytes)

	if err := verifyRuntime(s.invoker, ctx, manifest.DeploymentID, bundleJS, manifest.Functions, manifest.Migrations); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}

	existing, err := s.repo.GetDeployment(s.internalCtxFrom(ctx), s.app, manifest.DeploymentID)
	if err == nil {
		return s.resumeUpload(existing, manifest, bundleJS, req.Sha256, req.Size)
	}
	if !errors.Is(err, ErrDeploymentNotFound) {
		return nil, err
	}

	record, err := s.repo.CreateDeployment(s.internalCtxFrom(ctx), s.app, manifest, bundleJS, req.Sha256, req.Size)
	if err != nil {
		// A concurrent upload of the same deterministic artifact may have won the
		// unique deploymentId race after the lookup above. Treat that record like
		// any other retry, while preserving unrelated persistence errors.
		existing, lookupErr := s.repo.GetDeployment(s.internalCtxFrom(ctx), s.app, manifest.DeploymentID)
		if lookupErr == nil {
			return s.resumeUpload(existing, manifest, bundleJS, req.Sha256, req.Size)
		}
		return nil, err
	}

	deploymentID := record.GetString(schema.FieldDeploymentID)
	if err := compileRuntime(s.invoker, deploymentID, bundleJS, manifest.Functions, manifest.Migrations, NormalizeConfig(manifest.Config)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}

	if err := s.trimHistoryContext(ctx); err != nil {
		s.app.Logger().Warn("failed to trim deployment history", "error", err)
	}

	return &DeploymentUploadResponse{
		DeploymentID: deploymentID,
		BundleHash:   req.Sha256,
		AcceptedAt:   responseTimestamp(record.GetDateTime("created")),
	}, nil
}

func (s *Service) resumeUpload(record *core.Record, manifest DeploymentManifest, bundleJS, bundleHash string, bundleSize int64) (*DeploymentUploadResponse, error) {
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize manifest: %w", err)
	}
	if record.GetString(schema.FieldManifest) != string(manifestJSON) ||
		record.GetString(schema.FieldBundleHash) != bundleHash ||
		int64(record.GetInt(schema.FieldBundleSize)) != bundleSize ||
		record.GetString(schema.FieldBundle) != bundleJS {
		return nil, fmt.Errorf("%w: deployment id already exists with different content", ErrInvalidBundle)
	}

	deploymentID := record.GetString(schema.FieldDeploymentID)
	if err := compileRuntime(s.invoker, deploymentID, bundleJS, manifest.Functions, manifest.Migrations, NormalizeConfig(manifest.Config)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}

	return &DeploymentUploadResponse{
		DeploymentID: deploymentID,
		BundleHash:   bundleHash,
		AcceptedAt:   responseTimestamp(record.GetDateTime("created")),
	}, nil
}

// List returns all stored deployments with the active flag.
func (s *Service) List() (*DeploymentListResponse, error) {
	return s.ListContext(context.Background())
}

func (s *Service) ListContext(ctx context.Context) (*DeploymentListResponse, error) {
	records, err := s.repo.ListDeployments(s.internalCtxFrom(ctx), s.app)
	if err != nil {
		return nil, err
	}

	deployments := make([]Deployment, 0, len(records))
	for _, rec := range records {
		d, err := s.recordToDeployment(rec)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}

	return &DeploymentListResponse{Deployments: deployments}, nil
}

// Get returns a single deployment by deploymentId.
func (s *Service) Get(id string) (*Deployment, error) {
	return s.GetContext(context.Background(), id)
}

func (s *Service) GetContext(ctx context.Context, id string) (*Deployment, error) {
	record, err := s.repo.GetDeployment(ctx, s.app, id)
	if err != nil {
		return nil, err
	}
	d, err := s.recordToDeployment(record)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Active returns the currently active deployment.
func (s *Service) Active() (*Deployment, error) {
	return s.ActiveContext(context.Background())
}

func (s *Service) ActiveContext(ctx context.Context) (*Deployment, error) {
	activeID, err := s.activeID(ctx)
	if err != nil {
		return nil, err
	}
	return s.GetContext(ctx, activeID)
}

// Activate atomically switches the active deployment to id.
func (s *Service) Activate(id string, atomic bool) (*DeploymentActivateResponse, error) {
	return s.ActivateContext(context.Background(), id, atomic)
}

func (s *Service) ActivateContext(ctx context.Context, id string, atomic bool) (*DeploymentActivateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !atomic {
		return nil, fmt.Errorf("%w: activation must be atomic", ErrActivationFailed)
	}

	record, err := s.repo.GetDeployment(ctx, s.app, id)
	if err != nil {
		return nil, err
	}
	if record.GetBool(schema.FieldActive) {
		return s.activeDeploymentResponse(ctx, record)
	}

	manifest, bundleJS, err := s.recordManifestAndBundle(record)
	if err != nil {
		return nil, err
	}

	deploymentID := record.GetString(schema.FieldDeploymentID)
	if err := verifyRuntime(s.invoker, ctx, deploymentID, bundleJS, manifest.Functions, manifest.Migrations); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrActivationFailed, err)
	}
	if err := compileRuntime(s.invoker, deploymentID, bundleJS, manifest.Functions, manifest.Migrations, NormalizeConfig(manifest.Config)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrActivationFailed, err)
	}

	now := types.NowDateTime()
	var previousID string
	var warning *MigrationWarning
	if err := s.app.RunInTransaction(func(txApp core.App) error {
		internalCtx := s.internalCtxFrom(ctx)
		if err := internalCtx.Err(); err != nil {
			return err
		}
		state, err := s.repo.GetState(internalCtx, txApp)
		if err != nil {
			return err
		}

		currentActiveID := state.GetString(schema.FieldActiveID)
		if currentActiveID == id {
			return ErrAlreadyActive
		}
		sourceManifest := DeploymentManifest{Schema: map[string]any{"tables": []any{}}}
		if currentActiveID != "" {
			sourceRecord, err := s.repo.GetDeployment(internalCtx, txApp, currentActiveID)
			if err != nil {
				return err
			}
			sourceManifest, err = s.recordManifest(sourceRecord)
			if err != nil {
				return err
			}
		}
		plans, err := planMigrations(sourceManifest, manifest)
		if err != nil {
			return fmt.Errorf("%w: migration planning failed: %v", ErrActivationFailed, err)
		}
		if err := preflightMigrationHistory(internalCtx, txApp, manifest.Migrations); err != nil {
			return fmt.Errorf("%w: %v", ErrActivationFailed, err)
		}
		if err := authenticateComponentMountArgs(internalCtx, txApp, manifest); err != nil {
			return err
		}
		if err := preflightMaterializeSchema(internalCtx, txApp, manifest); err != nil {
			return err
		}
		work, skipMaterialization, err := schemaMigrationWork(sourceManifest, manifest, plans)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrActivationFailed, err)
		}
		rows, estimatedBytes, err := preflightMigrationPlans(internalCtx, txApp, work)
		if err != nil {
			return err
		}
		if len(work) > 0 {
			warning = migrationWarning(rows, estimatedBytes)
		}
		budget := &migrationBudget{}
		if err := s.applyMigrationPlans(internalCtx, txApp, deploymentID, "up", manifest, plans, now, budget); err != nil {
			return fmt.Errorf("%w: %v", ErrActivationFailed, err)
		}
		materializeCtx := withMigrationMaterialization(internalCtx, budget, skipMaterialization)
		if err := materializeSchema(materializeCtx, txApp, manifest); err != nil {
			return err
		}
		if len(work) > 0 {
			if actualWarning := migrationWarning(budget.rows, budget.bytes); actualWarning != nil {
				warning = actualWarning
			}
		}

		if currentActiveID != "" {
			if err := s.repo.SetDeploymentActive(internalCtx, txApp, currentActiveID, false); err != nil {
				return err
			}
		}

		if err := s.repo.SetDeploymentActive(internalCtx, txApp, id, true); err != nil {
			return err
		}
		if err := s.repo.SetDeploymentActivatedAt(internalCtx, txApp, id, now); err != nil {
			return err
		}

		previousID = currentActiveID
		state.Set(schema.FieldPreviousID, currentActiveID)
		state.Set(schema.FieldActiveID, id)
		return s.repo.SaveState(internalCtx, txApp, state)
	}); err != nil {
		if errors.Is(err, ErrAlreadyActive) {
			active, lookupErr := s.repo.GetDeployment(ctx, s.app, id)
			if lookupErr == nil && active.GetBool(schema.FieldActive) {
				return s.activeDeploymentResponse(ctx, active)
			}
		}
		return nil, err
	}

	resp := &DeploymentActivateResponse{
		DeploymentID: id,
		ActivatedAt:  responseTimestamp(now),
	}
	if previousID != "" {
		resp.PreviousDeploymentID = &previousID
	}
	if warning != nil {
		resp.Warnings = []MigrationWarning{*warning}
	}

	if s.invalidator != nil {
		s.invalidator.ReconnectAll()
	}
	if s.activationObserver != nil {
		s.activationObserver.ActiveDeploymentChanged(deploymentID, manifest)
	}

	return resp, nil
}

func (s *Service) activeDeploymentResponse(ctx context.Context, record *core.Record) (*DeploymentActivateResponse, error) {
	activatedAt := record.GetDateTime(schema.FieldActivatedAt)
	if activatedAt.IsZero() {
		return nil, fmt.Errorf("active deployment %q has no activation timestamp", record.GetString(schema.FieldDeploymentID))
	}
	state, err := s.repo.GetState(s.internalCtxFrom(ctx), s.app)
	if err != nil {
		return nil, err
	}
	resp := &DeploymentActivateResponse{
		DeploymentID: record.GetString(schema.FieldDeploymentID),
		ActivatedAt:  responseTimestamp(activatedAt),
	}
	if previousID := state.GetString(schema.FieldPreviousID); previousID != "" {
		resp.PreviousDeploymentID = &previousID
	}
	return resp, nil
}

func responseTimestamp(value types.DateTime) string {
	return value.Time().UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)
}

// authenticateComponentMountArgs authenticates every declared v.id against
// the installation key and exact mounted namespace. The uploaded manifest is
// immutable; runtime repeats this normalization when constructing ctx.args.
func authenticateComponentMountArgs(ctx context.Context, app core.App, manifest DeploymentManifest) error {
	if manifest.Components == nil {
		return nil
	}
	definitions := make(map[string]ComponentDefinition, len(manifest.Components.Definitions))
	for _, definition := range manifest.Components.Definitions {
		definitions[definition.ComponentID] = definition
	}
	var walk func([]ComponentMount, string) error
	walk = func(mounts []ComponentMount, parent string) error {
		for index := range mounts {
			mount := mounts[index]
			path := mount.MountPath(parent)
			definition, ok := definitions[mount.ComponentID]
			if !ok {
				return fmt.Errorf("component %q definition unavailable", path)
			}
			if definition.Args != nil {
				value, present := mount.Args, mount.ArgsPresent
				if !present {
					value, present = missingComponentMountArg(definition.Args)
				}
				if present {
					namespace, err := ComponentNamespaceID(path)
					if err != nil {
						return err
					}
					check, err := activeDocumentIDChecker(ctx, app, namespace)
					if err != nil {
						return err
					}
					_, err = schema.NormalizeValue(definition.Args, value, check)
					if err != nil {
						return fmt.Errorf("component %q mount args authentication failed", path)
					}
				}
			}
			if err := walk(mount.Children, path); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(manifest.Components.Mounts, "")
}

func missingComponentMountArg(descriptor any) (any, bool) {
	value, accepted, present := missingComponentMountArgState(descriptor)
	return value, accepted && present
}

func missingComponentMountArgState(descriptor any) (any, bool, bool) {
	raw, ok := descriptor.(map[string]any)
	if !ok {
		return nil, false, false
	}
	switch raw["type"] {
	case "optional":
		return nil, true, false
	case "defaulted":
		value, present := raw["defaultValue"]
		return value, present, present
	case "object":
		shape, _ := raw["shape"].(map[string]any)
		if shape == nil {
			shape, _ = raw["fields"].(map[string]any)
		}
		if shape == nil {
			return nil, false, false
		}
		value := map[string]any{}
		for key, child := range shape {
			childValue, accepted, present := missingComponentMountArgState(child)
			if !accepted {
				return nil, false, false
			}
			if present {
				value[key] = childValue
			}
		}
		return value, true, true
	case "union":
		branches, _ := raw["validators"].([]any)
		for _, branch := range branches {
			if value, accepted, present := missingComponentMountArgState(branch); accepted {
				return value, true, present
			}
		}
	}
	return nil, false, false
}

// materializeSchema is deliberately part of activation, never invocation.
// Existing user collections are checked for the PBVex backing fields instead
// of being silently reshaped while a function is running.
func materializeSchema(ctx context.Context, app core.App, manifest DeploymentManifest) error {
	if err := preflightMaterializeSchema(ctx, app, manifest); err != nil {
		return err
	}
	namespaces, err := ComponentNamespaces(manifest.Components)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(namespaces))
	for path := range namespaces {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if err := materializeNamespaceSchema(ctx, app, manifest.Schema, RootNamespace, nil); err != nil {
		return err
	}
	for _, path := range paths {
		namespace := namespaces[path]
		if err := materializeNamespaceSchema(ctx, app, namespace.Schema, namespace.ID, namespace.PhysicalByTable); err != nil {
			return fmt.Errorf("component %q schema: %w", path, err)
		}
	}
	return reconcileComponentCatalog(ctx, app, manifest, namespaces)
}

func preflightMaterializeSchema(ctx context.Context, app core.App, manifest DeploymentManifest) error {
	namespaces, err := ComponentNamespaces(manifest.Components)
	if err != nil {
		return err
	}
	if err := preflightPhysicalIdentities(manifest, namespaces); err != nil {
		return err
	}
	if err := preflightNamespaceSchema(ctx, app, manifest.Schema, RootNamespace, nil); err != nil {
		return err
	}
	paths := make([]string, 0, len(namespaces))
	for path := range namespaces {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		namespace := namespaces[path]
		if err := preflightNamespaceSchema(ctx, app, namespace.Schema, namespace.ID, namespace.PhysicalByTable); err != nil {
			return fmt.Errorf("component %q schema: %w", path, err)
		}
	}
	return nil
}

// preflightPhysicalIdentities rejects every manifest-wide collection or SQL
// index alias before activation mutates storage. Names are compared with
// SQLite/PocketBase's case-insensitive identity rules even when the desired
// backing ABI happens to be identical.
func preflightPhysicalIdentities(manifest DeploymentManifest, namespaces map[string]ComponentNamespace) error {
	collections := map[string]string{}
	indexes := map[string]string{}
	visit := func(owner string, rawSchema any, physicalByTable map[string]string) error {
		schemaObject, ok := rawSchema.(map[string]any)
		if !ok {
			return nil
		}
		for _, rawTable := range listJSON(schemaObject["tables"]) {
			table, ok := rawTable.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid schema")
			}
			logical, _ := table["tableName"].(string)
			physical := namespaceCollection(owner, logical, physicalByTable)
			key := strings.ToLower(physical)
			identity := owner + ":" + logical
			if previous, exists := collections[key]; exists && previous != identity {
				return fmt.Errorf("physical collection collision")
			}
			collections[key] = identity
			fields, _ := table["fields"].(map[string]any)
			for _, index := range schemaIndexes(table) {
				name, _, valid := physicalIndex(physical, index, fields)
				if !valid {
					return fmt.Errorf("invalid schema index")
				}
				indexKey := strings.ToLower(name)
				indexIdentity := identity + ":" + fmt.Sprint(index["name"])
				if previous, exists := indexes[indexKey]; exists && previous != indexIdentity {
					return fmt.Errorf("physical index collision")
				}
				indexes[indexKey] = indexIdentity
			}
		}
		return nil
	}
	if err := visit(RootNamespace, manifest.Schema, nil); err != nil {
		return err
	}
	paths := make([]string, 0, len(namespaces))
	for path := range namespaces {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		namespace := namespaces[path]
		if err := visit(namespace.ID, namespace.Schema, namespace.PhysicalByTable); err != nil {
			return fmt.Errorf("component %q schema: %w", path, err)
		}
	}
	return nil
}

func materializeNamespaceSchema(ctx context.Context, app core.App, rawSchema any, namespace string, physicalByTable map[string]string) error {
	if err := preflightNamespaceSchema(ctx, app, rawSchema, namespace, physicalByTable); err != nil {
		return err
	}
	s, ok := rawSchema.(map[string]any)
	if !ok {
		return nil
	}
	tables, _ := s["tables"].([]any)
	idCheck, err := activeDocumentIDChecker(ctx, app, namespace)
	if err != nil {
		return err
	}
	for _, raw := range tables {
		if err := ctx.Err(); err != nil {
			return err
		}
		t, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid schema")
		}
		name, _ := t["tableName"].(string)
		physical := namespaceCollection(namespace, name, physicalByTable)
		c, err := app.FindCollectionByNameOrId(physical)
		if errors.Is(err, sql.ErrNoRows) {
			c = backingCollection(physical)
		} else if err != nil {
			return fmt.Errorf("schema lookup failed")
		} else if err := validateBackingCollection(c, physical); err != nil {
			return fmt.Errorf("table %q (collection %q): %w", name, physical, err)
		}
		fields, ok := t["fields"].(map[string]any)
		if !ok {
			return fmt.Errorf("invalid schema")
		}
		desired := map[string]bool{}
		for _, rawIndex := range schemaIndexes(t) {
			indexName, columns, ok := physicalIndex(physical, rawIndex, fields)
			if !ok {
				return fmt.Errorf("invalid schema index")
			}
			desired[indexName] = true
			if existing := c.GetIndex(indexName); existing != "" {
				if !samePhysicalIndex(existing, physical, indexName, columns) {
					if _, legacyColumns, legacyOK := legacyPhysicalIndex(physical, rawIndex, fields); legacyOK && samePhysicalIndex(existing, physical, indexName, legacyColumns) {
						c.RemoveIndex(indexName)
						c.AddIndex(indexName, false, columns, "")
						continue
					}
					return fmt.Errorf("schema index drift")
				}
				continue
			}
			c.AddIndex(indexName, false, columns, "")
		}
		for _, existing := range append([]string(nil), c.Indexes...) {
			indexName := collectionIndexName(existing)
			if strings.HasPrefix(indexName, "idx_pbvex_"+physical+"_") && !desired[indexName] {
				c.RemoveIndex(indexName)
			}
		}
		if err := app.SaveWithContext(ctx, c); err != nil {
			return fmt.Errorf("schema materialization failed")
		}
		if err := materializeDocuments(ctx, app, physical, fields, idCheck); err != nil {
			return err
		}
	}
	return nil
}

func preflightNamespaceSchema(ctx context.Context, app core.App, rawSchema any, namespace string, physicalByTable map[string]string) error {
	s, ok := rawSchema.(map[string]any)
	if !ok {
		return nil
	}
	tables, _ := s["tables"].([]any)
	if len(tables) > maxSchemaTables {
		return fmt.Errorf("invalid schema")
	}
	// Do all bounded migration checks before creating a collection or altering
	// an index. This keeps a too-large/invalid activation entirely side-effect
	// free even when callers invoke this helper outside Service.Activate.
	for _, raw := range tables {
		if err := ctx.Err(); err != nil {
			return err
		}
		t, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid schema")
		}
		name, _ := t["tableName"].(string)
		if !isIdentifier(name) || schema.IsReservedCollection(name) {
			return fmt.Errorf("invalid schema table")
		}
		fields, ok := t["fields"].(map[string]any)
		if !ok || len(fields) > maxSchemaFields {
			return fmt.Errorf("invalid schema")
		}
		physical := namespaceCollection(namespace, name, physicalByTable)
		existing, err := app.FindCollectionByNameOrId(physical)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("schema lookup failed")
		}
		if err == nil {
			if namespace != RootNamespace {
				owned, ownershipErr := componentOwnsPhysicalCollection(ctx, app, namespace, name, physical)
				if ownershipErr != nil {
					return ownershipErr
				}
				if !owned {
					return fmt.Errorf("component physical collection ownership conflict")
				}
			}
			if err := validateBackingCollection(existing, physical); err != nil {
				return fmt.Errorf("table %q (collection %q): %w", name, physical, err)
			}
			count, err := backingRecordCount(ctx, app, physical)
			if err != nil || count > maxSchemaMigrationRows {
				return fmt.Errorf("schema migration exceeds limit")
			}
		}
	}
	if _, err := activeDocumentIDChecker(ctx, app, namespace); err != nil {
		return err
	}
	return nil
}

func componentOwnsPhysicalCollection(ctx context.Context, app core.App, namespace, logical, physical string) (bool, error) {
	record := &core.Record{}
	err := app.RecordQuery(schema.CollectionComponents).
		WithContext(ctx).
		AndWhere(dbx.HashExp{schema.CollectionComponents + "." + schema.FieldName: namespace}).
		Limit(1).One(record)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("component catalog unavailable")
	}
	collections, err := validatedComponentCatalogCollections(record, namespace)
	if err != nil {
		return false, err
	}
	owned := collections[logical]
	return owned == physical, nil
}

func validatedComponentCatalogCollections(record *core.Record, namespace string) (map[string]string, error) {
	metadata := map[string]any{}
	encoded, err := json.Marshal(record.Get(schema.FieldMetadata))
	if err != nil || json.Unmarshal(encoded, &metadata) != nil {
		return nil, fmt.Errorf("component catalog unavailable")
	}
	raw, ok := metadata["collections"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("component catalog ownership invalid")
	}
	collections := make(map[string]string, len(raw))
	for logical, value := range raw {
		physical, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("component catalog ownership invalid")
		}
		expected, deriveErr := ComponentCollectionName(namespace, logical)
		if deriveErr != nil || physical != expected {
			return nil, fmt.Errorf("component catalog ownership conflict")
		}
		collections[logical] = physical
	}
	return collections, nil
}

func namespaceCollection(namespace, table string, physicalByTable map[string]string) string {
	if namespace == RootNamespace || namespace == "" {
		return table
	}
	return physicalByTable[table]
}

// reconcileComponentCatalog records durable ownership of path-derived
// namespaces. Entries are never deleted during activation or rollback: a
// removed/renamed mount becomes dormant while its physical collections remain
// available if that canonical path is mounted again.
func reconcileComponentCatalog(ctx context.Context, app core.App, manifest DeploymentManifest, namespaces map[string]ComponentNamespace) error {
	paths := make([]string, 0, len(namespaces))
	for path := range namespaces {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		namespace := namespaces[path]
		record := &core.Record{}
		collections := map[string]string{}
		err := app.RecordQuery(schema.CollectionComponents).
			WithContext(ctx).
			AndWhere(dbx.HashExp{schema.CollectionComponents + "." + schema.FieldName: namespace.ID}).
			Limit(1).One(record)
		if errors.Is(err, sql.ErrNoRows) {
			collection, findErr := app.FindCollectionByNameOrId(schema.CollectionComponents)
			if findErr != nil {
				return fmt.Errorf("component catalog unavailable")
			}
			record = core.NewRecord(collection)
		} else if err != nil {
			return fmt.Errorf("component catalog unavailable")
		} else if record.GetString(schema.FieldType) != path {
			return fmt.Errorf("component namespace ownership conflict")
		} else {
			collections, err = validatedComponentCatalogCollections(record, namespace.ID)
			if err != nil {
				return err
			}
		}
		for logical, physical := range namespace.PhysicalByTable {
			expected, deriveErr := ComponentCollectionName(namespace.ID, logical)
			if deriveErr != nil || physical != expected {
				return fmt.Errorf("component namespace ownership conflict")
			}
			if owned, exists := collections[logical]; exists && owned != physical {
				return fmt.Errorf("component namespace ownership conflict")
			}
			collections[logical] = physical
		}
		record.Set(schema.FieldName, namespace.ID)
		record.Set(schema.FieldType, path)
		record.Set(schema.FieldDeploymentID, manifest.DeploymentID)
		record.Set(schema.FieldMetadata, map[string]any{
			"componentId": namespace.Definition.ComponentID,
			"collections": collections,
		})
		if err := app.SaveWithContext(ctx, record); err != nil {
			return fmt.Errorf("component catalog reconciliation failed")
		}
	}
	return nil
}

func backingRecordCount(ctx context.Context, app core.App, table string) (int64, error) {
	if !isIdentifier(table) {
		return 0, fmt.Errorf("invalid table")
	}
	var row struct {
		Count int64 `db:"count"`
	}
	err := app.DB().NewQuery("SELECT COUNT(*) AS count FROM [[" + table + "]]").WithContext(ctx).One(&row)
	return row.Count, err
}

// activeDocumentIDChecker uses the same persisted namespace and durable ID
// root as request-time validation. Activation must not accept a stored v.id
// merely because its JSON envelope looks plausible: a forged or foreign
// capability would otherwise become valid only after a schema migration.
func activeDocumentIDChecker(ctx context.Context, app core.App, namespace string) (schema.IDChecker, error) {
	state := &core.Record{}
	err := app.RecordQuery(schema.CollectionSchemaState).
		WithContext(ctx).
		AndWhere(dbx.HashExp{schema.CollectionSchemaState + "." + schema.FieldKey: schema.StateKeyActive}).
		Limit(1).
		One(state)
	if err != nil || state.Id == "" {
		return nil, fmt.Errorf("schema state unavailable")
	}
	decode := func(value string, required bool) ([]byte, error) {
		if value == "" && !required {
			return nil, nil
		}
		key, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil || len(key) < 32 {
			return nil, fmt.Errorf("schema state unavailable")
		}
		return key, nil
	}
	identity, err := decode(state.GetString(schema.FieldIDSecret), true)
	if err != nil {
		return nil, err
	}
	legacy, err := decode(state.GetString(schema.FieldLegacyIDSecret), true)
	if err != nil {
		return nil, err
	}
	current, err := decode(state.GetString(schema.FieldCursorSecret), true)
	if err != nil {
		return nil, err
	}
	previous, err := decode(state.GetString(schema.FieldCursorPreviousSecret), false)
	if err != nil || state.GetInt(schema.FieldCursorKeyID) < 1 {
		return nil, fmt.Errorf("schema state unavailable")
	}
	keyID := state.GetInt(schema.FieldCursorKeyID)
	return func(value, target string) bool {
		table, _, ok := schema.VerifyOpaqueID(value, namespace, identity, legacy, keyID, current, previous)
		if !ok && namespace == RootNamespace {
			table, _, ok = schema.VerifyOpaqueID(value, state.Id, identity, legacy, keyID, current, previous)
		}
		return ok && table == target
	}, nil
}

func collectionIndexName(expr string) string {
	return dbutils.ParseIndex(expr).IndexName
}

func schemaIndexes(table map[string]any) []map[string]any {
	var out []map[string]any
	indexes, _ := table["indexes"].([]any)
	for _, raw := range indexes {
		if index, ok := raw.(map[string]any); ok {
			out = append(out, index)
		}
	}
	return out
}

func physicalIndex(table string, index map[string]any, validators map[string]any) (string, string, bool) {
	return physicalIndexWithTieBreaker(table, index, validators, true)
}

// legacyPhysicalIndex recognizes only the exact owned definition emitted by
// the previous database phase. It exists solely so activation can repair that
// ABI in-place; it is not a permissive drift matcher.
func legacyPhysicalIndex(table string, index map[string]any, validators map[string]any) (string, string, bool) {
	return physicalIndexWithTieBreaker(table, index, validators, false)
}

func physicalIndexWithTieBreaker(table string, index map[string]any, validators map[string]any, includeCreationTime bool) (string, string, bool) {
	name, ok := index["name"].(string)
	if !ok || !isIdentifier(name) || !isIdentifier(table) {
		return "", "", false
	}
	fields, ok := index["fields"].([]any)
	if !ok || len(fields) == 0 {
		return "", "", false
	}
	columns := make([]string, 0, len(fields)+2)
	for _, raw := range fields {
		field, ok := raw.(string)
		validator, declared := schema.FieldValidator(validators, field)
		if !ok || !declared || !schema.IndexableValidator(validator) {
			return "", "", false
		}
		columns = append(columns, "json_extract("+schema.DocumentOrderField+", "+schema.SQLiteJSONPathLiteral(field)+")")
	}
	// Convex index order is declared fields, then _creationTime, with the raw
	// record id only as the final deterministic tie-breaker. The runtime and
	// cursor use this exact tuple.
	if includeCreationTime {
		columns = append(columns, "created")
	}
	columns = append(columns, "id")
	return "idx_pbvex_" + table + "_" + name, strings.Join(columns, ", "), true
}

// samePhysicalIndex compares the parsed index definition rather than looking
// for a column fragment.  In particular it catches uniqueness, a reordered
// expression list, a missing id tie-breaker and a predicate added by an
// operator, all of which would otherwise change query semantics.
func samePhysicalIndex(existing, table, name, columns string) bool {
	wantCollection := core.NewBaseCollection(table)
	wantCollection.AddIndex(name, false, columns, "")
	want := dbutils.ParseIndex(wantCollection.GetIndex(name))
	got := dbutils.ParseIndex(existing)
	if !got.IsValid() || got.Unique != want.Unique || got.Optional != want.Optional ||
		!strings.EqualFold(got.IndexName, want.IndexName) || !strings.EqualFold(got.TableName, want.TableName) ||
		canonicalIndexSQL(got.Where) != canonicalIndexSQL(want.Where) || len(got.Columns) != len(want.Columns) {
		return false
	}
	for i := range want.Columns {
		if canonicalIndexSQL(got.Columns[i].Name) != canonicalIndexSQL(want.Columns[i].Name) ||
			!strings.EqualFold(got.Columns[i].Collate, want.Columns[i].Collate) ||
			!strings.EqualFold(got.Columns[i].Sort, want.Columns[i].Sort) {
			return false
		}
	}
	return true
}

// canonicalIndexSQL removes insignificant whitespace outside quoted SQL text
// while preserving string literals (including JSON-path escaping) exactly.
func canonicalIndexSQL(raw string) string {
	var out strings.Builder
	quote := byte(0)
	space := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quote != 0 {
			out.WriteByte(c)
			if c == quote {
				if i+1 < len(raw) && raw[i+1] == quote { // SQL doubled quote
					out.WriteByte(raw[i+1])
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			if space && out.Len() > 0 {
				out.WriteByte(' ')
			}
			space = false
			quote = c
			out.WriteByte(c)
		default:
			if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
				space = out.Len() > 0
				continue
			}
			if space {
				out.WriteByte(' ')
				space = false
			}
			out.WriteByte(c)
		}
	}
	return out.String()
}

// materializeDocuments performs the activation migration under the same
// PocketBase transaction as deployment activation. Defaults are persisted,
// removed/unknown fields are rejected, and every row gets an order projection
// derived from the normalized document rather than stale JSON.
func materializeDocuments(ctx context.Context, app core.App, table string, fields map[string]any, idCheck schema.IDChecker) error {
	if !isIdentifier(table) {
		return fmt.Errorf("schema materialization failed")
	}
	if migrated, _ := ctx.Value(migratedTablesContextKey{}).(map[string]bool); migrated[table] {
		return nil
	}
	lastID := ""
	budget, _ := ctx.Value(migrationBudgetContextKey{}).(*migrationBudget)
	if budget == nil {
		budget = &migrationBudget{}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var rows []*core.Record
		query := app.RecordQuery(table).WithContext(ctx).OrderBy("id ASC").Limit(schemaMigrationBatch)
		if lastID != "" {
			query.AndWhere(dbx.NewExp("[["+table+".id]] > {:last}", dbx.Params{"last": lastID}))
		}
		if err := query.All(&rows); err != nil {
			return fmt.Errorf("schema materialization failed")
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			budget.rows++
			if budget.rows > maxSchemaMigrationRows {
				return fmt.Errorf("schema migration exceeds limit")
			}
			data := map[string]any{}
			switch raw := row.Get("_pbvex_data").(type) {
			case map[string]any:
				encoded, err := json.Marshal(raw)
				if err != nil || json.Unmarshal(encoded, &data) != nil {
					return fmt.Errorf("schema drift")
				}
			case string:
				if json.Unmarshal([]byte(raw), &data) != nil {
					return fmt.Errorf("schema drift")
				}
			case types.JSONRaw:
				if json.Unmarshal(raw, &data) != nil {
					return fmt.Errorf("schema drift")
				}
			default:
				return fmt.Errorf("schema drift")
			}
			before, err := CanonicalJSON(data)
			if err != nil {
				return fmt.Errorf("schema materialization failed")
			}
			// Charge the source before normalization, then charge both products
			// before assigning either to the record. Defaults can expand a tiny
			// legacy document substantially and the order projection is retained
			// alongside it, so counting only the old JSON was not a memory/work
			// budget.
			if err := chargeMigrationBytes(&budget.bytes, before); err != nil {
				return err
			}
			normalized, err := schema.NormalizeDocument(fields, data, false, true, idCheck)
			if err != nil {
				return fmt.Errorf("schema document invalid")
			}
			projection, err := schema.OrderDataWithID(fields, normalized, idCheck)
			if err != nil {
				return fmt.Errorf("schema index unsupported")
			}
			after, err := CanonicalJSON(normalized)
			if err != nil {
				return fmt.Errorf("schema materialization failed")
			}
			projectionJSON, err := CanonicalJSON(projection)
			if err != nil {
				return fmt.Errorf("schema materialization failed")
			}
			if err := chargeMigrationBytes(&budget.bytes, after); err != nil {
				return err
			}
			if err := chargeMigrationBytes(&budget.bytes, projectionJSON); err != nil {
				return err
			}
			if before != after {
				row.Set("_pbvex_data", normalized)
			}
			row.Set(schema.DocumentOrderField, projection)
			if err := app.SaveWithContext(ctx, row); err != nil {
				return fmt.Errorf("schema materialization failed")
			}
			lastID = row.Id
		}
		if len(rows) < schemaMigrationBatch {
			return nil
		}
	}
}

func chargeMigrationBytes(used *int64, encoded string) error {
	if used == nil || *used < 0 || *used > maxSchemaMigrationBytes || int64(len(encoded)) > maxSchemaMigrationBytes-*used {
		return fmt.Errorf("schema migration exceeds limit")
	}
	*used += int64(len(encoded))
	return nil
}

// Rollback restores the previous active deployment.
func (s *Service) Rollback(id string) (*DeploymentRollbackResponse, error) {
	return s.RollbackContext(context.Background(), id)
}

func (s *Service) RollbackContext(ctx context.Context, id string) (*DeploymentRollbackResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := s.repo.GetDeployment(ctx, s.app, id)
	if err != nil {
		return nil, err
	}
	if !record.GetBool(schema.FieldActive) {
		return nil, fmt.Errorf("%w: only the active deployment can be rolled back", ErrActivationFailed)
	}

	state, err := s.repo.GetState(s.internalCtxFrom(ctx), s.app)
	if err != nil {
		return nil, err
	}
	restoredID := state.GetString(schema.FieldPreviousID)
	if restoredID == "" {
		return nil, ErrActiveNotFound
	}

	currentManifest, currentBundle, err := s.recordManifestAndBundle(record)
	if err != nil {
		return nil, err
	}
	restoredRecord, err := s.repo.GetDeployment(ctx, s.app, restoredID)
	if err != nil {
		return nil, err
	}
	restoredManifest, err := s.recordManifest(restoredRecord)
	if err != nil {
		return nil, err
	}
	if err := verifyRuntime(s.invoker, ctx, id, currentBundle, currentManifest.Functions, currentManifest.Migrations); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrActivationFailed, err)
	}
	if err := compileRuntime(s.invoker, id, currentBundle, currentManifest.Functions, currentManifest.Migrations, NormalizeConfig(currentManifest.Config)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrActivationFailed, err)
	}
	now := types.NowDateTime()
	if err := s.app.RunInTransaction(func(txApp core.App) error {
		internalCtx := s.internalCtxFrom(ctx)
		state, err := s.repo.GetState(internalCtx, txApp)
		if err != nil {
			return err
		}
		if state.GetString(schema.FieldActiveID) != id || state.GetString(schema.FieldPreviousID) != restoredID {
			return fmt.Errorf("%w: active deployment changed", ErrActivationFailed)
		}
		plans, err := planMigrations(restoredManifest, currentManifest)
		if err != nil {
			return fmt.Errorf("%w: migration planning failed: %v", ErrActivationFailed, err)
		}
		if err := preflightMigrationHistory(internalCtx, txApp, currentManifest.Migrations); err != nil {
			return fmt.Errorf("%w: %v", ErrActivationFailed, err)
		}
		plans = reverseMigrationPlans(plans)
		if err := authenticateComponentMountArgs(internalCtx, txApp, restoredManifest); err != nil {
			return err
		}
		if err := preflightMaterializeSchema(internalCtx, txApp, restoredManifest); err != nil {
			return err
		}
		work, skipMaterialization, err := schemaMigrationWork(currentManifest, restoredManifest, plans)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrActivationFailed, err)
		}
		if _, _, err := preflightMigrationPlans(internalCtx, txApp, work); err != nil {
			return err
		}
		budget := &migrationBudget{}
		if err := s.applyMigrationPlans(internalCtx, txApp, id, "down", restoredManifest, plans, now, budget); err != nil {
			return fmt.Errorf("%w: %v", ErrActivationFailed, err)
		}
		if err := materializeSchema(withMigrationMaterialization(internalCtx, budget, skipMaterialization), txApp, restoredManifest); err != nil {
			return err
		}
		if err := s.repo.SetDeploymentActive(internalCtx, txApp, id, false); err != nil {
			return err
		}
		if err := s.repo.SetDeploymentActive(internalCtx, txApp, restoredID, true); err != nil {
			return err
		}
		if err := s.repo.SetDeploymentActivatedAt(internalCtx, txApp, restoredID, now); err != nil {
			return err
		}
		state.Set(schema.FieldActiveID, restoredID)
		state.Set(schema.FieldPreviousID, id)
		return s.repo.SaveState(internalCtx, txApp, state)
	}); err != nil {
		return nil, err
	}

	resp := &DeploymentRollbackResponse{
		DeploymentID: id,
		RolledBackAt: responseTimestamp(now),
	}
	resp.RestoredDeploymentID = &restoredID
	if s.invalidator != nil {
		s.invalidator.ReconnectAll()
	}
	if s.activationObserver != nil {
		s.activationObserver.ActiveDeploymentChanged(restoredID, restoredManifest)
	}
	return resp, nil
}

// Invoke loads a deployment bundle and calls a registered function.
func (s *Service) Invoke(ctx context.Context, deploymentID, functionName string, args any, authArgs ...any) (any, error) {
	ctx = withOptionalInvocationMetadata(ctx, authArgs)
	if s.invoker == nil {
		return nil, fmt.Errorf("runtime invoker is not configured")
	}
	record, err := s.repo.GetDeployment(ctx, s.app, deploymentID)
	if err != nil {
		return nil, err
	}
	bundleJS := record.GetString(schema.FieldBundle)
	manifest, err := s.recordManifest(record)
	if err != nil {
		return nil, err
	}
	deploymentID = record.GetString(schema.FieldDeploymentID)
	if err := compileRuntime(s.invoker, deploymentID, bundleJS, manifest.Functions, manifest.Migrations, NormalizeConfig(manifest.Config)); err != nil {
		return nil, err
	}
	return s.invokeWithLimits(ctx, deploymentID, functionName, args, NormalizeConfig(manifest.Config), manifest)
}

// CallSnapshot captures the resolved active deployment and function for a call.
// It is safe to use without invoking user code.
type CallSnapshot struct {
	DeploymentID string
	BundleJS     string
	Functions    []FunctionDescriptor
	Descriptor   *FunctionDescriptor
	Config       DeploymentConfig
	Manifest     DeploymentManifest
}

// ResolvePublic returns a snapshot for a public function on the active deployment.
func (s *Service) ResolvePublic(ctx context.Context, functionName string) (*CallSnapshot, error) {
	return s.resolve(ctx, functionName, "")
}

// ResolvePublicQuery returns a snapshot for a public query function on the active deployment.
func (s *Service) ResolvePublicQuery(ctx context.Context, functionName string) (*CallSnapshot, error) {
	return s.resolve(ctx, functionName, FunctionTypeQuery)
}

func (s *Service) resolve(ctx context.Context, functionName string, requiredType FunctionType) (*CallSnapshot, error) {
	activeID, err := s.activeID(ctx)
	if err != nil {
		return nil, err
	}
	record, err := s.repo.GetDeployment(ctx, s.app, activeID)
	if err != nil {
		return nil, err
	}
	manifest, err := s.recordManifest(record)
	if err != nil {
		return nil, err
	}
	var found *FunctionDescriptor
	for i := range manifest.Functions {
		if manifest.Functions[i].Name == functionName {
			found = &manifest.Functions[i]
			break
		}
	}
	// A public RPC cannot distinguish an absent function from an internal or
	// HTTP-only export. HTTP actions are deliberately routed by Request/Response
	// handling, never by the generic call endpoint.
	if found == nil || found.Visibility != FunctionVisibilityPublic || found.Type == FunctionTypeHTTPAction {
		return nil, ErrDeploymentNotFound
	}
	if requiredType != "" && found.Type != requiredType {
		return nil, ErrDeploymentNotFound
	}
	return &CallSnapshot{
		DeploymentID: record.GetString(schema.FieldDeploymentID),
		BundleJS:     record.GetString(schema.FieldBundle),
		Functions:    manifest.Functions,
		Descriptor:   found,
		Config:       NormalizeConfig(manifest.Config),
		Manifest:     manifest,
	}, nil
}

// Call invokes a public function on the currently active deployment.
func (s *Service) Call(ctx context.Context, functionName string, args any, authArgs ...any) (any, error) {
	ctx = withOptionalInvocationMetadata(ctx, authArgs)
	snap, err := s.ResolvePublic(ctx, functionName)
	if err != nil {
		return nil, err
	}
	if err := compileRuntime(s.invoker, snap.DeploymentID, snap.BundleJS, snap.Functions, snap.Manifest.Migrations, snap.Config); err != nil {
		return nil, err
	}
	return s.invokeWithLimits(ctx, snap.DeploymentID, functionName, args, snap.Config, snap.Manifest)
}

// CallQuery invokes a public query function on the currently active deployment.
func (s *Service) CallQuery(ctx context.Context, functionName string, args any) (any, error) {
	snap, err := s.ResolvePublicQuery(ctx, functionName)
	if err != nil {
		return nil, err
	}
	if err := compileRuntime(s.invoker, snap.DeploymentID, snap.BundleJS, snap.Functions, snap.Manifest.Migrations, snap.Config); err != nil {
		return nil, err
	}
	return s.invokeWithLimits(ctx, snap.DeploymentID, functionName, args, snap.Config, snap.Manifest)
}

// InvokeSnapshot invokes a function against a pre-resolved CallSnapshot
// without re-resolving the active deployment. This guarantees the invocation
// runs against the exact deployment that was active at admission time, even
// if a new deployment is activated mid-connection.
func (s *Service) InvokeSnapshot(ctx context.Context, snap *CallSnapshot, args any) (any, error) {
	if err := compileRuntime(s.invoker, snap.DeploymentID, snap.BundleJS, snap.Functions, snap.Manifest.Migrations, snap.Config); err != nil {
		return nil, err
	}
	return s.invokeWithLimits(ctx, snap.DeploymentID, snap.Descriptor.Name, args, snap.Config, snap.Manifest)
}

// HTTPAction resolves and invokes a public HTTP action on the active snapshot.
func (s *Service) HTTPAction(ctx context.Context, method, path string, envelope *HTTPRequestEnvelope, authArgs ...any) (*HTTPResponseEnvelope, error) {
	ctx = withOptionalInvocationMetadata(ctx, authArgs)
	if envelope == nil || ValidateHTTPHeaders(envelope.Headers) != nil {
		return nil, fmt.Errorf("%w: invalid HTTP request", ErrInvalidBundle)
	}
	activeID, err := s.activeID(ctx)
	if err != nil {
		return nil, err
	}
	record, err := s.repo.GetDeployment(ctx, s.app, activeID)
	if err != nil {
		return nil, err
	}
	manifest, err := s.recordManifest(record)
	if err != nil {
		return nil, err
	}
	name, _, err := matchHTTPRoute(method, path, manifest.Functions)
	if err != nil {
		return nil, err
	}
	cfg := NormalizeConfig(manifest.Config)
	if int64(len(envelope.Body)) > cfg.MaxFunctionArgsBytes {
		return nil, &ValueSizeError{Label: "httpAction request body", Limit: cfg.MaxFunctionArgsBytes}
	}
	deploymentID := record.GetString(schema.FieldDeploymentID)
	if err := compileRuntime(s.invoker, deploymentID, record.GetString(schema.FieldBundle), manifest.Functions, manifest.Migrations, cfg); err != nil {
		return nil, err
	}
	metadata := auth.InvocationMetadataFromContext(ctx)
	if invoker, ok := s.invoker.(interface {
		InvokeHTTPWithDatabase(context.Context, string, string, *HTTPRequestEnvelope, *auth.UserIdentity, string, core.App, DeploymentManifest) (*HTTPResponseEnvelope, error)
	}); ok {
		callCtx, cancel := contextWithTimeout(ctx, time.Duration(cfg.DefaultRequestTimeoutMs)*time.Millisecond)
		defer cancel()
		return invoker.InvokeHTTPWithDatabase(callCtx, deploymentID, name, envelope, metadata.Identity, metadata.RequestID, s.app, manifest)
	}
	invoker, ok := s.invoker.(interface {
		InvokeHTTP(context.Context, string, string, *HTTPRequestEnvelope, *auth.UserIdentity, string) (*HTTPResponseEnvelope, error)
	})
	if !ok {
		return nil, fmt.Errorf("HTTP action runtime is not configured")
	}
	callCtx, cancel := contextWithTimeout(ctx, time.Duration(cfg.DefaultRequestTimeoutMs)*time.Millisecond)
	defer cancel()
	return invoker.InvokeHTTP(callCtx, deploymentID, name, envelope, metadata.Identity, metadata.RequestID)
}

func (s *Service) MatchHTTPRouteContext(ctx context.Context, method, path string) (string, string, bool) {
	snapID, err := s.activeID(ctx)
	if err != nil {
		return "", "", false
	}
	record, err := s.repo.GetDeployment(ctx, s.app, snapID)
	if err != nil {
		return "", "", false
	}
	manifest, err := s.recordManifest(record)
	if err != nil {
		return "", "", false
	}
	name, matched, err := matchHTTPRoute(method, path, manifest.Functions)
	return name, matched, err == nil
}

func (s *Service) MatchHTTPRoute(method, path string) (string, string, bool) {
	return s.MatchHTTPRouteContext(context.Background(), method, path)
}

func withOptionalInvocationMetadata(ctx context.Context, extra []any) context.Context {
	if len(extra) == 0 {
		return ctx
	}
	var identity *auth.UserIdentity
	var requestID string
	if len(extra) > 0 {
		identity, _ = extra[0].(*auth.UserIdentity)
	}
	if len(extra) > 1 {
		requestID, _ = extra[1].(string)
	}
	return auth.WithInvocationMetadata(ctx, identity, requestID)
}

func matchHTTPRoute(method, path string, functions []FunctionDescriptor) (string, string, error) {
	path = strings.TrimPrefix(path, "/")
	var exact *FunctionDescriptor
	var prefix *FunctionDescriptor
	for i := range functions {
		f := &functions[i]
		if f.Type != FunctionTypeHTTPAction || f.Visibility != FunctionVisibilityPublic || f.Route == nil || !strings.EqualFold(f.Route.Method, method) {
			continue
		}
		if f.Route.Path != "" && strings.TrimPrefix(f.Route.Path, "/") == path {
			if exact != nil {
				return "", "", fmt.Errorf("ambiguous HTTP route")
			}
			exact = f
		}
		if f.Route.PathPrefix != "" && strings.HasPrefix(path, strings.TrimPrefix(f.Route.PathPrefix, "/")) && (prefix == nil || len(f.Route.PathPrefix) > len(prefix.Route.PathPrefix)) {
			prefix = f
		}
	}
	if exact != nil {
		return exact.Name, path, nil
	}
	if prefix != nil {
		return prefix.Name, path, nil
	}
	return "", "", ErrDeploymentNotFound
}

func (s *Service) MaxFunctionArgsBytes() int64 {
	active, err := s.Active()
	if err != nil || active == nil {
		return DefaultDeploymentConfig.MaxFunctionArgsBytes
	}
	return NormalizeConfig(active.Manifest.Config).MaxFunctionArgsBytes
}

func (s *Service) MaxUploadBytes() int64 {
	active, err := s.Active()
	if err != nil || active == nil {
		return DefaultDeploymentConfig.MaxUploadBytes
	}
	return NormalizeConfig(active.Manifest.Config).MaxUploadBytes
}

func (s *Service) ActiveUploadEnvelopeBytes() int64 {
	dynamic := UploadEnvelopeBytes(s.MaxUploadBytes())
	if dynamic > MaxUploadEnvelopeBytes {
		return MaxUploadEnvelopeBytes
	}
	return dynamic
}

// Resolve returns the immutable bundle metadata for a deployment. Scheduler
// jobs persist this snapshot and pin the deployment until the job is terminal.
func (s *Service) Resolve(ctx context.Context, deploymentID string) (DeploymentManifest, string, string, error) {
	app := s.app
	if txApp, ok := schema.AppFromContext(ctx); ok && txApp != nil {
		app = txApp
	}
	record, err := s.repo.GetDeployment(ctx, app, deploymentID)
	if err != nil {
		return DeploymentManifest{}, "", "", err
	}
	manifest, err := s.recordManifest(record)
	if err != nil {
		return DeploymentManifest{}, "", "", err
	}
	return manifest, record.GetString(schema.FieldBundleHash), record.GetString(schema.FieldBundle), nil
}

// InvokeDeploymentSnapshot invokes a scheduled function against the exact
// deployment and bundle hash captured when the job was created.
func (s *Service) InvokeDeploymentSnapshot(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error) {
	app := s.app
	if txApp, ok := schema.AppFromContext(ctx); ok && txApp != nil {
		app = txApp
	}
	record, err := s.repo.GetDeployment(ctx, app, deploymentID)
	if err != nil {
		return nil, err
	}
	if bundleHash != "" && record.GetString(schema.FieldBundleHash) != bundleHash {
		return nil, fmt.Errorf("%w: deployment snapshot mismatch", ErrInvalidBundle)
	}
	manifest, err := s.recordManifest(record)
	if err != nil {
		return nil, err
	}
	realDeploymentID := record.GetString(schema.FieldDeploymentID)
	if err := compileRuntime(s.invoker, realDeploymentID, record.GetString(schema.FieldBundle), manifest.Functions, manifest.Migrations, NormalizeConfig(manifest.Config)); err != nil {
		return nil, err
	}
	return s.invokeWithLimits(ctx, realDeploymentID, functionName, args, NormalizeConfig(manifest.Config), manifest)
}

// Pin atomically adjusts the deployment's durable scheduler reference count.
func (s *Service) Pin(ctx context.Context, deploymentID string, delta int) error {
	if delta == 0 {
		return nil
	}
	app := s.app
	if txApp, ok := schema.AppFromContext(ctx); ok && txApp != nil {
		app = txApp
	}
	record, err := s.repo.GetDeployment(ctx, app, deploymentID)
	if err != nil {
		return err
	}

	var value, where dbx.Expression
	if delta > 0 {
		value = dbx.NewExp("COALESCE(pinCount, 0) + {:delta}", dbx.Params{"delta": delta})
		where = dbx.NewExp("id = {:id}", dbx.Params{"id": record.Id})
	} else {
		value = dbx.NewExp("pinCount - 1")
		where = dbx.NewExp("id = {:id} AND pinCount > 0", dbx.Params{"id": record.Id})
	}
	res, err := app.DB().
		Update(schema.CollectionDeployments, dbx.Params{schema.FieldPinCount: value}, where).
		WithContext(ctx).
		Execute()
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		if delta < 0 {
			return ErrPinUnderflow
		}
		return ErrDeploymentNotFound
	}
	return nil
}

func (s *Service) invokeWithLimits(ctx context.Context, deploymentID, functionName string, args any, cfg DeploymentConfig, manifest DeploymentManifest) (any, error) {
	if err := checkWireSize(args, cfg.MaxFunctionArgsBytes, "function arguments"); err != nil {
		return nil, err
	}
	callCtx, cancel := contextWithTimeout(ctx, time.Duration(cfg.DefaultRequestTimeoutMs)*time.Millisecond)
	defer cancel()
	// The manifest and app are captured before the runtime begins. This pins the
	// schema/deployment view for the whole invocation, including transactions.
	if result, ok, err := invokeDatabaseRuntime(s.invoker, callCtx, deploymentID, functionName, args, s.app, manifest); ok {
		if err != nil {
			return nil, err
		}
		if err := checkWireSize(result, cfg.MaxReturnValueBytes, "function return value"); err != nil {
			return nil, err
		}
		return result, nil
	}
	result, err := invokeRuntime(s.invoker, callCtx, deploymentID, functionName, args)
	if err != nil {
		return nil, err
	}
	if err := checkWireSize(result, cfg.MaxReturnValueBytes, "function return value"); err != nil {
		return nil, err
	}
	return result, nil
}

func checkWireSize(value any, limit int64, label string) error {
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", label, err)
	}
	if int64(len(canonical)) > limit {
		return &ValueSizeError{Label: label, Limit: limit}
	}
	return nil
}

func contextWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithTimeout(ctx, 0)
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// WarmActive loads and verifies the currently active deployment runtime.
func (s *Service) WarmActive() error {
	activeID, err := s.activeID(context.Background())
	if err != nil {
		return nil
	}
	record, err := s.repo.GetDeployment(context.Background(), s.app, activeID)
	if err != nil {
		return err
	}
	manifest, bundleJS, err := s.recordManifestAndBundle(record)
	if err != nil {
		return err
	}
	deploymentID := record.GetString(schema.FieldDeploymentID)
	if err := compileRuntime(s.invoker, deploymentID, bundleJS, manifest.Functions, manifest.Migrations, NormalizeConfig(manifest.Config)); err != nil {
		return fmt.Errorf("%w: %v", ErrActivationFailed, err)
	}
	if err := verifyRuntime(s.invoker, context.Background(), deploymentID, bundleJS, manifest.Functions, manifest.Migrations); err != nil {
		return fmt.Errorf("%w: %v", ErrActivationFailed, err)
	}
	if s.activationObserver != nil {
		s.activationObserver.ActiveDeploymentChanged(deploymentID, manifest)
	}
	return nil
}

func (s *Service) activeID(ctx context.Context) (string, error) {
	state, err := s.repo.GetState(ctx, s.app)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrActiveNotFound
		}
		return "", err
	}
	id := state.GetString(schema.FieldActiveID)
	if id == "" {
		return "", ErrActiveNotFound
	}
	return id, nil
}

func (s *Service) trimHistory() error {
	return s.trimHistoryContext(context.Background())
}

func (s *Service) trimHistoryContext(ctx context.Context) error {
	keep := s.config.HistoryLimit
	if keep <= 0 {
		return nil
	}
	var deleted []string
	err := s.app.RunInTransaction(func(txApp core.App) error {
		var err error
		deleted, err = s.repo.DeleteOldestInactive(s.internalCtxFrom(ctx), txApp, keep)
		return err
	})
	if err != nil {
		return err
	}
	if dropper, ok := s.invoker.(interface{ Drop(string) }); ok {
		for _, id := range deleted {
			dropper.Drop(id)
		}
	}
	return nil
}

func (s *Service) internalCtx() context.Context {
	return s.internalCtxFrom(context.Background())
}

func (s *Service) internalCtxFrom(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, schema.InternalContextKey, true)
}

func (s *Service) recordToDeployment(record *core.Record) (Deployment, error) {
	manifest, err := s.recordManifest(record)
	if err != nil {
		return Deployment{}, err
	}
	bundleJS := record.GetString(schema.FieldBundle)
	bundleHash := record.GetString(schema.FieldBundleHash)
	bundleSize := record.GetInt(schema.FieldBundleSize)
	active := record.GetBool(schema.FieldActive)
	createdAt := record.GetDateTime("created").Time().Format(time.RFC3339Nano)
	var activatedAt *string
	if v := record.GetDateTime(schema.FieldActivatedAt); !v.IsZero() {
		s := v.Time().Format(time.RFC3339Nano)
		activatedAt = &s
	}
	deploymentID := record.GetString(schema.FieldDeploymentID)
	return Deployment{
		DeploymentID: deploymentID,
		Manifest:     manifest,
		Bundle: DeploymentBundle{
			JS:     base64.StdEncoding.EncodeToString([]byte(bundleJS)),
			Sha256: bundleHash,
			Size:   int64(bundleSize),
		},
		CreatedAt:   createdAt,
		ActivatedAt: activatedAt,
		Active:      active,
	}, nil
}

func (s *Service) recordManifest(record *core.Record) (DeploymentManifest, error) {
	raw := record.GetString(schema.FieldManifest)
	var manifest DeploymentManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return DeploymentManifest{}, fmt.Errorf("failed to parse manifest: %w", err)
	}
	return manifest, nil
}

func (s *Service) recordManifestAndBundle(record *core.Record) (DeploymentManifest, string, error) {
	manifest, err := s.recordManifest(record)
	if err != nil {
		return DeploymentManifest{}, "", err
	}
	return manifest, record.GetString(schema.FieldBundle), nil
}
