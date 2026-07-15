package pbvex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
)

func TestOutboundHTTPContextIsActionOnlyAndReturnsBoundedResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test" {
			t.Fatalf("unexpected request: %s %q", r.Method, r.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"X-Result":     {"one", "two"},
				"Content-Type": {"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{"checkoutUrl":"https://pay.example/checkout/1"}`)),
		}, nil
	})}

	descriptors := []deploy.FunctionDescriptor{
		{Name: "checkout", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/payments.ts", ExportName: "checkout"},
		{Name: "inspect", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/payments.ts", ExportName: "inspect"},
		{Name: "inspectHTTP", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/payments.ts", ExportName: "inspectHTTP", Route: &deploy.FunctionRoute{Method: "GET", Path: "inspect-http"}},
	}
	bundle := fmt.Sprintf(`
__pbvex.registerFunction({name:"checkout",type:"action",visibility:"internal",modulePath:"pbvex/payments.ts",exportName:"checkout"}, async (ctx) => {
  const response = await ctx.http.send({url:%q,method:"POST",headers:{authorization:"Bearer test","content-type":"application/json"},body:"{}",timeoutMs:1000});
  return {statusCode:response.statusCode,header:response.headers["X-Result"],body:Array.from(response.body),json:response.json};
});
__pbvex.registerFunction({name:"inspect",type:"query",visibility:"internal",modulePath:"pbvex/payments.ts",exportName:"inspect"}, (ctx) => typeof ctx.http);
__pbvex.registerFunction({name:"inspectHTTP",type:"httpAction",visibility:"public",modulePath:"pbvex/payments.ts",exportName:"inspectHTTP",route:{method:"GET",path:"inspect-http"}}, (ctx) => new Response(typeof ctx.http.send));`, "https://payments.example.test/checkouts")
	manager := runtime.NewManager(runtime.DefaultConfig())
	manager.AddContextExtender(outboundHTTPExtender(client))
	if err := manager.Compile("outbound-http", bundle, descriptors); err != nil {
		t.Fatal(err)
	}

	result, err := manager.Invoke(context.Background(), "outbound-http", "checkout", nil)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := result.(map[string]any)
	if !ok || value["statusCode"] != int64(http.StatusOK) {
		t.Fatalf("unexpected response: %#v", result)
	}
	parsed, ok := value["json"].(map[string]any)
	if !ok || parsed["checkoutUrl"] != "https://pay.example/checkout/1" {
		t.Fatalf("unexpected JSON: %#v", value["json"])
	}
	queryResult, err := manager.Invoke(context.Background(), "outbound-http", "inspect", nil)
	if err != nil || queryResult != "undefined" {
		t.Fatalf("query http capability = %#v, %v", queryResult, err)
	}
	httpResult, err := manager.InvokeHTTP(context.Background(), "outbound-http", "inspectHTTP", &deploy.HTTPRequestEnvelope{Method: "GET", URL: "https://app.example.test/api/pbvex/inspect-http"}, nil, "")
	if err != nil || string(httpResult.Body) != "function" {
		t.Fatalf("http action capability = %#v, %v", httpResult, err)
	}
}

func TestOutboundHTTPRejectsRedirectsTimeoutsAndOversizedResponses(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/redirect":
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://example.test/target"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
		case "/slow":
			<-r.Context().Done()
			return nil, r.Context().Err()
		case "/large":
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxOutboundHTTPResponseBody+1)))}, nil
		default:
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("target"))}, nil
		}
	})}
	redirect, err := sendOutboundHTTPRequest(context.Background(), client, outboundHTTPOptions{
		url: "https://example.test/redirect", method: http.MethodGet, timeout: time.Second,
	})
	if err != nil || redirect["statusCode"] != http.StatusFound {
		t.Fatalf("redirect response = %#v, %v", redirect, err)
	}

	if _, err := sendOutboundHTTPRequest(context.Background(), client, outboundHTTPOptions{
		url: "https://example.test/slow", method: http.MethodGet, timeout: 5 * time.Millisecond,
	}); err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("timeout error = %v", err)
	}
	if _, err := sendOutboundHTTPRequest(context.Background(), client, outboundHTTPOptions{
		url: "https://example.test/large", method: http.MethodGet, timeout: time.Second,
	}); err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("large response error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestOutboundHTTPOptionsValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{"relative URL", map[string]any{"url": "/internal"}},
		{"userinfo", map[string]any{"url": "https://secret@example.com"}},
		{"method", map[string]any{"url": "https://example.com", "method": "CONNECT"}},
		{"timeout", map[string]any{"url": "https://example.com", "timeoutMs": float64(maxOutboundHTTPTimeout.Milliseconds() + 1)}},
		{"header", map[string]any{"url": "https://example.com", "headers": map[string]any{"Host": "evil.example"}}},
		{"body", map[string]any{"url": "https://example.com", "body": strings.Repeat("x", maxOutboundHTTPRequestBody+1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			vm := goja.New()
			if _, err := parseOutboundHTTPOptions(vm.ToValue(test.value)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
