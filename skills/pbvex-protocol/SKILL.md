---
name: pbvex-protocol
description: Change or review PBVex protocol and compatibility contracts across packages/protocol, TypeScript authoring/codegen, Go manifest and wire validation, runtime bridges, shared fixtures, canonical hashes, IDs, deployment artifacts, and protocol ADRs. Use for cross-language contributor work, not ordinary application code.
---

# PBVex protocol compatibility

Treat protocol changes as cross-language trust-boundary changes. A new manifest field, validator, wire value, error code, ID form, function kind, component definition, migration registration, or runtime bridge method must stay aligned across `packages/protocol`, `packages/pbvex`, Go deploy/schema/runtime code, clients, fixtures, and documentation.

Preserve canonical JSON, hashes, deterministic artifact/codegen output, strict unknown-key rejection, depth/node/byte budgets, and protocol-version behavior. Do not silently reinterpret persisted artifacts, IDs, cursors, migration history, or pinned deployment snapshots. Make compatibility or rejection explicit and test adversarial malformed input in both languages.

Update shared valid/invalid fixtures and golden vectors rather than writing isolated validators that can drift. Regenerate checked-in artifacts and API docs through their owning scripts. Update the owning protocol ADRs—ADR 001 for protocol/runtime shape and ADR 002 for migration modes/lifecycle—when durable contracts change; ADRs are not substitutes for executable parity tests.

Use focused tests while iterating, then run the complete protocol and backend parity gates:

```bash
pnpm --filter @pbvex/protocol test
pnpm --filter pbvex test
pnpm --filter @pbvex/client test
(cd backend && go test -count=1 ./internal/schema ./internal/deploy ./internal/runtime ./internal/pbvex)
pnpm docs:api && pnpm docs:verify
```

Pair this skill with `pbvex-internals` for Go runtime work and `pbvex-operations` for full PR/release verification. Consult `CONTRIBUTING.md`, the protocol ADRs, and current source before assuming an older ADR example is exhaustive.
