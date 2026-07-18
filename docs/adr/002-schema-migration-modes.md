# ADR 002: PBVex Schema Migration Modes

## Status

Accepted in part. Transactional PBVex migrations are implemented and shipped.
Maintenance mode remains a proposal and is not available in the runtime or CLI.

## Context

PBVex materializes `pbvex/schema.ts` changes during deployment activation.
Compatible changes can normalize defaults and rebuild order/index projections,
but incompatible existing documents need an explicit, typed transformation.
Unbounded rewrites are unsafe in one SQLite transaction because they can hold
the write lock, grow the journal or WAL, and consume excessive memory and CPU.

PBVex and PocketBase migrations are separate systems. This ADR concerns PBVex
document transformations bundled in application deployment artifacts. Direct
PocketBase host migrations run from host files during backend startup and are
outside this decision.

## Shipped Decision: Transactional Mode

`defineMigration` accepts only `mode: 'transactional'`. A migration names one
root PBVex table, declares object validators for its source and target document
shapes, and supplies required synchronous `up` and `down` handlers. Component
tables and direct PocketBase collections are not in scope.

The handler context is pure: migration ID, stable activation time, and a
bounded failure method. It has no arbitrary database access or external side
effects. `_id` and `_creationTime` are readable but immutable, and every handler
result must validate against its target validator.

Activation performs document transformations, default normalization,
order/index projection updates, schema materialization, migration-history
writes, and the active-deployment switch in one database transaction. Therefore:

- a failed `up` leaves the previous deployment and documents unchanged;
- deployment rollback runs `down` in reverse migration order;
- a failed `down` leaves the current deployment and documents unchanged;
- no application request can observe partially migrated PBVex data.

Migration history records ID, checksum, source and target schema hashes,
deployment ID, direction, and applied time. Reusing an ID with different
content or schema bindings is rejected.

## Shipped Limits and Warnings

Transactional activation has fixed hard ceilings of 10,000 processed documents
and 64 MiB of encoded work. The byte budget charges source values, handler
outputs, normalized documents, and retained order/index projections, so it is
not equivalent to database size.

The backend preflights row counts and source bytes before the first write and
enforces full actual accounting during execution. Exceeding either ceiling
aborts the transaction. At 80 percent utilization, activation returns a
structured `transactional_migration_utilization` warning. These limits are not
application-configurable and there is no `--force` bypass.

`pbvex migrations plan` is intentionally structural. It reports source/target
deployment and schema hashes, changed tables/fields/indexes, and matching
migration chains. It does not inspect server records, execute handlers, count
candidate documents, estimate encoded work, or recommend a migration mode.
Offline planning can use `--active-artifact <path>` as its validated source.

## Shipped Rollback Boundary

Application deployment rollback atomically executes the applicable PBVex
`down` handlers and restores the previous PBVex deployment. It does not affect
direct PocketBase migrations. PocketBase host migration rollback remains a
separate operator workflow.

## Proposed: Maintenance Mode

Large migrations may exceed the shipped transactional ceilings. A future
`maintenance` mode could trade whole-migration atomicity for bounded
transactions and durable checkpoints while preventing clients from observing
mixed schemas. None of the behavior in this section exists today.

A complete maintenance design would need to:

1. Validate deployment, migration checksum, and source/target schema hashes.
2. Acquire a durable single-node lease and reject application traffic while
   mixed document shapes exist.
3. Transform documents in stable, bounded batches and persist checkpoints.
4. Resume safely after restart without serving traffic prematurely.
5. Verify the complete target schema before activation.
6. Support reverse checkpoint processing through the required `down` handler,
   or define backup restoration as the recovery path.

It would also require status APIs, stale-lease recovery, operator controls,
bounded error reporting, and tests for restart, resume, rollback, and backup
recovery. No `mode: 'maintenance'`, maintenance CLI command, maintenance state,
request fencing, checkpoint, or maintenance-related server flag should be
documented as current behavior until that design is implemented.

## Out of Scope

The shipped transactional mode does not include:

- serving traffic while old and new document shapes coexist;
- automatic dual writes or online backfills;
- cross-table joins or aggregation in migration handlers;
- external HTTP, storage, email, scheduler, or environment side effects;
- automatic semantic conversions inferred from field types;
- bypassing the hard transactional ceilings.

An eventual online mode would require a separate design for dual-schema reads
and writes, concurrent-write fencing, verification, cutover, and rollback.
