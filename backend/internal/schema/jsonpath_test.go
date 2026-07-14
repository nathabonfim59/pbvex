package schema

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"sort"
	"testing"

	"github.com/pocketbase/pocketbase/tests"
)

func TestSQLiteJSONPathLiteralEscapesHostileKeys(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	for _, field := range []string{"dot.key", `quote"key`, "apost'rophe", "br[acket]", `x'); DROP TABLE _collections; --`} {
		t.Run(field, func(t *testing.T) {
			var row struct {
				Value string `db:"value"`
			}
			payload, err := json.Marshal(map[string]string{field: "ok"})
			if err != nil {
				t.Fatal(err)
			}
			query := "SELECT json_extract({:doc}, " + SQLiteJSONPathLiteral(field) + ") AS value"
			if err := app.DB().NewQuery(query).Bind(map[string]any{"doc": string(payload)}).One(&row); err != nil {
				t.Fatal(err)
			}
			if row.Value != "ok" {
				t.Fatalf("got %q", row.Value)
			}
		})
	}
}

func TestOrderKeyIsTotalForProtocolScalars(t *testing.T) {
	integer := func(n int64) map[string]any {
		var raw [8]byte
		binary.LittleEndian.PutUint64(raw[:], uint64(n))
		return map[string]any{"$integer": base64.StdEncoding.EncodeToString(raw[:])}
	}
	opaqueID := func(version int, namespace, table, raw string) string {
		payload, err := json.Marshal(struct {
			V int    `json:"v"`
			K int    `json:"k"`
			N string `json:"n"`
			T string `json:"t"`
			R string `json:"r"`
		}{V: version, K: 1, N: namespace, T: table, R: raw})
		if err != nil {
			t.Fatal(err)
		}
		prefix := "pbv1"
		if version == 2 {
			prefix = "pbv2"
		}
		return prefix + ".1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	}
	legacyID := opaqueID(1, "test", "items", "abcdefghijklmno")
	values := []struct {
		validator any
		value     any
		wantRank  string
	}{
		{map[string]any{"type": "null"}, nil, "01"},
		{map[string]any{"type": "boolean"}, false, "02"},
		{map[string]any{"type": "boolean"}, true, "03"},
		{map[string]any{"type": "number"}, -1.5, "04"},
		{map[string]any{"type": "number"}, 1.5, "04"},
		{map[string]any{"type": "string"}, "z", "05"},
		{map[string]any{"type": "id", "tableName": "items"}, legacyID, "06"},
		{map[string]any{"type": "int64"}, integer(-9), "07"},
		{map[string]any{"type": "int64"}, integer(9), "07"},
		{map[string]any{"type": "bytes"}, map[string]any{"$bytes": "AP8="}, "08"},
	}
	keys := make([]string, 0, len(values)+1)
	if _, err := OrderKey(map[string]any{"type": "string"}, nil, true); err == nil {
		t.Fatal("required string accepted a null index key")
	}
	missing, err := OrderKey(map[string]any{"type": "string"}, nil, false)
	if err != nil || missing != "00" {
		t.Fatalf("missing key %q, %v", missing, err)
	}
	keys = append(keys, missing)
	for _, tc := range values {
		key, err := OrderKey(tc.validator, tc.value, true)
		if err != nil || len(key) < 2 || key[:2] != tc.wantRank {
			t.Fatalf("key %#v: %q, %v", tc.value, key, err)
		}
		keys = append(keys, key)
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	for i := range keys {
		if keys[i] != sorted[i] {
			t.Fatalf("non-deterministic scalar order: %#v", keys)
		}
	}
	negative, _ := OrderKey(map[string]any{"type": "int64"}, integer(-9), true)
	positive, _ := OrderKey(map[string]any{"type": "int64"}, integer(9), true)
	if negative >= positive {
		t.Fatalf("int64 ordering was lexical: %q >= %q", negative, positive)
	}
	bytesA, _ := OrderKey(map[string]any{"type": "bytes"}, map[string]any{"$bytes": "AA=="}, true)
	bytesB, _ := OrderKey(map[string]any{"type": "bytes"}, map[string]any{"$bytes": "AP8="}, true)
	if bytesA >= bytesB {
		t.Fatalf("bytes ordering was not bytewise: %q >= %q", bytesA, bytesB)
	}
	union := map[string]any{"type": "union", "validators": []any{
		map[string]any{"type": "string"}, map[string]any{"type": "id", "tableName": "items"},
	}}
	unionString, err := OrderKey(union, "pbv1.not-an-id", true)
	if err != nil {
		t.Fatal(err)
	}
	unionID, err := OrderKey(union, legacyID, true)
	if err != nil || unionString[:2] != "05" || unionID[:2] != "06" {
		t.Fatalf("string/id union ranks collapsed: %q %q %v", unionString, unionID, err)
	}
	authenticated, err := OrderKeyWithID(union, legacyID, true, func(string, string) bool { return true })
	if err != nil || authenticated[:2] != "06" {
		t.Fatalf("authenticated id union rank %q, %v", authenticated, err)
	}
	shapedString, err := OrderKeyWithID(union, legacyID, true, func(string, string) bool { return false })
	if err != nil || shapedString[:2] != "05" {
		t.Fatalf("unauthenticated id-shaped string rank %q, %v", shapedString, err)
	}
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	for name, id := range map[string]string{
		"root":      opaqueID(2, "root", "items", "bcdefghijklmnop"),
		"component": opaqueID(2, "cmp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "items", "cdefghijklmnopq"),
	} {
		t.Run("pbv2 "+name, func(t *testing.T) {
			checked := false
			key, err := OrderKeyWithID(union, id, true, func(gotID, table string) bool {
				checked = true
				return gotID == id && table == "items"
			})
			if err != nil || key[:2] != orderID || !checked {
				t.Fatalf("authenticated id union rank %q checked=%v, %v", key, checked, err)
			}
			projection, err := OrderDataWithID(map[string]any{"v": union}, map[string]any{"v": id}, func(string, string) bool { return true })
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(projection)
			if err != nil {
				t.Fatal(err)
			}
			var row struct {
				Value string `db:"value"`
			}
			query := "SELECT json_extract({:doc}, " + SQLiteJSONPathLiteral("v") + ") AS value"
			if err := app.DB().NewQuery(query).Bind(map[string]any{"doc": string(encoded)}).One(&row); err != nil || row.Value != key {
				t.Fatalf("JSONPath order projection %q, %v", row.Value, err)
			}
			key, err = OrderKeyWithID(union, id, true, func(string, string) bool { return false })
			if err != nil || key[:2] != orderString {
				t.Fatalf("unauthenticated id-shaped string rank %q, %v", key, err)
			}
		})
	}
	rootID := opaqueID(2, "root", "items", "defghijklmnopqr")
	for name, id := range map[string]string{
		"noncanonical": rootID + "=",
		"wrong table":  opaqueID(2, "root", "other", "efghijklmnopqrs"),
	} {
		t.Run("pbv2 "+name+" remains string", func(t *testing.T) {
			checked := false
			key, err := OrderKeyWithID(union, id, true, func(string, string) bool {
				checked = true
				return true
			})
			if err != nil || key[:2] != orderString || checked {
				t.Fatalf("rank %q checked=%v, %v", key, checked, err)
			}
		})
	}
}
