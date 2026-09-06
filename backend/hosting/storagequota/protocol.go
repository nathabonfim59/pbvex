// Package storagequota implements the provider-neutral PBVex storage byte
// quota protocol (version 1) on top of the same HTTP/JSON over Unix socket
// transport as the local policy protocol in the parent hosting package.
//
// The protocol reserves worst-case storage bytes at a host-owned service
// before any bytes are written, settles reservations to actual stored byte
// counts, releases reservations whose writes definitively did not persist,
// and credits confirmed freed bytes after deletions. Usage lives at the
// provider, never in the tenant database, so a tenant restore cannot reset
// consumed quota.
//
// This is an experimental foundation, not a complete hosted billing
// boundary. See docs/hosting-storage-quotas.md for the wire contract,
// delivery semantics and the reconciliation duties left to providers.
package storagequota

import (
	"github.com/nathabonfim59/pbvex/backend/hosting"
)

// Version is the supported storage quota protocol version.
const Version = "1"

// CapabilityStorageReserve is the capability a provider can grant or deny
// for storage byte reservations. A provider that does not list the
// capability in its hello response denies every reservation.
const CapabilityStorageReserve = "storage.reserve"

// Quota purposes. These are validated labels for accounting, not
// permission grants.
const (
	PurposeUpload  = "upload"
	PurposeVariant = "variant"
)

// Recommended denial codes for reservations.
const (
	CodeDenied         = "denied"
	CodeQuotaExhausted = "quota_exhausted"
	CodeSuspended      = "suspended"
)

// ReserveStorageRequest asks the provider to atomically reserve worst-case
// bytes before a write. RequestID is the idempotent identity of the
// reservation attempt: retries must reuse the same ID and body.
type ReserveStorageRequest struct {
	RequestID string `json:"requestId"`
	// Purpose is "upload" or "variant".
	Purpose string `json:"purpose"`
	// StorageID is the PBVex storage id when one exists (PBVex uploads and
	// variants). Native PocketBase record files have none and omit it.
	StorageID string `json:"storageId,omitempty"`
	// Key is the object key or prefix the write targets when known.
	Key   string `json:"key,omitempty"`
	Bytes int64  `json:"bytes"`
}

// ReserveStorageDecision mirrors the policy decision shape. Allowed
// decisions carry a nonempty ReservationID that callers pass to settle or
// release. A denied decision must prevent the write; its code is a bounded
// machine identifier.
type ReserveStorageDecision struct {
	Allowed       bool   `json:"allowed"`
	Code          string `json:"code"`
	PolicyVersion string `json:"policyVersion"`
	ReservationID string `json:"reservationId,omitempty"`
}

// SettleStorageRequest transitions a reservation to the actual stored byte
// count. Retries reuse identical IDs and content.
type SettleStorageRequest struct {
	ReservationID string `json:"reservationId"`
	Bytes         int64  `json:"bytes"`
}

// SettleStorageAck acknowledges one settlement. ChargedBytes echoes what
// the provider actually charged; a settlement above the reserved bound is
// rejected as a conflict, so it never exceeds the reservation.
type SettleStorageAck struct {
	ReservationID string `json:"reservationId"`
	ChargedBytes  int64  `json:"chargedBytes"`
}

// ReleaseStorageRequest returns a reservation because the write
// definitively did not persist. Uncertain writes must not be released.
type ReleaseStorageRequest struct {
	ReservationID string `json:"reservationId"`
}

// ReleaseStorageAck acknowledges one release.
type ReleaseStorageAck struct {
	ReservationID string `json:"reservationId"`
}

// CreditStorageRequest reports confirmed freed bytes for one deletion.
// EventID is the idempotent identity of the report.
type CreditStorageRequest struct {
	EventID   string `json:"eventId"`
	StorageID string `json:"storageId,omitempty"`
	Key       string `json:"key,omitempty"`
	Bytes     int64  `json:"bytes"`
}

// CreditStorageAck acknowledges one credit report.
type CreditStorageAck struct {
	EventID string `json:"eventId"`
}

// validObjectKey reports whether a storage object key or prefix satisfies
// the protocol identifier rules with an extended length bound.
func validObjectKey(s string) bool {
	return len(s) <= 256 && hosting.ValidToken(s)
}

func validReserve(r ReserveStorageRequest) bool {
	return hosting.ValidToken(r.RequestID) &&
		(r.Purpose == PurposeUpload || r.Purpose == PurposeVariant) &&
		optionalToken(r.StorageID) &&
		optionalKey(r.Key) &&
		r.Bytes >= 0
}

func validSettle(r SettleStorageRequest) bool {
	return hosting.ValidToken(r.ReservationID) && r.Bytes >= 0
}

func validRelease(r ReleaseStorageRequest) bool {
	return hosting.ValidToken(r.ReservationID)
}

func validCredit(r CreditStorageRequest) bool {
	return hosting.ValidToken(r.EventID) &&
		optionalToken(r.StorageID) &&
		optionalKey(r.Key) &&
		r.Bytes >= 0
}

func validReserveDecision(d ReserveStorageDecision) bool {
	return hosting.ValidToken(d.PolicyVersion) && hosting.ValidToken(d.Code) &&
		(!d.Allowed || hosting.ValidToken(d.ReservationID))
}

func optionalToken(s string) bool { return s == "" || hosting.ValidToken(s) }

func optionalKey(s string) bool { return s == "" || validObjectKey(s) }
