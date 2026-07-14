package deploy

import (
	"strings"
	"testing"
)

func TestValidateHTTPHeadersBoundaries(t *testing.T) {
	exactName := strings.Repeat("a", MaxHTTPHeaderNameBytes)
	exactValue := strings.Repeat("v", MaxHTTPHeaderValueBytes)
	if err := ValidateHTTPHeaders(map[string][]string{exactName: {exactValue}}); err != nil {
		t.Fatalf("exact per-header limits rejected: %v", err)
	}
	if err := ValidateHTTPHeaders(map[string][]string{"x": {strings.Repeat("v", MaxHTTPHeaderValueBytes+1)}}); err == nil {
		t.Fatal("value max+1 accepted")
	}
	if err := ValidateHTTPHeaderName(strings.Repeat("a", MaxHTTPHeaderNameBytes+1)); err == nil {
		t.Fatal("name max+1 accepted")
	}
	if err := ValidateHTTPHeaders(map[string][]string{"bad name": {"x"}}); err == nil {
		t.Fatal("invalid token name accepted")
	}
	if err := ValidateHTTPHeaders(map[string][]string{"x": {"safe\r\ninjected"}}); err == nil {
		t.Fatal("CRLF header value accepted")
	}
}

func TestValidateHTTPHeadersCountAndAggregateLimits(t *testing.T) {
	values := make([]string, MaxHTTPHeaderCount)
	for i := range values {
		values[i] = "v"
	}
	if err := ValidateHTTPHeaders(map[string][]string{"x": values}); err != nil {
		t.Fatalf("exact count rejected: %v", err)
	}
	if err := ValidateHTTPHeaders(map[string][]string{"x": append(values, "v")}); err == nil {
		t.Fatal("count max+1 accepted")
	}

	aggregateValue := strings.Repeat("v", MaxHTTPHeaderValueBytes)
	aggregate := make([]string, MaxHTTPHeadersBytes/(len("x")+len(aggregateValue))+1)
	for i := range aggregate {
		aggregate[i] = aggregateValue
	}
	// Aggregate size is independently bounded even when individual values are valid.
	if err := ValidateHTTPHeaders(map[string][]string{"x": aggregate}); err == nil {
		t.Fatal("aggregate max+1 accepted")
	}
}
