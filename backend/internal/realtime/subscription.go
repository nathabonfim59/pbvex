package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

// Subscription is a single realtime SSE subscription.
type Subscription struct {
	id   string
	path string
	args any
	snap *deploy.CallSnapshot

	requestID   string
	service     *deploy.Service
	broadcaster *Broadcaster

	w       http.ResponseWriter
	flusher *http.ResponseController

	ctx    context.Context
	cancel context.CancelFunc

	notify       chan struct{}
	done         chan struct{}
	pingInterval time.Duration

	lastSent     string
	maxEventSize int64
}

func (s *Subscription) run() {
	defer close(s.done)

	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()

	pending := true
	for {
		if pending {
			s.runOnce()
			pending = false
		}

		select {
		case <-s.notify:
			pending = true
		case <-ticker.C:
			s.sendPing()
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Subscription) runOnce() {
	if s.ctx.Err() != nil {
		return
	}

	if err := s.broadcaster.acquireQuery(s.ctx); err != nil {
		return
	}
	defer s.broadcaster.releaseQuery()

	// The subscription is pinned to the deployment snapshot resolved at
	// admission time. maxEventSize never changes mid-connection; activation
	// of a new deployment triggers ReconnectAll so the client reconnects
	// and re-negotiates.
	result, err := s.service.InvokeSnapshot(s.ctx, s.snap, s.args)

	if s.ctx.Err() != nil {
		return
	}

	var payload any
	if err != nil {
		payload = s.errorPayload(err)
	} else {
		payload = result
	}

	canonical, jsonErr := deploy.CanonicalJSON(payload)
	if jsonErr != nil {
		payload = s.internalErrorPayload()
		canonical, _ = deploy.CanonicalJSON(payload)
	}

	if s.lastSent == canonical {
		return
	}
	s.lastSent = canonical

	s.sendMessage(payload)
}

func (s *Subscription) sendMessage(payload any) {
	envelope := map[string]any{
		"data": map[string]any{
			"id":      s.id,
			"op":      "message",
			"payload": payload,
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	if int64(len(data)) > s.maxEventSize {
		payload = s.internalErrorPayload()
		data, err = json.Marshal(map[string]any{
			"data": map[string]any{
				"id":      s.id,
				"op":      "message",
				"payload": payload,
			},
		})
		if err != nil {
			return
		}
	}
	s.writeEvent(data)
}

func (s *Subscription) sendSubscribe() {
	envelope := map[string]any{
		"data": map[string]any{
			"id":           s.id,
			"op":           "subscribe",
			"maxEventSize": s.maxEventSize,
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	s.writeEvent(data)
}

func (s *Subscription) sendPing() {
	envelope := map[string]any{
		"data": map[string]any{
			"id": s.id,
			"op": "ping",
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	s.writeEvent(data)
}

func (s *Subscription) errorPayload(err error) map[string]any {
	code := deploy.ErrorCodeInternal
	message := "Function invocation failed."

	switch {
	case errors.Is(err, deploy.ErrForbidden),
		errors.Is(err, deploy.ErrFunctionNotFound),
		errors.Is(err, deploy.ErrDeploymentNotFound),
		errors.Is(err, deploy.ErrActiveNotFound):
		code = deploy.ErrorCodeNotFound
		message = "Function not found."
	case isArgumentSizeError(err):
		code = deploy.ErrorCodeBadRequest
		message = "Invalid function arguments."
	case isReturnValueSizeError(err):
		code = deploy.ErrorCodeInternal
		message = "Return value exceeds configured limit."
	case isTimeoutError(err):
		code = deploy.ErrorCodeInternal
		message = "Function invocation timed out."
	}

	return structuredErrorPayload(code, message, s.requestID)
}

func (s *Subscription) internalErrorPayload() map[string]any {
	return structuredErrorPayload(deploy.ErrorCodeInternal, "Internal server error.", s.requestID)
}

func structuredErrorPayload(code deploy.ErrorCode, message, requestID string) map[string]any {
	return map[string]any{
		"error":     true,
		"code":      string(code),
		"message":   message,
		"details":   []any{},
		"requestId": requestID,
	}
}

func (s *Subscription) writeEvent(data []byte) {
	if s.ctx.Err() != nil {
		return
	}

	if int64(len(data)) > s.maxEventSize {
		s.cancel()
		return
	}

	line := []byte("data: ")
	line = append(line, data...)
	line = append(line, '\n', '\n')

	if _, err := s.w.Write(line); err != nil {
		s.cancel()
		return
	}

	if err := s.flusher.Flush(); err != nil {
		s.cancel()
	}
}

func isArgumentSizeError(err error) bool {
	var vse *deploy.ValueSizeError
	if errors.As(err, &vse) && vse.Label == "function arguments" {
		return true
	}
	return false
}

func isReturnValueSizeError(err error) bool {
	var vse *deploy.ValueSizeError
	if errors.As(err, &vse) && vse.Label == "function return value" {
		return true
	}
	return false
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}
