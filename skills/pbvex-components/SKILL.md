---
name: pbvex-components
description: Define, mount, evolve, or troubleshoot PBVex components, including component schemas, function bindings, typed mount arguments, environment declarations, generated namespaces, and isolated component data. Use for code involving defineComponent, defineApp, mount, or component references.
---

# PBVex components

Use the project's local `pbvex` through package scripts or `npx pbvex`; a matching global CLI is optional.

Use public APIs from `pbvex/server`: define a component with optional module paths, schema, argument validator, environment declarations, and dependencies; bind functions with `defineComponentFns`; create a root app with `defineApp` and typed `mount` results.

```bash
rg -n "defineComponent|defineComponentFns|defineApp|mount\(" pbvex packages/pbvex docs
npx pbvex codegen
npx pbvex typecheck
```

Give every mount a stable, deliberate name. A mount path determines its namespace: two mounts of the same component get isolated tables and IDs, and IDs are invalid across namespaces. Keep an existing mount path for a compatible upgrade; renaming it deliberately selects new data. Removing a mount/table leaves owned data dormant for rollback or a later remount at the same path.

## Contracts and generated calls

Component mount args must use a validator. Required args are required; optional/defaulted args may be omitted. Environment bindings are strings at runtime: declare literal values or server environment variable names, never commit secrets merely to make them available. Component functions use the bound `ctx.args`/`ctx.env` plus normal capabilities; generated component `api.components...` and `internal.components...` references are the only supported typed call path.

Component schemas may own top-level `v.image()` fields. A component function's `generateUploadUrl({ table, field })` resolves the policy from that mounted component schema, while stored file bytes remain in the application-wide object store. Keep authorization and ownership inside the component and follow `pbvex-storage` for URL modes and variants.

An `envVar` declaration stores only the server variable name in the artifact. Provision its value in every target backend process; an unset variable fails the component invocation, while an explicitly empty value is considered present. Root functions do not receive `ctx.env`. Deployment target metadata and `PBVEX_TOKEN` variables do not inject application configuration. Follow `docs/guides/environment-variables.md` for provisioning and rotation.

Do not bypass `mount`, handwritten namespaces, generated references, validator/authentication checks, or component schema validation. `defineComponentFns` intentionally has no HTTP-action factory; components cannot get arbitrary cross-mount database access or component HTTP routes.

Keep implementation modules beneath the component definition directory. `modulePaths` may list them explicitly; when omitted, PBVex infers eligible component function modules below that directory. Discovery does not pull arbitrary outside files into a component. Moving modules or mount paths changes generated call paths or namespace identity, so treat those refactors as compatibility changes.

## Compatibility discipline

Component identity includes declared modules, schema, args, environment declarations, dependencies, and bundle. Activation validates/authenticates mounts and applies compatible schema work transactionally. Test component argument/schema/mount changes against a backup and an upgrade deployment; failed activation must leave the previous deployment active. Keep the graph acyclic and within its documented depth/definition bounds.
