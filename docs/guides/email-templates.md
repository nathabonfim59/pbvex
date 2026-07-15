# Application email templates

PBVex packages application-owned email templates with a deployment and sends them through PocketBase's configured mailer. These are separate from PocketBase's built-in verification, password-reset, OTP, and other auth templates, which remain configured in the PocketBase dashboard.

Create one strict JSON file per template in `pbvex/emails/`:

```json
{
  "subject": "Welcome, {{name}}",
  "text": "Hello {{name}}, visit {{url}}",
  "html": "<p>Hello <strong>{{name}}</strong>, <a href=\"{{url}}\">get started</a>.</p>"
}
```

For example, save that as `pbvex/emails/welcome.json`. File names become template names and must match `[a-z][a-z0-9_-]{0,63}`. A template needs a subject and at least one of `text` or `html`. Run `pbvex codegen` after adding or renaming a template; generated action contexts restrict `template` to the discovered name union.

```ts
import { action } from './_generated/server';

export const welcome = action({
  handler: async (ctx, args: { email: string; name: string }) => {
    await ctx.email.send({
      template: 'welcome',
      to: args.email,
      variables: { name: args.name, url: 'https://app.example.com/start' },
    });
  },
});
```

`to`, `cc`, and `bcc` accept one address or an array, with at most 50 recipients total. Every referenced variable is required. Values are HTML-escaped in `html`; text and subject interpolation remain plain text. Newlines are forbidden in rendered subjects and recipients, and inputs and rendered bodies are bounded.

HTML escaping prevents variables from adding markup, but it is not URL-scheme validation. Validate application URLs before passing them into `href`, `src`, or similar attribute positions.

`ctx.email` exists only on public/internal actions, component actions, and HTTP actions. Queries and mutations cannot send external email. Delivery uses PocketBase's `app.NewMailClient()`, so configure and test SMTP under **Settings > Mail settings**; PBVex does not introduce another SMTP stack.

Template entries are sorted in the deployment manifest. Their canonical hash is bound to the executable bundle hash, so upload rejects malformed or tampered templates before activation. Manifests without `emailTemplates` remain valid.

See [Going to production](./going-to-production.md#configure-mail-and-abuse-controls) and PocketBase's [production guide](https://pocketbase.io/docs/going-to-production/).
