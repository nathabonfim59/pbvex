# PBVex Backend

The backend uses `github.com/pocketbase/pocketbase` as a Go framework and adds
PBVex routes, schema, runtime services, and lifecycle hooks. It builds as one
executable; user TypeScript is bundled by the CLI and uploaded as deployment
data, not embedded in the binary.

## Build and test

```bash
go build ./cmd/pbvex
go test ./...
go vet ./...
```

Run it from the directory that should contain `pb_data`:

```bash
./pbvex serve --http 127.0.0.1:8090
```

The standard PocketBase commands remain available, including `superuser` and
`migrate`. The upstream self-update command is intentionally disabled because
PBVex releases must not replace themselves with an unmodified PocketBase
binary.

PBVex v1 hosts one application per server process and dedicated data directory.
Do not run multiple instances against the same data directory or assume
distributed scheduler/realtime coordination.

For deployment and production configuration, see
[the self-hosting guide](../docs/self-hosting.md).
