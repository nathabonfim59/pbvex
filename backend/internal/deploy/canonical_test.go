package deploy

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCanonicalNumberSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../fixtures/canonical/numbers.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Value     float64 `json:"value"`
		Canonical string  `json:"canonical"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		got, err := CanonicalJSON(vector.Value)
		if err != nil {
			t.Fatal(err)
		}
		if got != vector.Canonical {
			t.Fatalf("%g: got %s want %s", vector.Value, got, vector.Canonical)
		}
	}
}
