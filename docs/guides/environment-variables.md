# Environment variables and secrets

PBVex does not currently provide a Convex-style, deployment-managed environment-variable store. Deployed code runs in Goja rather than Node.js, so `process.env` is unavailable. Root queries, mutations, actions, and HTTP actions also do not receive `ctx.env`.

The supported application-secret boundary is a **component environment declaration**. A component declares the names available to its functions, and each declaration either embeds a non-secret literal or resolves a variable from the PBVex backend process.

## Declare component bindings

```ts
// pbvex/components/webhooks/component.ts
import { defineComponent, defineComponentFns } from 'pbvex/server';

export const webhooks = defineComponent({
  modulePaths: ['functions.ts'],
  env: {
    API_ORIGIN: { type: 'value', value: 'https://api.example.com' },
    WEBHOOK_SECRET: {
      type: 'envVar',
      name: 'PBVEX_APP_WEBHOOK_SECRET',
    },
  },
});

export const webhooksFns = defineComponentFns(webhooks);
```

Component functions receive a typed, read-only string map:

```ts
// pbvex/components/webhooks/functions.ts
import { v } from 'pbvex/values';
import { webhooksFns } from './component';
import { verifySignature } from './verify';

export const verify = webhooksFns.action({
  args: { signature: v.string() },
  returns: v.boolean(),
  handler: async (ctx, { signature }) => {
    return verifySignature(signature, ctx.env.WEBHOOK_SECRET);
  },
});
```

Mount the component in the application and regenerate the typed contract:

```ts
// pbvex/app.ts
import { defineApp, mount } from 'pbvex/server';
import { webhooks } from './components/webhooks/component';

export default defineApp({
  components: [mount(webhooks, 'webhooks')],
});
```

```bash
pbvex codegen
```

Use the generated component references for client and nested calls.

## Binding types

| Binding | Runtime behavior | Appropriate use |
| --- | --- | --- |
| `{ type: 'envVar', name: 'NAME' }` | Reads `NAME` from the backend process environment when the component invocation context is created. | Secrets and server-specific configuration. |
| `{ type: 'value', value: 'text' }` | Uses the literal stored in the component definition and deployment artifact. | Non-secret, versioned configuration. |

Every `ctx.env` value is a string. Parse and validate numbers, URLs, JSON, and enumerated values before use. Environment keys must be valid PBVex field names, and binding names and literal values must be non-empty.

An `envVar` artifact contains the variable name, not its server value. The value does not participate in component identity, generated code, target metadata, or the deployment manifest. A `value` literal does participate in the component definition and artifact, so never use it for secrets.

## Provision the backend process

Set each named variable in the environment of the PBVex server process. For a direct process:

```bash
export PBVEX_APP_WEBHOOK_SECRET='replace-with-secret-manager-value'
/usr/local/bin/pbvex --dir /var/lib/pbvex serve --http 127.0.0.1:8090
```

For systemd, keep secrets in a root-owned environment file rather than the unit or repository:

```ini
# /etc/systemd/system/pbvex.service
[Service]
EnvironmentFile=/etc/pbvex/app.env
ExecStart=/usr/local/bin/pbvex --dir /var/lib/pbvex serve --http 127.0.0.1:8090
```

Restrict `/etc/pbvex/app.env` to the service account or root, then reload and restart the service after changing it:

```bash
sudo chmod 600 /etc/pbvex/app.env
sudo systemctl daemon-reload
sudo systemctl restart pbvex
```

For containers or a process manager, use its secret integration to populate the process environment. Do not commit `.env` files, place secrets in target metadata, or pass them as component mount arguments.

Provision every deployment target independently because each target points to a separate backend process. `PBVEX_TOKEN` and `PBVEX_<TARGET>_TOKEN` authenticate the CLI deployment request only; deployed functions cannot read them.

## Validation and failure behavior

Deployment validates the binding shape, but it does not require an `envVar` to exist on the target server. The backend resolves the variable when it creates a component invocation context:

- An unset variable fails that component invocation and identifies the component, binding key, and missing server variable.
- A variable explicitly set to an empty string is considered present. Reject empty values in application code when they are invalid.
- Root functions and components that do not use that binding are unaffected.
- Changing a server value while retaining the same binding name does not require rebuilding the artifact. Restart or reload the server process so it receives the new environment.

After deployment or secret rotation, invoke a narrow authenticated health function that verifies required configuration without returning, logging, or storing secret values.

## Security rules

- Bind only the variables a component requires.
- Never return secrets to clients or include them in logs, errors, scheduled arguments, database records, or storage objects.
- Use separate credentials for development, staging, and production.
- Treat deployment artifacts as non-secret but potentially sensitive: they expose literal bindings and environment-variable names.
- Keep deployment credentials separate from application credentials and component bindings.

See [Components](./components.md) for component isolation and generated references, [Deployment](./deployment.md) for target releases, and the [self-hosting guide](../self-hosting.md) for running the server binary.
