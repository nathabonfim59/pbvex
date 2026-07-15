package pbvex

import (
	"context"
	"fmt"
	"html"
	"net/mail"
	"regexp"
	"strings"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
)

var emailPlaceholder = regexp.MustCompile(`\{\{\s*([a-zA-Z][a-zA-Z0-9_]*)\s*\}\}`)
var emailVariableName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

func emailExtender() runtime.ContextExtender {
	return func(vm *goja.Runtime, ctx context.Context, app core.App, fd deploy.FunctionDescriptor, obj *goja.Object) error {
		if app == nil || (fd.Type != deploy.FunctionTypeAction && fd.Type != deploy.FunctionTypeHTTPAction) {
			return nil
		}
		manifest, _ := runtime.ManifestFromContext(ctx)
		email := vm.NewObject()
		_ = email.Set("send", vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return wrapPromise(vm, func() (any, error) {
				options, ok := call.Argument(0).Export().(map[string]any)
				if !ok {
					return nil, fmt.Errorf("email.send expects an options object")
				}
				name, _ := options["template"].(string)
				var tmpl *deploy.EmailTemplate
				if manifest.EmailTemplates == nil {
					return nil, fmt.Errorf("deployment has no application email templates")
				}
				for i := range manifest.EmailTemplates.Entries {
					if manifest.EmailTemplates.Entries[i].Name == name {
						tmpl = &manifest.EmailTemplates.Entries[i]
						break
					}
				}
				if tmpl == nil {
					return nil, fmt.Errorf("unknown email template %q", name)
				}
				vars, err := emailVariables(options["variables"])
				if err != nil {
					return nil, err
				}
				subject, err := renderEmail(tmpl.Subject, vars, false)
				if err != nil {
					return nil, err
				}
				if strings.ContainsAny(subject, "\r\n") || len(subject) > 998 {
					return nil, fmt.Errorf("rendered email subject is invalid")
				}
				text, err := renderEmail(tmpl.Text, vars, false)
				if err != nil {
					return nil, err
				}
				body, err := renderEmail(tmpl.HTML, vars, true)
				if err != nil {
					return nil, err
				}
				to, err := emailAddresses(options["to"])
				if err != nil || len(to) == 0 {
					return nil, fmt.Errorf("email.to: at least one valid recipient is required")
				}
				cc, err := emailAddresses(options["cc"])
				if err != nil {
					return nil, fmt.Errorf("email.cc: %w", err)
				}
				bcc, err := emailAddresses(options["bcc"])
				if err != nil {
					return nil, fmt.Errorf("email.bcc: %w", err)
				}
				if len(to)+len(cc)+len(bcc) > 50 {
					return nil, fmt.Errorf("email recipient limit exceeded")
				}
				settings := app.Settings()
				message := &mailer.Message{From: mail.Address{Name: settings.Meta.SenderName, Address: settings.Meta.SenderAddress}, To: to, Cc: cc, Bcc: bcc, Subject: subject, Text: text, HTML: body}
				if err := app.NewMailClient().Send(message); err != nil {
					return nil, err
				}
				return nil, nil
			})
		}))
		return obj.Set("email", email)
	}
}

func emailVariables(raw any) (map[string]string, error) {
	if raw == nil {
		return map[string]string{}, nil
	}
	m, ok := raw.(map[string]any)
	if !ok || len(m) > 64 {
		return nil, fmt.Errorf("email variables must be an object with at most 64 fields")
	}
	out := make(map[string]string, len(m))
	for key, value := range m {
		if !emailVariableName.MatchString(key) {
			return nil, fmt.Errorf("invalid email variable %q", key)
		}
		switch v := value.(type) {
		case nil:
			out[key] = ""
		case string:
			out[key] = v
		case bool, int64, float64:
			out[key] = fmt.Sprint(v)
		default:
			return nil, fmt.Errorf("invalid email variable %q", key)
		}
		if len(out[key]) > 64*1024 {
			return nil, fmt.Errorf("email variable %q is too large", key)
		}
	}
	return out, nil
}

func renderEmail(source string, vars map[string]string, escape bool) (string, error) {
	var renderErr error
	result := emailPlaceholder.ReplaceAllStringFunc(source, func(match string) string {
		key := emailPlaceholder.FindStringSubmatch(match)[1]
		value, ok := vars[key]
		if !ok {
			renderErr = fmt.Errorf("missing email variable %q", key)
			return ""
		}
		if escape {
			return html.EscapeString(value)
		}
		return value
	})
	if renderErr != nil {
		return "", renderErr
	}
	if len(result) > 256*1024 {
		return "", fmt.Errorf("rendered email body is too large")
	}
	return result, nil
}

func emailAddresses(raw any) ([]mail.Address, error) {
	if raw == nil {
		return nil, nil
	}
	values := []any{raw}
	if list, ok := raw.([]any); ok {
		values = list
	}
	out := make([]mail.Address, 0, len(values))
	for _, value := range values {
		s, ok := value.(string)
		if !ok || len(s) > 998 || strings.ContainsAny(s, "\r\n") {
			return nil, fmt.Errorf("invalid recipient")
		}
		parsed, err := mail.ParseAddress(s)
		if err != nil || parsed.Address == "" {
			return nil, fmt.Errorf("invalid recipient")
		}
		out = append(out, *parsed)
	}
	return out, nil
}
