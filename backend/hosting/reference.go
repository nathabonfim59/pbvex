package hosting

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"sync"
)

// ReferenceService is a bounded, in-memory example, NOT a durable billing ledger.
// One instance must be bound to one trusted tenant socket. No tenant ID is accepted.
type ReferenceService struct {
	mu         sync.Mutex
	policy     map[string]bool
	version    string
	limit      int
	admissions map[string]referenceAdmission
	events     map[string]Event
	states     map[string]string
}
type referenceAdmission struct {
	Request  AdmissionRequest
	Decision Decision
}

func NewReferenceService(limit int) *ReferenceService {
	if limit < 1 {
		limit = 1000
	}
	return &ReferenceService{policy: map[string]bool{}, version: "initial", limit: limit, admissions: map[string]referenceAdmission{}, events: map[string]Event{}, states: map[string]string{}}
}

// SetPolicy atomically replaces cached capabilities. Unknown capabilities deny.
func (s *ReferenceService) SetPolicy(version string, capabilities map[string]bool) error {
	if !token(version) {
		return ErrProtocol
	}
	p := map[string]bool{}
	for k, v := range capabilities {
		if !token(k) {
			return ErrProtocol
		}
		p[k] = v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = p
	s.version = version
	return nil
}
func (s *ReferenceService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxPayload))
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
		if decode(b, &req) != nil {
			fail(400)
			return
		}
		caps := []string{}
		for k := range s.policy {
			caps = append(caps, k)
		}
		out = Hello{Version, "pbvex-reference-memory", caps}
	case "/v1/check":
		var req CheckRequest
		if decode(b, &req) != nil || !token(req.Capability) {
			fail(400)
			return
		}
		out = s.decision(req.Capability)
	case "/v1/admit":
		var req AdmissionRequest
		if decode(b, &req) != nil || !validAdmission(req) {
			fail(400)
			return
		}
		if old, ok := s.admissions[req.RequestID]; ok {
			if !reflect.DeepEqual(old.Request, req) {
				fail(409)
				return
			}
			out = old.Decision
			break
		}
		if len(s.admissions) >= s.limit {
			fail(503)
			return
		}
		// Invocation identity cannot reserve twice under different request IDs.
		for _, old := range s.admissions {
			if old.Request.Operation.ID == req.Operation.ID && old.Request.Operation.SessionID == req.Operation.SessionID {
				fail(409)
				return
			}
		}
		d := s.decision(req.Capability)
		if d.Allowed {
			d.ReservationID = NewID()
			s.states[d.ReservationID] = "admitted"
		}
		s.admissions[req.RequestID] = referenceAdmission{req, d}
		out = d
	case "/v1/events":
		var e Event
		if decode(b, &e) != nil || !validEvent(e) {
			fail(400)
			return
		}
		if old, ok := s.events[e.EventID]; ok {
			if !reflect.DeepEqual(old, e) {
				fail(409)
				return
			}
			out = Ack{e.EventID}
			break
		}
		if len(s.events) >= s.limit*3 {
			fail(503)
			return
		}
		var admission *referenceAdmission
		for _, a := range s.admissions {
			if a.Decision.ReservationID == e.ReservationID {
				copy := a
				admission = &copy
				break
			}
		}
		if admission == nil || !reflect.DeepEqual(admission.Request.Operation, e.Operation) || admission.Decision.PolicyVersion != e.PolicyVersion {
			fail(409)
			return
		}
		for _, old := range s.events {
			if old.Operation.SessionID == e.Operation.SessionID && old.Sequence == e.Sequence {
				fail(409)
				return
			}
		}
		state := s.states[e.ReservationID]
		if !(state == "admitted" && (e.Phase == "started" || e.Phase == "released") || state == "started" && e.Phase == "completed") {
			fail(409)
			return
		}
		s.events[e.EventID] = e
		s.states[e.ReservationID] = e.Phase
		out = Ack{e.EventID}
	default:
		fail(426)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
func (s *ReferenceService) decision(capability string) Decision {
	d := Decision{Allowed: s.policy[capability], PolicyVersion: s.version, Code: "denied"}
	if d.Allowed {
		d.Code = "allowed"
	}
	return d
}
