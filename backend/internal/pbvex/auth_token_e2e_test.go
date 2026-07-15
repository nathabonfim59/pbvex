package pbvex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/realtime"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const authTokenE2EBundle = `
const identity = async (ctx) => await ctx.auth.getUserIdentity();
__pbvex.registerFunction({name:"whoQuery",type:"query",visibility:"public",modulePath:"auth",exportName:"whoQuery"}, async function(ctx,args){ return identity(ctx); });
__pbvex.registerFunction({name:"whoMutation",type:"mutation",visibility:"public",modulePath:"auth",exportName:"whoMutation"}, async function(ctx,args){ return identity(ctx); });
__pbvex.registerFunction({name:"whoAction",type:"action",visibility:"public",modulePath:"auth",exportName:"whoAction"}, async function(ctx,args){ return await ctx.runQuery({_path:"whoQuery",_type:"query",_visibility:"public"},{}); });
__pbvex.registerFunction({name:"whoHTTP",type:"httpAction",visibility:"public",modulePath:"auth",exportName:"whoHTTP",route:{method:"GET",path:"who"}}, async function(ctx,request){ const user=await ctx.runQuery({_path:"whoQuery",_type:"query",_visibility:"public"},{}); return new Response(JSON.stringify(user),{status:200,headers:{"content-type":"application/json"}}); });
__pbvex.registerFunction({name:"whoRealtime",type:"query",visibility:"public",modulePath:"auth",exportName:"whoRealtime"}, async function(ctx,args){ const user=await identity(ctx); return {tokenIdentifier:user&&user.tokenIdentifier,nonce:Math.random()}; });`

func setupBearerAuthE2E(t *testing.T) (core.App, *core.Record, string, *deploy.Service, *httptest.Server) {
	t.Helper()
	app, service := newTestApp(t)
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	// The upstream test fixture enables MFA for this collection. This helper
	// exercises the completed, single-method token flow; the shared auth routes
	// used by MFA are covered separately in the route matrix.
	users.MFA.Enabled = false
	if err := app.Save(users); err != nil {
		t.Fatal(err)
	}
	user := core.NewRecord(users)
	user.Set("email", "auth-e2e@example.com")
	user.SetPassword("password123")
	if err := app.Save(user); err != nil {
		t.Fatal(err)
	}
	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}

	functions := []any{
		map[string]any{"name": "whoQuery", "type": "query", "visibility": "public", "modulePath": "auth", "exportName": "whoQuery"},
		map[string]any{"name": "whoMutation", "type": "mutation", "visibility": "public", "modulePath": "auth", "exportName": "whoMutation"},
		map[string]any{"name": "whoAction", "type": "action", "visibility": "public", "modulePath": "auth", "exportName": "whoAction"},
		map[string]any{"name": "whoHTTP", "type": "httpAction", "visibility": "public", "modulePath": "auth", "exportName": "whoHTTP", "route": map[string]any{"method": "GET", "path": "who"}},
		map[string]any{"name": "whoRealtime", "type": "query", "visibility": "public", "modulePath": "auth", "exportName": "whoRealtime"},
	}
	upload := uploadRequestWithFunctions("auth_token_e2e", authTokenE2EBundle, functions, nil)
	resp, err := service.Upload(upload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatal(err)
	}

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
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return app, user, token, service, server
}

func bearerRequest(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestPocketBaseBearerTokenFlowsThroughCallsAndHTTPAction(t *testing.T) {
	_, user, token, _, server := setupBearerAuthE2E(t)
	want := "pocketbase:" + user.Collection().Id + ":" + user.Id
	for _, name := range []string{"whoQuery", "whoMutation", "whoAction"} {
		resp := bearerRequest(t, http.MethodPost, server.URL+"/api/pbvex/call", token, map[string]any{"name": name, "args": map[string]any{}})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d", name, resp.StatusCode)
		}
		var envelope struct {
			Result map[string]any `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if got := envelope.Result["tokenIdentifier"]; got != want {
			t.Fatalf("%s tokenIdentifier=%v want %s", name, got, want)
		}
	}

	resp := bearerRequest(t, http.MethodGet, server.URL+"/api/pbvex/who", token, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("httpAction status=%d", resp.StatusCode)
	}
	var identity map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatal(err)
	}
	if got := identity["tokenIdentifier"]; got != want {
		t.Fatalf("http tokenIdentifier=%v want %s", got, want)
	}
}

func TestPasswordAndRefreshedAuthTokensAuthenticatePBVexCall(t *testing.T) {
	_, user, _, _, server := setupBearerAuthE2E(t)

	authResponse, err := http.Post(
		server.URL+"/api/collections/users/auth-with-password",
		"application/json",
		strings.NewReader(`{"identity":"auth-e2e@example.com","password":"password123"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer authResponse.Body.Close()
	if authResponse.StatusCode != http.StatusOK {
		var failure map[string]any
		_ = json.NewDecoder(authResponse.Body).Decode(&failure)
		t.Fatalf("password auth status=%d body=%v", authResponse.StatusCode, failure)
	}
	var authData struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(authResponse.Body).Decode(&authData); err != nil {
		t.Fatal(err)
	}
	if authData.Token == "" {
		t.Fatal("password auth returned an empty token")
	}

	want := "pocketbase:" + user.Collection().Id + ":" + user.Id
	assertPBVexIdentity := func(token string) {
		t.Helper()
		callResponse := bearerRequest(t, http.MethodPost, server.URL+"/api/pbvex/call", token, map[string]any{
			"name": "whoQuery",
			"args": map[string]any{},
		})
		defer callResponse.Body.Close()
		if callResponse.StatusCode != http.StatusOK {
			t.Fatalf("PBVex call status=%d", callResponse.StatusCode)
		}
		var envelope struct {
			Result map[string]any `json:"result"`
		}
		if err := json.NewDecoder(callResponse.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if got := envelope.Result["tokenIdentifier"]; got != want {
			t.Fatalf("tokenIdentifier=%v want %s", got, want)
		}
	}
	assertPBVexIdentity(authData.Token)

	refreshResponse := bearerRequest(t, http.MethodPost, server.URL+"/api/collections/users/auth-refresh", authData.Token, nil)
	defer refreshResponse.Body.Close()
	if refreshResponse.StatusCode != http.StatusOK {
		t.Fatalf("auth refresh status=%d", refreshResponse.StatusCode)
	}
	var refreshed struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(refreshResponse.Body).Decode(&refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.Token == "" {
		t.Fatal("auth refresh returned an empty token")
	}
	assertPBVexIdentity(refreshed.Token)
}

func TestPocketBaseBearerTokenPersistsAcrossRealtimeRerun(t *testing.T) {
	app, user, token, _, server := setupBearerAuthE2E(t)
	want := "pocketbase:" + user.Collection().Id + ":" + user.Id
	args := map[string]any{}
	subscriptionID := realtime.DeriveSubscriptionID(deploy.SupportedProtocolVersion, "whoRealtime", args)
	body, err := json.Marshal(map[string]any{"id": subscriptionID, "path": "whoRealtime", "args": args})
	if err != nil {
		t.Fatal(err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/api/pbvex/realtime", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	req, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer req.Body.Close()
	if req.StatusCode != http.StatusOK {
		var failure map[string]any
		_ = json.NewDecoder(req.Body).Decode(&failure)
		t.Fatalf("realtime status=%d body=%v", req.StatusCode, failure)
	}
	reader := bufio.NewReader(req.Body)
	_ = expectSSEMessage(t, reader)
	first := expectSSEMessage(t, reader)
	if !strings.Contains(first, fmt.Sprintf(`"tokenIdentifier":"%s"`, want)) {
		t.Fatalf("first authenticated result=%s", first)
	}

	// Any successful record mutation invalidates active subscriptions.
	appUser := core.NewRecord(user.Collection())
	appUser.Set("email", "auth-rerun-trigger@example.com")
	appUser.SetPassword("password123")
	if err := app.Save(appUser); err != nil {
		t.Fatal(err)
	}
	second := expectSSEMessage(t, reader)
	if !strings.Contains(second, fmt.Sprintf(`"tokenIdentifier":"%s"`, want)) {
		t.Fatalf("rerun lost identity: %s", second)
	}
}
