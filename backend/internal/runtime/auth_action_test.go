package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
)

// testIdentity returns a deterministic UserIdentity for auth-propagation tests.
func testIdentity() *auth.UserIdentity {
	return &auth.UserIdentity{
		Subject:         "rec_123",
		TokenIdentifier: "pocketbase:users:rec_123",
		Issuer:          "pocketbase:users",
		Email:           "user@example.com",
	}
}

// compileAndInvoke is a test helper that compiles a bundle and invokes a
// function, returning the result or error.
func compileAndInvoke(t *testing.T, bundle string, descriptors []deploy.FunctionDescriptor, fn string, args any, identity *auth.UserIdentity) (any, error) {
	t.Helper()
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle, descriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatalf("compile: %v", err)
	}
	return m.Invoke(context.Background(), "test", fn, args, identity, "")
}

func queryDescriptor(name string) deploy.FunctionDescriptor {
	return deploy.FunctionDescriptor{Name: name, Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: name}
}

func mutationDescriptor(name string) deploy.FunctionDescriptor {
	return deploy.FunctionDescriptor{Name: name, Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: name}
}

func actionDescriptor(name string) deploy.FunctionDescriptor {
	return deploy.FunctionDescriptor{Name: name, Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: name}
}

func httpActionDescriptor(name string) deploy.FunctionDescriptor {
	return deploy.FunctionDescriptor{
		Name:       name,
		Type:       deploy.FunctionTypeHTTPAction,
		Visibility: deploy.FunctionVisibilityPublic,
		ModulePath: "x.js",
		ExportName: name,
		Route:      &deploy.FunctionRoute{Method: "POST", Path: "/echo"},
	}
}

func TestAuthPropagationUserIdentityAvailable(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"whoami",type:"query",visibility:"public",modulePath:"x.js",exportName:"whoami"},
			async function(ctx) { const id = await ctx.auth.getUserIdentity(); return id ? id.subject : null; }
		);
	})();`
	result, err := compileAndInvoke(t, bundle, []deploy.FunctionDescriptor{queryDescriptor("whoami")}, "whoami", nil, testIdentity())
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result != "rec_123" {
		t.Fatalf("got %v, want rec_123", result)
	}
}

func TestAuthPropagationNullWhenUnauthenticated(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"whoami",type:"query",visibility:"public",modulePath:"x.js",exportName:"whoami"},
			async function(ctx) { const id = await ctx.auth.getUserIdentity(); return id === null ? "anon" : "known"; }
		);
	})();`
	result, err := compileAndInvoke(t, bundle, []deploy.FunctionDescriptor{queryDescriptor("whoami")}, "whoami", nil, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result != "anon" {
		t.Fatalf("got %v, want anon", result)
	}
}

func TestNestedQueryFromActionSucceeds(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"inner",type:"query",visibility:"public",modulePath:"x.js",exportName:"inner"},
			async function(ctx, args) { return args.n * 2; }
		);
		__pbvex.registerFunction(
			{name:"outer",type:"action",visibility:"public",modulePath:"x.js",exportName:"outer"},
			async function(ctx, args) { return ctx.runQuery("inner", {n: args.n}); }
		);
	})();`
	result, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("inner"), actionDescriptor("outer")},
		"outer", map[string]any{"n": float64(21)}, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result != float64(42) && result != int64(42) && result != 42 {
		t.Fatalf("got %v (%T), want 42", result, result)
	}
}

func TestNestedInvocationUsesFreshModuleState(t *testing.T) {
	bundle := `(function(){
		let counter = 0;
		__pbvex.registerFunction({name:"inner",type:"query",visibility:"public",modulePath:"x.js",exportName:"inner"},
			function() { return counter; });
		__pbvex.registerFunction({name:"outer",type:"action",visibility:"public",modulePath:"x.js",exportName:"outer"},
			async function(ctx) { counter++; return [counter, await ctx.runQuery("inner", {})]; });
	})();`
	result, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("inner"), actionDescriptor("outer")}, "outer", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := result.([]any)
	if !ok || len(values) != 2 || fmt.Sprint(values[0]) != "1" || fmt.Sprint(values[1]) != "0" {
		t.Fatalf("nested call shared parent module state: %#v", result)
	}
}

func TestNestedInvocationAppliesTargetValidators(t *testing.T) {
	number := map[string]any{"type": "number"}
	descriptors := []deploy.FunctionDescriptor{
		{Name: "badArgs", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "badArgs", Args: number, Returns: number},
		{Name: "badReturn", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "badReturn", Returns: number},
		actionDescriptor("outerArgs"), actionDescriptor("outerReturn"),
	}
	bundle := `(function(){
		__pbvex.registerFunction({name:"badArgs",type:"query",visibility:"public",modulePath:"x.js",exportName:"badArgs",args:{type:"number"},returns:{type:"number"}}, function(ctx,args){ return args; });
		__pbvex.registerFunction({name:"badReturn",type:"query",visibility:"public",modulePath:"x.js",exportName:"badReturn",returns:{type:"number"}}, function(){ return "wrong"; });
		__pbvex.registerFunction({name:"outerArgs",type:"action",visibility:"public",modulePath:"x.js",exportName:"outerArgs"}, async function(ctx){ return ctx.runQuery("badArgs", "wrong"); });
		__pbvex.registerFunction({name:"outerReturn",type:"action",visibility:"public",modulePath:"x.js",exportName:"outerReturn"}, async function(ctx){ return ctx.runQuery("badReturn", {}); });
	})();`
	for _, name := range []string{"outerArgs", "outerReturn"} {
		if _, err := compileAndInvoke(t, bundle, descriptors, name, nil, nil); err == nil {
			t.Fatalf("%s bypassed target validators", name)
		}
	}
}

func TestReturnValidatorAppliesDefaults(t *testing.T) {
	returns := map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "fallback"}}}
	descriptor := queryDescriptor("defaulted")
	descriptor.Returns = returns
	bundle := `__pbvex.registerFunction({name:"defaulted",type:"query",visibility:"public",modulePath:"x.js",exportName:"defaulted",returns:{type:"object",shape:{name:{type:"defaulted",validator:{type:"string"},defaultValue:"fallback"}}}},function(){return {}});`
	result, err := compileAndInvoke(t, bundle, []deploy.FunctionDescriptor{descriptor}, "defaulted", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(map[string]any)
	if !ok || got["name"] != "fallback" {
		t.Fatalf("return default not applied: %#v", result)
	}
}

func TestNestedInvocationPreservesRequestMetadata(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction({name:"inner",type:"query",visibility:"public",modulePath:"x.js",exportName:"inner"}, async function(ctx){ const id=await ctx.auth.getUserIdentity(); return {requestId:ctx.requestId,subject:id.subject}; });
		__pbvex.registerFunction({name:"outer",type:"action",visibility:"public",modulePath:"x.js",exportName:"outer"}, async function(ctx){ return ctx.runQuery("inner", {}); });
	})();`
	m := NewManager(DefaultConfig())
	m.AddContextExtender(func(_ *goja.Runtime, ctx context.Context, _ core.App, _ deploy.FunctionDescriptor, obj *goja.Object) error {
		authCtx, _ := AuthFromContext(ctx)
		return obj.Set("requestId", authCtx.RequestID)
	})
	if err := m.Compile("metadata", bundle, []deploy.FunctionDescriptor{queryDescriptor("inner"), actionDescriptor("outer")}, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	result, err := m.Invoke(context.Background(), "metadata", "outer", nil, testIdentity(), "request-123")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(map[string]any)
	if !ok || got["requestId"] != "request-123" || got["subject"] != "rec_123" {
		t.Fatalf("nested metadata not preserved: %#v", result)
	}
}

func TestNestedQueryPreservesIdentity(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"inner",type:"query",visibility:"public",modulePath:"x.js",exportName:"inner"},
			async function(ctx) { const id = await ctx.auth.getUserIdentity(); return id ? id.subject : null; }
		);
		__pbvex.registerFunction(
			{name:"outer",type:"action",visibility:"public",modulePath:"x.js",exportName:"outer"},
			async function(ctx) { return ctx.runQuery("inner", {}); }
		);
	})();`
	result, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("inner"), actionDescriptor("outer")},
		"outer", map[string]any{}, testIdentity())
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result != "rec_123" {
		t.Fatalf("got %v, want rec_123", result)
	}
}

func TestQueriesCannotRunNested(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"inner",type:"query",visibility:"public",modulePath:"x.js",exportName:"inner"},
			async function(ctx) { return 1; }
		);
		__pbvex.registerFunction(
			{name:"outer",type:"query",visibility:"public",modulePath:"x.js",exportName:"outer"},
			async function(ctx) {
				if (ctx.runQuery) return "has-runQuery";
				return "no-runQuery";
			}
		);
	})();`
	result, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("inner"), queryDescriptor("outer")},
		"outer", nil, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result != "no-runQuery" {
		t.Fatalf("query context should not expose runQuery; got %v", result)
	}
}

func TestNestedKindEnforcement(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"mut",type:"mutation",visibility:"public",modulePath:"x.js",exportName:"mut"},
			async function(ctx) { return 1; }
		);
		__pbvex.registerFunction(
			{name:"outer",type:"action",visibility:"public",modulePath:"x.js",exportName:"outer"},
			async function(ctx) { return ctx.runQuery("mut", {}); }
		);
	})();`
	_, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{mutationDescriptor("mut"), actionDescriptor("outer")},
		"outer", nil, nil)
	if err == nil {
		t.Fatal("expected error calling mutation via runQuery")
	}
}

func TestHTTPActionCannotBeNested(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"webhook",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"webhook",route:{method:"POST",path:"/hook"}},
			async function(ctx, req) { return new Response(null, {status: 200}); }
		);
		__pbvex.registerFunction(
			{name:"outer",type:"action",visibility:"public",modulePath:"x.js",exportName:"outer"},
			async function(ctx) { return ctx.runAction("webhook", {}); }
		);
	})();`
	_, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{httpActionDescriptor("webhook"), actionDescriptor("outer")},
		"outer", nil, nil)
	if err == nil {
		t.Fatal("expected error nesting httpAction")
	}
}

func TestNestedDepthLimitExceeded(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"recurse",type:"action",visibility:"public",modulePath:"x.js",exportName:"recurse"},
			async function(ctx, args) {
				if (args.depth <= 0) return args.depth;
				return ctx.runAction("recurse", {depth: args.depth - 1});
			}
		);
	})();`
	_, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{actionDescriptor("recurse")},
		"recurse", map[string]any{"depth": 30}, nil)
	if err == nil {
		t.Fatal("expected depth limit error")
	}
}

func TestAsyncFunctionResolves(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"slow",type:"query",visibility:"public",modulePath:"x.js",exportName:"slow"},
			async function(ctx) { return new Promise(function(resolve) { setTimeout(function() { resolve(99); }, 5); }); }
		);
	})();`
	result, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("slow")},
		"slow", nil, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result != float64(99) && result != int64(99) && result != 99 {
		t.Fatalf("got %v (%T), want 99", result, result)
	}
}

func TestTimeoutInterruptsNeverSettlingPromise(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"hang",type:"query",visibility:"public",modulePath:"x.js",exportName:"hang"},
			async function(ctx) { return new Promise(function() {}); }
		);
	})();`
	m := NewManager(Config{PoolSize: 1, Timeout: 100 * time.Millisecond})
	if err := m.Compile("test", bundle, []deploy.FunctionDescriptor{queryDescriptor("hang")}, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := m.Invoke(ctx, "test", "hang", nil, nil, "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestInvokeHTTPRejectsNonHTTPAction(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"myquery",type:"query",visibility:"public",modulePath:"x.js",exportName:"myquery"},
			async function(ctx) { return 1; }
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle, []deploy.FunctionDescriptor{queryDescriptor("myquery")}, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "POST", URL: "/echo"}
	_, err := m.InvokeHTTP(context.Background(), "test", "myquery", env, nil, "")
	if err == nil {
		t.Fatal("expected error invoking non-httpAction via InvokeHTTP")
	}
	if !strings.Contains(err.Error(), "not an httpAction") {
		t.Fatalf("expected kind enforcement error, got: %v", err)
	}
}

func TestHTTPActionRequestResponseRoundtrip(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"echo",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"echo",route:{method:"POST",path:"/echo"}},
			async function(ctx, request) {
				var body = request.body ? new TextDecoder().decode(request.body) : "";
				return new Response(body, {status: 201, headers: {"x-custom": "val"}});
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle, []deploy.FunctionDescriptor{httpActionDescriptor("echo")}, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{
		Method:  "POST",
		URL:     "/api/pbvex/echo",
		Headers: map[string][]string{"Content-Type": {"text/plain"}},
		Body:    []byte("hello"),
	}
	resp, err := m.InvokeHTTP(context.Background(), "test", "echo", env, nil, "")
	if err != nil {
		t.Fatalf("invokeHTTP: %v", err)
	}
	if resp.Status != 201 {
		t.Fatalf("status %d, want 201", resp.Status)
	}
	if string(resp.Body) != "hello" {
		t.Fatalf("body %q, want hello", string(resp.Body))
	}
	if v, ok := resp.Headers["X-Custom"]; !ok || len(v) != 1 || v[0] != "val" {
		t.Fatalf("headers %v, want X-Custom=val", resp.Headers)
	}
}

func TestHTTPActionHeadersUseSharedBounds(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"headers",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"headers",route:{method:"POST",path:"/echo"}},
			function(ctx, request) {
				var headers = new Headers();
				for (var i = 0; i < Number(request.headers.get("x-count")); i++) headers.append("x-value", "v");
				return new Response("ok", {headers: headers});
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle, []deploy.FunctionDescriptor{httpActionDescriptor("headers")}, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	invoke := func(count string) (*deploy.HTTPResponseEnvelope, error) {
		return m.InvokeHTTP(context.Background(), "test", "headers", &deploy.HTTPRequestEnvelope{
			Method: "POST", URL: "/echo", Headers: map[string][]string{"X-Count": {count}},
		}, nil, "")
	}
	resp, err := invoke("100")
	if err != nil {
		t.Fatalf("exact header count rejected: %v", err)
	}
	if got := len(resp.Headers["X-Value"]); got != 100 {
		t.Fatalf("response header value count = %d, want 100", got)
	}
	if _, err := invoke("101"); err == nil {
		t.Fatal("header count max+1 accepted by JS Headers")
	}
	_, err = m.InvokeHTTP(context.Background(), "test", "headers", &deploy.HTTPRequestEnvelope{
		Method: "POST", URL: "/echo", Headers: map[string][]string{"Bad Name": {"x"}},
	}, nil, "")
	if err == nil {
		t.Fatal("invalid request header accepted by runtime")
	}
}

func TestHTTPActionHeadersRejectInvalidNameAndValue(t *testing.T) {
	for name, expression := range map[string]string{
		"name":  `new Response("bad", {headers: [["bad name", "x"]]})`,
		"value": `new Response("bad", {headers: [["x-test", "safe\r\ninjected"]]})`,
	} {
		t.Run(name, func(t *testing.T) {
			bundle := `__pbvex.registerFunction({name:"bad",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"bad",route:{method:"POST",path:"/echo"}},function(){return ` + expression + `;});`
			m := NewManager(DefaultConfig())
			if err := m.Compile("test", bundle, []deploy.FunctionDescriptor{httpActionDescriptor("bad")}, deploy.DefaultDeploymentConfig); err != nil {
				t.Fatal(err)
			}
			if _, err := m.InvokeHTTP(context.Background(), "test", "bad", &deploy.HTTPRequestEnvelope{Method: "POST", URL: "/echo"}, nil, ""); err == nil {
				t.Fatal("invalid response header accepted")
			}
		})
	}
}

func TestHTTPActionRejectsNonResponseReturn(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"bad",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"bad",route:{method:"POST",path:"/bad"}},
			async function(ctx, request) { return {not: "a response"}; }
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle, []deploy.FunctionDescriptor{httpActionDescriptor("bad")}, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "POST", URL: "/bad"}
	_, err := m.InvokeHTTP(context.Background(), "test", "bad", env, nil, "")
	if err == nil {
		t.Fatal("expected error for non-Response return")
	}
}

func TestHTTPActionRejectsInvalidStatus(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"badstatus",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"badstatus",route:{method:"GET",path:"/badstatus"}},
			async function(ctx, request) { return new Response(null, {status: 99}); }
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "badstatus", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "badstatus", Route: &deploy.FunctionRoute{Method: "GET", Path: "/badstatus"}}},
		deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "GET", URL: "/badstatus"}
	_, err := m.InvokeHTTP(context.Background(), "test", "badstatus", env, nil, "")
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestHTTPActionRejectsOversizedResponseBody(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"big",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"big",route:{method:"GET",path:"/big"}},
			async function(ctx, request) { return new Response(new Array(100).join("x"), {status: 200}); }
		);
	})();`
	cfg := deploy.DefaultDeploymentConfig
	cfg.MaxReturnValueBytes = 10
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "big", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "big", Route: &deploy.FunctionRoute{Method: "GET", Path: "/big"}}},
		cfg); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "GET", URL: "/big"}
	_, err := m.InvokeHTTP(context.Background(), "test", "big", env, nil, "")
	if err == nil {
		t.Fatal("expected error for oversized response body")
	}
}

func TestHTTPActionAuthPropagation(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"authcheck",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"authcheck",route:{method:"GET",path:"/authcheck"}},
			async function(ctx, request) {
				var id = await ctx.auth.getUserIdentity();
				return new Response(id ? id.subject : "anon", {status: 200});
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "authcheck", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "authcheck", Route: &deploy.FunctionRoute{Method: "GET", Path: "/authcheck"}}},
		deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "GET", URL: "/authcheck"}
	resp, err := m.InvokeHTTP(context.Background(), "test", "authcheck", env, testIdentity(), "")
	if err != nil {
		t.Fatalf("invokeHTTP: %v", err)
	}
	if string(resp.Body) != "rec_123" {
		t.Fatalf("body %q, want rec_123", string(resp.Body))
	}
}

func TestFunctionNotFoundError(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"real",type:"query",visibility:"public",modulePath:"x.js",exportName:"real"},
			async function(ctx) { return 1; }
		);
	})();`
	_, err := compileAndInvoke(t, bundle, []deploy.FunctionDescriptor{queryDescriptor("real")}, "nonexistent", nil, nil)
	if err == nil {
		t.Fatal("expected error for non-existent function")
	}
	if !errors.Is(err, err) && !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected registration error, got: %v", err)
	}
}

// TestNestedTotalWorkSharedAcrossSiblings verifies that the cumulative work
// budget is shared across sibling nested calls, not reset per-call.
func TestNestedTotalWorkSharedAcrossSiblings(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"noop",type:"query",visibility:"public",modulePath:"x.js",exportName:"noop"},
			async function(ctx) { return 1; }
		);
		__pbvex.registerFunction(
			{name:"fanout",type:"action",visibility:"public",modulePath:"x.js",exportName:"fanout"},
			async function(ctx, args) {
				var count = args.count;
				for (var i = 0; i < count; i++) {
					await ctx.runQuery("noop", {});
				}
				return count;
			}
		);
	})();`
	result, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("noop"), actionDescriptor("fanout")},
		"fanout", map[string]any{"count": float64(64)}, nil)
	if err != nil {
		t.Fatalf("64 siblings should succeed: %v", err)
	}
	if result != float64(64) && result != int64(64) {
		t.Fatalf("got %v, want 64", result)
	}
	_, err = compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("noop"), actionDescriptor("fanout")},
		"fanout", map[string]any{"count": float64(65)}, nil)
	if err == nil {
		t.Fatal("expected total work exceeded error for 65 siblings")
	}
}

// TestRequestBodyUsedSingleConsumption verifies that body consumption methods
// set bodyUsed and throw on re-consumption.
func TestRequestBodyUsedSingleConsumption(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"consume",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"consume",route:{method:"POST",path:"/consume"}},
			async function(ctx, request) {
				var first = await request.text();
				try {
					await request.text();
					return new Response("no-throw", {status: 500});
				} catch(e) {
					return new Response("bodyUsed=" + request.bodyUsed, {status: 200});
				}
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "consume", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "consume", Route: &deploy.FunctionRoute{Method: "POST", Path: "/consume"}}},
		deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "POST", URL: "http://localhost/consume", Body: []byte("data")}
	resp, err := m.InvokeHTTP(context.Background(), "test", "consume", env, nil, "")
	if err != nil {
		t.Fatalf("invokeHTTP: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("status %d, want 200 (re-consumption should throw)", resp.Status)
	}
	if string(resp.Body) != "bodyUsed=true" {
		t.Fatalf("body %q, want bodyUsed=true", string(resp.Body))
	}
}

// TestResponseBodyUsedSingleConsumption verifies Response bodyUsed tracking.
func TestResponseBodyUsedSingleConsumption(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"rcon",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"rcon",route:{method:"POST",path:"/rcon"}},
			async function(ctx, request) {
				var r = new Response("hello", {status: 200});
				await r.text();
				if (!r.bodyUsed) return new Response("bodyUsed-not-set", {status: 500});
				try {
					await r.text();
					return new Response("no-throw", {status: 500});
				} catch(e) {
					return new Response("ok", {status: 200});
				}
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "rcon", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "rcon", Route: &deploy.FunctionRoute{Method: "POST", Path: "/rcon"}}},
		deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "POST", URL: "http://localhost/rcon"}
	resp, err := m.InvokeHTTP(context.Background(), "test", "rcon", env, nil, "")
	if err != nil {
		t.Fatalf("invokeHTTP: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("status %d, want 200", resp.Status)
	}
	if string(resp.Body) != "ok" {
		t.Fatalf("body %q, want ok", string(resp.Body))
	}
}

// TestRequestGETHEADBodyIsNull verifies GET/HEAD requests have null body.
func TestRequestGETHEADBodyIsNull(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"getcheck",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"getcheck",route:{method:"GET",path:"/getcheck"}},
			async function(ctx, request) {
				return new Response(request.body === null ? "null" : "not-null", {status: 200});
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "getcheck", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "getcheck", Route: &deploy.FunctionRoute{Method: "GET", Path: "/getcheck"}}},
		deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "GET", URL: "http://localhost/getcheck"}
	resp, err := m.InvokeHTTP(context.Background(), "test", "getcheck", env, nil, "")
	if err != nil {
		t.Fatalf("invokeHTTP: %v", err)
	}
	if string(resp.Body) != "null" {
		t.Fatalf("body %q, want null for GET body", string(resp.Body))
	}
}

// TestHTTPRequestAbsoluteURL verifies that the httpAction handler receives an
// absolute URL in Request.url (F6 at runtime level: buildRequest must use the
// envelope URL as-is, which should be absolute from the API layer).
func TestHTTPRequestAbsoluteURL(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"urlcheck",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"urlcheck",route:{method:"GET",path:"/urlcheck"}},
			async function(ctx, request) {
				return new Response(request.url, {status: 200});
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "urlcheck", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "urlcheck", Route: &deploy.FunctionRoute{Method: "GET", Path: "/urlcheck"}}},
		deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "GET", URL: "https://example.com/api/pbvex/urlcheck"}
	resp, err := m.InvokeHTTP(context.Background(), "test", "urlcheck", env, nil, "")
	if err != nil {
		t.Fatalf("invokeHTTP: %v", err)
	}
	if string(resp.Body) != "https://example.com/api/pbvex/urlcheck" {
		t.Fatalf("url %q, want absolute URL echoed", string(resp.Body))
	}
}

// TestHTTPActionAuthorizationHeaderReachesHandler verifies that Authorization
// headers from the envelope reach the httpAction handler (F6).
func TestHTTPActionAuthorizationHeaderReachesHandler(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"authhdr",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"authhdr",route:{method:"GET",path:"/authhdr"}},
			async function(ctx, request) {
				var auth = request.headers.get("authorization");
				return new Response(auth || "none", {status: 200});
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "authhdr", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "authhdr", Route: &deploy.FunctionRoute{Method: "GET", Path: "/authhdr"}}},
		deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{
		Method:  "GET",
		URL:     "http://localhost/authhdr",
		Headers: map[string][]string{"Authorization": {"Bearer test-token"}},
	}
	resp, err := m.InvokeHTTP(context.Background(), "test", "authhdr", env, nil, "")
	if err != nil {
		t.Fatalf("invokeHTTP: %v", err)
	}
	if string(resp.Body) != "Bearer test-token" {
		t.Fatalf("body %q, want Bearer test-token", string(resp.Body))
	}
}

// TestHTTPResponseRejectsConsumedBody verifies that toHTTPResponse rejects
// a Response whose body has been consumed via text()/json()/arrayBuffer().
func TestHTTPResponseRejectsConsumedBody(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"consumed",type:"httpAction",visibility:"public",modulePath:"x.js",exportName:"consumed",route:{method:"POST",path:"/consumed"}},
			async function(ctx, request) {
				var r = new Response("hello", {status: 200});
				await r.text();
				return r;
			}
		);
	})();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle,
		[]deploy.FunctionDescriptor{{Name: "consumed", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "consumed", Route: &deploy.FunctionRoute{Method: "POST", Path: "/consumed"}}},
		deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	env := &deploy.HTTPRequestEnvelope{Method: "POST", URL: "http://localhost/consumed"}
	_, err := m.InvokeHTTP(context.Background(), "test", "consumed", env, nil, "")
	if err == nil {
		t.Fatal("expected error for consumed response body")
	}
	if !strings.Contains(err.Error(), "already been consumed") {
		t.Fatalf("expected bodyUsed error, got: %v", err)
	}
}

// TestNestedWorkBudgetNonrefundable verifies that failed/over-budget nested
// call attempts are not refunded: after 64 successful calls, the 65th AND
// all subsequent attempts are rejected even if earlier attempts errored.
func TestNestedWorkBudgetNonrefundable(t *testing.T) {
	bundle := `(function(){
		__pbvex.registerFunction(
			{name:"noop",type:"query",visibility:"public",modulePath:"x.js",exportName:"noop"},
			async function(ctx) { return 1; }
		);
		__pbvex.registerFunction(
			{name:"fanout",type:"action",visibility:"public",modulePath:"x.js",exportName:"fanout"},
			async function(ctx, args) {
				var count = args.count;
				var errors = 0;
				for (var i = 0; i < count; i++) {
					try {
						await ctx.runQuery("noop", {});
					} catch(e) {
						errors++;
					}
				}
				return errors;
			}
		);
	})();`
	// 64 calls should succeed with 0 errors
	result, err := compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("noop"), actionDescriptor("fanout")},
		"fanout", map[string]any{"count": float64(64)}, nil)
	if err != nil {
		t.Fatalf("64 calls should succeed: %v", err)
	}
	if result != float64(0) && result != int64(0) {
		t.Fatalf("expected 0 errors for 64 calls, got %v", result)
	}
	// 66 calls: the last 2 should error, and the 65 failed attempt must not
	// be refunded (still counts against the budget).
	result, err = compileAndInvoke(t, bundle,
		[]deploy.FunctionDescriptor{queryDescriptor("noop"), actionDescriptor("fanout")},
		"fanout", map[string]any{"count": float64(66)}, nil)
	if err != nil {
		t.Fatalf("invoke should succeed: %v", err)
	}
	if result != float64(2) && result != int64(2) {
		t.Fatalf("expected exactly 2 errors (65th+66th rejected, nonrefundable), got %v", result)
	}
}
