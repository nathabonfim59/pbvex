package deploy

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/tools/cron"
)

const (
	SupportedProtocolVersion = "v1"

	MaxIdentifierLength = 1024
	MaxPathLength       = 4096
	MaxFieldLength      = 1024
	MaxValueDepth       = 128
	SHA256HexLength     = 64

	MaxEventEnvelopeOverhead = int64(4096)

	// MaxFunctionArgsLimit is the canonical protocol ceiling for a deployment's
	// maxFunctionArgsBytes config. Manifest validation rejects values above
	// this bound so realtime body admission can use limit+overhead as a single
	// static ceiling.
	MaxFunctionArgsLimit = int64(16 * 1024 * 1024)
	// MaxReturnValueLimit is the canonical protocol ceiling for maxReturnValueBytes.
	MaxReturnValueLimit = int64(16 * 1024 * 1024)

	maxSchemaTables     = 64
	maxSchemaFields     = 256
	maxSchemaIndexes    = 64
	maxSchemaIndexWidth = 16
	maxCronJobs         = 64
	maxCronNameLength   = 64
	maxCronExprLength   = 128
	maxMigrations       = 256
	maxSchemaNodes      = 16 * 1024
	// MaxDeploymentUploadBytes is the accepted v1 deployment upload contract
	// from ADR 001: a 64 MiB decoded bundle. PocketBase's global body limit is
	// only 32 MiB, so the deploy route binds an explicit apis.BodyLimit below
	// that accepts a maximally base64-encoded bundle plus the bounded manifest
	// envelope. ValidateUploadRequest still enforces the decoded cap.
	MaxDeploymentUploadBytes int64 = 64 << 20
	// maxManifestEnvelopeBytes bounds the manifest + request envelope that
	// surrounds the base64 bundle. A valid manifest is far smaller (its schema
	// is bounded to maxSchemaNodes), so this is a generous admission ceiling.
	maxManifestEnvelopeBytes int64 = 4 << 20
	// MaxUploadEnvelopeBytes is the route-level body limit for the deploy
	// endpoint. It overrides PocketBase's 32 MiB global default to accept the
	// full v1 contract: a maximally base64-encoded 64 MiB bundle (4/3 ratio)
	// plus the bounded manifest envelope. ((n+2)/3)*4 matches
	// base64.StdEncoding.EncodedLen for the standard padded alphabet.
	MaxUploadEnvelopeBytes int64 = ((MaxDeploymentUploadBytes+2)/3)*4 + maxManifestEnvelopeBytes
)

// UploadEnvelopeBytes returns the maximum wire-level body size that can carry
// a deployment upload whose decoded bundle is at most decodedLimit bytes.
// It accounts for base64 padding (((n+2)/3)*4, matching StdEncoding.EncodedLen)
// and the bounded manifest envelope overhead. When decodedLimit equals
// MaxDeploymentUploadBytes the result equals MaxUploadEnvelopeBytes.
func UploadEnvelopeBytes(decodedLimit int64) int64 {
	return ((decodedLimit+2)/3)*4 + maxManifestEnvelopeBytes
}

var (
	identifierRe  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
	cronNameRe    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	modulePathRe  = regexp.MustCompile(`^[a-zA-Z0-9._/\-]+$`)
	sha256HexRe   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	migrationIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	reservedKeys  = map[string]struct{}{
		"__proto__":   {},
		"constructor": {},
		"prototype":   {},
	}
)

// JSONValue is the Go equivalent of the protocol JSONValue union.
type JSONValue = any

// FunctionType enumerates the supported function types.
type FunctionType string

const (
	FunctionTypeQuery      FunctionType = "query"
	FunctionTypeMutation   FunctionType = "mutation"
	FunctionTypeAction     FunctionType = "action"
	FunctionTypeHTTPAction FunctionType = "httpAction"
)

// FunctionVisibility enumerates the supported function visibilities.
type FunctionVisibility string

const (
	FunctionVisibilityPublic   FunctionVisibility = "public"
	FunctionVisibilityInternal FunctionVisibility = "internal"
)

// FunctionRoute is the HTTP route metadata for an httpAction.
type FunctionRoute struct {
	Method     string `json:"method,omitempty"`
	Path       string `json:"path,omitempty"`
	PathPrefix string `json:"pathPrefix,omitempty"`
}

// FunctionDescriptor is the v1 protocol function descriptor.
type FunctionDescriptor struct {
	Name       string             `json:"name"`
	Type       FunctionType       `json:"type"`
	Visibility FunctionVisibility `json:"visibility"`
	ModulePath string             `json:"modulePath"`
	ExportName string             `json:"exportName"`
	Args       JSONValue          `json:"args,omitempty"`
	Returns    JSONValue          `json:"returns,omitempty"`
	Route      *FunctionRoute     `json:"route,omitempty"`
}

// DeploymentConfig is the runtime config embedded in a manifest.
type DeploymentConfig struct {
	HTTPPathPrefix          string `json:"httpPathPrefix"`
	RealtimePath            string `json:"realtimePath"`
	MaxUploadBytes          int64  `json:"maxUploadBytes"`
	MaxFunctionArgsBytes    int64  `json:"maxFunctionArgsBytes"`
	MaxReturnValueBytes     int64  `json:"maxReturnValueBytes"`
	DefaultRequestTimeoutMs int64  `json:"defaultRequestTimeoutMs"`
	present                 map[string]bool
}

// DefaultDeploymentConfig is the fallback configuration used when a manifest does not override it.
var DefaultDeploymentConfig = DeploymentConfig{
	HTTPPathPrefix:          "/api/pbvex",
	RealtimePath:            "/api/pbvex/realtime",
	MaxUploadBytes:          MaxDeploymentUploadBytes,
	MaxFunctionArgsBytes:    1024 * 1024,
	MaxReturnValueBytes:     1024 * 1024,
	DefaultRequestTimeoutMs: 30000,
}

// NormalizeConfig fills missing fields with default values.
func NormalizeConfig(cfg *DeploymentConfig) DeploymentConfig {
	if cfg == nil {
		return DefaultDeploymentConfig
	}
	out := DefaultDeploymentConfig
	if cfg.has("httpPathPrefix") || cfg.HTTPPathPrefix != "" {
		out.HTTPPathPrefix = cfg.HTTPPathPrefix
	}
	if cfg.has("realtimePath") || cfg.RealtimePath != "" {
		out.RealtimePath = cfg.RealtimePath
	}
	if cfg.has("maxUploadBytes") || cfg.MaxUploadBytes != 0 {
		out.MaxUploadBytes = cfg.MaxUploadBytes
	}
	if cfg.has("maxFunctionArgsBytes") || cfg.MaxFunctionArgsBytes != 0 {
		out.MaxFunctionArgsBytes = cfg.MaxFunctionArgsBytes
	}
	if cfg.has("maxReturnValueBytes") || cfg.MaxReturnValueBytes != 0 {
		out.MaxReturnValueBytes = cfg.MaxReturnValueBytes
	}
	if cfg.has("defaultRequestTimeoutMs") || cfg.DefaultRequestTimeoutMs != 0 {
		out.DefaultRequestTimeoutMs = cfg.DefaultRequestTimeoutMs
	}
	return out
}

func (c *DeploymentConfig) has(field string) bool {
	return c != nil && c.present != nil && c.present[field]
}

// DeploymentManifest is the v1 protocol deployment manifest.
type DeploymentManifest struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	DeploymentID    string                 `json:"deploymentId"`
	Functions       []FunctionDescriptor   `json:"functions,omitempty"`
	Components      *ComponentGraph        `json:"components,omitempty"`
	Config          *DeploymentConfig      `json:"config,omitempty"`
	Schema          JSONValue              `json:"schema,omitempty"`
	EmailTemplates  *EmailTemplateManifest `json:"emailTemplates,omitempty"`
	CronJobs        []CronJobDescriptor    `json:"cronJobs,omitempty"`
	Migrations      []MigrationDescriptor  `json:"migrations,omitempty"`
}

// MigrationDescriptor is a pure, reversible document transform registered by
// the deployment bundle.
type MigrationDescriptor struct {
	ID               string    `json:"id"`
	Table            string    `json:"table"`
	Mode             string    `json:"mode"`
	From             JSONValue `json:"from"`
	To               JSONValue `json:"to"`
	SourceSchemaHash string    `json:"sourceSchemaHash"`
	TargetSchemaHash string    `json:"targetSchemaHash"`
	Checksum         string    `json:"checksum"`
	ModulePath       string    `json:"modulePath"`
	ExportName       string    `json:"exportName"`
	Reversibility    string    `json:"reversibility"`
}

// CronJobDescriptor is a recurring PocketBase cron tick that enqueues a
// durable PBVex mutation or action.
type CronJobDescriptor struct {
	Name         string    `json:"name"`
	Schedule     string    `json:"schedule"`
	FunctionName string    `json:"functionName"`
	Args         JSONValue `json:"args"`
}

type EmailTemplate struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Text    string `json:"text,omitempty"`
	HTML    string `json:"html,omitempty"`
}
type EmailTemplateManifest struct {
	Sha256  string          `json:"sha256"`
	Entries []EmailTemplate `json:"entries"`
}

// ModuleSource authenticates the unbundled source assigned to a component
// mount. The executable bundle remains the runtime artifact; these bytes bind
// each component definition and function namespace to reviewed source.
type ModuleSource struct {
	Path  string `json:"path"`
	Bytes string `json:"bytes"`
}

// DeploymentBundle is the stored representation of a bundle.
type DeploymentBundle struct {
	JS     string `json:"js"`
	Sha256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Deployment is a stored deployment record.
type Deployment struct {
	DeploymentID string             `json:"deploymentId"`
	Manifest     DeploymentManifest `json:"manifest"`
	Bundle       DeploymentBundle   `json:"bundle"`
	CreatedAt    string             `json:"createdAt"`
	ActivatedAt  *string            `json:"activatedAt,omitempty"`
	Active       bool               `json:"active"`
}

// DeploymentUploadRequest is the body of the upload API.
type DeploymentUploadRequest struct {
	Manifest DeploymentManifest `json:"manifest"`
	Bundle   string             `json:"bundle"`
	Sha256   string             `json:"sha256"`
	Size     int64              `json:"size"`
	Modules  []ModuleSource     `json:"modules,omitempty"`
}

// DeploymentUploadResponse is the upload API response.
type DeploymentUploadResponse struct {
	DeploymentID string `json:"deploymentId"`
	BundleHash   string `json:"bundleHash"`
	AcceptedAt   string `json:"acceptedAt"`
}

// DeploymentListResponse is the list API response.
type DeploymentListResponse struct {
	Deployments []Deployment `json:"deployments"`
}

// DeploymentActivateRequest is the body of the activate API.
type DeploymentActivateRequest struct {
	Atomic bool `json:"atomic"`
}

// DeploymentActivateResponse is the activate API response.
type DeploymentActivateResponse struct {
	DeploymentID         string             `json:"deploymentId"`
	ActivatedAt          string             `json:"activatedAt"`
	PreviousDeploymentID *string            `json:"previousDeploymentId,omitempty"`
	Warnings             []MigrationWarning `json:"warnings,omitempty"`
}

type MigrationWarning struct {
	Code               string `json:"code"`
	Rows               int    `json:"rows"`
	RowLimit           int    `json:"rowLimit"`
	EstimatedBytes     int64  `json:"estimatedBytes"`
	ByteLimit          int64  `json:"byteLimit"`
	UtilizationPercent int    `json:"utilizationPercent"`
}

// DeploymentRollbackResponse is the rollback API response.
type DeploymentRollbackResponse struct {
	DeploymentID         string  `json:"deploymentId"`
	RolledBackAt         string  `json:"rolledBackAt"`
	RestoredDeploymentID *string `json:"restoredDeploymentId,omitempty"`
}

// ErrorCode enumerates the protocol error codes.
type ErrorCode string

const (
	ErrorCodeBadRequest         ErrorCode = "bad_request"
	ErrorCodeInvalidManifest    ErrorCode = "invalid_manifest"
	ErrorCodeInvalidFunction    ErrorCode = "invalid_function"
	ErrorCodeBundleNotFound     ErrorCode = "bundle_not_found"
	ErrorCodeBundleHashMismatch ErrorCode = "bundle_hash_mismatch"
	ErrorCodeActivationFailed   ErrorCode = "activation_failed"
	ErrorCodeNotFound           ErrorCode = "not_found"
	ErrorCodeUnauthorized       ErrorCode = "unauthorized"
	ErrorCodeForbidden          ErrorCode = "forbidden"
	ErrorCodeInternal           ErrorCode = "internal"
	ErrorCodeUploadExpired      ErrorCode = "upload_expired"
	ErrorCodeUploadConsumed     ErrorCode = "upload_consumed"
	ErrorCodeUploadPending      ErrorCode = "upload_pending"
	ErrorCodeUploadTooLarge     ErrorCode = "upload_too_large"
	ErrorCodeInvalidContent     ErrorCode = "invalid_content"
	ErrorCodeStorageFull        ErrorCode = "storage_full"
)

// StructuredError is the protocol error envelope.
type StructuredError struct {
	Error     bool      `json:"error"`
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Details   []any     `json:"details,omitempty"`
	RequestID string    `json:"requestId,omitempty"`
}

// ValidateManifest validates the manifest per protocol v1.
func ValidateManifest(value any) (DeploymentManifest, error) {
	if value == nil {
		return DeploymentManifest{}, fmt.Errorf("manifest must be a JSON object")
	}
	o, ok := value.(map[string]any)
	if !ok {
		return DeploymentManifest{}, fmt.Errorf("manifest must be a JSON object")
	}
	if o["protocolVersion"] != SupportedProtocolVersion {
		return DeploymentManifest{}, fmt.Errorf("protocolVersion must be %q", SupportedProtocolVersion)
	}
	deploymentID, ok := o["deploymentId"].(string)
	if !ok || !isIdentifier(deploymentID) {
		return DeploymentManifest{}, fmt.Errorf("invalid deploymentId")
	}
	functions, err := validateFunctions(o["functions"])
	if err != nil {
		return DeploymentManifest{}, err
	}
	components, err := ValidateComponents(o["components"])
	if err != nil {
		return DeploymentManifest{}, err
	}
	if err := validateComponentFunctionBinding(functions, components); err != nil {
		return DeploymentManifest{}, err
	}
	config, err := validateConfig(o["config"])
	if err != nil {
		return DeploymentManifest{}, err
	}
	emailTemplates, err := validateEmailTemplates(o["emailTemplates"])
	if err != nil {
		return DeploymentManifest{}, err
	}
	cronJobs, err := validateCronJobs(o["cronJobs"], functions, NormalizeConfig(config))
	if err != nil {
		return DeploymentManifest{}, err
	}
	var schema JSONValue
	if rawSchema, present := o["schema"]; present {
		var err error
		schema, err = validateSchema(rawSchema)
		if err != nil {
			return DeploymentManifest{}, fmt.Errorf("schema is invalid: %w", err)
		}
	}
	migrations, err := validateMigrations(o["migrations"], schema)
	if err != nil {
		return DeploymentManifest{}, err
	}
	// Function v.id descriptors must always be tied to a declared deployment
	// schema. A schema-less manifest has no table authority to validate a
	// capability target against, so accepting one would defer an invalid public
	// contract until invocation time.
	if err := validateFunctionIDTargetsByNamespace(functions, schema, components); err != nil {
		return DeploymentManifest{}, fmt.Errorf("schema is invalid: %w", err)
	}
	return DeploymentManifest{
		ProtocolVersion: SupportedProtocolVersion,
		DeploymentID:    deploymentID,
		Functions:       functions,
		Components:      components,
		Config:          config,
		Schema:          schema,
		EmailTemplates:  emailTemplates,
		CronJobs:        cronJobs,
		Migrations:      migrations,
	}, nil
}

func validateMigrations(value, targetSchema any) ([]MigrationDescriptor, error) {
	if value == nil {
		return nil, nil
	}
	entries, ok := value.([]any)
	if !ok || len(entries) > maxMigrations {
		return nil, fmt.Errorf("migrations must be an array with at most %d entries", maxMigrations)
	}
	tables := schemaTableNames(targetSchema)
	seen := make(map[string]bool, len(entries))
	out := make([]MigrationDescriptor, len(entries))
	for i, raw := range entries {
		o, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("migration[%d] must be an object", i)
		}
		if !onlyKeys(o, "id", "table", "mode", "from", "to", "sourceSchemaHash", "targetSchemaHash", "checksum", "modulePath", "exportName", "reversibility") || len(o) != 11 {
			return nil, fmt.Errorf("migration[%d] has unknown or missing fields", i)
		}
		m := MigrationDescriptor{}
		m.ID, _ = o["id"].(string)
		m.Table, _ = o["table"].(string)
		m.Mode, _ = o["mode"].(string)
		m.From, m.To = o["from"], o["to"]
		m.SourceSchemaHash, _ = o["sourceSchemaHash"].(string)
		m.TargetSchemaHash, _ = o["targetSchemaHash"].(string)
		m.Checksum, _ = o["checksum"].(string)
		m.ModulePath, _ = o["modulePath"].(string)
		m.ExportName, _ = o["exportName"].(string)
		m.Reversibility, _ = o["reversibility"].(string)
		if !migrationIDRe.MatchString(m.ID) {
			return nil, fmt.Errorf("migration[%d] id is invalid", i)
		}
		if seen[m.ID] {
			return nil, fmt.Errorf("migration[%d] id is duplicated", i)
		}
		if !isIdentifier(m.Table) || strings.HasPrefix(strings.ToLower(m.Table), "pbvex_cmp_") || !tables[m.Table] {
			return nil, fmt.Errorf("migration[%d] table is invalid", i)
		}
		if m.Mode != "transactional" || m.Reversibility != "reversible" {
			return nil, fmt.Errorf("migration[%d] mode or reversibility is invalid", i)
		}
		fromObject, fromOK := m.From.(map[string]any)
		toObject, toOK := m.To.(map[string]any)
		if !fromOK || fromObject["type"] != "object" || !validateValidatorDescriptor(m.From) ||
			!toOK || toObject["type"] != "object" || !validateValidatorDescriptor(m.To) {
			return nil, fmt.Errorf("migration[%d] from and to must be object validators", i)
		}
		fromHash, fromErr := CanonicalHash(m.From)
		toHash, toErr := CanonicalHash(m.To)
		if fromErr != nil || toErr != nil || !IsSha256Hex(m.SourceSchemaHash) || !IsSha256Hex(m.TargetSchemaHash) || fromHash != m.SourceSchemaHash || toHash != m.TargetSchemaHash {
			return nil, fmt.Errorf("migration[%d] schema hash is invalid", i)
		}
		if !IsSha256Hex(m.Checksum) || !isModulePath(m.ModulePath) || !strings.HasPrefix(m.ModulePath, "pbvex/migrations/") || !strings.HasSuffix(m.ModulePath, ".ts") || !isExportName(m.ExportName) {
			return nil, fmt.Errorf("migration[%d] source binding or checksum is invalid", i)
		}
		seen[m.ID] = true
		out[i] = m
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].ID >= out[i].ID {
			return nil, fmt.Errorf("migrations entries must be sorted by id")
		}
	}
	return out, nil
}

func schemaTableNames(raw any) map[string]bool {
	out := map[string]bool{}
	o, _ := raw.(map[string]any)
	for _, entry := range listJSON(o["tables"]) {
		if table, ok := entry.(map[string]any); ok {
			if name, ok := table["tableName"].(string); ok {
				out[name] = true
			}
		}
	}
	return out
}

func validateCronJobs(value any, functions []FunctionDescriptor, config DeploymentConfig) ([]CronJobDescriptor, error) {
	if value == nil {
		return nil, nil
	}
	entries, ok := value.([]any)
	if !ok || len(entries) > maxCronJobs {
		return nil, fmt.Errorf("cronJobs must be an array with at most %d entries", maxCronJobs)
	}
	functionsByName := make(map[string]FunctionDescriptor, len(functions))
	for _, fn := range functions {
		functionsByName[fn.Name] = fn
	}
	names := make(map[string]struct{}, len(entries))
	out := make([]CronJobDescriptor, len(entries))
	for index, raw := range entries {
		job, ok := raw.(map[string]any)
		if !ok || len(job) != 4 {
			return nil, fmt.Errorf("cron job[%d] must contain name, schedule, functionName and args", index)
		}
		name, nameOK := job["name"].(string)
		if !nameOK || len(name) == 0 || len(name) > maxCronNameLength || !cronNameRe.MatchString(name) {
			return nil, fmt.Errorf("cron job[%d] name is invalid", index)
		}
		if _, exists := names[name]; exists {
			return nil, fmt.Errorf("cron job[%d] name is duplicated", index)
		}
		schedule, scheduleOK := job["schedule"].(string)
		if !scheduleOK || len(schedule) == 0 || len(schedule) > maxCronExprLength {
			return nil, fmt.Errorf("cron job[%d] schedule is invalid", index)
		}
		if _, err := cron.NewSchedule(schedule); err != nil {
			return nil, fmt.Errorf("cron job[%d] schedule is invalid: %w", index, err)
		}
		functionName, functionOK := job["functionName"].(string)
		target, targetOK := functionsByName[functionName]
		if !functionOK || !targetOK || (target.Type != FunctionTypeMutation && target.Type != FunctionTypeAction) {
			return nil, fmt.Errorf("cron job[%d] target must be a deployed mutation or action", index)
		}
		args, err := validateOptionalJsonValue(job["args"])
		if err != nil {
			return nil, fmt.Errorf("cron job[%d] args must be a valid PBVex wire value", index)
		}
		encoded, err := CanonicalJSON(args)
		if err != nil || int64(len(encoded)) > config.MaxFunctionArgsBytes {
			return nil, fmt.Errorf("cron job[%d] args exceed maxFunctionArgsBytes", index)
		}
		if target.Args != nil {
			if _, err := schema.NormalizeValue(target.Args, args, nil); err != nil {
				return nil, fmt.Errorf("cron job[%d] args do not match the target validator", index)
			}
		}
		names[name] = struct{}{}
		out[index] = CronJobDescriptor{Name: name, Schedule: schedule, FunctionName: functionName, Args: args}
	}
	for index := 1; index < len(out); index++ {
		if out[index-1].Name >= out[index].Name {
			return nil, fmt.Errorf("cronJobs entries must be sorted by name")
		}
	}
	return out, nil
}

func validateFunctionIDTargetsByNamespace(functions []FunctionDescriptor, rootSchema any, graph *ComponentGraph) error {
	if graph == nil {
		return validateFunctionIDTargets(functions, rootSchema)
	}
	defs := make(map[string]ComponentDefinition, len(graph.Definitions))
	for _, def := range graph.Definitions {
		defs[def.ComponentID] = def
	}
	for _, fn := range functions {
		schemaForFunction := rootSchema
		if mount, ok := ComponentMountForModule(graph, fn.ModulePath); ok {
			schemaForFunction = defs[mount.ComponentID].Schema
		}
		if err := validateFunctionIDTargets([]FunctionDescriptor{fn}, schemaForFunction); err != nil {
			return err
		}
	}
	return nil
}

func validateFunctionIDTargets(functions []FunctionDescriptor, rawSchema any) error {
	tables := []any(nil)
	if rawSchema != nil {
		s, ok := rawSchema.(map[string]any)
		if !ok {
			return fmt.Errorf("schema must be an object")
		}
		tables, _ = s["tables"].([]any)
	}
	names := make(map[string]bool, len(tables))
	for _, raw := range tables {
		if table, ok := raw.(map[string]any); ok {
			if name, ok := table["tableName"].(string); ok {
				names[name] = true
			}
		}
	}
	for _, function := range functions {
		for _, validator := range []any{function.Args, function.Returns} {
			if validator == nil {
				continue
			}
			for _, target := range validatorIDTargets(validator, 0, map[uintptr]bool{}) {
				if !names[target] {
					return fmt.Errorf("function id validator targets unknown table")
				}
			}
		}
	}
	return nil
}

func validateSchema(value any) (JSONValue, error) {
	if value == nil {
		return nil, fmt.Errorf("schema must be an object")
	}
	o, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema must be an object")
	}
	if !onlyKeys(o, "tables") {
		return nil, fmt.Errorf("schema has unknown fields")
	}
	tables, ok := o["tables"].([]any)
	if !ok {
		return nil, fmt.Errorf("schema.tables must be an array")
	}
	if len(tables) > maxSchemaTables {
		return nil, fmt.Errorf("schema has too many tables")
	}
	tableNames := map[string]bool{}
	totalNodes := 0
	allFields := make([]map[string]any, 0)
	for i, raw := range tables {
		t, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schema.tables[%d] must be an object", i)
		}
		if !onlyKeys(t, "tableName", "fields", "indexes") {
			return nil, fmt.Errorf("schema.tables[%d] has unknown fields", i)
		}
		name, ok := t["tableName"].(string)
		if !ok || !isIdentifier(name) || schema.IsReservedCollection(name) || tableNames[name] {
			return nil, fmt.Errorf("schema.tables[%d] tableName is invalid or duplicate", i)
		}
		tableNames[name] = true
		fields, ok := t["fields"].(map[string]any)
		if !ok || len(fields) > maxSchemaFields {
			return nil, fmt.Errorf("schema.tables[%d] fields must be an object", i)
		}
		for k, field := range fields {
			if !isUserDocumentField(k) || !validateValidatorDescriptor(field) || !unambiguousDocumentObjectPaths(field, 0, map[uintptr]bool{}) {
				return nil, fmt.Errorf("schema.tables[%d] invalid field %q", i, k)
			}
			nodes, ok := validatorNodeCount(field, 0, map[uintptr]bool{})
			if !ok || nodes > maxSchemaNodes {
				return nil, fmt.Errorf("schema.tables[%d] invalid field %q", i, k)
			}
			totalNodes += nodes
			if totalNodes > maxSchemaNodes {
				return nil, fmt.Errorf("schema has too many validator nodes")
			}
		}
		allFields = append(allFields, fields)
		if indexes, exists := t["indexes"]; exists {
			arr, ok := indexes.([]any)
			if !ok || len(arr) > maxSchemaIndexes {
				return nil, fmt.Errorf("schema.tables[%d].indexes must be an array", i)
			}
			names := map[string]bool{}
			for j, rawIndex := range arr {
				idx, ok := rawIndex.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("schema.tables[%d].indexes[%d] must be an object", i, j)
				}
				if !onlyKeys(idx, "name", "fields") {
					return nil, fmt.Errorf("schema.tables[%d].indexes[%d] has unknown fields", i, j)
				}
				n, ok := idx["name"].(string)
				if !ok || !isIdentifier(n) || names[n] {
					return nil, fmt.Errorf("schema.tables[%d].indexes[%d] name is invalid or duplicate", i, j)
				}
				names[n] = true
				fs, ok := idx["fields"].([]any)
				if !ok {
					return nil, fmt.Errorf("schema.tables[%d].indexes[%d] fields must be an array", i, j)
				}
				if len(fs) == 0 || len(fs) > maxSchemaIndexWidth {
					return nil, fmt.Errorf("schema.tables[%d].indexes[%d] fields must not be empty", i, j)
				}
				seenFields := map[string]bool{}
				for _, f := range fs {
					s, ok := f.(string)
					validator, declared := schema.FieldValidator(fields, s)
					if !ok || !declared || seenFields[s] || !schema.IndexableValidator(validator) {
						return nil, fmt.Errorf("schema.tables[%d].indexes[%d] field is invalid", i, j)
					}
					seenFields[s] = true
				}
			}
		}
	}
	for _, fields := range allFields {
		for _, validator := range fields {
			for _, target := range validatorIDTargets(validator, 0, map[uintptr]bool{}) {
				if !tableNames[target] {
					return nil, fmt.Errorf("schema id validator targets unknown table")
				}
			}
		}
	}
	return value, nil
}

func isUserDocumentField(key string) bool {
	// q.field uses dot-separated schema paths. Allow punctuation that is safely
	// JSON-path quoted, but reject a literal dot at the document schema level
	// so `profile.name` can never ambiguously mean either a top-level field or
	// a nested object path.
	return isSafeFieldName(key) && !strings.Contains(key, ".") && key != "_id" && key != "_creationTime" && !strings.HasPrefix(key, "_pbvex_")
}

// unambiguousDocumentObjectPaths applies the q.field path grammar only to
// table documents. Function args/returns remain ordinary protocol objects and
// may retain dotted application keys; document object shapes that are exposed
// to q.field must not let `profile.name` collide with a literal dotted child.
func unambiguousDocumentObjectPaths(validator any, depth int, stack map[uintptr]bool) bool {
	if depth > MaxValueDepth {
		return false
	}
	o, ok := validator.(map[string]any)
	if !ok {
		return false
	}
	p := reflect.ValueOf(o).Pointer()
	if p != 0 && stack[p] {
		return false
	}
	if p != 0 {
		stack[p] = true
		defer delete(stack, p)
	}
	typ, _ := o["type"].(string)
	switch typ {
	case "optional", "defaulted":
		return unambiguousDocumentObjectPaths(o["validator"], depth+1, stack)
	case "object":
		if len(o) == 1 {
			return true
		}
		shape, ok := o["shape"].(map[string]any)
		if !ok {
			shape, ok = o["fields"].(map[string]any)
		}
		if !ok {
			return false
		}
		for key, child := range shape {
			if strings.Contains(key, ".") || !unambiguousDocumentObjectPaths(child, depth+1, stack) {
				return false
			}
		}
		return true
	default:
		// q.field cannot traverse arrays, records or unions, so their
		// application keys cannot collide with an addressable document path.
		return true
	}
}

func onlyKeys(o map[string]any, allowed ...string) bool {
	allowedSet := map[string]bool{}
	for _, key := range allowed {
		allowedSet[key] = true
	}
	for key := range o {
		if !allowedSet[key] {
			return false
		}
	}
	return true
}

func validateValidatorDescriptor(value any) bool {
	return schema.ValidateDescriptor(value)
}

func validatorNodeCount(value any, depth int, stack map[uintptr]bool) (int, bool) {
	if depth > MaxValueDepth {
		return 0, false
	}
	o, ok := value.(map[string]any)
	if !ok {
		return 0, false
	}
	p := getPointer(o)
	if p != 0 && stack[p] {
		return 0, false
	}
	if p != 0 {
		stack[p] = true
		defer delete(stack, p)
	}
	n := 1
	children := []any{}
	switch o["type"] {
	case "array":
		children = append(children, o["item"])
	case "record":
		children = append(children, o["key"], o["value"])
	case "object":
		shape, ok := o["shape"].(map[string]any)
		if !ok {
			shape, ok = o["fields"].(map[string]any)
			if !ok {
				// The v1 unconstrained object spelling has no child graph.
				return n, len(o) == 1
			}
		}
		for _, child := range shape {
			children = append(children, child)
		}
	case "union":
		branches, ok := o["validators"].([]any)
		if !ok {
			return 0, false
		}
		children = append(children, branches...)
	case "optional", "defaulted":
		children = append(children, o["validator"])
	}
	for _, child := range children {
		count, ok := validatorNodeCount(child, depth+1, stack)
		if !ok || n+count > maxSchemaNodes {
			return 0, false
		}
		n += count
	}
	return n, true
}

func validatorIDTargets(value any, depth int, stack map[uintptr]bool) []string {
	if depth > MaxValueDepth {
		return nil
	}
	o, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	p := getPointer(o)
	if p != 0 && stack[p] {
		return nil
	}
	if p != 0 {
		stack[p] = true
		defer delete(stack, p)
	}
	var children []any
	var out []string
	if o["type"] == "id" {
		if table, ok := o["tableName"].(string); ok {
			out = append(out, table)
		}
	}
	switch o["type"] {
	case "array":
		children = append(children, o["item"])
	case "record":
		children = append(children, o["key"], o["value"])
	case "object":
		shape, ok := o["shape"].(map[string]any)
		if !ok {
			shape, _ = o["fields"].(map[string]any)
		}
		for _, child := range shape {
			children = append(children, child)
		}
	case "union":
		if branches, ok := o["validators"].([]any); ok {
			children = append(children, branches...)
		}
	case "optional", "defaulted":
		children = append(children, o["validator"])
	}
	for _, child := range children {
		for _, table := range validatorIDTargets(child, depth+1, stack) {
			if !containsString(out, table) {
				out = append(out, table)
			}
		}
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// ValidateUploadRequest validates the request body and decoded bundle bytes.
func ValidateUploadRequest(value any) (DeploymentUploadRequest, []byte, error) {
	if value == nil {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("upload request must be an object")}
	}
	o, ok := value.(map[string]any)
	if !ok {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("upload request must be an object")}
	}
	manifest, err := ValidateManifest(o["manifest"])
	if err != nil {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeInvalidManifest, Err: fmt.Errorf("invalid manifest: %w", err)}
	}
	bundle, ok := o["bundle"].(string)
	if !ok || !isBase64String(bundle) {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("bundle must be a valid base64 string")}
	}
	sha256Hex, ok := o["sha256"].(string)
	if !ok || !IsSha256Hex(sha256Hex) {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("sha256 must be a lowercase hex SHA-256")}
	}
	size, ok := toInt64(o["size"])
	if !ok || size < 0 || size > MaxDeploymentUploadBytes {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("size must be a non-negative integer")}
	}
	// DecodedLen can overestimate by at most two padding bytes. Bound the
	// attacker-controlled base64 string before DecodeString allocates its
	// output, then enforce the exact decoded size below.
	if int64(base64.StdEncoding.DecodedLen(len(bundle))) > MaxDeploymentUploadBytes+2 {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("bundle exceeds upload limit")}
	}
	bytes, err := base64.StdEncoding.DecodeString(bundle)
	if err != nil {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("bundle must be a valid base64 string")}
	}
	if int64(len(bytes)) != size {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("size mismatch: expected %d bytes", len(bytes))}
	}
	if int64(len(bytes)) > MaxDeploymentUploadBytes {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBadRequest, Err: fmt.Errorf("bundle exceeds upload limit")}
	}
	hash := hashSha256Bytes(bytes)
	if hash != sha256Hex {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeBundleHashMismatch, Err: fmt.Errorf("sha256 does not match bundle bytes")}
	}
	if manifest.EmailTemplates != nil {
		expected, err := CanonicalHash(emailTemplateHashInput(sha256Hex, manifest.EmailTemplates.Entries))
		if err != nil || expected != manifest.EmailTemplates.Sha256 {
			return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeInvalidManifest, Err: fmt.Errorf("emailTemplates hash does not match bundle and entries")}
		}
	}
	modules, err := validateModuleSources(o["modules"])
	if err != nil {
		return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeInvalidManifest, Err: err}
	}
	if manifest.Components != nil {
		if len(modules) == 0 {
			return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeInvalidManifest, Err: fmt.Errorf("modules are required when components are declared")}
		}
		if err := VerifyModuleSources(modules, manifest); err != nil {
			return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeInvalidManifest, Err: err}
		}
		if err := AuthenticateComponentIDs(manifest, sha256Hex); err != nil {
			return DeploymentUploadRequest{}, nil, &UploadValidationError{Code: ErrorCodeInvalidManifest, Err: err}
		}
	}
	return DeploymentUploadRequest{
		Manifest: manifest,
		Bundle:   bundle,
		Sha256:   sha256Hex,
		Size:     size,
		Modules:  modules,
	}, bytes, nil
}

func validateModuleSources(value any) ([]ModuleSource, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok || len(items) > 16*1024 {
		return nil, fmt.Errorf("modules must be an array")
	}
	out := make([]ModuleSource, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, raw := range items {
		o, ok := raw.(map[string]any)
		if !ok || !onlyKeys(o, "path", "bytes") {
			return nil, fmt.Errorf("modules[%d] must contain path and bytes", i)
		}
		path, pathOK := o["path"].(string)
		encoded, bytesOK := o["bytes"].(string)
		if !pathOK || !isModulePath(path) || seen[path] || !bytesOK || (encoded != "" && !isBase64String(encoded)) {
			return nil, fmt.Errorf("modules[%d] is invalid", i)
		}
		seen[path] = true
		out = append(out, ModuleSource{Path: path, Bytes: encoded})
	}
	return out, nil
}

// ValidateDeployment validates a stored deployment shape.
func ValidateDeployment(value any) (Deployment, error) {
	if value == nil {
		return Deployment{}, fmt.Errorf("deployment must be an object")
	}
	o, ok := value.(map[string]any)
	if !ok {
		return Deployment{}, fmt.Errorf("deployment must be an object")
	}
	deploymentID, ok := o["deploymentId"].(string)
	if !ok || !isIdentifier(deploymentID) {
		return Deployment{}, fmt.Errorf("invalid deploymentId")
	}
	manifest, err := ValidateManifest(o["manifest"])
	if err != nil {
		return Deployment{}, fmt.Errorf("invalid manifest: %w", err)
	}
	bundle, err := validateDeploymentBundle(o["bundle"])
	if err != nil {
		return Deployment{}, fmt.Errorf("invalid bundle: %w", err)
	}
	if err := AuthenticateComponentIDs(manifest, bundle.Sha256); err != nil {
		return Deployment{}, fmt.Errorf("invalid manifest: %w", err)
	}
	createdAt, ok := o["createdAt"].(string)
	if !ok {
		return Deployment{}, fmt.Errorf("createdAt must be an ISO timestamp string")
	}
	active, ok := o["active"].(bool)
	if !ok {
		return Deployment{}, fmt.Errorf("active must be a boolean")
	}
	var activatedAt *string
	if v, present := o["activatedAt"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return Deployment{}, fmt.Errorf("activatedAt must be a string")
		}
		activatedAt = &s
	}
	return Deployment{
		DeploymentID: deploymentID,
		Manifest:     manifest,
		Bundle:       bundle,
		CreatedAt:    createdAt,
		ActivatedAt:  activatedAt,
		Active:       active,
	}, nil
}

// ValidateDeploymentListResponse validates a list response.
func ValidateDeploymentListResponse(value any) (DeploymentListResponse, error) {
	if value == nil {
		return DeploymentListResponse{}, fmt.Errorf("deployment list response must be an object")
	}
	o, ok := value.(map[string]any)
	if !ok {
		return DeploymentListResponse{}, fmt.Errorf("deployment list response must be an object")
	}
	arr, ok := o["deployments"].([]any)
	if !ok {
		return DeploymentListResponse{}, fmt.Errorf("deployments must be an array")
	}
	deployments := make([]Deployment, len(arr))
	for i, v := range arr {
		d, err := ValidateDeployment(v)
		if err != nil {
			return DeploymentListResponse{}, fmt.Errorf("deployments[%d]: %w", i, err)
		}
		deployments[i] = d
	}
	return DeploymentListResponse{Deployments: deployments}, nil
}

// ValidateActivateRequest validates the activate request body.
func ValidateActivateRequest(value any) (DeploymentActivateRequest, error) {
	if value == nil {
		return DeploymentActivateRequest{}, fmt.Errorf("activate request must be an object")
	}
	o, ok := value.(map[string]any)
	if !ok {
		return DeploymentActivateRequest{}, fmt.Errorf("activate request must be an object")
	}
	atomic, ok := o["atomic"].(bool)
	if !ok {
		return DeploymentActivateRequest{}, fmt.Errorf("atomic must be a boolean")
	}
	return DeploymentActivateRequest{Atomic: atomic}, nil
}

// CanonicalJSON returns a deterministic JSON string for a JSON value.
func CanonicalJSON(value JSONValue) (string, error) {
	var buf bytes.Buffer
	if err := canonicalWrite(&buf, value, 0, map[uintptr]struct{}{}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// CanonicalHash returns the SHA-256 of the canonical JSON of a value.
func CanonicalHash(value JSONValue) (string, error) {
	s, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return hashSha256String(s), nil
}

// HashSha256Bytes returns the SHA-256 hex of bytes.
func HashSha256Bytes(b []byte) string {
	return hashSha256Bytes(b)
}

func hashSha256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hashSha256String(s string) string {
	return hashSha256Bytes([]byte(s))
}

func validateDeploymentBundle(value any) (DeploymentBundle, error) {
	if value == nil {
		return DeploymentBundle{}, fmt.Errorf("bundle must be an object")
	}
	o, ok := value.(map[string]any)
	if !ok {
		return DeploymentBundle{}, fmt.Errorf("bundle must be an object")
	}
	js, ok := o["js"].(string)
	if !ok || !isBase64String(js) {
		return DeploymentBundle{}, fmt.Errorf("bundle.js must be a base64 string")
	}
	sha256Hex, ok := o["sha256"].(string)
	if !ok || !IsSha256Hex(sha256Hex) {
		return DeploymentBundle{}, fmt.Errorf("bundle.sha256 must be a hex SHA-256")
	}
	size, ok := toInt64(o["size"])
	if !ok || size < 0 {
		return DeploymentBundle{}, fmt.Errorf("bundle.size must be a non-negative integer")
	}
	return DeploymentBundle{JS: js, Sha256: sha256Hex, Size: size}, nil
}

func validateFunctions(value any) ([]FunctionDescriptor, error) {
	if value == nil {
		return nil, nil
	}
	arr, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("functions must be an array")
	}
	out := make([]FunctionDescriptor, len(arr))
	names := make(map[string]struct{}, len(arr))
	for i, v := range arr {
		f, err := validateFunctionDescriptor(v, i)
		if err != nil {
			return nil, err
		}
		if _, exists := names[f.Name]; exists {
			return nil, fmt.Errorf("function[%d] name is duplicate", i)
		}
		names[f.Name] = struct{}{}
		out[i] = f
	}
	return out, nil
}

func validateFunctionDescriptor(value any, index int) (FunctionDescriptor, error) {
	if value == nil {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] must be an object", index)
	}
	o, ok := value.(map[string]any)
	if !ok {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] must be an object", index)
	}
	name, ok := o["name"].(string)
	if !ok || !isIdentifier(name) {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] name is invalid", index)
	}
	ft, ok := o["type"].(string)
	if !ok || !isFunctionType(ft) {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] type is invalid", index)
	}
	visibility, ok := o["visibility"].(string)
	if !ok || !isFunctionVisibility(visibility) {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] visibility must be public or internal", index)
	}
	if ft == "httpAction" && visibility != "public" {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] httpAction visibility must be public", index)
	}
	modulePath, ok := o["modulePath"].(string)
	if !ok || !isModulePath(modulePath) {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] modulePath is invalid", index)
	}
	exportName, ok := o["exportName"].(string)
	if !ok || !isExportName(exportName) {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] exportName is invalid", index)
	}
	args, err := validateOptionalJsonValue(o["args"])
	if err != nil {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] args must be a valid JSON value: %w", index, err)
	}
	if args != nil && !schema.ValidateDescriptor(args) {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] args must be a validator", index)
	}
	returns, err := validateOptionalJsonValue(o["returns"])
	if err != nil {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] returns must be a valid JSON value: %w", index, err)
	}
	if returns != nil && !schema.ValidateDescriptor(returns) {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] returns must be a validator", index)
	}
	routeValue, routePresent := o["route"]
	if routePresent && routeValue == nil {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] route must be an object", index)
	}
	route, err := validateRoute(routeValue, index)
	if err != nil {
		return FunctionDescriptor{}, err
	}
	if FunctionType(ft) == FunctionTypeHTTPAction && route == nil {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] httpAction requires route", index)
	}
	if FunctionType(ft) != FunctionTypeHTTPAction && route != nil {
		return FunctionDescriptor{}, fmt.Errorf("function[%d] route is only valid for httpAction", index)
	}
	return FunctionDescriptor{
		Name:       name,
		Type:       FunctionType(ft),
		Visibility: FunctionVisibility(visibility),
		ModulePath: modulePath,
		ExportName: exportName,
		Args:       args,
		Returns:    returns,
		Route:      route,
	}, nil
}

func validateRoute(value any, index int) (*FunctionRoute, error) {
	if value == nil {
		return nil, nil
	}
	o, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("function[%d] route must be an object", index)
	}
	if !onlyKeys(o, "method", "path", "pathPrefix") {
		return nil, fmt.Errorf("function[%d] route has unknown fields", index)
	}
	route := &FunctionRoute{}
	if m, ok := o["method"].(string); ok && isHTTPActionMethod(m) {
		route.Method = m
	} else {
		return nil, fmt.Errorf("function[%d] route method is invalid", index)
	}
	pathValue, pathPresent := o["path"]
	if pathPresent {
		path, ok := pathValue.(string)
		if !ok {
			return nil, fmt.Errorf("function[%d] route path must be a string", index)
		}
		route.Path = path
	}
	prefixValue, prefixPresent := o["pathPrefix"]
	if prefixPresent {
		prefix, ok := prefixValue.(string)
		if !ok {
			return nil, fmt.Errorf("function[%d] route pathPrefix must be a string", index)
		}
		route.PathPrefix = prefix
	}
	if pathPresent == prefixPresent {
		return nil, fmt.Errorf("function[%d] route must have exactly one of path or pathPrefix", index)
	}
	if route.Path == "" && route.PathPrefix == "" {
		return nil, fmt.Errorf("function[%d] route path must not be empty", index)
	}
	if route.PathPrefix != "" && !strings.HasSuffix(route.PathPrefix, "/") {
		return nil, fmt.Errorf("function[%d] route pathPrefix must end with /", index)
	}
	if route.PathPrefix != "" && strings.HasPrefix(route.PathPrefix, "/") {
		return nil, fmt.Errorf("function[%d] route pathPrefix must be relative", index)
	}
	if route.Path != "" && strings.HasPrefix(route.Path, "/") {
		return nil, fmt.Errorf("function[%d] route path must be relative", index)
	}
	path := route.Path
	if path == "" {
		path = route.PathPrefix
	}
	if isReservedHTTPActionPath(path) {
		return nil, fmt.Errorf("function[%d] route uses a reserved platform path", index)
	}
	return route, nil
}

func isHTTPActionMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

func isReservedHTTPActionPath(path string) bool {
	segment := strings.TrimSuffix(path, "/")
	if i := strings.IndexByte(segment, '/'); i >= 0 {
		segment = segment[:i]
	}
	switch segment {
	case "call", "realtime", "deployments", "jobs", "storage", "admin":
		return true
	}
	return false
}

func validateConfig(value any) (*DeploymentConfig, error) {
	if value == nil {
		return nil, nil
	}
	o, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("config must be an object")
	}
	cfg := &DeploymentConfig{present: make(map[string]bool)}
	if v, present := o["httpPathPrefix"]; present {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("config.httpPathPrefix must be a string")
		}
		if s != DefaultDeploymentConfig.HTTPPathPrefix {
			return nil, fmt.Errorf("config.httpPathPrefix is immutable; must be %q", DefaultDeploymentConfig.HTTPPathPrefix)
		}
		cfg.HTTPPathPrefix = s
		cfg.present["httpPathPrefix"] = true
	}
	if v, present := o["realtimePath"]; present {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("config.realtimePath must be a string")
		}
		if s != DefaultDeploymentConfig.RealtimePath {
			return nil, fmt.Errorf("config.realtimePath is immutable; must be %q", DefaultDeploymentConfig.RealtimePath)
		}
		cfg.RealtimePath = s
		cfg.present["realtimePath"] = true
	}
	if v, present := o["maxUploadBytes"]; present {
		n, ok := toInt64(v)
		if !ok || n < 0 || n > MaxDeploymentUploadBytes {
			return nil, fmt.Errorf("config.maxUploadBytes must be a non-negative integer")
		}
		cfg.MaxUploadBytes = n
		cfg.present["maxUploadBytes"] = true
	}
	if v, present := o["maxFunctionArgsBytes"]; present {
		n, ok := toInt64(v)
		if !ok || n < 0 {
			return nil, fmt.Errorf("config.maxFunctionArgsBytes must be a non-negative integer")
		}
		if n > MaxFunctionArgsLimit {
			return nil, fmt.Errorf("config.maxFunctionArgsBytes exceeds protocol limit %d", MaxFunctionArgsLimit)
		}
		cfg.MaxFunctionArgsBytes = n
		cfg.present["maxFunctionArgsBytes"] = true
	}
	if v, present := o["maxReturnValueBytes"]; present {
		n, ok := toInt64(v)
		if !ok || n < 0 {
			return nil, fmt.Errorf("config.maxReturnValueBytes must be a non-negative integer")
		}
		if n > MaxReturnValueLimit {
			return nil, fmt.Errorf("config.maxReturnValueBytes exceeds protocol limit %d", MaxReturnValueLimit)
		}
		cfg.MaxReturnValueBytes = n
		cfg.present["maxReturnValueBytes"] = true
	}
	if v, present := o["defaultRequestTimeoutMs"]; present {
		n, ok := toInt64(v)
		if !ok || n < 0 {
			return nil, fmt.Errorf("config.defaultRequestTimeoutMs must be a non-negative integer")
		}
		cfg.DefaultRequestTimeoutMs = n
		cfg.present["defaultRequestTimeoutMs"] = true
	}
	if len(cfg.present) == 0 {
		return nil, nil
	}
	return cfg, nil
}

func validateOptionalJsonValue(value any) (JSONValue, error) {
	if value == nil {
		return nil, nil
	}
	if !isJsonValue(value, 0, map[uintptr]struct{}{}) {
		return nil, fmt.Errorf("value must be a valid JSON value")
	}
	return value, nil
}

func isIdentifier(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return IsIdentifier(s)
}

// IsIdentifier reports whether s is a valid protocol identifier.
func IsIdentifier(s string) bool {
	return s != "" && len(s) <= MaxIdentifierLength && identifierRe.MatchString(s)
}

func isModulePath(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return s != "" && len(s) <= MaxPathLength && !strings.HasPrefix(s, "/") && !strings.Contains(s, "..") && modulePathRe.MatchString(s)
}

func isFunctionType(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return s == "query" || s == "mutation" || s == "action" || s == "httpAction"
}

func isFunctionVisibility(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return s == "public" || s == "internal"
}

func isExportName(value any) bool {
	s, ok := value.(string)
	if !ok || s == "" {
		return false
	}
	return s == "default" || isIdentifier(s)
}

func IsSha256Hex(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return sha256HexRe.MatchString(s)
}

func isBase64String(value string) bool {
	if value == "" {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(value)
	return err == nil
}

func isJsonValue(value any, depth int, seen map[uintptr]struct{}) bool {
	if depth > MaxValueDepth {
		return false
	}
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case bool:
		return true
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case int64:
		return true
	case int:
		return true
	case string:
		return true
	case []any:
		for _, item := range v {
			if !isJsonValue(item, depth+1, seen) {
				return false
			}
		}
		return true
	case map[string]any:
		ptr := getPointer(v)
		if _, ok := seen[ptr]; ok {
			return false
		}
		seen[ptr] = struct{}{}
		defer delete(seen, ptr)
		for k, val := range v {
			if !isSafeFieldName(k) {
				return false
			}
			if !isJsonValue(val, depth+1, seen) {
				return false
			}
		}
		return true
	}
	return false
}

func isSafeFieldName(key string) bool {
	if key == "" || len(key) > MaxFieldLength {
		return false
	}
	if strings.HasPrefix(key, "$") {
		return false
	}
	if _, ok := reservedKeys[key]; ok {
		return false
	}
	return isAsciiPrintable(key)
}

func isAsciiPrintable(key string) bool {
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

func canonicalWrite(w *bytes.Buffer, value any, depth int, seen map[uintptr]struct{}) error {
	if depth > MaxValueDepth {
		return fmt.Errorf("value depth exceeded in canonical JSON")
	}
	if value == nil {
		w.WriteString("null")
		return nil
	}
	switch v := value.(type) {
	case bool:
		if v {
			w.WriteString("true")
		} else {
			w.WriteString("false")
		}
		return nil
	case float64:
		if !isFiniteNumber(v) {
			return fmt.Errorf("non-finite number cannot be canonicalized")
		}
		if v == 0 {
			w.WriteString("0")
			return nil
		}
		spelling, err := jsonNumberSpelling(v)
		if err != nil {
			return err
		}
		w.WriteString(spelling)
		return nil
	case int64:
		w.WriteString(strconv.FormatInt(v, 10))
		return nil
	case int:
		w.WriteString(strconv.Itoa(v))
		return nil
	case string:
		return canonicalWriteString(w, v)
	case []any:
		w.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := canonicalWrite(w, item, depth+1, seen); err != nil {
				return err
			}
		}
		w.WriteByte(']')
		return nil
	case map[string]any:
		ptr := getPointer(v)
		if _, ok := seen[ptr]; ok {
			return fmt.Errorf("cyclic reference in canonical JSON")
		}
		seen[ptr] = struct{}{}
		defer delete(seen, ptr)
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := canonicalWriteString(w, k); err != nil {
				return err
			}
			w.WriteByte(':')
			if err := canonicalWrite(w, v[k], depth+1, seen); err != nil {
				return err
			}
		}
		w.WriteByte('}')
		return nil
	}
	return fmt.Errorf("unsupported JSON value in canonical JSON")
}

// JSON.stringify is the normative number formatter for the protocol. Go's
// FormatFloat uses different exponent thresholds and padding, so delegate this
// narrow operation to the same ECMAScript semantics used by the runtime.
func jsonNumberSpelling(value float64) (string, error) {
	vm := goja.New()
	if err := vm.Set("n", value); err != nil {
		return "", err
	}
	v, err := vm.RunString("JSON.stringify(n)")
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

func canonicalWriteString(w *bytes.Buffer, s string) error {
	// Match JSON.stringify semantics: do not escape HTML characters
	// (<, >, &) like Go's default json.Marshal does, but escape U+2028
	// and U+2029 which JSON.stringify escapes for safe parsing.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	b = bytes.ReplaceAll(b, []byte{0xE2, 0x80, 0xA8}, []byte(`\u2028`))
	b = bytes.ReplaceAll(b, []byte{0xE2, 0x80, 0xA9}, []byte(`\u2029`))
	w.Write(b)
	return nil
}

func isFiniteNumber(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func getPointer(v any) uintptr {
	return reflect.ValueOf(v).Pointer()
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true
	case float32:
		return int64(n), true
	case float64:
		if n == float64(int64(n)) {
			return int64(n), true
		}
		return 0, false
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}
