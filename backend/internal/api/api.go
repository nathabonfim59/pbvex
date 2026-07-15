package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/realtime"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/nathabonfim59/pbvex/backend/internal/scheduler"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/nathabonfim59/pbvex/backend/internal/storage"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// CORSConfig is the per-service CORS policy.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	AllowCredentials bool
	MaxAgeSeconds    int
	// AppURL is the canonical external origin (e.g. "https://app.example.com")
	// used to construct absolute Request.url values for httpAction handlers.
	// When empty, "http://localhost:8090" is used as fallback.
	AppURL string
}

// DefaultCORSConfig returns a sensible default CORS policy.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "X-Request-Id", "Authorization"},
		AllowCredentials: false,
		MaxAgeSeconds:    86400,
	}
}

// allowedOrigin returns the origin value to use for the response and whether
// it is allowed. A wildcard origin ("*") is never returned when credentials
// are enabled, as per the CORS specification.
func (c CORSConfig) allowedOrigin(origin string) (string, bool) {
	if origin == "" {
		return "", false
	}
	for _, o := range c.AllowedOrigins {
		if o == "*" {
			if c.AllowCredentials {
				continue
			}
			return "*", true
		}
		if strings.EqualFold(o, origin) {
			return origin, true
		}
	}
	return "", false
}

// validateAppURL validates and normalizes an AppURL candidate. It accepts
// only http/https URLs with a non-empty host and returns the canonical form
// scheme://host[:port][/base-path] with no trailing slash. The path is
// reconstructed from EscapedPath so percent-encoding (e.g. %2F, %20) is
// preserved exactly as supplied. Userinfo, query, and fragment are rejected.
func validateAppURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid AppURL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid AppURL %q: scheme must be http or https", raw)
	}
	if u.Opaque != "" {
		return "", fmt.Errorf("invalid AppURL %q: opaque URLs are not supported", raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("invalid AppURL %q: userinfo is not supported", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid AppURL %q: host is required", raw)
	}
	if u.ForceQuery || len(u.RawQuery) > 0 {
		return "", fmt.Errorf("invalid AppURL %q: query is not supported", raw)
	}
	// u.Fragment=="" does not imply no '#' delimiter: a bare trailing '#'
	// is consumed by url.Parse with an empty fragment. Detect the literal
	// delimiter in the raw string. Percent-encoded %23 in the path never
	// contains a '#' character so it is correctly accepted.
	if strings.Contains(raw, "#") {
		return "", fmt.Errorf("invalid AppURL %q: fragment is not supported", raw)
	}
	normalized := u.Scheme + "://" + u.Host
	if escaped := u.EscapedPath(); escaped != "" && escaped != "/" {
		normalized += strings.TrimRight(escaped, "/")
	}
	return normalized, nil
}

// resolveAppURL resolves and validates the AppURL for httpAction Request.url
// construction. An explicit CORSConfig.AppURL takes precedence; PocketBase
// Settings.Meta.AppURL is the fallback. A nonempty value from either source
// that fails validation is a startup error. An empty value (truly unset in
// both sources) is allowed and falls back to localhost:8090 in
// absoluteRequestURL.
func resolveAppURL(cors CORSConfig, app core.App) (CORSConfig, error) {
	raw := cors.AppURL
	source := "CORSConfig.AppURL"
	if raw == "" {
		raw = app.Settings().Meta.AppURL
		source = "Settings.Meta.AppURL"
	}
	if raw == "" {
		return cors, nil
	}
	normalized, err := validateAppURL(raw)
	if err != nil {
		return cors, fmt.Errorf("%s: %w", source, err)
	}
	cors.AppURL = normalized
	return cors, nil
}

// Register mounts the PBVex admin API and public endpoints.
func Register(app core.App, service *deploy.Service, bcast *realtime.Broadcaster, schedulerService *scheduler.Service, storageService *storage.Service, storageBasePath string, cors CORSConfig, devDeployToken string) {
	storageBasePath = normalizeBasePath(storageBasePath)
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id:       "pbvexApi",
		Priority: 0,
		Func: func(e *core.ServeEvent) error {
			// Resolve from the original captured cors each trigger so
			// that changed Settings.Meta.AppURL is reread when no
			// explicit override is configured. Never mutate cors.
			resolved, err := resolveAppURL(cors, app)
			if err != nil {
				return err
			}

			sub := e.Router.Group("/api/pbvex")
			sub.Bind(&hook.Handler[*core.RequestEvent]{Id: "pbvexRequestMetadata", Priority: -100, Func: func(e *core.RequestEvent) error {
				requestIDFor(e)
				return e.Next()
			}})
			sub.Bind(&hook.Handler[*core.RequestEvent]{Id: "pbvexProtocolAuth", Func: requireProtocolAuth(devDeployToken)})

			sub.POST("/deployments", handleUpload(service)).Bind(apis.BodyLimit(deploy.MaxUploadEnvelopeBytes))
			sub.GET("/deployments", handleList(service))
			sub.POST("/deployments/{id}/activate", handleActivate(service))
			sub.POST("/deployments/{id}/rollback", handleRollback(service))

			sub.GET("/jobs", handleListJobs(schedulerService))
			sub.GET("/jobs/{id}", handleGetJob(schedulerService))
			sub.POST("/jobs/{id}/cancel", handleCancelJob(schedulerService))
			sub.POST("/jobs/{id}/retry", handleRetryJob(schedulerService))

			// Public call endpoint (not gated by superuser auth).
			e.Router.POST("/api/pbvex/call", withPublicEndpoint(resolved, handleCall(service, resolved)))
			e.Router.OPTIONS("/api/pbvex/call", withPublicEndpoint(resolved, handlePreflight(resolved)))

			// POST is the primary bounded transport; GET remains the accepted
			// compatibility form. Both use the same pinned broadcaster.
			e.Router.POST("/api/pbvex/realtime", withPublicEndpoint(resolved, bcast.Handle))
			e.Router.GET("/api/pbvex/realtime", withPublicEndpoint(resolved, bcast.Handle))
			e.Router.OPTIONS("/api/pbvex/realtime", withPublicEndpoint(resolved, handlePreflight(resolved)))

			e.Router.POST(storageBasePath+"/upload/{token}", withPublicEndpoint(resolved, handleStorageUpload(storageService)))
			e.Router.OPTIONS(storageBasePath+"/upload/{token}", withPublicEndpoint(resolved, handlePreflight(resolved)))
			e.Router.GET(storageBasePath+"/{id}", withPublicEndpoint(resolved, handleStorageDownload(storageService)))
			e.Router.HEAD(storageBasePath+"/{id}", withPublicEndpoint(resolved, handleStorageDownload(storageService)))
			e.Router.OPTIONS(storageBasePath+"/{id}", withPublicEndpoint(resolved, handlePreflight(resolved)))

			// HTTP action catch-all routes under the stable PBVex API prefix.
			// Register concrete methods because Go's ServeMux rejects a methodless
			// catch-all alongside PocketBase's GET static-file fallback. HEAD maps
			// to deployed GET actions and OPTIONS handles CORS preflight requests.
			// The prefix is fixed so that route dispatch survives deployment
			// activation regardless of the manifest's configured httpPathPrefix.
			httpActionHandler := withPublicEndpoint(resolved, handleHTTPAction(service, storageBasePath, resolved))
			for _, method := range []string{
				http.MethodGet,
				http.MethodPost,
				http.MethodPut,
				http.MethodPatch,
				http.MethodDelete,
				http.MethodOptions,
			} {
				e.Router.Route(method, "/api/pbvex/{path...}", httpActionHandler)
			}

			return e.Next()
		},
	})
}

func handleUpload(service *deploy.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		limit := service.ActiveUploadEnvelopeBytes()
		if e.Request.ContentLength > limit {
			return protocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Upload exceeds active size limit.", nil)
		}
		if e.Request.Body != nil {
			e.Request.Body = http.MaxBytesReader(e.Response, e.Request.Body, limit)
		}
		var raw map[string]any
		if err := e.BindBody(&raw); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return protocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Upload exceeds active size limit.", nil)
			}
			return protocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
		}

		resp, err := service.UploadContext(e.Request.Context(), raw)
		if err != nil {
			return protocolServiceError(err, e)
		}

		return e.JSON(http.StatusCreated, resp)
	}
}

func handleList(service *deploy.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		resp, err := service.ListContext(e.Request.Context())
		if err != nil {
			return protocolServiceError(err, e)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func handleActivate(service *deploy.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		var raw map[string]any
		if err := e.BindBody(&raw); err != nil {
			return protocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
		}
		req, err := deploy.ValidateActivateRequest(raw)
		if err != nil {
			return protocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid activation request.", err)
		}
		resp, err := service.ActivateContext(e.Request.Context(), id, req.Atomic)
		if err != nil {
			return protocolServiceError(err, e)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func handleRollback(service *deploy.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		resp, err := service.RollbackContext(e.Request.Context(), id)
		if err != nil {
			return protocolServiceError(err, e)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func normalizeBasePath(basePath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return "/api/pbvex/storage"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	return strings.TrimRight(basePath, "/")
}

func handleListJobs(s *scheduler.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		limit := parseIntQuery(e.Request.URL.Query().Get("limit"), 50)
		if limit <= 0 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}
		result, err := s.List(schema.WithApp(e.Request.Context(), e.App), e.Request.URL.Query().Get("status"), limit, e.Request.URL.Query().Get("cursor"))
		if err != nil {
			return protocolServiceError(err, e)
		}
		return e.JSON(http.StatusOK, result)
	}
}

func parseIntQuery(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func handleGetJob(s *scheduler.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		result, err := s.Get(schema.WithApp(e.Request.Context(), e.App), e.Request.PathValue("id"))
		if err != nil {
			return protocolServiceError(err, e)
		}
		return e.JSON(http.StatusOK, result)
	}
}

func handleCancelJob(s *scheduler.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := s.Cancel(schema.WithApp(e.Request.Context(), e.App), e.Request.PathValue("id")); err != nil {
			return protocolServiceError(err, e)
		}
		return e.JSON(http.StatusOK, map[string]any{"canceled": true})
	}
}

func handleRetryJob(s *scheduler.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := s.Retry(schema.WithApp(e.Request.Context(), e.App), e.Request.PathValue("id")); err != nil {
			return protocolServiceError(err, e)
		}
		return e.JSON(http.StatusOK, map[string]any{"retried": true})
	}
}

func handleStorageUpload(s *storage.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		filename := e.Request.Header.Get("X-Upload-Filename")
		if filename == "" {
			filename = e.Request.URL.Query().Get("filename")
		}
		id, err := s.Upload(e.Request.Context(), e.Request.PathValue("token"), e.Request.Body, e.Request.Header.Get("Content-Type"), filename, e.Request.ContentLength)
		if err != nil {
			return storageServiceError(err, e)
		}
		return e.JSON(http.StatusOK, map[string]any{"storageId": id})
	}
}

func handleStorageDownload(s *storage.Service) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		identity := auth.IdentityFromRequest(e)
		authCtx := storage.AuthContext{}
		if identity != nil {
			authCtx.IsAuthenticated = true
			authCtx.TokenIdentifier = identity.TokenIdentifier
		}
		if err := s.Download(e.Response, e.Request, e.Request.PathValue("id"), authCtx); err != nil {
			return storageServiceError(err, e)
		}
		return nil
	}
}

func storageServiceError(err error, e *core.RequestEvent) error {
	status, code, message := http.StatusInternalServerError, deploy.ErrorCodeInternal, "Internal server error."
	var uploadErr *storage.UploadError
	if errors.As(err, &uploadErr) {
		message = uploadErr.Message
		switch uploadErr.Code {
		case storage.ErrorCodeBadRequest:
			status, code = http.StatusBadRequest, deploy.ErrorCodeBadRequest
		case storage.ErrorCodeUnauthorized:
			status, code = http.StatusUnauthorized, deploy.ErrorCodeUnauthorized
		case storage.ErrorCodeNotFound:
			status, code = http.StatusNotFound, deploy.ErrorCodeNotFound
		case storage.ErrorCodeForbidden:
			status, code = http.StatusForbidden, deploy.ErrorCodeForbidden
		case storage.ErrorCodeUploadTooLarge:
			status, code = http.StatusRequestEntityTooLarge, deploy.ErrorCodeUploadTooLarge
		case storage.ErrorCodeInvalidContent:
			status, code = http.StatusUnsupportedMediaType, deploy.ErrorCodeInvalidContent
		case storage.ErrorCodeUploadExpired:
			status, code = http.StatusUnauthorized, deploy.ErrorCodeUploadExpired
		case storage.ErrorCodeUploadConsumed:
			status, code = http.StatusUnauthorized, deploy.ErrorCodeUploadConsumed
		case storage.ErrorCodeUploadPending:
			status, code = http.StatusConflict, deploy.ErrorCodeUploadPending
		case storage.ErrorCodeStorageFull:
			status, code = http.StatusInsufficientStorage, deploy.ErrorCodeStorageFull
		}
	} else if errors.Is(err, storage.ErrStorageNotFound) {
		status, code, message = http.StatusNotFound, deploy.ErrorCodeNotFound, "File not found."
	} else if errors.Is(err, storage.ErrInvalidStorageID) {
		status, code, message = http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid storage id."
	}
	return protocolError(e, status, code, message, err)
}

func handleCall(service *deploy.Service, cors CORSConfig) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		bodyLimit := service.MaxFunctionArgsBytes()
		if bodyLimit > math.MaxInt64-deploy.MaxEventEnvelopeOverhead {
			bodyLimit = math.MaxInt64
		} else {
			bodyLimit += deploy.MaxEventEnvelopeOverhead
		}
		if e.Request.ContentLength > bodyLimit {
			return protocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Request body too large.", nil)
		}
		if e.Request.Body != nil {
			e.Request.Body = http.MaxBytesReader(e.Response, e.Request.Body, bodyLimit)
		}
		var raw map[string]any
		if err := e.BindBody(&raw); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return protocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Request body too large.", err)
			}
			return protocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
		}

		name, _ := raw["name"].(string)
		args := raw["args"]
		if name == "" {
			return protocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Missing function name.", nil)
		}

		// Strict envelope shape: only name and (optional) args are allowed.
		if len(raw) > 2 || (len(raw) == 2 && !containsKey(raw, "args")) {
			return protocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", nil)
		}

		requestID := requestIDFor(e)
		identity := auth.IdentityFromRequest(e)
		result, err := service.Call(e.Request.Context(), name, args, identity, requestID)
		if err != nil {
			return callServiceError(err, e)
		}
		setCORSHeaders(e, cors)
		return e.JSON(http.StatusOK, map[string]any{"result": result, "requestId": requestID})
	}
}

func callServiceError(err error, e *core.RequestEvent) error {
	if errors.Is(err, deploy.ErrFunctionNotFound) || errors.Is(err, deploy.ErrForbidden) ||
		errors.Is(err, deploy.ErrActiveNotFound) || errors.Is(err, deploy.ErrDeploymentNotFound) {
		return protocolError(e, http.StatusNotFound, deploy.ErrorCodeNotFound, "Function not found.", nil)
	}
	return protocolServiceError(err, e)
}

const apiPrefix = "/api/pbvex"

func handleHTTPAction(service *deploy.Service, storageBasePath string, cors CORSConfig) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		path := e.Request.URL.Path
		if !strings.HasPrefix(path, apiPrefix) {
			return protocolError(e, http.StatusNotFound, deploy.ErrorCodeNotFound, "Not found.", nil)
		}
		path = strings.TrimPrefix(path, apiPrefix)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if IsReservedPlatformPath(e.Request.URL.Path, storageBasePath) {
			return e.Next()
		}

		requestID := requestIDFor(e)
		identity := auth.IdentityFromRequest(e)
		headers := multiHeaders(e.Request.Header)
		if err := deploy.ValidateHTTPHeaders(headers); err != nil {
			return protocolError(e, http.StatusRequestHeaderFieldsTooLarge, deploy.ErrorCodeBadRequest, "Request headers too large or invalid.", nil)
		}

		method := e.Request.Method
		if method == http.MethodOptions && isPreflight(e) {
			matchMethod := e.Request.Header.Get("Access-Control-Request-Method")
			if matchMethod == http.MethodHead {
				matchMethod = http.MethodGet
			}
			if _, _, ok := service.MatchHTTPRouteContext(e.Request.Context(), matchMethod, path); ok {
				return handlePreflight(cors)(e)
			}
			return protocolError(e, http.StatusNotFound, deploy.ErrorCodeNotFound, "Not found.", nil)
		}

		// HEAD requests are matched as GET but the response must not include a body.
		matchMethod := method
		if matchMethod == http.MethodHead {
			matchMethod = http.MethodGet
		}

		limit := service.MaxFunctionArgsBytes()
		if e.Request.ContentLength > limit {
			return protocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Request body too large.", nil)
		}
		body, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, limit))
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return protocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Request body too large.", nil)
			}
			return protocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
		}

		envelope := &deploy.HTTPRequestEnvelope{
			Method:  method,
			URL:     absoluteRequestURL(e.Request, cors.AppURL),
			Headers: headers,
		}
		if len(body) > 0 {
			envelope.Body = body
		}

		resp, err := service.HTTPAction(e.Request.Context(), matchMethod, path, envelope, identity, requestID)
		if err != nil {
			return protocolServiceError(err, e)
		}
		return writeHTTPResponse(e, resp, cors)
	}
}

func handlePreflight(cors CORSConfig) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if !isPreflight(e) {
			e.Response.WriteHeader(http.StatusNoContent)
			return nil
		}
		origin := e.Request.Header.Get("Origin")
		allowedOrigin, ok := cors.allowedOrigin(origin)
		if !ok {
			e.Response.WriteHeader(http.StatusForbidden)
			return nil
		}

		h := e.Response.Header()
		h.Set("Access-Control-Allow-Origin", allowedOrigin)
		if allowedOrigin != "*" {
			h.Set("Vary", "Origin")
		}
		if cors.AllowCredentials {
			h.Set("Access-Control-Allow-Credentials", "true")
		}
		if len(cors.AllowedMethods) > 0 {
			h.Set("Access-Control-Allow-Methods", strings.Join(cors.AllowedMethods, ", "))
		}
		if len(cors.AllowedHeaders) > 0 {
			h.Set("Access-Control-Allow-Headers", strings.Join(cors.AllowedHeaders, ", "))
		}
		if cors.MaxAgeSeconds > 0 {
			h.Set("Access-Control-Max-Age", fmt.Sprintf("%d", cors.MaxAgeSeconds))
		}
		e.Response.WriteHeader(http.StatusNoContent)
		return nil
	}
}

func setCORSHeaders(e *core.RequestEvent, cors CORSConfig) {
	h := e.Response.Header()
	if h.Get("Access-Control-Allow-Origin") != "" {
		return
	}
	origin := e.Request.Header.Get("Origin")
	allowedOrigin, ok := cors.allowedOrigin(origin)
	if !ok {
		return
	}
	h.Set("Access-Control-Allow-Origin", allowedOrigin)
	if allowedOrigin != "*" {
		h.Set("Vary", "Origin")
	}
	if cors.AllowCredentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
	h.Set("Access-Control-Expose-Headers", "X-Request-Id")
}

func writeHTTPResponse(e *core.RequestEvent, resp *deploy.HTTPResponseEnvelope, cors CORSConfig) error {
	if err := deploy.ValidateHTTPHeaders(resp.Headers); err != nil {
		return protocolError(e, http.StatusInternalServerError, deploy.ErrorCodeInternal, "Internal server error.", err)
	}
	status := resp.Status
	if status < 100 || status > 599 {
		status = http.StatusInternalServerError
	}
	body := resp.Body
	if e.Request.Method == http.MethodHead || status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified {
		body = nil
	}

	h := e.Response.Header()
	for k, vs := range resp.Headers {
		canonical := http.CanonicalHeaderKey(k)
		if canonical == "" || isForbiddenResponseHeader(canonical) || isCORSResponseHeader(canonical) {
			continue
		}
		for _, v := range vs {
			h.Add(canonical, v)
		}
	}

	if body != nil && h.Get("Content-Type") == "" {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	}

	setCORSHeaders(e, cors)
	e.Response.WriteHeader(status)
	if body != nil {
		_, _ = e.Response.Write(body)
	}
	return nil
}

func isPreflight(e *core.RequestEvent) bool {
	return e.Request.Header.Get("Origin") != "" && e.Request.Header.Get("Access-Control-Request-Method") != ""
}

func isForbiddenResponseHeader(h string) bool {
	switch h {
	case "Connection", "Keep-Alive", "Transfer-Encoding", "Trailer", "Upgrade", "Proxy-Authenticate", "Proxy-Authorization", "Host", "Content-Length":
		return true
	}
	return false
}

// isCORSResponseHeader reports whether the header is a CORS response header
// that must be controlled exclusively by the server CORS policy, never by
// user-supplied action responses.
func isCORSResponseHeader(h string) bool {
	switch h {
	case "Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Expose-Headers", "Access-Control-Max-Age":
		return true
	}
	return false
}

type requestIDContextKey struct{}

func requestIDFor(e *core.RequestEvent) string {
	if e == nil || e.Request == nil {
		return uuid.NewString()
	}
	if requestID, ok := e.Request.Context().Value(requestIDContextKey{}).(string); ok && requestID != "" {
		e.Request.Header.Set("X-Request-Id", requestID)
		e.Response.Header().Set("X-Request-Id", requestID)
		return requestID
	}
	raw := e.Request.Header.Get("X-Request-Id")
	requestID := auth.SanitizedRequestID(raw)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	e.Request = e.Request.WithContext(context.WithValue(e.Request.Context(), requestIDContextKey{}, requestID))
	e.Request.Header.Set("X-Request-Id", requestID)
	e.Response.Header().Set("X-Request-Id", requestID)
	return requestID
}

func withPublicEndpoint(cors CORSConfig, next func(*core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		requestID := requestIDFor(e)
		identity := auth.IdentityFromRequest(e)
		e.Request = e.Request.WithContext(auth.WithInvocationMetadata(e.Request.Context(), identity, requestID))
		e.Request = e.Request.WithContext(runtime.WithAuthContext(e.Request.Context(), runtime.AuthContext{
			IsAuthenticated: identity != nil,
			TokenIdentifier: func() string {
				if identity == nil {
					return ""
				}
				return identity.TokenIdentifier
			}(),
			Identity:  identity,
			RequestID: requestID,
		}))
		setCORSHeaders(e, cors)
		return next(e)
	}
}

func IsReservedPlatformPath(path, storageBasePath string) bool {
	storageBasePath = normalizeBasePath(storageBasePath)
	for _, prefix := range []string{"/api/pbvex/call", "/api/pbvex/realtime", "/api/pbvex/deployments", "/api/pbvex/jobs", "/api/pbvex/admin", storageBasePath} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// isReservedPlatformPath retains the route-relative helper used by focused
// dispatch tests while production catch-all checks use the full path helper.
func isReservedPlatformPath(path string) bool {
	segment := strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(segment, '/'); i >= 0 {
		segment = segment[:i]
	}
	switch segment {
	case "call", "realtime", "deployments", "jobs", "storage", "admin":
		return true
	default:
		return false
	}
}

// absoluteRequestURL builds an absolute URL from the configured AppURL origin
// and the request path/query. Raw Host and X-Forwarded-Proto headers are
// never trusted; only the server-side AppURL determines the origin. When
// AppURL includes a base-path (e.g. "https://app.example.com/myapp"), it is
// preserved as a prefix to the request URI.
func absoluteRequestURL(r *http.Request, appURL string) string {
	base := appURL
	if base == "" {
		base = "http://localhost:8090"
	}
	return base + r.URL.RequestURI()
}

func multiHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs
		}
	}
	return out
}

func containsKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func requireProtocolAuth(devDeployToken string) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if e.Auth != nil && e.Auth.IsSuperuser() {
			return e.Next()
		}
		if validDevDeploymentRequest(e, devDeployToken) {
			e.Request = e.Request.WithContext(context.WithValue(e.Request.Context(), devDeploymentRequestKey{}, true))
			return e.Next()
		}
		return protocolError(e, http.StatusUnauthorized, deploy.ErrorCodeUnauthorized, "Unauthorized.", nil)
	}
}

type devDeploymentRequestKey struct{}

func isDevDeploymentRequest(e *core.RequestEvent) bool {
	return e != nil && e.Request != nil && e.Request.Context().Value(devDeploymentRequestKey{}) == true
}

func validDevDeploymentRequest(e *core.RequestEvent, expected string) bool {
	if expected == "" || e == nil || e.Request == nil {
		return false
	}
	path := e.Request.URL.Path
	if path != "/api/pbvex/deployments" && !strings.HasPrefix(path, "/api/pbvex/deployments/") {
		return false
	}
	host, _, err := net.SplitHostPort(e.Request.RemoteAddr)
	if err != nil {
		host = e.Request.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	const prefix = "Bearer "
	authorization := e.Request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, prefix) {
		return false
	}
	actual := strings.TrimPrefix(authorization, prefix)
	if len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func protocolServiceError(err error, e *core.RequestEvent) error {
	status, code, message := http.StatusInternalServerError, deploy.ErrorCodeInternal, "Internal server error."
	switch {
	case func() bool {
		var upload *deploy.UploadValidationError
		if errors.As(err, &upload) {
			status, code, message = http.StatusBadRequest, upload.Code, "Invalid deployment upload."
			return true
		}
		return false
	}():
	case errors.Is(err, deploy.ErrDeploymentNotFound), errors.Is(err, deploy.ErrActiveNotFound):
		status, code, message = http.StatusNotFound, deploy.ErrorCodeNotFound, "Deployment not found."
	case errors.Is(err, scheduler.ErrJobNotFound):
		status, code, message = http.StatusNotFound, deploy.ErrorCodeNotFound, "Job not found."
	case errors.Is(err, scheduler.ErrJobNotCancelable), errors.Is(err, scheduler.ErrJobNotRetryable), errors.Is(err, scheduler.ErrJobInvalidStatus), errors.Is(err, scheduler.ErrDeploymentSnapshotNotFound):
		status, code, message = http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Job state does not allow this operation."
	case errors.Is(err, deploy.ErrForbidden):
		status, code, message = http.StatusForbidden, deploy.ErrorCodeForbidden, "Forbidden."
	case errors.Is(err, deploy.ErrInvalidManifest):
		status, code, message = http.StatusBadRequest, deploy.ErrorCodeInvalidManifest, "Invalid manifest."
	case errors.Is(err, deploy.ErrInvalidBundle):
		status, code, message = http.StatusBadRequest, deploy.ErrorCodeInvalidFunction, "Invalid bundle."
	case errors.Is(err, deploy.ErrActivationFailed):
		status, code, message = http.StatusBadRequest, deploy.ErrorCodeActivationFailed, "Activation failed."
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusGatewayTimeout, deploy.ErrorCodeInternal, "Request timed out."
	case errors.Is(err, context.Canceled):
		status, code, message = http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Request canceled."
	default:
		status, code, message = http.StatusInternalServerError, deploy.ErrorCodeInternal, "Internal server error."
	}
	if e.App.IsDev() || isDevDeploymentRequest(e) {
		e.App.Logger().Error("PBVex protocol request failed", "requestId", requestIDFor(e), "code", code, "error", err)
	}
	if err != nil && isDevDeploymentRequest(e) {
		message = fmt.Sprintf("%s Cause: %v", message, err)
	}
	return protocolError(e, status, code, message, err)
}

func protocolError(e *core.RequestEvent, status int, code deploy.ErrorCode, message string, cause error) error {
	// Public envelopes are intentionally independent of wrapped Go/database
	// errors. Apart from leaking paths/SQL, exposing a cause makes missing and
	// internal functions distinguishable by clients.
	details := []any{}
	if cause != nil && code != deploy.ErrorCodeInternal {
		details = []any{"Request rejected."}
	}
	requestID := requestIDFor(e)
	return e.JSON(status, deploy.StructuredError{Error: true, Code: code, Message: message, Details: details, RequestID: requestID})
}
