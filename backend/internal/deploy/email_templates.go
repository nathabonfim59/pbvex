package deploy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const MaxEmailTemplates = 64
const MaxEmailTemplateBytes = 512 * 1024

var emailTemplateName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func validateEmailTemplates(value any) (*EmailTemplateManifest, error) {
	if value == nil {
		return nil, nil
	}
	o, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("emailTemplates must be an object")
	}
	for key := range o {
		if key != "sha256" && key != "entries" {
			return nil, fmt.Errorf("unknown emailTemplates field %q", key)
		}
	}
	sha, ok := o["sha256"].(string)
	if !ok || !IsSha256Hex(sha) {
		return nil, fmt.Errorf("invalid emailTemplates sha256")
	}
	raw, ok := o["entries"].([]any)
	if !ok || len(raw) > MaxEmailTemplates {
		return nil, fmt.Errorf("invalid emailTemplates entries")
	}
	out := make([]EmailTemplate, 0, len(raw))
	seen := map[string]bool{}
	total := 0
	for _, item := range raw {
		t, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid email template")
		}
		for key := range t {
			if key != "name" && key != "subject" && key != "text" && key != "html" {
				return nil, fmt.Errorf("unknown email template field %q", key)
			}
		}
		name, _ := t["name"].(string)
		subject, _ := t["subject"].(string)
		text, textOK := t["text"].(string)
		if _, present := t["text"]; present && (!textOK || text == "") {
			return nil, fmt.Errorf("invalid email template text")
		}
		html, htmlOK := t["html"].(string)
		if _, present := t["html"]; present && (!htmlOK || html == "") {
			return nil, fmt.Errorf("invalid email template html")
		}
		if !emailTemplateName.MatchString(name) || seen[name] {
			return nil, fmt.Errorf("invalid or duplicate email template name")
		}
		if subject == "" || len(subject) > 998 || strings.ContainsAny(subject, "\r\n") || text == "" && html == "" {
			return nil, fmt.Errorf("invalid email template %q", name)
		}
		seen[name] = true
		total += len(subject) + len(text) + len(html)
		if total > MaxEmailTemplateBytes {
			return nil, fmt.Errorf("email templates exceed size limit")
		}
		out = append(out, EmailTemplate{Name: name, Subject: subject, Text: text, HTML: html})
	}
	if !sort.SliceIsSorted(out, func(i, j int) bool { return out[i].Name < out[j].Name }) {
		return nil, fmt.Errorf("email template entries must be sorted")
	}
	return &EmailTemplateManifest{Sha256: sha, Entries: out}, nil
}

func emailTemplateHashInput(bundleSha256 string, entries []EmailTemplate) map[string]any {
	raw := make([]any, len(entries))
	for i, entry := range entries {
		item := map[string]any{"name": entry.Name, "subject": entry.Subject}
		if entry.Text != "" {
			item["text"] = entry.Text
		}
		if entry.HTML != "" {
			item["html"] = entry.HTML
		}
		raw[i] = item
	}
	return map[string]any{"bundleSha256": bundleSha256, "entries": raw}
}
