package deploy

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestValidateEmailTemplatesIsOptionalAndStrict(t *testing.T) {
	if got, err := validateEmailTemplates(nil); err != nil || got != nil {
		t.Fatalf("legacy manifest: %#v %v", got, err)
	}
	valid := map[string]any{"sha256": string(make([]byte, 64)), "entries": []any{map[string]any{"name": "welcome", "subject": "Welcome {{name}}", "html": "<b>{{name}}</b>"}}}
	valid["sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := validateEmailTemplates(valid); err != nil {
		t.Fatal(err)
	}
	valid["entries"] = []any{map[string]any{"name": "welcome", "subject": "bad\nBcc: x", "text": "x"}}
	if _, err := validateEmailTemplates(valid); err == nil {
		t.Fatal("expected subject injection rejection")
	}
	valid["entries"] = []any{map[string]any{"name": "welcome", "subject": "Welcome", "text": "", "html": "<p>x</p>"}}
	if _, err := validateEmailTemplates(valid); err == nil {
		t.Fatal("expected provided empty body rejection")
	}
	valid["entries"] = []any{map[string]any{"name": "welcome", "subject": strings.Repeat("é", 500), "text": "x"}}
	if _, err := validateEmailTemplates(valid); err == nil {
		t.Fatal("expected UTF-8 subject byte limit rejection")
	}
}

func TestValidateUploadAuthenticatesEmailTemplatesWithBundle(t *testing.T) {
	bundle := []byte("(()=>{})()")
	bundleHash := hashSha256Bytes(bundle)
	entries := []EmailTemplate{{Name: "welcome", Subject: "Welcome", Text: "Hello {{name}}"}}
	templateHash, err := CanonicalHash(emailTemplateHashInput(bundleHash, entries))
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{
		"manifest": map[string]any{"protocolVersion": "v1", "deploymentId": "email_test", "functions": []any{}, "emailTemplates": map[string]any{"sha256": templateHash, "entries": []any{map[string]any{"name": "welcome", "subject": "Welcome", "text": "Hello {{name}}"}}}},
		"bundle":   base64.StdEncoding.EncodeToString(bundle), "sha256": bundleHash, "size": len(bundle),
	}
	if _, _, err := ValidateUploadRequest(request); err != nil {
		t.Fatal(err)
	}
	request["manifest"].(map[string]any)["emailTemplates"].(map[string]any)["entries"].([]any)[0].(map[string]any)["subject"] = "Tampered"
	if _, _, err := ValidateUploadRequest(request); err == nil {
		t.Fatal("expected template hash mismatch")
	}
}
