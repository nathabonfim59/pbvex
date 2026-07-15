package pbvex

import (
	"context"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/pocketbase/pocketbase/core"
	pbtests "github.com/pocketbase/pocketbase/tests"
)

func TestRenderEmailEscapesHTMLAndRequiresVariables(t *testing.T) {
	vars := map[string]string{"name": `<Admin & "ops">`}
	got, err := renderEmail(`<p>{{ name }}</p>`, vars, true)
	if err != nil || got != `<p>&lt;Admin &amp; &#34;ops&#34;&gt;</p>` {
		t.Fatalf("render = %q, %v", got, err)
	}
	plain, err := renderEmail(`Hello {{name}}`, vars, false)
	if err != nil || plain != `Hello <Admin & "ops">` {
		t.Fatalf("plain = %q, %v", plain, err)
	}
	if _, err := renderEmail(`{{missing}}`, vars, false); err == nil {
		t.Fatal("expected missing variable error")
	}
}

func TestEmailExtenderUsesPocketBaseMailerOnlyForActions(t *testing.T) {
	app, err := pbtests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	descriptors := []deploy.FunctionDescriptor{
		{Name: "send", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/mail.ts", ExportName: "send"},
		{Name: "inspect", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/mail.ts", ExportName: "inspect"},
		{Name: "inspectMutation", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/mail.ts", ExportName: "inspectMutation"},
		{Name: "inspectHttp", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/mail.ts", ExportName: "inspectHttp", Route: &deploy.FunctionRoute{Method: "GET", Path: "/email-capability"}},
	}
	bundle := `
__pbvex.registerFunction({name:"send",type:"action",visibility:"internal",modulePath:"pbvex/mail.ts",exportName:"send"}, async (ctx) => { await ctx.email.send({template:"welcome",to:"person@example.com",variables:{name:"<Pat>"}}); return null; });
__pbvex.registerFunction({name:"inspect",type:"query",visibility:"internal",modulePath:"pbvex/mail.ts",exportName:"inspect"}, (ctx) => typeof ctx.email);
__pbvex.registerFunction({name:"inspectMutation",type:"mutation",visibility:"internal",modulePath:"pbvex/mail.ts",exportName:"inspectMutation"}, (ctx) => typeof ctx.email);
__pbvex.registerFunction({name:"inspectHttp",type:"httpAction",visibility:"public",modulePath:"pbvex/mail.ts",exportName:"inspectHttp",route:{method:"GET",path:"/email-capability"}}, (ctx) => new Response(typeof ctx.email));`
	manager := runtime.NewManager(runtime.DefaultConfig())
	manager.AddContextExtender(emailExtender())
	if err := manager.Compile("email-test", bundle, descriptors); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "email-test", Functions: descriptors, EmailTemplates: &deploy.EmailTemplateManifest{Entries: []deploy.EmailTemplate{{Name: "welcome", Subject: "Hi {{name}}", HTML: "<p>{{name}}</p>"}}}}
	if _, err := manager.InvokeWithDatabase(context.Background(), "email-test", "send", nil, nil, "", app, manifest); err != nil {
		t.Fatal(err)
	}
	if app.TestMailer.TotalSend() != 1 {
		t.Fatalf("sent %d messages", app.TestMailer.TotalSend())
	}
	message := app.TestMailer.LastMessage()
	if message.Subject != "Hi <Pat>" || message.HTML != "<p>&lt;Pat&gt;</p>" {
		t.Fatalf("unexpected message: %#v", message)
	}
	got, err := manager.InvokeWithDatabase(context.Background(), "email-test", "inspect", nil, nil, "", app, manifest)
	if err != nil || got != "undefined" {
		t.Fatalf("query capability = %#v, %v", got, err)
	}
	got, err = manager.InvokeWithDatabase(context.Background(), "email-test", "inspectMutation", nil, nil, "", app, manifest)
	if err != nil || got != "undefined" {
		t.Fatalf("mutation capability = %#v, %v", got, err)
	}
	httpResult, err := manager.InvokeHTTPWithDatabase(context.Background(), "email-test", "inspectHttp", &deploy.HTTPRequestEnvelope{Method: "GET", URL: "http://localhost/email-capability"}, nil, "", app, manifest)
	if err != nil || string(httpResult.Body) != "object" {
		t.Fatalf("http capability = %#v, %v", httpResult, err)
	}
}

func TestEmailAddressesRejectHeaderInjectionAndBoundRenderedOutput(t *testing.T) {
	if _, err := emailAddresses("user@example.com\r\nBcc: bad@example.com"); err == nil {
		t.Fatal("expected injected address rejection")
	}
	huge := map[string]string{"value": string(make([]byte, 256*1024+1))}
	if _, err := renderEmail("{{value}}", huge, false); err == nil {
		t.Fatal("expected rendered size rejection")
	}
}
