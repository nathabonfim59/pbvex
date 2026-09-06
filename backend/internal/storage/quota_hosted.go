package storage

import (
	"context"

	"github.com/nathabonfim59/pbvex/backend/hosting/storagequota"
)

// hostedQuotaObserver implements QuotaObserver over the public
// hosting/storagequota client. Reservations, settlements, releases and
// credits are single wire attempts: bounded retries with identical
// identities are applied by the Service helpers, never by re-issuing a
// different operation.
type hostedQuotaObserver struct {
	client *storagequota.Client
}

// NewHostedQuotaObserver returns a QuotaObserver backed by the public
// storage byte quota client. The client must have completed a successful
// Handshake before the observer is used, mirroring the policy protocol's
// enabled-startup requirements.
func NewHostedQuotaObserver(client *storagequota.Client) QuotaObserver {
	if client == nil {
		return nil
	}
	return &hostedQuotaObserver{client: client}
}

// Reserve performs one reservation request. Denials become ErrQuotaDenied;
// transport, saturation and protocol failures become ErrQuotaUnavailable so
// callers fail closed either way. This method never retries.
func (o *hostedQuotaObserver) Reserve(ctx context.Context, req QuotaReservationRequest) (QuotaReservation, error) {
	decision, err := o.client.ReserveStorage(ctx, storagequota.ReserveStorageRequest{
		RequestID: req.OpID,
		Purpose:   string(req.Purpose),
		StorageID: req.StorageID,
		Key:       req.Key,
		Bytes:     req.WorstCase,
	})
	if err != nil {
		return nil, normalizeQuotaClientError(err)
	}
	if !decision.Allowed {
		return nil, ErrQuotaDenied
	}
	return hostedQuotaReservation{obs: o, id: decision.ReservationID}, nil
}

// Credit reports confirmed freed bytes for a deletion.
func (o *hostedQuotaObserver) Credit(ctx context.Context, req QuotaCreditRequest) error {
	_, err := o.client.CreditStorage(ctx, storagequota.CreditStorageRequest{
		EventID:   req.OpID,
		StorageID: req.StorageID,
		Key:       req.Key,
		Bytes:     req.Bytes,
	})
	if err != nil {
		return normalizeQuotaClientError(err)
	}
	return nil
}

// hostedQuotaReservation carries the observer and provider reservation id.
type hostedQuotaReservation struct {
	obs *hostedQuotaObserver
	id  string
}

func (r hostedQuotaReservation) ID() string { return r.id }

// Settle transitions the reservation to the actual stored byte count. The
// provider clamps the charge to the reserved bound.
func (r hostedQuotaReservation) Settle(ctx context.Context, actualBytes int64) error {
	_, err := r.obs.client.SettleStorage(ctx, storagequota.SettleStorageRequest{
		ReservationID: r.id,
		Bytes:         actualBytes,
	})
	if err != nil {
		return normalizeQuotaClientError(err)
	}
	return nil
}

// Release returns the reservation because the write definitively did not
// persist.
func (r hostedQuotaReservation) Release(ctx context.Context) error {
	_, err := r.obs.client.ReleaseStorage(ctx, storagequota.ReleaseStorageRequest{
		ReservationID: r.id,
	})
	if err != nil {
		return normalizeQuotaClientError(err)
	}
	return nil
}

// normalizeQuotaClientError maps every bounded client error onto the two
// quota error sentinels. Raw provider, transport or custom observer errors
// are never propagated: they may carry implementation detail or secrets,
// and callers only classify (fail closed) either way. Denials are mapped
// by the Reserve caller before this helper.
func normalizeQuotaClientError(err error) error {
	return ErrQuotaUnavailable
}
