package storagequota

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
)

// Client is a bounded client for the storage byte quota protocol. One
// Client instance is shared for the process lifetime; it is safe for
// concurrent use.
//
// The transport intentionally mirrors the parent hosting.Client: HTTP/1.1
// POST with JSON bodies over a persistent Unix socket, no redirects,
// proxies, compression or retries, bounded payload sizes, immediate
// ErrBusy on local saturation, and identical response decoding discipline.
// SEAM NOTE for maintainers: exporting a generic
// `hosting.Client.Call(ctx, path, in, out)` (or a transport constructor)
// would let this package delegate all request framing; until that seam
// exists the small transport below stays in sync with the parent client's
// documented behavior and tests.
type Client struct {
	http  *http.Client
	slots chan struct{}
}

// NewClient validates the bootstrap configuration and returns a client for
// an enabled hosting configuration. Zero Timeout selects the 2s default;
// zero MaxInFlight selects 32. The configuration is shared with the policy
// protocol: one socket serves both the parent hosting endpoints and the
// /v1/storage/ endpoints of this package.
func NewClient(cfg hosting.Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, hosting.ErrUnavailable
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.MaxInFlight == 0 {
		cfg.MaxInFlight = 32
	}
	tr := &http.Transport{
		Proxy: nil, MaxConnsPerHost: cfg.MaxInFlight, MaxIdleConnsPerHost: cfg.MaxInFlight,
		IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: cfg.Timeout,
		MaxResponseHeaderBytes: 4096, DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: cfg.Timeout}).DialContext(ctx, "unix", cfg.SocketPath)
		},
	}
	return &Client{
		http:  &http.Client{Transport: tr, Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		slots: make(chan struct{}, cfg.MaxInFlight),
	}, nil
}

// Close releases idle socket connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

// call performs one bounded request. Each call has a total deadline from
// the client configuration and the caller context; cancellation may
// shorten it. Capacity is bounded: saturation returns hosting.ErrBusy
// without queueing.
func (c *Client) call(ctx context.Context, path string, in, out any) error {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return hosting.ErrBusy
	}
	b, err := json.Marshal(in)
	if err != nil || len(b) > hosting.MaxPayload {
		return hosting.ErrProtocol
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://policy/v1/"+path, bytes.NewReader(b))
	if err != nil {
		return hosting.ErrProtocol
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return hosting.ErrUnavailable
	}
	defer res.Body.Close()
	// Drain the bounded body before checking the status so the persistent
	// connection can be reused without a salvage race (see parent client).
	b, err = io.ReadAll(io.LimitReader(res.Body, hosting.MaxPayload+1))
	if res.StatusCode != http.StatusOK {
		return hosting.ErrUnavailable
	}
	if err != nil || len(b) > hosting.MaxPayload {
		return hosting.ErrProtocol
	}
	return decode(b, out)
}

func decode(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return hosting.ErrProtocol
	}
	return nil
}

// Handshake performs the shared /v1/hello handshake. A version other than
// the supported one is a protocol error; there is no silent downgrade.
func (c *Client) Handshake(ctx context.Context) (hosting.Hello, error) {
	var h hosting.Hello
	err := c.call(ctx, "hello", struct{}{}, &h)
	if err == nil && h.Version != hosting.Version {
		err = hosting.ErrProtocol
	}
	return h, err
}

// ReserveStorage requests an atomic worst-case byte reservation before a
// write. The decision must be checked by the caller even on a nil error;
// a denial prevents the write. This method does not retry: reservation
// attempts that fail closed are aborted, and a new attempt uses a new
// request ID.
func (c *Client) ReserveStorage(ctx context.Context, r ReserveStorageRequest) (ReserveStorageDecision, error) {
	var d ReserveStorageDecision
	if !validReserve(r) {
		return d, hosting.ErrProtocol
	}
	err := c.call(ctx, "storage/reserve", r, &d)
	if err == nil && !validReserveDecision(d) {
		err = hosting.ErrProtocol
	}
	return d, err
}

// SettleStorage transitions a reservation to the actual stored byte count.
// Retries must reuse identical IDs and content.
func (c *Client) SettleStorage(ctx context.Context, r SettleStorageRequest) (SettleStorageAck, error) {
	var a SettleStorageAck
	if !validSettle(r) {
		return a, hosting.ErrProtocol
	}
	err := c.call(ctx, "storage/settle", r, &a)
	if err == nil && (a.ReservationID != r.ReservationID || a.ChargedBytes < 0 || a.ChargedBytes > r.Bytes) {
		err = hosting.ErrProtocol
	}
	return a, err
}

// ReleaseStorage returns a reservation whose write definitively did not
// persist. Retries must reuse identical IDs and content.
func (c *Client) ReleaseStorage(ctx context.Context, r ReleaseStorageRequest) (ReleaseStorageAck, error) {
	var a ReleaseStorageAck
	if !validRelease(r) {
		return a, hosting.ErrProtocol
	}
	err := c.call(ctx, "storage/release", r, &a)
	if err == nil && a.ReservationID != r.ReservationID {
		err = hosting.ErrProtocol
	}
	return a, err
}

// CreditStorage reports confirmed freed bytes for one deletion. Retries
// must reuse identical IDs and content.
func (c *Client) CreditStorage(ctx context.Context, r CreditStorageRequest) (CreditStorageAck, error) {
	var a CreditStorageAck
	if !validCredit(r) {
		return a, hosting.ErrProtocol
	}
	err := c.call(ctx, "storage/credit", r, &a)
	if err == nil && a.EventID != r.EventID {
		err = hosting.ErrProtocol
	}
	return a, err
}
