package storagequota

import (
	"context"

	"github.com/nathabonfim59/pbvex/backend/hosting"
)

// Client is a bounded client for the storage byte quota protocol. One
// Client instance is shared for the process lifetime; it is safe for
// concurrent use.
//
// The transport is the parent hosting.Client: HTTP/1.1 POST with JSON
// bodies over a persistent Unix socket, no redirects, proxies, compression
// or retries, bounded payload sizes, immediate ErrBusy on local saturation,
// and the parent's strict response decoding discipline. Requests travel
// through hosting.Client.Call, whose relative-endpoint path rules keep the
// target on this client's own /v1 socket tree. A Client either owns a root
// client (NewClient, for standalone single-protocol callers) or borrows the
// application's policy client (NewClientFromRoot, so one socket serves both
// protocols through a single connection pool and in-flight budget). A
// borrowed client never closes the shared root.
type Client struct {
	root  *hosting.Client
	owned bool
}

// NewClient validates the bootstrap configuration and returns a client that
// owns its root transport for an enabled hosting configuration. Zero
// Timeout selects the 2s default; zero MaxInFlight selects 32. The
// configuration is shared with the policy protocol: one socket serves both
// the parent hosting endpoints and the /v1/storage/ endpoints of this
// package. Embedders that already run a policy client on the same socket
// should prefer NewClientFromRoot so both protocols share one connection
// pool and in-flight budget instead of duplicating connections.
func NewClient(cfg hosting.Config) (*Client, error) {
	if !cfg.Enabled {
		return nil, hosting.ErrUnavailable
	}
	root, err := hosting.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{root: root, owned: true}, nil
}

// NewClientFromRoot returns a storage quota client that sends its requests
// through an already-running policy client for the same socket. The root
// client is borrowed: its connection pool, in-flight budget and Close
// remain owned by the caller, and this client's Close is deliberately a
// no-op so shutting down one protocol cannot close the other's transport. A
// nil root returns hosting.ErrUnavailable.
func NewClientFromRoot(root *hosting.Client) (*Client, error) {
	if root == nil {
		return nil, hosting.ErrUnavailable
	}
	return &Client{root: root}, nil
}

// Close releases idle socket connections of an owned root client. A
// borrowed client (NewClientFromRoot) leaves the shared root untouched.
func (c *Client) Close() {
	if c.owned {
		c.root.Close()
	}
}

// SupportsReserve reports whether a successful handshake lists the
// storage.reserve capability. Embedders must require it before installing
// quota enforcement: an enabled deployment whose provider cannot account
// bytes must fail startup instead of silently relying on a provider that
// denies (or cannot serve) every reservation.
func SupportsReserve(h hosting.Hello) bool {
	for _, c := range h.Capabilities {
		if c == CapabilityStorageReserve {
			return true
		}
	}
	return false
}

// Handshake performs the shared /v1/hello handshake. A version other than
// the supported one is a protocol error; there is no silent downgrade.
// Providers that serve storage quotas list the storage.reserve capability
// (check with SupportsReserve).
func (c *Client) Handshake(ctx context.Context) (hosting.Hello, error) {
	var h hosting.Hello
	err := c.root.Call(ctx, "hello", struct{}{}, &h)
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
	err := c.root.Call(ctx, "storage/reserve", r, &d)
	if err == nil && !validReserveDecision(d) {
		err = hosting.ErrProtocol
	}
	return d, err
}

// SettleStorage transitions a reservation to the actual stored byte count.
// Retries must reuse identical IDs and content. A provider must reject a
// settlement above the reserved bound as a conflict and keep the
// reservation for reconciliation instead of acknowledging an undercount.
func (c *Client) SettleStorage(ctx context.Context, r SettleStorageRequest) (SettleStorageAck, error) {
	var a SettleStorageAck
	if !validSettle(r) {
		return a, hosting.ErrProtocol
	}
	err := c.root.Call(ctx, "storage/settle", r, &a)
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
	err := c.root.Call(ctx, "storage/release", r, &a)
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
	err := c.root.Call(ctx, "storage/credit", r, &a)
	if err == nil && a.EventID != r.EventID {
		err = hosting.ErrProtocol
	}
	return a, err
}
