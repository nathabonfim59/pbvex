// Package realtime implements the PBVex SSE realtime endpoint.
package realtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
)

// Config controls realtime behavior.
type Config struct {
	PingInterval         time.Duration
	MaxConnections       int
	MaxConnectionsPerIP  int
	MaxConcurrentQueries int
	MaxBodyBytes         int64
	MaxGETArgsBytes      int64
}

// safeAdd returns a+b, capping at math.MaxInt64 to avoid overflow.
func safeAdd(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

// DefaultConfig returns the default realtime configuration.
func DefaultConfig() Config {
	return Config{
		PingInterval:         30 * time.Second,
		MaxConnections:       1000,
		MaxConnectionsPerIP:  100,
		MaxConcurrentQueries: 100,
		// Body admission ceiling is the canonical args limit plus envelope
		// overhead so a maximally-configured deployment (maxFunctionArgsBytes
		// == MaxFunctionArgsLimit) plus the SSE envelope fits in one read.
		// The deployment-specific limit is enforced after manifest resolution.
		MaxBodyBytes:    safeAdd(deploy.MaxFunctionArgsLimit, deploy.MaxEventEnvelopeOverhead),
		MaxGETArgsBytes: deploy.MaxFunctionArgsLimit,
	}
}

// Broadcaster manages active realtime subscriptions and broadcasts invalidation
// events to them using a bounded, coalescing design.
type Broadcaster struct {
	service *deploy.Service
	config  Config

	mu   sync.RWMutex
	subs map[*Subscription]struct{}

	connsSem   chan struct{}
	queriesSem chan struct{}
	perIPMu    sync.Mutex
	perIPConns map[string]int

	// generation is incremented under mu on every ReconnectAll so that
	// subscriptions admitted on an older generation can detect the activation
	// gap and refuse to register.
	generation uint64
}

// NewBroadcaster creates a new realtime broadcaster.
func NewBroadcaster(service *deploy.Service, config Config) *Broadcaster {
	if config.PingInterval <= 0 {
		config.PingInterval = DefaultConfig().PingInterval
	}
	if config.MaxConnections <= 0 {
		config.MaxConnections = DefaultConfig().MaxConnections
	}
	if config.MaxConnectionsPerIP <= 0 {
		config.MaxConnectionsPerIP = DefaultConfig().MaxConnectionsPerIP
	}
	if config.MaxConcurrentQueries <= 0 {
		config.MaxConcurrentQueries = DefaultConfig().MaxConcurrentQueries
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = DefaultConfig().MaxBodyBytes
	}
	if config.MaxGETArgsBytes <= 0 {
		config.MaxGETArgsBytes = DefaultConfig().MaxGETArgsBytes
	}

	return &Broadcaster{
		service:    service,
		config:     config,
		subs:       make(map[*Subscription]struct{}),
		connsSem:   make(chan struct{}, config.MaxConnections),
		queriesSem: make(chan struct{}, config.MaxConcurrentQueries),
		perIPConns: make(map[string]int),
	}
}

// InvalidateAll notifies every active subscription to re-run its query.
// It is non-blocking and coalesces: slow subscriptions receive a single
// pending notification that will be processed after the current run.
func (b *Broadcaster) InvalidateAll() {
	b.mu.RLock()
	subs := make([]*Subscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	for _, s := range subs {
		select {
		case s.notify <- struct{}{}:
		default:
		}
	}
}

// ReconnectAll closes all active subscription connections so that clients
// reconnect and re-negotiate event-size limits with the newly active
// deployment. Called on activation/rollback where the pinned deployment
// snapshot (and its maxReturnValueBytes) may differ from the new one.
// The generation is incremented under the write lock so that subscriptions
// admitted before this call but not yet registered are rejected by
// subscribeWithFence.
func (b *Broadcaster) ReconnectAll() {
	b.mu.Lock()
	b.generation++
	subs := make([]*Subscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()

	for _, s := range subs {
		s.cancel()
	}
}

// admissionGeneration returns the current activation generation under the
// read lock. Callers record this value before admission and pass it to
// subscribeWithFence to detect activation during the admission gap.
func (b *Broadcaster) admissionGeneration() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.generation
}

// subscribeWithFence registers a subscription only if the activation
// generation has not changed since admission. Returns false (without
// registering) if a ReconnectAll occurred in the gap, so the caller can
// close the connection and force the client to reconnect.
func (b *Broadcaster) subscribeWithFence(s *Subscription, admissionGen uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.generation != admissionGen {
		return false
	}
	b.subs[s] = struct{}{}
	return true
}

func (b *Broadcaster) unsubscribe(s *Subscription) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
}

func (b *Broadcaster) acquireQuery(ctx context.Context) error {
	select {
	case b.queriesSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Broadcaster) releaseQuery() {
	<-b.queriesSem
}

func (b *Broadcaster) acquireConnPerIP(e *core.RequestEvent) (string, bool) {
	ip := e.Request.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	b.perIPMu.Lock()
	defer b.perIPMu.Unlock()
	if b.perIPConns[ip] >= b.config.MaxConnectionsPerIP {
		return ip, false
	}
	b.perIPConns[ip]++
	return ip, true
}

func (b *Broadcaster) releaseConnPerIP(ip string) {
	b.perIPMu.Lock()
	defer b.perIPMu.Unlock()
	if b.perIPConns[ip] > 0 {
		b.perIPConns[ip]--
	}
	if b.perIPConns[ip] == 0 {
		delete(b.perIPConns, ip)
	}
}

// Handle is the POST /api/pbvex/realtime handler (GET is retained as a
// strictly bounded compatibility fallback).
func (b *Broadcaster) Handle(e *core.RequestEvent) error {
	if !acceptsEventStream(e.Request.Header.Get("Accept")) {
		return ProtocolError(e, http.StatusNotAcceptable, deploy.ErrorCodeBadRequest, "Accept: text/event-stream is required.", nil)
	}

	select {
	case b.connsSem <- struct{}{}:
	default:
		return ProtocolError(e, http.StatusServiceUnavailable, deploy.ErrorCodeBadRequest, "Realtime connection limit reached.", nil)
	}
	defer func() { <-b.connsSem }()

	ip, ok := b.acquireConnPerIP(e)
	if !ok {
		return ProtocolError(e, http.StatusServiceUnavailable, deploy.ErrorCodeBadRequest, "Realtime per-IP connection limit reached.", nil)
	}
	defer b.releaseConnPerIP(ip)

	// Record the activation generation before admission so we can detect
	// an activation during the admission→registration gap.
	admissionGen := b.admissionGeneration()

	req, err := b.newRealtimeRequest(e)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(e.Request.Context())
	defer cancel()

	// SSE response headers must be written before the first event.
	e.Response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	e.Response.Header().Set("Cache-Control", "no-cache, no-transform")
	e.Response.Header().Set("Connection", "keep-alive")
	e.Response.Header().Set("X-Accel-Buffering", "no")
	e.Response.WriteHeader(http.StatusOK)

	sub := &Subscription{
		id:           req.id,
		path:         req.path,
		args:         req.args,
		snap:         req.snap,
		requestID:    RequestID(e),
		service:      b.service,
		broadcaster:  b,
		w:            e.Response,
		flusher:      http.NewResponseController(e.Response),
		ctx:          ctx,
		cancel:       cancel,
		notify:       make(chan struct{}, 1),
		done:         make(chan struct{}),
		pingInterval: b.config.PingInterval,
		maxEventSize: safeAdd(req.snap.Config.MaxReturnValueBytes, deploy.MaxEventEnvelopeOverhead),
	}

	if err := sub.flusher.Flush(); err != nil {
		return nil
	}

	// Send the subscribe envelope immediately so the client connection reaches
	// "connected" before the potentially slow query runs.
	sub.sendSubscribe()

	// Register with generation fence: if activation changed during admission,
	// reject the subscription so the client reconnects against the new deployment.
	if !b.subscribeWithFence(sub, admissionGen) {
		return nil
	}

	go sub.run()

	// Block until the client disconnects or the subscription shuts itself down.
	select {
	case <-sub.done:
	case <-ctx.Done():
		cancel()
	}
	<-sub.done

	b.unsubscribe(sub)
	return nil
}

// realtimeRequest holds validated request parameters.
type realtimeRequest struct {
	id   string
	path string
	args any
	snap *deploy.CallSnapshot
}

func (b *Broadcaster) newRealtimeRequest(e *core.RequestEvent) (*realtimeRequest, error) {
	method := e.Request.Method

	switch method {
	case http.MethodPost:
		return b.newRealtimeRequestPost(e)
	case http.MethodGet:
		return b.newRealtimeRequestGet(e)
	default:
		return nil, ProtocolError(e, http.StatusMethodNotAllowed, deploy.ErrorCodeBadRequest, "Method not allowed.", nil)
	}
}

func (b *Broadcaster) newRealtimeRequestPost(e *core.RequestEvent) (*realtimeRequest, error) {
	contentType := e.Request.Header.Get("Content-Type")
	if contentType == "" {
		return nil, ProtocolError(e, http.StatusUnsupportedMediaType, deploy.ErrorCodeBadRequest, "Content-Type is required.", nil)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return nil, ProtocolError(e, http.StatusUnsupportedMediaType, deploy.ErrorCodeBadRequest, "Content-Type must be application/json.", nil)
	}

	body, err := io.ReadAll(io.LimitReader(e.Request.Body, safeAdd(b.config.MaxBodyBytes, 1)))
	if err != nil {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
	}
	if int64(len(body)) > b.config.MaxBodyBytes {
		return nil, ProtocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Request body too large.", nil)
	}

	obj, err := parseJSONObjectStrict(body)
	if err != nil {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
	}

	if len(obj) == 0 {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Missing id, path, or args.", nil)
	}

	id, err := stringField(obj, "id")
	if err != nil {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid subscription id.", nil)
	}
	if !isValidSubscriptionID(id) {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid subscription id.", nil)
	}

	path, err := stringField(obj, "path")
	if err != nil {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid function path.", nil)
	}
	if !deploy.IsIdentifier(path) {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid function path.", nil)
	}

	rawArgs, ok := obj["args"]
	if !ok {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Missing args.", nil)
	}

	snap, err := b.service.ResolvePublicQuery(e.Request.Context(), path)
	if err != nil {
		return nil, ProtocolError(e, http.StatusNotFound, deploy.ErrorCodeNotFound, "Function not found.", nil)
	}

	// Enforce the deployment-specific body limit after manifest resolution so
	// a deployment with maxFunctionArgsBytes below the protocol ceiling still
	// rejects oversized bodies at the correct bound.
	deployBodyLimit := safeAdd(snap.Config.MaxFunctionArgsBytes, deploy.MaxEventEnvelopeOverhead)
	if int64(len(body)) > deployBodyLimit {
		return nil, ProtocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Request body too large.", nil)
	}

	args, err := parseCanonicalArgs(string(rawArgs), snap.Config.MaxFunctionArgsBytes)
	if err != nil {
		return nil, argsValidationError(e, err)
	}

	canonicalArgs, err := deploy.CanonicalJSON(args)
	if err != nil {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid function arguments.", nil)
	}

	if expected := deriveSubscriptionID(deploy.SupportedProtocolVersion, path, canonicalArgs); expected != id {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Subscription id does not match path and args.", nil)
	}

	return &realtimeRequest{id: id, path: path, args: args, snap: snap}, nil
}

func (b *Broadcaster) newRealtimeRequestGet(e *core.RequestEvent) (*realtimeRequest, error) {
	q := e.Request.URL.Query()

	if len(q["id"]) > 1 || len(q["path"]) > 1 || len(q["args"]) > 1 {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Duplicate query parameter.", nil)
	}

	id := q.Get("id")
	path := q.Get("path")
	rawArgs := q.Get("args")

	if id == "" || path == "" || rawArgs == "" {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Missing id, path, or args query parameter.", nil)
	}
	if !isValidSubscriptionID(id) {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid subscription id.", nil)
	}
	if !deploy.IsIdentifier(path) {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid function path.", nil)
	}
	if int64(len(rawArgs)) > b.config.MaxGETArgsBytes {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Args too large for GET.", nil)
	}

	snap, err := b.service.ResolvePublicQuery(e.Request.Context(), path)
	if err != nil {
		return nil, ProtocolError(e, http.StatusNotFound, deploy.ErrorCodeNotFound, "Function not found.", nil)
	}

	// Enforce the deployment-specific args limit after manifest resolution.
	if int64(len(rawArgs)) > snap.Config.MaxFunctionArgsBytes {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Args too large for GET.", nil)
	}

	args, err := parseCanonicalArgs(rawArgs, snap.Config.MaxFunctionArgsBytes)
	if err != nil {
		return nil, argsValidationError(e, err)
	}

	canonicalArgs, err := deploy.CanonicalJSON(args)
	if err != nil {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid function arguments.", nil)
	}

	if expected := deriveSubscriptionID(deploy.SupportedProtocolVersion, path, canonicalArgs); expected != id {
		return nil, ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Subscription id does not match path and args.", nil)
	}

	return &realtimeRequest{id: id, path: path, args: args, snap: snap}, nil
}

func stringField(obj map[string]json.RawMessage, key string) (string, error) {
	raw, ok := obj[key]
	if !ok {
		return "", errors.New("missing field")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

var (
	ErrArgsNotJSON      = errors.New("args is not valid JSON")
	ErrArgsNotCanonical = errors.New("args is not canonical JSON")
	ErrArgsInvalid      = errors.New("invalid args")
)

// parseCanonicalArgs parses the args value, validates the wire value, and
// canonicalizes it. It accepts semantically identical JSON (e.g. insertion-order
// objects, trailing .0, \u escapes) as long as it decodes to the same wire value.
func parseCanonicalArgs(raw string, maxArgsBytes int64) (any, error) {
	if int64(len(raw)) > maxArgsBytes {
		return nil, &deploy.ValueSizeError{Label: "function arguments", Limit: maxArgsBytes}
	}

	args, err := parseJSONStrict(raw)
	if err != nil {
		if errors.Is(err, errDuplicateJSONKey) {
			return nil, fmt.Errorf("%w: %v", ErrArgsInvalid, err)
		}
		return nil, ErrArgsNotJSON
	}

	if err := validateWireValue(args, 0); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArgsInvalid, err)
	}

	return args, nil
}

var errDuplicateJSONKey = errors.New("duplicate field")

// parseJSONStrict parses any JSON value and rejects duplicate object keys.
func parseJSONStrict(raw string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	v, err := parseJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("trailing data")
	}
	return v, nil
}

func parseJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}

	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := make(map[string]any)
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, errors.New("expected string key")
				}
				if _, seen := obj[key]; seen {
					return nil, errDuplicateJSONKey
				}
				val, err := parseJSONValue(dec)
				if err != nil {
					return nil, err
				}
				obj[key] = val
			}
			end, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if d, ok := end.(json.Delim); !ok || d != '}' {
				return nil, errors.New("expected object end")
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := parseJSONValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			end, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if d, ok := end.(json.Delim); !ok || d != ']' {
				return nil, errors.New("expected array end")
			}
			return arr, nil
		default:
			return nil, errors.New("unexpected delimiter")
		}
	case float64:
		return t, nil
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	case string, bool:
		return t, nil
	case nil:
		return nil, nil
	}
	return nil, errors.New("unexpected token")
}

func parseJSONObjectStrict(body []byte) (map[string]json.RawMessage, error) {
	if len(body) == 0 {
		return nil, errors.New("empty body")
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	t, err := dec.Token()
	if err != nil {
		return nil, errors.New("invalid JSON")
	}
	delim, ok := t.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("expected object")
	}

	obj := make(map[string]json.RawMessage)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, errors.New("invalid JSON")
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("expected string key")
		}
		if _, seen := obj[key]; seen {
			return nil, errors.New("duplicate key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, errors.New("invalid JSON")
		}
		obj[key] = raw
	}

	t, err = dec.Token()
	if err != nil {
		return nil, errors.New("invalid JSON")
	}
	delim, ok = t.(json.Delim)
	if !ok || delim != '}' {
		return nil, errors.New("expected object")
	}

	if dec.More() {
		return nil, errors.New("trailing data")
	}
	return obj, nil
}

func argsValidationError(e *core.RequestEvent, err error) error {
	var sizeErr *deploy.ValueSizeError
	if errors.As(err, &sizeErr) {
		return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid function arguments.", nil)
	}
	if errors.Is(err, ErrArgsNotJSON) || errors.Is(err, ErrArgsNotCanonical) || errors.Is(err, ErrArgsInvalid) {
		return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", nil)
	}
	return ProtocolError(e, http.StatusInternalServerError, deploy.ErrorCodeInternal, "Internal server error.", err)
}

// deriveSubscriptionID returns a bounded deterministic subscription ID from
// the protocol version, function path, and canonical JSON arguments.
func deriveSubscriptionID(version, path, canonicalArgs string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s:%s:%s", version, path, canonicalArgs)
	return hex.EncodeToString(h.Sum(nil))
}

// DeriveSubscriptionID is a test/export helper that canonicalizes args before
// deriving the subscription ID.
func DeriveSubscriptionID(version, path string, args any) string {
	canonical, err := deploy.CanonicalJSON(args)
	if err != nil {
		return ""
	}
	return deriveSubscriptionID(version, path, canonical)
}

func isValidSubscriptionID(id string) bool {
	return len(id) == 64 && deploy.IsSha256Hex(id)
}

// validateWireValue validates the protocol wire value shape.
// It accepts $integer and $bytes special objects, finite numbers, safe field
// names, and rejects cyclic references using a depth limit.
func validateWireValue(v any, depth int) error {
	if depth > deploy.MaxValueDepth {
		return fmt.Errorf("value depth exceeded")
	}
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case bool, string:
		return nil
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return fmt.Errorf("non-finite number")
		}
		return nil
	case float32:
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return fmt.Errorf("non-finite number")
		}
		return nil
	case int, int8, int16, int32, int64:
		return nil
	case uint, uint8, uint16, uint32, uint64:
		return nil
	case []any:
		for i, item := range val {
			if err := validateWireValue(item, depth+1); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		return nil
	case map[string]any:
		if len(val) == 1 {
			if raw, ok := val["$integer"].(string); ok {
				if err := validateBase64Integer(raw); err != nil {
					return err
				}
				return nil
			}
			if raw, ok := val["$bytes"].(string); ok {
				if err := validateBase64Bytes(raw); err != nil {
					return err
				}
				return nil
			}
		}
		for k, item := range val {
			if !isSafeWireKey(k) {
				return fmt.Errorf("invalid field name %q", k)
			}
			if err := validateWireValue(item, depth+1); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported value type %T", v)
}

func isSafeWireKey(k string) bool {
	if k == "" || len(k) > deploy.MaxFieldLength {
		return false
	}
	if strings.HasPrefix(k, "$") {
		return false
	}
	if k == "__proto__" || k == "constructor" || k == "prototype" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

// acceptsEventStream parses the Accept header as a list of media ranges and
// returns true only when a non-zero-q text/event-stream range is present.
// It rejects malformed q values and duplicate parameters.
func acceptsEventStream(accept string) bool {
	if accept == "" {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		mediaType, params, err := mime.ParseMediaType(part)
		if err != nil {
			continue
		}
		if mediaType != "text/event-stream" {
			continue
		}
		if q, ok := params["q"]; ok {
			if !isAcceptableQValue(q) {
				continue
			}
		}
		if hasDuplicateParameter(part) {
			continue
		}
		return true
	}
	return false
}

// isAcceptableQValue validates a q-value against the RFC 7231 grammar:
//
//	qvalue = ( "0" [ "." 0*3DIGIT ] ) / ( "1" [ "." 0*3("0") ] )
//
// and returns true only when the value is syntactically valid AND greater
// than zero. NaN, Inf, scientific notation, leading zeros, and out-of-range
// values are rejected.
func isAcceptableQValue(q string) bool {
	switch q {
	case "0", "0.", "0.0", "0.00", "0.000":
		return false // syntactically valid but q=0 means "not acceptable"
	case "1", "1.", "1.0", "1.00", "1.000":
		return true
	}

	dot := strings.IndexByte(q, '.')
	if dot == -1 {
		return false // no decimal point and not "0" or "1"
	}
	intPart := q[:dot]
	fracPart := q[dot+1:]

	if len(fracPart) == 0 || len(fracPart) > 3 {
		return false
	}
	for i := 0; i < len(fracPart); i++ {
		c := fracPart[i]
		if c < '0' || c > '9' {
			return false
		}
	}

	switch intPart {
	case "0":
		return true // 0.1 through 0.999 (any digits, all > 0)
	case "1":
		// 1.000 is the only valid "1" fraction; already handled non-zero
		// fractions above, so all digits must be '0'.
		for i := 0; i < len(fracPart); i++ {
			if fracPart[i] != '0' {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// hasDuplicateParameter reports whether a single Accept range contains a
// parameter key more than once.
func hasDuplicateParameter(part string) bool {
	semi := strings.Index(part, ";")
	if semi == -1 {
		return false
	}
	saw := make(map[string]bool)
	for _, p := range strings.Split(part[semi+1:], ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.Index(p, "=")
		key := p
		if eq != -1 {
			key = strings.TrimSpace(p[:eq])
		}
		if saw[key] {
			return true
		}
		saw[key] = true
	}
	return false
}

func validateBase64Integer(raw string) error {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(b) != 8 {
		return fmt.Errorf("malformed $integer")
	}
	if base64.StdEncoding.EncodeToString(b) != raw {
		return fmt.Errorf("non-canonical $integer")
	}
	return nil
}

func validateBase64Bytes(raw string) error {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("malformed $bytes")
	}
	if base64.StdEncoding.EncodeToString(b) != raw {
		return fmt.Errorf("non-canonical $bytes")
	}
	return nil
}

func canonicalJSON(v any) string {
	s, _ := deploy.CanonicalJSON(v)
	return s
}

// RequestID returns a bounded request ID, preferring the X-Request-Id header
// and falling back to a generated UUID. It strips or rejects unsafe values.
func RequestID(e *core.RequestEvent) string {
	id := e.Request.Header.Get("X-Request-Id")
	if isValidRequestID(id) {
		return id
	}
	return uuid.NewString()
}

func isValidRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}

// ProtocolError writes a structured PBVex error response.
func ProtocolError(e *core.RequestEvent, status int, code deploy.ErrorCode, message string, cause error) error {
	details := []any{}
	if cause != nil && code != deploy.ErrorCodeInternal {
		details = append(details, cause.Error())
	}
	return e.JSON(status, deploy.StructuredError{Error: true, Code: code, Message: message, Details: details, RequestID: RequestID(e)})
}
