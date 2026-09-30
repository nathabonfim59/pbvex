package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
)

// maxControlEntries bounds the subscribe plus unsubscribe entries of one
// session control request.
const maxControlEntries = 256

// session multiplexes many query subscriptions over one SSE stream. Browsers
// allow only ~6 concurrent HTTP/1.1 connections per origin, so a stream per
// subscription starves every other request once a page watches a few queries.
// The client opens one session stream and adds or removes subscriptions with
// short control requests that name the session id.
type session struct {
	id string
	// authKey is the auth record the stream was opened with. Control requests
	// must present the same record, and queries run as that identity.
	authKey string
	ctx     context.Context
	cancel  context.CancelFunc
	stream  *eventStream

	mu     sync.Mutex
	closed bool
	subs   map[string]*Subscription
	wg     sync.WaitGroup
}

func newSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func authKey(e *core.RequestEvent) string {
	if e.Auth == nil {
		return ""
	}
	return e.Auth.Collection().Id + "/" + e.Auth.Id
}

// HandleSession is the POST /api/pbvex/realtime/session handler. It opens a
// multiplexed SSE stream whose first event is {op: "session", id}.
func (b *Broadcaster) HandleSession(e *core.RequestEvent) error {
	release, err := b.admitStream(e)
	if err != nil {
		return err
	}
	defer release()

	admissionGen := b.admissionGeneration()

	id, err := newSessionID()
	if err != nil {
		return ProtocolError(e, http.StatusInternalServerError, deploy.ErrorCodeInternal, "Internal server error.", err)
	}

	ctx, cancel := context.WithCancel(e.Request.Context())
	defer cancel()

	writeStreamHeaders(e)
	sess := &session{
		id:      id,
		authKey: authKey(e),
		ctx:     ctx,
		cancel:  cancel,
		stream:  newEventStream(e.Response, cancel),
		subs:    make(map[string]*Subscription),
	}
	if err := sess.stream.flush(); err != nil {
		return nil
	}
	if !b.registerSession(sess, admissionGen) {
		return nil
	}
	defer b.closeSession(sess)

	// Announced only once registered, so a control request sent after the
	// client sees the id always finds the session.
	sess.sendControl("session")

	ticker := time.NewTicker(b.config.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sess.sendControl("ping")
		case <-ctx.Done():
			return nil
		}
	}
}

func (sess *session) sendControl(op string) {
	data, err := json.Marshal(map[string]any{"data": map[string]any{"id": sess.id, "op": op}})
	if err != nil {
		return
	}
	sess.stream.write(sess.ctx, data)
}

// sendError reports a subscription that could not be added as a message
// carrying a structured error, the same shape a failing query produces.
func (sess *session) sendError(id string, reqErr *requestError, requestID string) {
	data, err := json.Marshal(map[string]any{"data": map[string]any{
		"id":      id,
		"op":      "message",
		"payload": structuredErrorPayload(reqErr.code, reqErr.message, requestID),
	}})
	if err != nil {
		return
	}
	sess.stream.write(sess.ctx, data)
}

// registerSession fences the session against an activation that happened
// during admission, like subscribeWithFence.
func (b *Broadcaster) registerSession(sess *session, admissionGen uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.generation != admissionGen {
		return false
	}
	b.sessions[sess.id] = sess
	return true
}

func (b *Broadcaster) closeSession(sess *session) {
	b.mu.Lock()
	delete(b.sessions, sess.id)
	b.mu.Unlock()

	sess.mu.Lock()
	sess.closed = true
	sess.mu.Unlock()
	sess.cancel()
	sess.wg.Wait()
}

// HandleSessionControl is the POST /api/pbvex/realtime/session/subscriptions
// handler. The JSON body is {session, subscribe?: [{id, path, args}],
// unsubscribe?: [id]}. Unsubscribes apply first. A subscription that fails
// validation gets an error message on the session stream rather than failing
// the whole request.
func (b *Broadcaster) HandleSessionControl(e *core.RequestEvent) error {
	contentType := e.Request.Header.Get("Content-Type")
	if mediaType, _, err := mime.ParseMediaType(contentType); err != nil || mediaType != "application/json" {
		return ProtocolError(e, http.StatusUnsupportedMediaType, deploy.ErrorCodeBadRequest, "Content-Type must be application/json.", nil)
	}

	body, err := io.ReadAll(io.LimitReader(e.Request.Body, safeAdd(b.config.MaxBodyBytes, 1)))
	if err != nil {
		return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
	}
	if int64(len(body)) > b.config.MaxBodyBytes {
		return ProtocolError(e, http.StatusRequestEntityTooLarge, deploy.ErrorCodeBadRequest, "Request body too large.", nil)
	}

	obj, err := parseJSONObjectStrict(body)
	if err != nil {
		return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
	}

	sessionID, err := stringField(obj, "session")
	if err != nil || !isValidSubscriptionID(sessionID) {
		return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid session id.", nil)
	}

	var unsubscribe []string
	if raw, ok := obj["unsubscribe"]; ok {
		if err := json.Unmarshal(raw, &unsubscribe); err != nil {
			return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", nil)
		}
	}
	var subscribe []json.RawMessage
	if raw, ok := obj["subscribe"]; ok {
		if err := json.Unmarshal(raw, &subscribe); err != nil {
			return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", nil)
		}
	}
	if len(subscribe)+len(unsubscribe) > maxControlEntries {
		return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Too many subscription changes in one request.", nil)
	}
	for _, id := range unsubscribe {
		if !isValidSubscriptionID(id) {
			return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid subscription id.", nil)
		}
	}
	entries := make([]map[string]json.RawMessage, len(subscribe))
	for i, raw := range subscribe {
		entry, err := parseJSONObjectStrict(raw)
		if err != nil {
			return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid request body.", err)
		}
		// Errors are reported against the entry id, so it must be well formed.
		if id, err := stringField(entry, "id"); err != nil || !isValidSubscriptionID(id) {
			return ProtocolError(e, http.StatusBadRequest, deploy.ErrorCodeBadRequest, "Invalid subscription id.", nil)
		}
		entries[i] = entry
	}

	b.mu.RLock()
	sess := b.sessions[sessionID]
	b.mu.RUnlock()
	if sess == nil {
		return ProtocolError(e, http.StatusNotFound, deploy.ErrorCodeNotFound, "Realtime session not found.", nil)
	}
	if sess.authKey != authKey(e) {
		return ProtocolError(e, http.StatusForbidden, deploy.ErrorCodeForbidden, "Realtime session belongs to a different auth identity.", nil)
	}

	admissionGen := b.admissionGeneration()
	requestID := RequestID(e)

	for _, id := range unsubscribe {
		sess.remove(id)
	}
	for i, entry := range entries {
		req, reqErr := b.resolveSubscription(e.Request.Context(), entry, len(subscribe[i]))
		if reqErr != nil {
			id, _ := stringField(entry, "id")
			sess.sendError(id, reqErr, requestID)
			continue
		}
		if !b.addSessionSubscription(sess, req, requestID, admissionGen) {
			return ProtocolError(e, http.StatusNotFound, deploy.ErrorCodeNotFound, "Realtime session not found.", nil)
		}
	}

	return e.NoContent(http.StatusNoContent)
}

// addSessionSubscription starts req on the session stream. It returns false
// when the session is gone or must be closed, so the client reconnects.
func (b *Broadcaster) addSessionSubscription(sess *session, req *realtimeRequest, requestID string, admissionGen uint64) bool {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return false
	}
	if _, exists := sess.subs[req.id]; exists {
		sess.mu.Unlock()
		return true
	}
	if len(sess.subs) >= b.config.MaxSubscriptionsPerSession {
		sess.mu.Unlock()
		sess.sendError(req.id, &requestError{code: deploy.ErrorCodeBadRequest, message: "Realtime session subscription limit reached."}, requestID)
		return true
	}

	ctx, cancel := context.WithCancel(sess.ctx)
	sub := &Subscription{
		id:           req.id,
		path:         req.path,
		args:         req.args,
		snap:         req.snap,
		requestID:    requestID,
		service:      b.service,
		broadcaster:  b,
		stream:       sess.stream,
		ctx:          ctx,
		cancel:       cancel,
		notify:       make(chan struct{}, 1),
		done:         make(chan struct{}),
		maxEventSize: safeAdd(req.snap.Config.MaxReturnValueBytes, deploy.MaxEventEnvelopeOverhead),
	}
	// A deployment activated since the request was admitted may have resolved
	// req against the old snapshot: close the session so everything
	// resubscribes against the new one.
	if !b.subscribeWithFence(sub, admissionGen) {
		sess.mu.Unlock()
		cancel()
		sess.cancel()
		return false
	}
	sess.subs[req.id] = sub
	sess.wg.Add(1)
	sess.mu.Unlock()

	// Same ordering as Handle: registered before announcing, announced before
	// the first run.
	sub.sendSubscribe()
	go func() {
		defer sess.wg.Done()
		sub.run()
		b.unsubscribe(sub)
		sess.forget(sub)
	}()
	return true
}

func (sess *session) remove(id string) {
	sess.mu.Lock()
	sub := sess.subs[id]
	delete(sess.subs, id)
	sess.mu.Unlock()
	if sub != nil {
		sub.cancel()
	}
}

// forget drops sub if it is still the live subscription for its id; a
// resubscribe may already have replaced it.
func (sess *session) forget(sub *Subscription) {
	sess.mu.Lock()
	if sess.subs[sub.id] == sub {
		delete(sess.subs, sub.id)
	}
	sess.mu.Unlock()
}
