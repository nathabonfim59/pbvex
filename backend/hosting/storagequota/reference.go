package storagequota

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/nathabonfim59/pbvex/backend/hosting"
)

// ReferenceQuotaService is a bounded, in-memory example of the storage
// byte quota protocol, NOT a durable ledger or a production billing
// implementation. One instance must be bound to one trusted tenant socket.
// It loses all state on restart, never evicts live records (it returns 503
// when full, while identical retries still work), and performs no
// reconciliation. Providers use it as a compatibility fixture or local
// development target, then implement a durable service behind the same
// routes.
type ReferenceQuotaService struct {
	mu            sync.Mutex
	limit         int
	capacityBytes int64
	usedBytes     int64
	reservations  map[string]*referenceReservation
	credits       map[string]CreditStorageRequest
}

type referenceReservation struct {
	Request  ReserveStorageRequest
	Decision ReserveStorageDecision
	Bytes    int64 // reserved bound; zero for denials
	Charged  int64 // settled usage
	Status   string
}

const (
	referenceReserved = "reserved"
	referenceSettled  = "settled"
	referenceReleased = "released"
)

// NewReferenceQuotaService returns a reference service that keeps at most
// limit reservation records and credit records, and enforces the given
// total byte capacity. A limit below 1 selects 1000.
func NewReferenceQuotaService(limit int, capacityBytes int64) *ReferenceQuotaService {
	if limit < 1 {
		limit = 1000
	}
	return &ReferenceQuotaService{
		limit:         limit,
		capacityBytes: capacityBytes,
		reservations:  map[string]*referenceReservation{},
		credits:       map[string]CreditStorageRequest{},
	}
}

// SetCapacity atomically replaces the total byte capacity. The change
// applies to the next reservation; existing records are unaffected.
func (s *ReferenceQuotaService) SetCapacity(capacityBytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.capacityBytes = capacityBytes
}

// UsedBytes reports the currently charged byte total. Monitoring helper;
// it is not part of the wire protocol.
func (s *ReferenceQuotaService) UsedBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usedBytes
}

func (s *ReferenceQuotaService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int) { w.WriteHeader(code); _, _ = w.Write([]byte(`{"code":"rejected"}`)) }
	if r.Method != http.MethodPost {
		fail(405)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		fail(415)
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, hosting.MaxPayload))
	if err != nil {
		fail(413)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out any
	switch r.URL.Path {
	case "/v1/hello":
		var req struct{}
		if decodeStrict(b, &req) != nil {
			fail(400)
			return
		}
		out = hosting.Hello{Version: Version, Implementation: "pbvex-reference-storagequota", Capabilities: []string{CapabilityStorageReserve}}
	case "/v1/storage/reserve":
		var req ReserveStorageRequest
		if decodeStrict(b, &req) != nil || !validReserve(req) {
			fail(400)
			return
		}
		if old, ok := s.reservations[req.RequestID]; ok {
			if old.Request != req {
				fail(409)
				return
			}
			out = old.Decision
			break
		}
		if len(s.reservations) >= s.limit {
			fail(503)
			return
		}
		// Atomic capacity check under the service lock: concurrent
		// reservations cannot push inflight plus settled usage past the
		// capacity.
		inflight := int64(0)
		for _, res := range s.reservations {
			if res.Status == referenceReserved {
				inflight += res.Bytes
			}
		}
		d := ReserveStorageDecision{Allowed: true, Code: "allowed", PolicyVersion: referencePolicyVersion}
		reservedBytes := int64(0)
		if s.usedBytes+inflight+req.Bytes > s.capacityBytes {
			d = ReserveStorageDecision{Allowed: false, Code: CodeQuotaExhausted, PolicyVersion: referencePolicyVersion}
		} else {
			d.ReservationID = hosting.NewID()
			// Only allowed reservations hold inflight capacity; denials
			// reserve nothing.
			reservedBytes = req.Bytes
		}
		s.reservations[req.RequestID] = &referenceReservation{Request: req, Decision: d, Bytes: reservedBytes, Status: referenceReserved}
		out = d
	case "/v1/storage/settle":
		var req SettleStorageRequest
		if decodeStrict(b, &req) != nil || !validSettle(req) {
			fail(400)
			return
		}
		res, ok := s.reservationByID(req.ReservationID)
		if !ok {
			fail(409)
			return
		}
		switch res.Status {
		case referenceSettled:
			if res.Charged != req.Bytes {
				// Identical retries acknowledge; conflicting bodies conflict.
				fail(409)
				return
			}
			out = SettleStorageAck{ReservationID: req.ReservationID, ChargedBytes: res.Charged}
		case referenceReserved:
			charged := req.Bytes
			if charged > res.Bytes {
				charged = res.Bytes
			}
			res.Status = referenceSettled
			res.Charged = charged
			s.usedBytes += charged
			out = SettleStorageAck{ReservationID: req.ReservationID, ChargedBytes: charged}
		default: // released or otherwise terminal
			fail(409)
			return
		}
	case "/v1/storage/release":
		var req ReleaseStorageRequest
		if decodeStrict(b, &req) != nil || !validRelease(req) {
			fail(400)
			return
		}
		res, ok := s.reservationByID(req.ReservationID)
		if !ok {
			fail(409)
			return
		}
		switch res.Status {
		case referenceReleased:
			out = ReleaseStorageAck{ReservationID: req.ReservationID}
		case referenceReserved:
			res.Status = referenceReleased
			out = ReleaseStorageAck{ReservationID: req.ReservationID}
		default: // settled
			fail(409)
			return
		}
	case "/v1/storage/credit":
		var req CreditStorageRequest
		if decodeStrict(b, &req) != nil || !validCredit(req) {
			fail(400)
			return
		}
		if old, ok := s.credits[req.EventID]; ok {
			if old != req {
				fail(409)
				return
			}
			out = CreditStorageAck{EventID: req.EventID}
			break
		}
		if len(s.credits) >= s.limit {
			fail(503)
			return
		}
		s.credits[req.EventID] = req
		s.usedBytes -= req.Bytes
		if s.usedBytes < 0 {
			// A provider must never report negative usage; over-credits
			// floor at zero and reconciliation corrects drift.
			s.usedBytes = 0
		}
		out = CreditStorageAck{EventID: req.EventID}
	default:
		fail(426)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

const referencePolicyVersion = "initial"

// reservationByID looks a reservation up by its reservation id. The
// reference implementation scans; a production provider indexes.
func (s *ReferenceQuotaService) reservationByID(id string) (*referenceReservation, bool) {
	for _, res := range s.reservations {
		if res.Decision.ReservationID == id {
			return res, true
		}
	}
	return nil, false
}

// decodeStrict decodes one JSON value with unknown-field and trailing-value
// rejection, matching the parent protocol's framing discipline.
func decodeStrict(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errStrictDecode
	}
	return nil
}

var errStrictDecode = errors.New("invalid storage quota protocol payload")
