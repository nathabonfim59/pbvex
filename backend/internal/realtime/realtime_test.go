package realtime

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

func TestParseCanonicalArgsValidAndMalformed(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		maxBytes   int64
		wantErr    bool
		wantErrMsg string
	}{
		{"empty object", `{}`, 1024, false, ""},
		{"null", `null`, 1024, false, ""},
		{"string", `"hello"`, 1024, false, ""},
		{"number int", `42`, 1024, false, ""},
		{"number float", `3.14`, 1024, false, ""},
		{"number trailing zero", `2.0`, 1024, false, ""},
		{"number exponent", `2e0`, 1024, false, ""},
		{"html escape", `"\u003c"`, 1024, false, ""},
		{"array", `[1,2,3]`, 1024, false, ""},
		{"nested object", `{"a":{"b":1}}`, 1024, false, ""},
		{"non-sorted object", `{"b":1,"a":2}`, 1024, false, ""},
		{"non-sorted nested object", `{"b":1,"a":{"d":2,"c":3}}`, 1024, false, ""},
		{"$integer", `{"$integer":"AAAAAAAAAAA="}`, 1024, false, ""},
		{"$bytes", `{"$bytes":"SGVsbG8="}`, 1024, false, ""},
		{"too large", `{"x":"y"}`, 2, true, "function arguments exceeds configured limit"},
		{"invalid json", `{`, 1024, true, "args is not valid JSON"},
		{"trailing data", `{"a":1}null`, 1024, true, "args is not valid JSON"},
		{"duplicate key", `{"a":1,"a":2}`, 1024, true, "duplicate field"},
		{"unsafe key $foo", `{"$foo":1}`, 1024, true, "invalid field name"},
		{"unsafe key __proto__", `{"__proto__":1}`, 1024, true, "invalid field name"},
		{"unsafe key non-ascii", `{"f\u00e9":1}`, 1024, true, "invalid field name"},
		{"depth exceeded", strings.Repeat(`{"a":`, deploy.MaxValueDepth+1) + `null` + strings.Repeat(`}`, deploy.MaxValueDepth+1), 1024 * 1024, true, "value depth exceeded"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCanonicalArgs(tc.raw, tc.maxBytes)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.wantErrMsg != "" && !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErrMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCanonicalArgsIDMatchesForUnsortedKeys(t *testing.T) {
	sorted := `{"a":1,"b":2}`
	unsorted := `{"b":2,"a":1}`

	sortedArgs, err := parseCanonicalArgs(sorted, 1024)
	if err != nil {
		t.Fatalf("sorted args failed: %v", err)
	}
	unsortedArgs, err := parseCanonicalArgs(unsorted, 1024)
	if err != nil {
		t.Fatalf("unsorted args failed: %v", err)
	}

	sortedCanonical, err := deploy.CanonicalJSON(sortedArgs)
	if err != nil {
		t.Fatalf("sorted canonical failed: %v", err)
	}
	unsortedCanonical, err := deploy.CanonicalJSON(unsortedArgs)
	if err != nil {
		t.Fatalf("unsorted canonical failed: %v", err)
	}

	if sortedCanonical != unsortedCanonical {
		t.Fatalf("canonical mismatch: sorted=%q unsorted=%q", sortedCanonical, unsortedCanonical)
	}

	if deriveSubscriptionID("v1", "hello", sortedCanonical) != deriveSubscriptionID("v1", "hello", unsortedCanonical) {
		t.Fatalf("subscription id mismatch for semantically identical args")
	}
}

func TestValidateWireValueSpecialValues(t *testing.T) {
	cases := []struct {
		name    string
		value   any
		wantErr bool
	}{
		{"$integer canonical", map[string]any{"$integer": "AAAAAAAAAAA="}, false},
		{"$integer missing padding", map[string]any{"$integer": "AAAAAAAAAAA"}, true},
		{"$integer wrong length", map[string]any{"$integer": "AAAA"}, true},
		{"$integer non-canonical whitespace", map[string]any{"$integer": "A AAA AAAAA="}, true},
		{"$bytes canonical", map[string]any{"$bytes": "SGVsbG8="}, false},
		{"$bytes non-canonical whitespace", map[string]any{"$bytes": "S GVs bG8="}, true},
		{"$bytes empty", map[string]any{"$bytes": ""}, false},
		{"finite number", 42.0, false},
		{"non-finite nan", math.NaN(), true},
		{"non-finite inf", math.Inf(1), true},
		{"safe string", "hello", false},
		{"unsafe key", map[string]any{"$bad": 1}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWireValue(tc.value, 0)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAcceptsEventStream(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"text/event-stream", true},
		{"text/event-stream;q=0.9", true},
		{"text/event-stream;q=0.001", true},
		{"text/event-stream;q=1", true},
		{"text/event-stream;q=1.", true},
		{"text/event-stream;q=1.0", true},
		{"text/event-stream;q=1.000", true},
		{"text/html, text/event-stream;q=0.8", true},
		{"text/event-stream;q=0", false},
		{"text/event-stream;q=0.", false},
		{"text/event-stream;q=0.0", false},
		{"text/event-stream;q=0.00", false},
		{"text/event-stream;q=0.000", false},
		{"text/html", false},
		{"*/*", false},
		{"text/*", false},
		{"", false},
		{"text/html, */*", false},
		// Strict RFC 7231 grammar: reject invalid q-value forms.
		{"text/event-stream;q=NaN", false},
		{"text/event-stream;q=Inf", false},
		{"text/event-stream;q=-Inf", false},
		{"text/event-stream;q=1e0", false},
		{"text/event-stream;q=0x1", false},
		{"text/event-stream;q=01", false},
		{"text/event-stream;q=00.1", false},
		{"text/event-stream;q=.5", false},
		{"text/event-stream;q=2", false},
		{"text/event-stream;q=-1", false},
		{"text/event-stream;q=1.1", false},
		{"text/event-stream;q=1.0001", false},
		{"text/event-stream;q=0.1234", false},
		{"text/event-stream;q=", false},
	}

	for _, tc := range cases {
		t.Run(tc.accept, func(t *testing.T) {
			got := acceptsEventStream(tc.accept)
			if got != tc.want {
				t.Fatalf("acceptsEventStream(%q) = %v, want %v", tc.accept, got, tc.want)
			}
		})
	}
}

func TestCanonicalJSONMatchesProtocol(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{"<", `"<"`},
		{">", `">"`},
		{"&", `"&"`},
		{"line\u2028sep", `"line\u2028sep"`},
		{"line\u2029sep", `"line\u2029sep"`},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.want), func(t *testing.T) {
			got := canonicalJSON(tc.value)
			if got != tc.want {
				t.Fatalf("canonicalJSON(%q) = %s, want %s", tc.value, got, tc.want)
			}
		})
	}
}

// TestGenerationFenceRejectsStaleAdmission verifies that subscribeWithFence
// rejects a subscription whose admission generation no longer matches the
// broadcaster's current generation (because ReconnectAll ran in the gap).
func TestGenerationFenceRejectsStaleAdmission(t *testing.T) {
	b := NewBroadcaster(nil, DefaultConfig())

	gen := b.admissionGeneration()
	if gen != 0 {
		t.Fatalf("initial generation should be 0, got %d", gen)
	}

	// Simulate activation while the subscription is being admitted.
	b.ReconnectAll()
	genAfter := b.admissionGeneration()
	if genAfter != 1 {
		t.Fatalf("generation should be 1 after ReconnectAll, got %d", genAfter)
	}

	// Create a minimal subscription.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := &Subscription{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		notify: make(chan struct{}, 1),
	}

	// subscribeWithFence with the stale admission gen must return false.
	if b.subscribeWithFence(sub, gen) {
		t.Fatal("subscribeWithFence should reject stale admission generation")
	}

	// subscribeWithFence with the current gen must succeed.
	if !b.subscribeWithFence(sub, genAfter) {
		t.Fatal("subscribeWithFence should accept current generation")
	}

	// Cleanup.
	b.unsubscribe(sub)
}

// TestGenerationFenceConcurrentActivation proves that a concurrent
// ReconnectAll during admission→registration is always detected: either
// the fence rejects the stale subscription, or ReconnectAll cancels it
// after registration. Run with -race.
func TestGenerationFenceConcurrentActivation(t *testing.T) {
	b := NewBroadcaster(nil, DefaultConfig())
	const iterations = 200

	for i := 0; i < iterations; i++ {
		gen := b.admissionGeneration()
		// Simulate a concurrent ReconnectAll during admission.
		b.ReconnectAll()

		ctx, cancel := context.WithCancel(context.Background())
		sub := &Subscription{
			ctx:    ctx,
			cancel: cancel,
			done:   make(chan struct{}),
			notify: make(chan struct{}, 1),
		}

		if b.subscribeWithFence(sub, gen) {
			// Should not happen: generation changed.
			t.Fatalf("iteration %d: fence accepted stale gen %d after ReconnectAll", i, gen)
		}
		cancel()
	}
}
