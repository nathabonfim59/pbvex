package deploy

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func testServiceUploadRequest(deploymentID, bundle string) map[string]any {
	return map[string]any{
		"manifest": map[string]any{
			"protocolVersion": "v1",
			"deploymentId":    deploymentID,
			"functions":       []any{},
		},
		"bundle": base64.StdEncoding.EncodeToString([]byte(bundle)),
		"sha256": HashSha256Bytes([]byte(bundle)),
		"size":   int64(len(bundle)),
	}
}

type noopInvoker struct{}

type recordingActivationObserver struct {
	deploymentID string
	manifest     DeploymentManifest
}

func (o *recordingActivationObserver) ActiveDeploymentChanged(deploymentID string, manifest DeploymentManifest) {
	o.deploymentID = deploymentID
	o.manifest = manifest
}

func (*noopInvoker) Compile(string, string, []FunctionDescriptor, DeploymentConfig) error { return nil }
func (*noopInvoker) Verify(context.Context, string, string, []FunctionDescriptor) error {
	return nil
}
func (*noopInvoker) Invoke(context.Context, string, string, any, *auth.UserIdentity, string) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (*noopInvoker) InvokeHTTP(context.Context, string, string, *HTTPRequestEnvelope, *auth.UserIdentity, string) (*HTTPResponseEnvelope, error) {
	return &HTTPResponseEnvelope{Status: 200, Body: []byte("ok")}, nil
}
func (*noopInvoker) Drop(string) {}

func setupTestService(t *testing.T) *Service {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	service := NewService(app, NewRepo(), &noopInvoker{}, Config{HistoryLimit: 5})
	manifest := DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "dep_test",
		Functions: []FunctionDescriptor{
			{Name: "myQuery", Type: FunctionTypeQuery, Visibility: FunctionVisibilityPublic, ModulePath: "q.js", ExportName: "myQuery"},
			{Name: "internalQuery", Type: FunctionTypeQuery, Visibility: FunctionVisibilityInternal, ModulePath: "q.js", ExportName: "internalQuery"},
			{Name: "webhook", Type: FunctionTypeHTTPAction, Visibility: FunctionVisibilityPublic, ModulePath: "h.js", ExportName: "webhook", Route: &FunctionRoute{Method: "POST", Path: "/webhook"}},
		},
	}
	if _, err := service.repo.CreateDeployment(service.internalCtx(), app, manifest, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	state, err := service.repo.GetState(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	state.Set(schema.FieldActiveID, "dep_test")
	if err := service.repo.SaveState(service.internalCtx(), app, state); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestUploadAndActivationAreIdempotent(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	service := NewService(app, NewRepo(), &noopInvoker{}, Config{HistoryLimit: 5})
	raw := testServiceUploadRequest("dep_idempotent", "globalThis.idempotent = true")

	firstUpload, err := service.Upload(raw)
	if err != nil {
		t.Fatal(err)
	}
	secondUpload, err := service.Upload(raw)
	if err != nil {
		t.Fatalf("retrying identical upload: %v", err)
	}
	if secondUpload.DeploymentID != firstUpload.DeploymentID || secondUpload.BundleHash != firstUpload.BundleHash || secondUpload.AcceptedAt != firstUpload.AcceptedAt {
		t.Fatalf("retry response changed: first=%#v second=%#v", firstUpload, secondUpload)
	}
	if count, err := service.repo.CountDeployments(context.Background(), app); err != nil || count != 1 {
		t.Fatalf("deployment count = %d, err = %v; want 1", count, err)
	}

	firstActivation, err := service.Activate(firstUpload.DeploymentID, true)
	if err != nil {
		t.Fatal(err)
	}
	secondActivation, err := service.Activate(firstUpload.DeploymentID, true)
	if err != nil {
		t.Fatalf("retrying activation: %v", err)
	}
	if secondActivation.DeploymentID != firstActivation.DeploymentID || secondActivation.ActivatedAt != firstActivation.ActivatedAt {
		t.Fatalf("retry activation response changed: first=%#v second=%#v", firstActivation, secondActivation)
	}
}

func TestUploadRejectsReusedDeploymentIDWithDifferentContent(t *testing.T) {
	service := setupTestService(t)
	if _, err := service.Upload(testServiceUploadRequest("dep_retry_collision", "first")); err != nil {
		t.Fatal(err)
	}
	_, err := service.Upload(testServiceUploadRequest("dep_retry_collision", "second"))
	if !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("collision error = %v, want ErrInvalidBundle", err)
	}
}

func TestServiceCallRejectsHTTPActionViaGenericCall(t *testing.T) {
	service := setupTestService(t)
	_, err := service.Call(context.Background(), "webhook", nil, nil, "")
	if !errors.Is(err, ErrDeploymentNotFound) {
		t.Fatalf("expected ErrDeploymentNotFound for httpAction via generic call, got: %v", err)
	}
}

func TestWarmActiveNotifiesActivationObserver(t *testing.T) {
	service := setupTestService(t)
	observer := &recordingActivationObserver{}
	service.SetActivationObserver(observer)
	if err := service.WarmActive(); err != nil {
		t.Fatal(err)
	}
	if observer.deploymentID != "dep_test" || observer.manifest.DeploymentID != "dep_test" {
		t.Fatalf("activation notification = %#v", observer)
	}
}

func TestServiceCallRejectsInternalFunction(t *testing.T) {
	service := setupTestService(t)
	_, err := service.Call(context.Background(), "internalQuery", nil, nil, "")
	if !errors.Is(err, ErrDeploymentNotFound) {
		t.Fatalf("expected ErrDeploymentNotFound for internal function, got: %v", err)
	}
}

func TestServiceCallAllowsPublicQuery(t *testing.T) {
	service := setupTestService(t)
	result, err := service.Call(context.Background(), "myQuery", nil, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestMatchHTTPRouteExact(t *testing.T) {
	service := setupTestService(t)
	fn, matched, ok := service.MatchHTTPRoute("POST", "/webhook")
	if !ok {
		t.Fatal("expected route match")
	}
	if fn != "webhook" {
		t.Fatalf("got function %q, want webhook", fn)
	}
	if matched == "" {
		t.Fatal("expected non-empty matched path")
	}
}

func TestMatchHTTPRouteAmbiguousConflict(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	service := NewService(app, NewRepo(), &noopInvoker{}, Config{HistoryLimit: 5})
	manifest := DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "dep_amb",
		Functions: []FunctionDescriptor{
			{Name: "a", Type: FunctionTypeHTTPAction, Visibility: FunctionVisibilityPublic, ModulePath: "a.js", ExportName: "a", Route: &FunctionRoute{Method: "GET", Path: "/dup"}},
			{Name: "b", Type: FunctionTypeHTTPAction, Visibility: FunctionVisibilityPublic, ModulePath: "b.js", ExportName: "b", Route: &FunctionRoute{Method: "GET", Path: "/dup"}},
		},
	}
	if _, err := service.repo.CreateDeployment(service.internalCtx(), app, manifest, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	state, err := service.repo.GetState(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	state.Set(schema.FieldActiveID, "dep_amb")
	if err := service.repo.SaveState(service.internalCtx(), app, state); err != nil {
		t.Fatal(err)
	}
	_, _, ok := service.MatchHTTPRoute("GET", "/dup")
	if ok {
		t.Fatal("expected ambiguous route to not resolve")
	}
	envelope := &HTTPRequestEnvelope{Method: "GET", URL: "/dup"}
	_, err = service.HTTPAction(context.Background(), "GET", "/dup", envelope, nil, "")
	if err == nil {
		t.Fatal("expected error for ambiguous routes")
	}
}

func TestMatchHTTPRouteWrongMethod(t *testing.T) {
	service := setupTestService(t)
	_, _, ok := service.MatchHTTPRoute("DELETE", "/webhook")
	if ok {
		t.Fatal("expected no match for wrong method")
	}
}

func TestMatchHTTPRouteUnknownPath(t *testing.T) {
	service := setupTestService(t)
	_, _, ok := service.MatchHTTPRoute("POST", "/nonexistent")
	if ok {
		t.Fatal("expected no match for unknown path")
	}
}

func TestHTTPActionRequestBodyLimit(t *testing.T) {
	service := setupTestService(t)
	cfg := DefaultDeploymentConfig
	cfg.MaxFunctionArgsBytes = 4
	manifest := DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "dep_limit",
		Config:          &cfg,
		Functions: []FunctionDescriptor{
			{Name: "upload", Type: FunctionTypeHTTPAction, Visibility: FunctionVisibilityPublic, ModulePath: "h.js", ExportName: "upload", Route: &FunctionRoute{Method: "POST", Path: "/upload"}},
		},
	}
	if _, err := service.repo.CreateDeployment(service.internalCtx(), service.app, manifest, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	state, err := service.repo.GetState(context.Background(), service.app)
	if err != nil {
		t.Fatal(err)
	}
	state.Set(schema.FieldActiveID, "dep_limit")
	if err := service.repo.SaveState(service.internalCtx(), service.app, state); err != nil {
		t.Fatal(err)
	}
	envelope := &HTTPRequestEnvelope{
		Method:  "POST",
		URL:     "/api/pbvex/upload",
		Headers: map[string][]string{"Content-Type": {"text/plain"}},
		Body:    []byte("this body is too long"),
	}
	_, err = service.HTTPAction(context.Background(), "POST", "/upload", envelope, nil, "")
	if err == nil {
		t.Fatal("expected error for oversized request body")
	}
}

func TestHTTPActionHonorsCanceledCallerContext(t *testing.T) {
	service := setupTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.HTTPAction(ctx, "POST", "/webhook", &HTTPRequestEnvelope{Method: "POST", URL: "/webhook"}, nil, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("HTTPAction error = %v, want context.Canceled", err)
	}
}

func TestMatchHTTPRouteRejectsInternalHTTPAction(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	service := NewService(app, NewRepo(), &noopInvoker{}, Config{HistoryLimit: 5})
	manifest := DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "dep_internal_http",
		Functions: []FunctionDescriptor{
			{Name: "publicHook", Type: FunctionTypeHTTPAction, Visibility: FunctionVisibilityPublic, ModulePath: "h.js", ExportName: "publicHook", Route: &FunctionRoute{Method: "POST", Path: "/public"}},
			{Name: "internalHook", Type: FunctionTypeHTTPAction, Visibility: FunctionVisibilityInternal, ModulePath: "h.js", ExportName: "internalHook", Route: &FunctionRoute{Method: "POST", Path: "/internal"}},
		},
	}
	if _, err := service.repo.CreateDeployment(service.internalCtx(), app, manifest, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	state, err := service.repo.GetState(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	state.Set(schema.FieldActiveID, "dep_internal_http")
	if err := service.repo.SaveState(service.internalCtx(), app, state); err != nil {
		t.Fatal(err)
	}

	_, _, ok := service.MatchHTTPRoute("POST", "/internal")
	if ok {
		t.Fatal("internal httpAction route must never be publicly dispatchable")
	}

	envelope := &HTTPRequestEnvelope{Method: "POST", URL: "http://localhost/internal"}
	_, err = service.HTTPAction(context.Background(), "POST", "/internal", envelope, nil, "")
	if err == nil {
		t.Fatal("expected error dispatching internal httpAction route")
	}
}

func TestMaxFunctionArgsBytesReflectsActiveDeployment(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	service := NewService(app, NewRepo(), &noopInvoker{}, Config{HistoryLimit: 5})
	if service.MaxFunctionArgsBytes() != DefaultDeploymentConfig.MaxFunctionArgsBytes {
		t.Fatalf("default limit mismatch: got %d", service.MaxFunctionArgsBytes())
	}
	cfg := DefaultDeploymentConfig
	cfg.MaxFunctionArgsBytes = 500
	manifest := DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "dep_small_limit",
		Config:          &cfg,
		Functions:       []FunctionDescriptor{},
	}
	if _, err := service.repo.CreateDeployment(service.internalCtx(), app, manifest, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	state, err := service.repo.GetState(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	state.Set(schema.FieldActiveID, "dep_small_limit")
	if err := service.repo.SaveState(service.internalCtx(), app, state); err != nil {
		t.Fatal(err)
	}
	if service.MaxFunctionArgsBytes() != 500 {
		t.Fatalf("active deployment limit: got %d, want 500", service.MaxFunctionArgsBytes())
	}
}

func TestUploadAdmissionEnforcesActiveMaxUploadBytes(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	service := NewService(app, NewRepo(), &noopInvoker{}, Config{HistoryLimit: 5})

	// Bootstrap: no active deployment → default cap applies.
	defaultCap := service.MaxUploadBytes()
	if defaultCap != DefaultDeploymentConfig.MaxUploadBytes {
		t.Fatalf("bootstrap cap: got %d, want %d", defaultCap, DefaultDeploymentConfig.MaxUploadBytes)
	}

	// Activate a deployment with a small cap (100 bytes).
	smallCfg := DefaultDeploymentConfig
	smallCfg.MaxUploadBytes = 100
	smallManifest := DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "dep_small_upload",
		Config:          &smallCfg,
	}
	if _, err := service.repo.CreateDeployment(service.internalCtx(), app, smallManifest, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	state, err := service.repo.GetState(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	state.Set(schema.FieldActiveID, "dep_small_upload")
	if err := service.repo.SaveState(service.internalCtx(), app, state); err != nil {
		t.Fatal(err)
	}

	// Active cap must now be 100.
	if got := service.MaxUploadBytes(); got != 100 {
		t.Fatalf("active cap: got %d, want 100", got)
	}

	// Candidate with maxUploadBytes=64MiB must still be capped at 100.
	candidateCfg := DefaultDeploymentConfig
	candidateCfg.MaxUploadBytes = MaxDeploymentUploadBytes
	raw := map[string]any{
		"manifest": map[string]any{
			"protocolVersion": "v1",
			"deploymentId":    "dep_candidate",
			"config": map[string]any{
				"maxUploadBytes": MaxDeploymentUploadBytes,
			},
		},
		"bundle": "eA==",
		"sha256": "0000000000000000000000000000000000000000000000000000000000000000",
		"size":   int64(101),
	}
	if _, err := service.Upload(raw); err == nil {
		t.Fatal("upload exceeding active cap must be rejected")
	}

	// Candidate with size exactly 100 must be admitted by the cap check.
	raw["size"] = int64(100)
	raw["manifest"].(map[string]any)["deploymentId"] = "dep_ok"
	_, err = service.Upload(raw)
	// Upload may fail later in verification, but the size check must pass.
	if err != nil && strings.Contains(err.Error(), "maxUploadBytes") {
		t.Fatalf("upload at exactly active cap must not be rejected by size: %v", err)
	}
}
