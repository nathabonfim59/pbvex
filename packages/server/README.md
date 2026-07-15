# @pbvex/server

The self-contained PBVex backend distribution for npm. The package contains
the supported Go server binaries and selects the binary matching the current
Node.js platform and architecture at runtime.

Most users install `pbvex`, which depends on this package and exposes
`pbvex serve` and `pbvex dev`. For a server-only npm installation:

```bash
npm install --global @pbvex/server
pbvex-server serve --http 127.0.0.1:8090
```

The PocketBase admin UI is disabled by default. Add `--admin-ui` after
`serve` only when dashboard access is deliberately required:

```bash
pbvex-server serve --admin-ui --http 127.0.0.1:8090
```

Standalone release archives remain available for hosts that do not use npm.
