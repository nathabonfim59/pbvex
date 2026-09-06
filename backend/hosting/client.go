// Package hosting implements the provider-neutral PBVex local policy protocol.
package hosting

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"
)

const Version = "1"
const MaxPayload = 16 << 10
const (
	SettingsStorage = "settings.storage.write"
	SettingsBackups = "settings.backups.write"
	SettingsSMTP    = "settings.smtp.write"
	BackupRestore   = "backup.restore"
	BackupCreate    = "backup.create"
	HostScripts     = "host.scripts"
	FunctionExecute = "function.execute"
)

var ErrUnavailable = errors.New("hosting policy unavailable")
var ErrProtocol = errors.New("invalid hosting protocol response")
var ErrBusy = errors.New("hosting client capacity exceeded")

// Config contains bootstrap configuration only. The service owns dynamic policy.
type Config struct {
	Enabled     bool
	SocketPath  string
	Timeout     time.Duration
	MaxInFlight int
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if !filepath.IsAbs(c.SocketPath) || len(c.SocketPath) > 100 {
		return errors.New("hosting requires an absolute Unix socket path of at most 100 bytes")
	}
	if c.Timeout < 0 || c.Timeout > 30*time.Second {
		return errors.New("hosting timeout must be between zero and 30s")
	}
	if c.MaxInFlight < 0 || c.MaxInFlight > 1024 {
		return errors.New("hosting max in-flight must be between zero and 1024")
	}
	return nil
}

type CheckRequest struct {
	Capability string `json:"capability"`
}
type Decision struct {
	Allowed       bool   `json:"allowed"`
	Code          string `json:"code"`
	PolicyVersion string `json:"policyVersion"`
	ReservationID string `json:"reservationId,omitempty"`
}
type Hello struct {
	Version        string   `json:"version"`
	Implementation string   `json:"implementation"`
	Capabilities   []string `json:"capabilities"`
}
type Operation struct {
	ID           string `json:"id"`
	RootID       string `json:"rootId"`
	ParentID     string `json:"parentId,omitempty"`
	SessionID    string `json:"sessionId"`
	Kind         string `json:"kind"`
	Origin       string `json:"origin"`
	DeploymentID string `json:"deploymentId,omitempty"`
	FunctionName string `json:"functionName,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
}
type AdmissionRequest struct {
	RequestID  string    `json:"requestId"`
	Capability string    `json:"capability"`
	Operation  Operation `json:"operation"`
}

// Event deliberately has no arbitrary payload or error-message field.
type Event struct {
	EventID        string    `json:"eventId"`
	Sequence       uint64    `json:"sequence"`
	Operation      Operation `json:"operation"`
	ReservationID  string    `json:"reservationId"`
	PolicyVersion  string    `json:"policyVersion"`
	Phase          string    `json:"phase"`
	Outcome        string    `json:"outcome,omitempty"`
	At             time.Time `json:"at"`
	DurationMicros int64     `json:"durationMicros"`
}
type Ack struct {
	EventID string `json:"eventId"`
}
type DeniedError struct{ Code string }

func (e *DeniedError) Error() string { return "hosting policy denied: " + e.Code }
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type Client struct {
	http  *http.Client
	slots chan struct{}
}

func NewClient(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, errors.New("hosting client requires enabled configuration")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.MaxInFlight == 0 {
		cfg.MaxInFlight = 32
	}
	tr := &http.Transport{Proxy: nil, MaxConnsPerHost: cfg.MaxInFlight, MaxIdleConnsPerHost: cfg.MaxInFlight, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: cfg.Timeout, MaxResponseHeaderBytes: 4096, DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: cfg.Timeout}).DialContext(ctx, "unix", cfg.SocketPath)
		}}
	return &Client{http: &http.Client{Transport: tr, Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, slots: make(chan struct{}, cfg.MaxInFlight)}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) call(ctx context.Context, path string, in, out any) error {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return ErrBusy
	}
	b, err := json.Marshal(in)
	if err != nil || len(b) > MaxPayload {
		return ErrProtocol
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://policy/v1/"+path, bytes.NewReader(b))
	if err != nil {
		return ErrProtocol
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	b, err = io.ReadAll(io.LimitReader(res.Body, MaxPayload+1))
	if err != nil || len(b) > MaxPayload {
		return ErrProtocol
	}
	return decode(b, out)
}
func decode(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return ErrProtocol
	}
	return nil
}
func (c *Client) Handshake(ctx context.Context) (Hello, error) {
	var h Hello
	err := c.call(ctx, "hello", struct{}{}, &h)
	if err == nil && h.Version != Version {
		err = ErrProtocol
	}
	return h, err
}
func validDecision(d Decision, reservation bool) bool {
	return token(d.PolicyVersion) && token(d.Code) && (!d.Allowed || !reservation || token(d.ReservationID))
}
func (c *Client) Check(ctx context.Context, capability string) (Decision, error) {
	var d Decision
	err := c.call(ctx, "check", CheckRequest{capability}, &d)
	if err == nil && !validDecision(d, false) {
		err = ErrProtocol
	}
	return d, err
}
func (c *Client) Require(ctx context.Context, capability string) error {
	d, err := c.Check(ctx, capability)
	if err != nil {
		return err
	}
	if !d.Allowed {
		return &DeniedError{d.Code}
	}
	return nil
}
func (c *Client) Admit(ctx context.Context, r AdmissionRequest) (Decision, error) {
	var d Decision
	if !validAdmission(r) {
		return d, ErrProtocol
	}
	err := c.call(ctx, "admit", r, &d)
	if err == nil && !validDecision(d, true) {
		err = ErrProtocol
	}
	return d, err
}
func (c *Client) Report(ctx context.Context, e Event) error {
	if !validEvent(e) {
		return ErrProtocol
	}
	var a Ack
	if err := c.call(ctx, "events", e, &a); err != nil {
		return err
	}
	if a.EventID != e.EventID {
		return ErrProtocol
	}
	return nil
}

// ValidToken reports whether s satisfies the protocol identifier rules
// (1-128 ASCII letters/digits or _ - . : /). Callers composing capability or
// operation identifiers from external input should validate them with this
// check before sending.
func ValidToken(s string) bool { return token(s) }

func token(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':' || r == '/') {
			return false
		}
	}
	return true
}
func optional(s string) bool { return s == "" || token(s) }
func validOperation(o Operation) bool {
	return token(o.ID) && token(o.RootID) && optional(o.ParentID) && token(o.SessionID) && token(o.Kind) && token(o.Origin) && optional(o.DeploymentID) && optional(o.FunctionName) && optional(o.Namespace)
}
func validAdmission(r AdmissionRequest) bool {
	return token(r.RequestID) && token(r.Capability) && validOperation(r.Operation)
}
func validEvent(e Event) bool {
	return token(e.EventID) && e.Sequence > 0 && validOperation(e.Operation) && token(e.ReservationID) && token(e.PolicyVersion) && !e.At.IsZero() && e.DurationMicros >= 0 && ((e.Phase == "started" && e.Outcome == "" && e.DurationMicros == 0) || (e.Phase == "completed" && (e.Outcome == "success" || e.Outcome == "error" || e.Outcome == "timeout" || e.Outcome == "canceled")) || (e.Phase == "released" && e.Outcome == "canceled" && e.DurationMicros == 0))
}
