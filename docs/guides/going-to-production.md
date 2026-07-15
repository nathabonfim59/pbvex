# Going to production

PBVex embeds PocketBase, the optionally enabled PocketBase dashboard, and the
PBVex runtime in one executable. A production PBVex backend serves **one application**: its successive
PBVex deployments are releases of that application, not independent tenants.
Run exactly one PBVex process for a data directory. Do not point multiple
processes, containers, or replicas at the same `pb_data` directory.

This guide turns the upstream [PocketBase going-to-production
recommendations](https://pocketbase.io/docs/going-to-production/) into a
practical PBVex setup. Replace the example paths, user, domain, and limits for
your host.

## Production layout

A simple Linux installation can use:

```text
/usr/local/bin/pbvex             # versioned/replaced executable
/etc/pbvex/pbvex.env             # root-owned process secrets
/var/lib/pbvex/
└── pb_data/                     # database, local files, settings, backups
```

Create a dedicated account and writable state directory; do not run the service
as root:

```bash
sudo useradd --system --home /var/lib/pbvex --shell /usr/sbin/nologin pbvex
sudo install -d -o pbvex -g pbvex -m 0750 /var/lib/pbvex/pb_data
sudo install -d -o root -g pbvex -m 0750 /etc/pbvex
sudo install -o root -g root -m 0755 ./pbvex /usr/local/bin/pbvex
sudo install -o root -g pbvex -m 0640 /dev/null /etc/pbvex/pbvex.env
```

Put component environment bindings and other process secrets in
`/etc/pbvex/pbvex.env`. If settings encryption is enabled, add a randomly
generated, exactly 32-character value without committing it anywhere:

```dotenv
PB_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef
GOMEMLIMIT=512MiB
```

The shown key is only a length-correct placeholder; replace it with 32 random
characters, for example the output of `openssl rand -hex 16`.

`GOMEMLIMIT` is a soft Go runtime limit, not a hard memory cap. Size it below the
service or container limit with enough headroom for uploads and traffic.

## Run under systemd

Create `/etc/systemd/system/pbvex.service`:

```ini
[Unit]
Description=PBVex backend
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=pbvex
Group=pbvex
WorkingDirectory=/var/lib/pbvex
EnvironmentFile=/etc/pbvex/pbvex.env
ExecStart=/usr/local/bin/pbvex --dir /var/lib/pbvex/pb_data --dev=false --encryptionEnv=PB_ENCRYPTION_KEY serve --http=127.0.0.1:8090
Restart=on-failure
RestartSec=5s
LimitNOFILE=4096
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/pbvex

[Install]
WantedBy=multi-user.target
```

Omit `--encryptionEnv=PB_ENCRYPTION_KEY` if you are not enabling settings
encryption. The flag names the environment variable containing the key; it does
not accept the key itself. Preserve that key in secret management and in your
recovery procedure, because encrypted settings cannot be recovered without it.

Enable the service and verify the private listener:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now pbvex
sudo systemctl status pbvex
curl -f http://127.0.0.1:8090/api/health
```

`LimitNOFILE=4096` is a starting point. Raise it when measured concurrent
connections, especially realtime subscriptions, require more descriptors.
The service command deliberately omits `serve --admin-ui`, so `/_/` is not
registered during normal production operation.

## Put Nginx and TLS in front

Keep PBVex bound to loopback and terminate public TLS at Nginx or another trusted
edge. The following Nginx configuration preserves PocketBase/PBVex realtime
streams, forwards the original request context, and admits uploads up to 64 MiB
(the default PBVex file limit). The 90 MiB proxy limit also leaves room for a
maximum-size PBVex deployment's base64 and manifest envelope:

```nginx
server {
    listen 80;
    server_name app.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name app.example.com;

    ssl_certificate     /etc/letsencrypt/live/app.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/app.example.com/privkey.pem;

    client_max_body_size 90m;

    location / {
        proxy_pass http://127.0.0.1:8090;
        proxy_http_version 1.1;
        proxy_set_header Connection "";

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_read_timeout 360s;
        proxy_buffering off;
        proxy_cache off;
        add_header X-Accel-Buffering no;
    }
}
```

Provision and renew the certificate using your normal ACME tooling. The public
firewall should expose only the TLS edge, not port 8090. If a CDN or load
balancer sits before Nginx, configure its trusted-address/real-IP handling and
sanitize forwarded headers there too; never accept a client-supplied IP header
unchanged.

During an explicit admin session, enable the PocketBase dashboard as described
below and set **Settings > Application > Application URL**
to the single canonical external origin, for example
`https://app.example.com`. PBVex uses this trusted value for HTTP-action request
URLs and, unless `--storageBaseUrl` or `PBVEX_STORAGE_BASE_URL` overrides it,
generated storage URLs.

Also configure **User IP proxy headers** in PocketBase settings for the proxy
headers your trusted edge sets (normally `X-Real-IP`, and `X-Forwarded-For` when
needed). This is required for correct client-IP logging, rate limiting, and
superuser IP restrictions behind a proxy.

## Secure the dashboard and superusers

The dashboard is disabled by default. To perform administration, temporarily
add `--admin-ui` after `serve` in `ExecStart`, restart PBVex, and access it over
a loopback SSH tunnel or a separately protected HTTPS route. With the example
proxy it would be available at:

```text
https://app.example.com/_/
```

There is no separate PBVex admin service to deploy. Treat dashboard and
superuser access as production credentials, and remove `--admin-ui` and
restart the service when the session is complete:

- Use unique superuser accounts and strong passwords; do not share deployment
  tokens or commit them to configuration.
- In **Settings > Application > Superuser IPs**, restrict superusers to the
  administrator and CI egress IPs/subnets that actually need access. You can
  recover or change the list locally with
  `pbvex --dir /var/lib/pbvex/pb_data superuser ips <ip-or-subnet> ...`.
- Enable MFA and OTP on the `_superusers` collection after SMTP is working. If
  email delivery is unavailable, generate a code locally with
  `pbvex --dir /var/lib/pbvex/pb_data superuser otp admin@example.com`.
- Require an additional Nginx/VPN IP restriction on `/_/` if the dashboard is
  enabled through the public proxy. Remember that CI
  deployment uses authenticated `/api/pbvex/*` endpoints, so dashboard-only
  proxy restrictions do not replace the PocketBase superuser allowlist.

## Configure mail and abuse controls

Configure a production SMTP provider under **Settings > Mail settings** and
test delivery. PocketBase's development-oriented `sendmail` fallback is not a
reliable production mail path; OTP, verification, and password-reset flows
depend on mail delivery.

Application-owned templates under `pbvex/emails/` use this same configured
mailer when called from an action or HTTP action. Test one representative
plain-text and HTML delivery after each SMTP or sender-domain change; see
[Application email templates](./email-templates.md). PocketBase auth templates
remain configured separately on their auth collections in the dashboard.

Enable and tune PocketBase's built-in limiter under **Settings > Application >
Rate limits**. Start with coverage for authentication and record/API mutation
routes, then tune from observed traffic. An edge limiter can add coarse DDoS or
per-route controls, but it does not remove the need for correct real-client-IP
configuration in PocketBase.

## Back up and restore

All local persistent state lives under the configured `pb_data` directory.
Choose one of these supported approaches:

- Use **Settings > Backups** in the dashboard (or the corresponding PocketBase
  backup API). A built-in backup snapshots `pb_data` while temporarily placing
  the application in read-only mode. Store off-host copies with retention.
- Stop PBVex before copying or replacing `pb_data` directly. Copying a live
  SQLite data directory is not a safe backup procedure.

PocketBase backup archives exclude the local `backups` directory and objects
stored in S3-compatible storage. Back up external object storage separately and
keep it consistent with the database snapshot. PBVex deployment state,
scheduler state, component collections, signing material, and local storage
objects must be restored together.

Before an upgrade or schema-changing deployment, take a backup. Regularly test
a restore on an isolated host with a different data directory, then verify the
health endpoint, a representative query/mutation, realtime, scheduled work, and
file download. A PBVex application rollback is not a database restore or binary
downgrade.

## Production checklist

- One application, one PBVex process, and one private `pb_data` directory.
- Loopback listener; TLS and request limits enforced at a trusted proxy.
- Canonical Application URL and trusted client-IP proxy headers configured.
- Dashboard reachable at `/_/`, with superuser IP restrictions and MFA/OTP.
- SMTP delivery tested and rate limits enabled.
- Off-host database and object backups with a tested restore procedure.
- `LimitNOFILE`, `GOMEMLIMIT`, upload limits, logs, and alerts sized from
  observed load.
- Settings encryption key retained securely if `--encryptionEnv` is enabled.

For release upload, activation, and application rollback, continue with the
[deployment guide](./deployment.md). For upstream operational details and
version-specific recommendations, consult PocketBase's official [going to
production guide](https://pocketbase.io/docs/going-to-production/).
