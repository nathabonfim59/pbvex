package pbvex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/logger"
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

func TestHandlerApplicationErrorsMapToHTTPAndMaskOtherErrors(t *testing.T) {
	app, service := newTestApp(t)
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}); err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(`{"name":"hello","args":{}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-Id", "application-rid")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	cases := []struct {
		category string
		status   int
		message  string
		async    bool
	}{
		{"bad_request", 400, "Bad request.", false},
		{"unauthorized", 401, "Unauthorized.", true},
		{"forbidden", 403, "Forbidden.", false},
		{"not_found", 404, "Not found.", false},
		{"conflict", 409, "Conflict.", false},
	}
	for _, tc := range cases {
		handler := `throw applicationError("` + tc.category + `",{kind:"safe",attempt:1n})`
		if tc.async {
			handler = `return Promise.reject(applicationError("` + tc.category + `",{kind:"safe",attempt:1n}))`
		}
		bundle := `function applicationError(category,data){return __pbvex.createApplicationError(category,data,arguments.length>=2);}__pbvex.registerFunction({name:"hello",type:"query",visibility:"public",modulePath:"hello",exportName:"default"},function(){` + handler + `;})`
		if _, err := service.Upload(testUploadRequest("application_"+tc.category, bundle)); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Activate("application_"+tc.category, true); err != nil {
			t.Fatal(err)
		}
		rr := call()
		if rr.Code != tc.status {
			t.Fatalf("%s status %d: %s", tc.category, rr.Code, rr.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		data, ok := got["data"].(map[string]any)
		integer, integerOK := data["attempt"].(map[string]any)
		if got["code"] != tc.category || got["message"] != tc.message || got["requestId"] != "application-rid" || !ok || !integerOK || data["kind"] != "safe" || integer["$integer"] != "AQAAAAAAAAA=" {
			t.Fatalf("%s envelope %#v", tc.category, got)
		}
	}

	httpBundle := `function applicationError(category,data){return __pbvex.createApplicationError(category,data,arguments.length>=2);}__pbvex.registerFunction({name:"httpError",type:"httpAction",visibility:"public",modulePath:"httpError",exportName:"default",route:{method:"GET",path:"http-error"}},function(){throw applicationError("conflict",null);})`
	httpDescriptor := functionDescriptor("httpError", "httpAction", "public")
	httpDescriptor["route"] = map[string]any{"method": "GET", "path": "http-error"}
	if _, err := service.Upload(uploadRequest("application_http", httpBundle, httpDescriptor, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("application_http", true); err != nil {
		t.Fatal(err)
	}
	httpRequest := httptest.NewRequest(http.MethodGet, "/api/pbvex/http-error", nil)
	httpRequest.Header.Set("X-Request-Id", "http-application-rid")
	httpResponse := httptest.NewRecorder()
	mux.ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusConflict {
		t.Fatalf("HTTP action status %d: %s", httpResponse.Code, httpResponse.Body.String())
	}
	var httpError map[string]any
	if err := json.Unmarshal(httpResponse.Body.Bytes(), &httpError); err != nil {
		t.Fatal(err)
	}
	if httpError["code"] != "conflict" || httpError["requestId"] != "http-application-rid" {
		t.Fatalf("HTTP action envelope %#v", httpError)
	}
	if data, exists := httpError["data"]; !exists || data != nil {
		t.Fatalf("HTTP action null data was not preserved: %#v", httpError)
	}

	masked := []string{
		`throw new Error("handler secret")`,
		`throw {name:"ApplicationError",category:"forbidden",data:{secret:true}}`,
		`throw __pbvex.createApplicationError("bad_request",function(){},true)`,
	}
	for i, handler := range masked {
		id := "masked_" + string(rune('a'+i))
		bundle := `__pbvex.registerFunction({name:"hello",type:"query",visibility:"public",modulePath:"hello",exportName:"default"},function(){` + handler + `;})`
		if _, err := service.Upload(testUploadRequest(id, bundle)); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Activate(id, true); err != nil {
			t.Fatal(err)
		}
		rr := call()
		assertProtocolError(t, rr, 500, "internal", "Internal server error.", "application-rid", false)
		if strings.Contains(rr.Body.String(), "handler secret") || strings.Contains(rr.Body.String(), `"secret":true`) {
			t.Fatal("unexpected handler error data leaked")
		}
	}
}

func TestUnexpectedHTTPHandlerFailureIsLoggedOnceInNonDevMode(t *testing.T) {
	app, service := newTestApp(t)
	if app.IsDev() {
		t.Fatal("test requires non-dev logging behavior")
	}
	app.Settings().Logs.MaxDays = 1
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}); err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	bundle := `
const fail = () => { throw new Error("server-only cause") };
__pbvex.registerFunction({name:"read",type:"query",visibility:"public",modulePath:"read",exportName:"default"},fail);
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"write",exportName:"default"},fail);
__pbvex.registerFunction({name:"act",type:"action",visibility:"public",modulePath:"act",exportName:"default"},fail);
__pbvex.registerFunction({name:"web",type:"httpAction",visibility:"public",modulePath:"web",exportName:"default",route:{method:"GET",path:"observable-web"}},fail);`
	descriptors := []any{
		functionDescriptor("read", "query", "public"),
		functionDescriptor("write", "mutation", "public"),
		functionDescriptor("act", "action", "public"),
		functionDescriptor("web", "httpAction", "public"),
	}
	descriptors[3].(map[string]any)["route"] = map[string]any{"method": "GET", "path": "observable-web"}
	if _, err := service.Upload(uploadRequestWithFunctions("observable_failure", bundle, descriptors, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("observable_failure", true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"read", "write", "act"} {
		requestID := "rid-" + name
		req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(`{"name":"`+name+`","args":{}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-Id", requestID)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		assertProtocolError(t, response, http.StatusInternalServerError, "internal", "Internal server error.", requestID, false)
		if strings.Contains(response.Body.String(), "server-only cause") {
			t.Fatal("unexpected cause leaked to client")
		}
	}
	httpReq := httptest.NewRequest(http.MethodGet, "/api/pbvex/observable-web", nil)
	httpReq.Header.Set("X-Request-Id", "rid-web")
	httpResponse := httptest.NewRecorder()
	mux.ServeHTTP(httpResponse, httpReq)
	assertProtocolError(t, httpResponse, http.StatusInternalServerError, "internal", "Internal server error.", "rid-web", false)
	if strings.Contains(httpResponse.Body.String(), "server-only cause") {
		t.Fatal("unexpected HTTP action cause leaked to client")
	}

	handler, ok := app.Logger().Handler().(*logger.BatchHandler)
	if !ok {
		t.Fatalf("logger handler is %T", app.Logger().Handler())
	}
	if err := handler.WriteAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	var logs []*core.Log
	if err := app.AuxDB().Select("*").From(core.LogsTableName).
		Where(dbx.HashExp{"message": "PBVex handler failed"}).All(&logs); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 4 {
		t.Fatalf("handler log count = %d, want 4", len(logs))
	}
	wantTypes := map[string]string{"read": "query", "write": "mutation", "act": "action", "web": "httpAction"}
	for _, entry := range logs {
		data := entry.Data
		name, _ := data["function"].(string)
		if data["requestId"] != "rid-"+name || data["functionType"] != wantTypes[name] ||
			data["phase"] != "handler_execution" || data["errorType"] != "javascript_exception" || data["error"] != nil {
			t.Fatalf("unexpected handler log: %#v", data)
		}
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
