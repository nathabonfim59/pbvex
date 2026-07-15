package pbvex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/pocketbase/pocketbase/core"
)

const (
	defaultOutboundHTTPTimeout  = 10 * time.Second
	maxOutboundHTTPTimeout      = 30 * time.Second
	maxOutboundHTTPRequestBody  = 1 << 20
	maxOutboundHTTPResponseBody = 4 << 20
)

var outboundHTTPMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// outboundHTTPExtender exposes a bounded PocketBase-style ctx.http.send helper
// to actions and HTTP actions. Queries and mutations cannot perform external
// network side effects.
func outboundHTTPExtender(client *http.Client) runtime.ContextExtender {
	if client == nil {
		client = &http.Client{}
	}
	baseClient := *client
	baseClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return func(vm *goja.Runtime, ctx context.Context, _ core.App, fd deploy.FunctionDescriptor, obj *goja.Object) error {
		if fd.Type != deploy.FunctionTypeAction && fd.Type != deploy.FunctionTypeHTTPAction {
			return nil
		}
		httpObj := vm.NewObject()
		_ = httpObj.Set("send", vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return wrapPromise(vm, func() (any, error) {
				options, err := parseOutboundHTTPOptions(call.Argument(0))
				if err != nil {
					return nil, err
				}
				return sendOutboundHTTPRequest(ctx, &baseClient, options)
			})
		}))
		return obj.Set("http", httpObj)
	}
}

type outboundHTTPOptions struct {
	url     string
	method  string
	headers map[string][]string
	body    []byte
	timeout time.Duration
}

func parseOutboundHTTPOptions(value goja.Value) (outboundHTTPOptions, error) {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return outboundHTTPOptions{}, fmt.Errorf("http.send expects an options object")
	}
	raw, ok := value.Export().(map[string]any)
	if !ok {
		return outboundHTTPOptions{}, fmt.Errorf("http.send expects an options object")
	}
	for key := range raw {
		switch key {
		case "url", "method", "headers", "body", "timeoutMs":
		default:
			return outboundHTTPOptions{}, fmt.Errorf("http.send has unknown option %q", key)
		}
	}
	requestURL, _ := raw["url"].(string)
	parsed, err := url.Parse(requestURL)
	if err != nil || parsed.IsAbs() == false || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return outboundHTTPOptions{}, fmt.Errorf("http.send url must be an absolute http or https URL without user info")
	}

	method := http.MethodGet
	if supplied, ok := raw["method"].(string); ok && supplied != "" {
		method = strings.ToUpper(supplied)
	}
	if !outboundHTTPMethods[method] {
		return outboundHTTPOptions{}, fmt.Errorf("http.send method %q is not supported", method)
	}

	headers := map[string][]string{}
	if rawHeaders := raw["headers"]; rawHeaders != nil {
		values, ok := rawHeaders.(map[string]any)
		if !ok {
			return outboundHTTPOptions{}, fmt.Errorf("http.send headers must be a string map")
		}
		for name, value := range values {
			text, ok := value.(string)
			if !ok {
				return outboundHTTPOptions{}, fmt.Errorf("http.send header %q must be a string", name)
			}
			if strings.EqualFold(name, "host") || strings.EqualFold(name, "content-length") || strings.EqualFold(name, "connection") || strings.EqualFold(name, "transfer-encoding") {
				return outboundHTTPOptions{}, fmt.Errorf("http.send header %q is controlled by the runtime", name)
			}
			headers[name] = []string{text}
		}
	}
	if err := deploy.ValidateHTTPHeaders(headers); err != nil {
		return outboundHTTPOptions{}, fmt.Errorf("invalid http.send headers: %w", err)
	}

	body, err := outboundHTTPBody(raw["body"])
	if err != nil {
		return outboundHTTPOptions{}, err
	}
	if len(body) > maxOutboundHTTPRequestBody {
		return outboundHTTPOptions{}, fmt.Errorf("http.send request body exceeds %d bytes", maxOutboundHTTPRequestBody)
	}

	timeout := defaultOutboundHTTPTimeout
	if rawTimeout := raw["timeoutMs"]; rawTimeout != nil {
		milliseconds, ok := numericMilliseconds(rawTimeout)
		if !ok || milliseconds < 1 || milliseconds > maxOutboundHTTPTimeout.Milliseconds() {
			return outboundHTTPOptions{}, fmt.Errorf("http.send timeoutMs must be between 1 and %d", maxOutboundHTTPTimeout.Milliseconds())
		}
		timeout = time.Duration(milliseconds) * time.Millisecond
	}
	return outboundHTTPOptions{url: parsed.String(), method: method, headers: headers, body: body, timeout: timeout}, nil
}

func outboundHTTPBody(raw any) ([]byte, error) {
	if raw == nil {
		return nil, nil
	}
	switch value := raw.(type) {
	case string:
		return []byte(value), nil
	case []byte:
		return value, nil
	case goja.ArrayBuffer:
		return value.Bytes(), nil
	default:
		return nil, fmt.Errorf("http.send body must be a string, Uint8Array, or ArrayBuffer")
	}
}

func numericMilliseconds(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case float64:
		milliseconds := int64(value)
		return milliseconds, float64(milliseconds) == value
	default:
		return 0, false
	}
}

func sendOutboundHTTPRequest(parent context.Context, client *http.Client, options outboundHTTPOptions) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, options.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, options.method, options.url, bytes.NewReader(options.body))
	if err != nil {
		return nil, fmt.Errorf("http.send request: %w", err)
	}
	for name, values := range options.headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	requestClient := *client
	requestClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := requestClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http.send failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxOutboundHTTPResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("http.send response: %w", err)
	}
	if len(body) > maxOutboundHTTPResponseBody {
		return nil, fmt.Errorf("http.send response body exceeds %d bytes", maxOutboundHTTPResponseBody)
	}

	var parsedJSON any
	if len(body) > 0 && json.Unmarshal(body, &parsedJSON) != nil {
		parsedJSON = nil
	}
	headers := make(map[string][]string, len(response.Header))
	for name, values := range response.Header {
		headers[name] = append([]string(nil), values...)
	}
	if err := deploy.ValidateHTTPHeaders(headers); err != nil {
		return nil, fmt.Errorf("invalid http.send response headers: %w", err)
	}
	return map[string]any{
		"statusCode": response.StatusCode,
		"headers":    headers,
		"body":       body,
		"json":       parsedJSON,
	}, nil
}
