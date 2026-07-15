#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR="$(mktemp -d)"
PACK_DIR="$TMP_DIR/packs"
CONSUMER_DIR="$TMP_DIR/consumer"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

mkdir -p "$PACK_DIR" "$CONSUMER_DIR"
cd "$REPO_ROOT"

node --test "$SCRIPT_DIR/pack-smoke-config.test.mjs"
pnpm build
pnpm --dir packages/protocol pack --pack-destination "$PACK_DIR"
pnpm --dir packages/pbvex pack --pack-destination "$PACK_DIR"
pnpm --dir packages/client pack --pack-destination "$PACK_DIR"
pnpm --dir packages/react pack --pack-destination "$PACK_DIR"
pnpm --dir packages/svelte pack --pack-destination "$PACK_DIR"

protocol_tgz=("$PACK_DIR"/pbvex-protocol-*.tgz)
pbvex_tgz=("$PACK_DIR"/pbvex-*.tgz)
client_tgz=("$PACK_DIR"/pbvex-client-*.tgz)
react_tgz=("$PACK_DIR"/pbvex-react-*.tgz)
svelte_tgz=("$PACK_DIR"/pbvex-svelte-*.tgz)

# The unscoped package glob also sees scoped tarballs; select its exact basename.
pbvex_package=""
for candidate in "${pbvex_tgz[@]}"; do
  if [[ "$(basename "$candidate")" =~ ^pbvex-[0-9] ]]; then
    pbvex_package="$candidate"
    break
  fi
done

for archive in "${protocol_tgz[0]}" "$pbvex_package" "${client_tgz[0]}" "${react_tgz[0]}" "${svelte_tgz[0]}"; do
  if [[ -z "$archive" || ! -f "$archive" ]]; then
    echo "Missing expected package archive: $archive" >&2
    exit 1
  fi
done

node "$SCRIPT_DIR/pack-smoke-config.mjs" \
  "$CONSUMER_DIR/package.json" \
  "${protocol_tgz[0]}" \
  "$pbvex_package" \
  "${client_tgz[0]}" \
  "${react_tgz[0]}" \
  "${svelte_tgz[0]}"

cat > "$CONSUMER_DIR/tsconfig.json" <<'EOF'
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "strict": true,
    "skipLibCheck": false
  },
  "include": ["smoke.ts"]
}
EOF

cat > "$CONSUMER_DIR/smoke.ts" <<'EOF'
import { canonicalJson, type FunctionReference } from '@pbvex/protocol';
import {
  action,
  defineApp,
  defineComponent,
  defineComponentFns,
  mount,
  mutation,
  query,
  type ActionCtx,
  type MutationCtx,
  type QueryCtx,
  type StorageId,
} from 'pbvex/server';
import type { TypedComponent } from 'pbvex/component';
import { v } from 'pbvex/values';
import { Client } from '@pbvex/client';
import { PBVexProvider, useQuery } from '@pbvex/react';
import { createQuery } from '@pbvex/svelte';

const definition = query({
  args: { value: v.string() },
  returns: v.string(),
  handler: async (_ctx, args) => args.value,
});
const component = defineComponent({
  modulePaths: ['store.ts'],
  args: v.object({ label: v.string() }),
});
const componentFns = defineComponentFns(component);
const componentQuery = componentFns.query({
  returns: v.string(),
  handler: async (ctx) => {
    const identity = await ctx.auth.getUserIdentity();
    const url = await ctx.storage.getUrl('storage-id' as StorageId);
    return `${ctx.args.label}:${identity?.subject ?? 'anonymous'}:${url ?? ''}`;
  },
});
const app = defineApp({ components: [mount(component, 'packed', { args: { label: 'ok' } })] });
const jobRef = { _path: 'messages_schedule', _type: 'mutation', _visibility: 'internal' } as
  FunctionReference<'mutation', { value: string }, null, 'internal'>;
const capabilityMutation = mutation({
  handler: async (ctx) => {
    const identity = await ctx.auth.getUserIdentity();
    const uploadUrl = await ctx.storage.generateUploadUrl();
    const job = await ctx.scheduler.runAfter(0, jobRef, { value: 'scheduled' });
    return { identity, uploadUrl, job };
  },
});
const capabilityAction = action({
  handler: async (ctx) => {
    const uploadUrl = await ctx.storage.generateUploadUrl();
    return { uploadUrl, identity: await ctx.auth.getUserIdentity() };
  },
});
const ref = { _path: 'messages_get', _type: 'query', _visibility: 'public' } as
  FunctionReference<'query', { value: string }, string, 'public'>;
const client = new Client('http://127.0.0.1:8090');
const call: Promise<string> = client.query(ref, { value: 'ok' });

void canonicalJson;
void definition;
void componentQuery;
void app;
void capabilityMutation;
void capabilityAction;
void (null as unknown as TypedComponent);
void (null as unknown as QueryCtx);
void (null as unknown as MutationCtx);
void (null as unknown as ActionCtx);
void call;
void PBVexProvider;
void useQuery;
void createQuery;
EOF

cat > "$CONSUMER_DIR/smoke.mjs" <<'EOF'
import { canonicalJson } from '@pbvex/protocol';
import { defineApp, defineComponent, mount, query } from 'pbvex/server';
import * as componentSdk from 'pbvex/component';
import { v } from 'pbvex/values';
import { Client } from '@pbvex/client';
import * as reactSdk from '@pbvex/react';
import * as svelteSdk from '@pbvex/svelte';

if (canonicalJson({ ok: true }) !== '{"ok":true}') throw new Error('protocol import failed');
if (typeof query !== 'function' || typeof v.string !== 'function') throw new Error('authoring SDK import failed');
const component = defineComponent({ modulePaths: ['store.ts'] });
if (defineApp({ components: [mount(component, 'packed')] }).kind !== 'app') throw new Error('component authoring failed');
if (typeof componentSdk.defineComponent !== 'function') throw new Error('component subpath import failed');
if (typeof Client !== 'function') throw new Error('core SDK import failed');
if (typeof reactSdk.useQuery !== 'function') throw new Error('React SDK import failed');
if (typeof svelteSdk.createQuery !== 'function') throw new Error('Svelte SDK import failed');
EOF

cd "$CONSUMER_DIR"
pnpm install --no-frozen-lockfile
pnpm typecheck
pnpm smoke
./node_modules/.bin/pbvex --help >/dev/null

echo "Packed package consumer smoke test passed."
