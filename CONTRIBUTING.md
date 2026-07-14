# Contributing to PBVex

PBVex is a pnpm TypeScript monorepo with a Go backend built on PocketBase.
Changes should preserve the shared protocol contract across the authoring SDK,
client SDKs, and backend.

Report security-sensitive issues using the private process in
[the security policy](.github/SECURITY.md), not a public issue or pull request.

## Prerequisites

- Git.
- Node.js 18 or newer. CI and trusted publishing use Node.js 24.17.
- pnpm 8.15, as declared by the root `packageManager` field.
- Go 1.25.6 or newer for backend and release validation.
- A C compiler when running the Go race detector with `CGO_ENABLED=1`.

GoReleaser and actionlint are only required when reproducing their release
checks locally.

## Repository layout

- `packages/protocol`: shared wire types, validators, canonical hashing, and
  deployment validation.
- `packages/pbvex`: TypeScript authoring APIs, bundler, code generation, and
  CLI.
- `packages/sdk-core`: browser-neutral client and realtime transport.
- `packages/sdk-react` and `packages/sdk-svelte`: framework adapters.
- `backend`: the PocketBase-based Go server and the single-binary entry point.
- `scripts`: package, tag, release, and binary smoke validation.
- `docs`: the protocol ADR and operator documentation.

## Set up the workspace

From the repository root:

```bash
pnpm install --frozen-lockfile
pnpm build
```

Run the backend during development with:

```bash
cd backend
go run ./cmd/pbvex serve --http 127.0.0.1:8090
```

The default data directory is `backend/pb_data` when the command is run from
`backend`; it is ignored by Git.

## Make changes

- Keep protocol validation behavior aligned between TypeScript and Go.
- Add focused tests for changed behavior and adversarial cases at trust
  boundaries.
- Preserve deterministic artifact, hashing, and code-generation output.
- Do not commit credentials, `pb_data`, `node_modules`, Turbo caches, or local
  build output that is not already tracked.
- The SDK `dist` files already tracked by Git are release artifacts. Rebuild
  them when their sources change and verify that the resulting diff is
  intentional.
- Update user, operator, protocol, or release documentation when a public
  contract changes.

## Verify TypeScript changes

From the repository root:

```bash
pnpm lint
pnpm build
pnpm test
pnpm pack:smoke
git diff --check
```

`pnpm pack:smoke` builds all five published packages, installs their tarballs
in a clean consumer, checks their type surfaces, imports them at runtime, and
invokes the packed CLI.

## Verify backend changes

From `backend`:

```bash
test -z "$(gofmt -l .)"
go vet ./...
go test -count=1 ./...
CGO_ENABLED=1 go test -race -count=1 ./...
go build ./cmd/pbvex
```

The complete race suite can take several minutes. Do not replace it with only
the package touched by a change when preparing a pull request.

## Verify release changes

For changes to packaging, workflows, embedded assets, version handling, or the
binary lifecycle, also run from the repository root:

```bash
goreleaser check
./scripts/release-validate.sh
VALIDATE_TAG_TEST=1 ./scripts/validate-tag.sh
```

Run actionlint against `.github/workflows` when workflow files change. See the
[release guide](docs/releasing.md) for release gates, npm trusted publishing,
and repository protections.

## Pull requests

Keep commits focused and explain behavioral or protocol compatibility impacts.
In the pull request, list the verification commands run and any checks that
could not be run locally. Do not mix unrelated generated output or refactors
into a functional change.
