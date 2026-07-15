package pbvex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIGeneratedAsyncBundleWireRoundTrip(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(root, "fixtures/cli-async/artifact.json"))
	if err != nil {
		t.Fatal(err)
	}
	var upload map[string]any
	if err := json.Unmarshal(out, &upload); err != nil {
		t.Fatal(err)
	}
	// A real client sends toUploadRequest(artifact): strip artifact-only
	// fields and convert module entries to the {path, bytes} upload shape.
	delete(upload, "project")
	delete(upload, "target")
	if mods, ok := upload["modules"].([]any); ok {
		sources := make([]any, 0, len(mods))
		for _, m := range mods {
			entry, ok := m.(map[string]any)
			if !ok {
				continue
			}
			path, _ := entry["path"].(string)
			code, _ := entry["code"].(string)
			sources = append(sources, map[string]any{
				"path":  path,
				"bytes": base64.StdEncoding.EncodeToString([]byte(code)),
			})
		}
		upload["modules"] = sources
	}
	_, service := newTestApp(t)
	response, err := service.Upload(upload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(response.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	// Use the exact hashed name the manifest declares (do not assume a suffix).
	functionName, ok := upload["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["name"].(string)
	if !ok {
		t.Fatal("expected function name in manifest")
	}
	result, err := service.Call(context.Background(), functionName, map[string]any{"integer": map[string]any{"$integer": "AQAAAAAAAAA="}, "bytes": map[string]any{"$bytes": "AP9C"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(result)
	want := `{"bytes":{"$bytes":"AP9C"},"integer":{"$integer":"AQAAAAAAAAA="}}`
	if string(got) != want {
		t.Fatalf("got %s", got)
	}
}
