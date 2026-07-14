package pbvex

import (
	"context"
	"encoding/json"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProtocolErrorEnvelopeHTTP(t *testing.T) {
	app, service := newTestApp(t)
	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base}); err != nil {
		t.Fatal(err)
	}
	mux, err := base.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	token := superuserToken(t, app)
	cases := []struct {
		name, body    string
		auth          bool
		status        int
		code, message string
		expectDetails bool
	}{
		{"unauthorized", "{}", false, 401, "unauthorized", "Unauthorized.", false}, {"malformed", "{", true, 400, "bad_request", "Invalid request body.", true},
		{"invalid manifest", `{"manifest":{},"bundle":"eA==","sha256":"0000000000000000000000000000000000000000000000000000000000000000","size":1}`, true, 400, "invalid_manifest", "Invalid deployment upload.", true},
		{"bad base64", `{"manifest":{"protocolVersion":"v1","deploymentId":"x"},"bundle":"!","sha256":"0000000000000000000000000000000000000000000000000000000000000000","size":1}`, true, 400, "bad_request", "Invalid deployment upload.", true},
		{"size", `{"manifest":{"protocolVersion":"v1","deploymentId":"x"},"bundle":"eA==","sha256":"0000000000000000000000000000000000000000000000000000000000000000","size":2}`, true, 400, "bad_request", "Invalid deployment upload.", true},
		{"hash", `{"manifest":{"protocolVersion":"v1","deploymentId":"x"},"bundle":"eA==","sha256":"0000000000000000000000000000000000000000000000000000000000000000","size":1}`, true, 400, "bundle_hash_mismatch", "Invalid deployment upload.", true},
		{"array root", `[]`, true, 400, "bad_request", "Invalid request body.", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/pbvex/deployments", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-Id", "rid")
			if tc.auth {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			assertProtocolError(t, rr, tc.status, tc.code, tc.message, "rid", tc.expectDetails)
		})
	}
	for _, tc := range []struct {
		name, bundle, fn string
		code, message    string
		expectDetails    bool
		status           int
	}{
		{"missing", testBundleJS, "absent", "not_found", "Function not found.", false, 404},
		{"internal", `__pbvex.registerFunction({name:"secret",type:"query",visibility:"internal",modulePath:"secret",exportName:"default"},function(ctx,args){return 1})`, "secret", "not_found", "Function not found.", false, 404},
		{"httpAction", `__pbvex.registerFunction({name:"http",type:"httpAction",visibility:"public",modulePath:"http",exportName:"default",route:{method:"GET",path:"http"}},function(ctx,args){return 1})`, "http", "not_found", "Function not found.", false, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "api_" + tc.name
			if tc.name == "internal" {
				raw := testUploadRequest(id, tc.bundle, "secret")
				raw["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["visibility"] = "internal"
				if _, err := service.Upload(raw); err != nil {
					t.Fatal(err)
				}
			} else if tc.name == "httpAction" {
				raw := uploadRequest(id, tc.bundle, functionDescriptor("http", "httpAction", "public"), nil)
				raw["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["route"] = map[string]any{"method": "GET", "path": "http"}
				if _, err := service.Upload(raw); err != nil {
					t.Fatal(err)
				}
			} else if _, err := service.Upload(testUploadRequest(id, tc.bundle)); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Activate(id, true); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(`{"name":"`+tc.fn+`","args":{}}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-Id", "rid")
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			assertProtocolError(t, rr, tc.status, tc.code, tc.message, "rid", tc.expectDetails)
		})
	}
	// Corrupt an already active record through the internal persistence context;
	// this is an unclassified server fault and must never become bad_request.
	if _, err := service.Upload(testUploadRequest("corrupt", testBundleJS)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("corrupt", true); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindFirstRecordByFilter(schema.CollectionDeployments, "deploymentId = {:id}", dbx.Params{"id": "corrupt"})
	if err != nil {
		t.Fatal(err)
	}
	record.Set(schema.FieldManifest, "{")
	if err := app.SaveWithContext(context.WithValue(context.Background(), schema.InternalContextKey, true), record); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(`{"name":"hello","args":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "rid500")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	assertProtocolError(t, rr, http.StatusInternalServerError, "internal", "Internal server error.", "rid500", false)
	if strings.Contains(rr.Body.String(), "failed to parse manifest") {
		t.Fatal("internal cause leaked")
	}
}

func assertProtocolError(t *testing.T, rr *httptest.ResponseRecorder, status int, code, message, requestID string, expectDetails bool) map[string]any {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["error"] != true || got["code"] != code || got["message"] != message || got["requestId"] != requestID {
		t.Fatalf("envelope %#v", got)
	}
	if _, ok := got["data"]; ok {
		t.Fatal("PocketBase envelope")
	}
	details, has := got["details"]
	items, isArray := details.([]any)
	if expectDetails && (!has || !isArray || len(items) == 0) {
		t.Fatalf("missing details %#v", got)
	}
	if !expectDetails && has && (!isArray || len(items) != 0) {
		t.Fatalf("unexpected details %#v", got)
	}
	return got
}
