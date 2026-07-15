#!/usr/bin/env bash
set -euo pipefail

# Standalone PBVex backend smoke test.
# Runs the pbvex binary from a clean, isolated temp directory and verifies:
#  - default data directory is ./pb_data (cwd), not the executable directory
#  - explicit --dir works and writes data to the specified directory
#  - a missing public directory does not prevent API startup
#  - version output is source-backed (not "untracked")
#  - PocketBase health and PBVex endpoints respond correctly
#  - a real artifact can be deployed, activated, queried, and mutated
#  - realtime emits an initial query result
#  - the active deployment and application data survive a process restart
#  - the server can be stopped cleanly

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

BINARY=""
EXPECTED_VERSION=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      BINARY="$2"
      shift 2
      ;;
    --version)
      EXPECTED_VERSION="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

TMP_DIR="$(mktemp -d)"

PID=""
PORT=""
LOG_FILE=""
BIN_DIR="$TMP_DIR/bin"
ENV_BIN_DIR="$TMP_DIR/envbin"
APP_DIR="$TMP_DIR/app"
ARTIFACT_PATH="$APP_DIR/.pbvex/dist/artifact.json"
SUPERUSER_EMAIL="smoke@example.com"
SUPERUSER_PASSWORD="pbvex-smoke-password"
AUTH_TOKEN=""
CREATE_FUNCTION=""
LIST_FUNCTION=""
COMPONENT_CREATE_FUNCTION=""
COMPONENT_LIST_FUNCTION=""

stop_server() {
  if [[ -n "${PID:-}" ]] && kill -0 "$PID" 2>/dev/null; then
    kill -TERM "$PID" 2>/dev/null || true
    local stopped=0
    for i in $(seq 1 30); do
      if ! kill -0 "$PID" 2>/dev/null; then
        stopped=1
        break
      fi
      sleep 0.5
    done
    if [[ "$stopped" -eq 0 ]]; then
      kill -9 "$PID" 2>/dev/null || true
    fi
    wait "$PID" 2>/dev/null || true
    PID=""
  fi
}

cleanup() {
  stop_server
  chmod -R +w "$TMP_DIR" 2>/dev/null || true
  rm -rf "$TMP_DIR" 2>/dev/null || true
}

trap cleanup EXIT

mkdir -p "$BIN_DIR" "$ENV_BIN_DIR"

if [[ -n "$BINARY" ]]; then
  cp -p "$BINARY" "$BIN_DIR/pbvex"
else
  VERSION_FLAG="0.0.0"
  [[ -n "$EXPECTED_VERSION" ]] && VERSION_FLAG="$EXPECTED_VERSION"
  (cd "$REPO_ROOT/backend" && CGO_ENABLED=0 go build \
    -ldflags "-X github.com/pocketbase/pocketbase.Version=$VERSION_FLAG" \
    -o "$BIN_DIR/pbvex" ./cmd/pbvex)
  EXPECTED_VERSION="${EXPECTED_VERSION:-$VERSION_FLAG}"
fi
chmod +x "$BIN_DIR/pbvex"
chmod -R a-w "$BIN_DIR"

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(('127.0.0.1', 0))
print(s.getsockname()[1])
s.close()
PY
}

assert_isolated() {
  local dir="$1"
  for name in pb_public packages dist; do
    if [[ -e "$dir/$name" ]]; then
      echo "Unexpected sibling dependency found in $dir: $name" >&2
      return 1
    fi
  done
}

assert_static_linkage() {
  local binary="$1"
  local file_output
  if command -v file >/dev/null 2>&1; then
    file_output="$(file "$binary" 2>&1 || true)"
    if [[ "$file_output" == *"statically linked"* ]]; then
      return 0
    fi
    echo "Static linkage check failed: $file_output" >&2
    return 1
  fi
  if command -v ldd >/dev/null 2>&1; then
    if ldd "$binary" 2>&1 | grep -q 'not a dynamic executable'; then
      return 0
    fi
    echo "Static linkage check failed: binary is dynamically linked" >&2
    ldd "$binary" >&2 || true
    return 1
  fi
  echo "Neither file nor ldd available; skipping static linkage check" >&2
}

assert_tools_hidden() {
  # Ensure the child environment cannot find node, npm, pnpm, or git.
  env -i HOME="$HOME" PATH="$ENV_BIN_DIR" /bin/sh -c '
    for cmd in node npm pnpm git; do
      if command -v "$cmd" >/dev/null 2>&1; then
        echo "$cmd is available in restricted PATH" >&2
        exit 1
      fi
    done
  '
}

prepare_artifact() {
  if [[ ! -f "$REPO_ROOT/packages/pbvex/dist/cli/index.js" ]]; then
    echo "PBVex CLI is not built; run pnpm build before scripts/smoke.sh" >&2
    return 1
  fi

  mkdir -p "$APP_DIR/pbvex"
  cat > "$APP_DIR/pbvex/pbvex.config.ts" <<'EOF'
export default { targets: { local: { url: 'http://127.0.0.1:8090' } } };
EOF
  cat > "$APP_DIR/pbvex/schema.ts" <<'EOF'
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  messages: defineTable({ body: v.string() }),
});
EOF
  cat > "$APP_DIR/pbvex/smoke.ts" <<'EOF'
import { mutation, query } from 'pbvex/server';
import { v } from 'pbvex/values';

export const create = mutation({
  args: { body: v.string() },
  returns: v.string(),
  handler: async (ctx, args) => ctx.db.insert('messages', { body: args.body }),
});

export const list = query({
  args: {},
  returns: v.array(v.string()),
  handler: async (ctx) => {
    const messages = await ctx.db.query('messages').collect();
    return messages.map((message) => message.body);
  },
});
EOF
  mkdir -p "$APP_DIR/pbvex/components/counter"
  cat > "$APP_DIR/pbvex/components/counter/component.ts" <<'EOF'
import { defineComponent } from 'pbvex/server';
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export const counter = defineComponent({
  modulePaths: ['store.ts'],
  schema: defineSchema({ entries: defineTable({ value: v.string() }) }),
});
EOF
  cat > "$APP_DIR/pbvex/components/counter/store.ts" <<'EOF'
import { defineComponentFns } from 'pbvex/server';
import { v } from 'pbvex/values';
import { counter } from './component.js';

const functions = defineComponentFns(counter);

export const componentCreate = functions.mutation({
  args: { value: v.string() },
  returns: v.id('entries'),
  handler: async (ctx, args) => ctx.db.insert('entries', { value: args.value }),
});

export const componentList = functions.query({
  args: {},
  returns: v.array(v.string()),
  handler: async (ctx) => {
    const entries = await ctx.db.query('entries').collect();
    return entries.map((entry) => entry.value);
  },
});
EOF
  cat > "$APP_DIR/pbvex/app.ts" <<'EOF'
import { defineApp, mount } from 'pbvex/server';
import { counter } from './components/counter/component.js';

export default defineApp({ components: [mount(counter, 'counter')] });
EOF

  (cd "$APP_DIR" && node "$REPO_ROOT/packages/pbvex/dist/cli/index.js" build >/dev/null)
  [[ -s "$ARTIFACT_PATH" ]]
  readarray -t function_names < <(python3 - "$ARTIFACT_PATH" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    functions = json.load(f)['manifest']['functions']
by_export = {fn['exportName']: fn['name'] for fn in functions}
print(by_export['create'])
print(by_export['list'])
print(by_export['componentCreate'])
print(by_export['componentList'])
PY
)
  CREATE_FUNCTION="${function_names[0]}"
  LIST_FUNCTION="${function_names[1]}"
  COMPONENT_CREATE_FUNCTION="${function_names[2]}"
  COMPONENT_LIST_FUNCTION="${function_names[3]}"
}

create_superuser() {
  local data_dir="$1"
  env -i HOME="$HOME" PATH="$ENV_BIN_DIR" \
    "$BIN_DIR/pbvex" --dir "$data_dir" superuser create \
    "$SUPERUSER_EMAIL" "$SUPERUSER_PASSWORD" >/dev/null
}

authenticate_superuser() {
  local body
  body="$(curl -fsS -X POST -H 'Content-Type: application/json' \
    -d "{\"identity\":\"$SUPERUSER_EMAIL\",\"password\":\"$SUPERUSER_PASSWORD\"}" \
    "http://127.0.0.1:$PORT/api/collections/_superusers/auth-with-password")"
  AUTH_TOKEN="$(python3 - "$body" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
assert isinstance(d.get('token'), str) and d['token'], sys.argv[1]
print(d['token'])
PY
)"
}

deploy_and_activate() {
  local body deployment_id
  body="$(curl -fsS -X POST \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H 'Content-Type: application/json' \
    --data-binary "@$ARTIFACT_PATH" \
    "http://127.0.0.1:$PORT/api/pbvex/deployments")"
  deployment_id="$(python3 - "$body" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
assert isinstance(d.get('deploymentId'), str) and d['deploymentId'], sys.argv[1]
print(d['deploymentId'])
PY
)"
  curl -fsS -X POST \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H 'Content-Type: application/json' \
    -d '{"atomic":true}' \
    "http://127.0.0.1:$PORT/api/pbvex/deployments/$deployment_id/activate" >/dev/null
}

call_function() {
  local name="$1"
  local args="$2"
  curl -fsS -X POST -H 'Content-Type: application/json' \
    -d "{\"name\":\"$name\",\"args\":$args}" \
    "http://127.0.0.1:$PORT/api/pbvex/call"
}

create_message() {
  local body
  body="$(call_function "$CREATE_FUNCTION" '{"body":"survives restart"}')"
  python3 - "$body" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
assert isinstance(d.get('result'), str) and d['result'], sys.argv[1]
PY
}

assert_message_list() {
  local body
  body="$(call_function "$LIST_FUNCTION" '{}')"
  python3 - "$body" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
assert d.get('result') == ['survives restart'], sys.argv[1]
PY
}

create_component_entry() {
  local body
  body="$(call_function "$COMPONENT_CREATE_FUNCTION" '{"value":"component survives restart"}')"
  python3 - "$body" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
assert isinstance(d.get('result'), str) and d['result'].startswith('pbv2.'), sys.argv[1]
PY
}

assert_component_list() {
  local body
  body="$(call_function "$COMPONENT_LIST_FUNCTION" '{}')"
  python3 - "$body" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
assert d.get('result') == ['component survives restart'], sys.argv[1]
PY
}

assert_realtime_initial_result() {
  local subscription_id request output
  subscription_id="$(python3 - "$LIST_FUNCTION" <<'PY'
import hashlib, sys
path = sys.argv[1]
print(hashlib.sha256(f'v1:{path}:{{}}'.encode()).hexdigest())
PY
)"
  request="{\"id\":\"$subscription_id\",\"path\":\"$LIST_FUNCTION\",\"args\":{}}"
  output="$(curl -s -N --max-time 4 -X POST \
    -H 'Accept: text/event-stream' \
    -H 'Content-Type: application/json' \
    -d "$request" \
    "http://127.0.0.1:$PORT/api/pbvex/realtime" || true)"
  python3 - "$output" <<'PY'
import json, sys
events = []
for line in sys.argv[1].splitlines():
    if line.startswith('data:'):
        events.append(json.loads(line[5:].strip()))
assert any(e.get('data', {}).get('op') == 'subscribe' for e in events), sys.argv[1]
assert any(e.get('data', {}).get('op') == 'message' and e['data'].get('payload') == ['survives restart'] for e in events), sys.argv[1]
PY
}

start_server() {
  local run_dir="$1"
  local log_file="$2"
  local data_dir="${3:-}"
  local admin_ui="${4:-false}"
  local -a command=("$BIN_DIR/pbvex")
  mkdir -p "$run_dir"
  assert_isolated "$run_dir"
  PORT="$(free_port)"
  LOG_FILE="$log_file"
  cd "$run_dir"
  # Run the binary with a minimal environment so it cannot accidentally rely on
  # the repository checkout, node/pnpm, or a sibling public directory. The test
  # harness still uses curl/python from the caller's environment.
  if [[ -n "$data_dir" ]]; then
    command+=(--dir "$data_dir")
  fi
  command+=(serve --http "127.0.0.1:$PORT")
  if [[ "$admin_ui" == "true" ]]; then
    command+=(--admin-ui)
  fi
  env -i HOME="$HOME" PATH="$ENV_BIN_DIR" \
    "${command[@]}" > "$LOG_FILE" 2>&1 &
  PID=$!
}

wait_for_health() {
  local healthy=0
  for i in $(seq 1 60); do
    if curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/api/health" 2>/dev/null | grep -q '^200$'; then
      healthy=1
      break
    fi
    sleep 0.5
  done
  if [[ "$healthy" -eq 0 ]]; then
    echo "Server did not become healthy" >&2
    cat "$LOG_FILE" >&2 || true
    return 1
  fi
}

api_health_ok() {
  local body
  body="$(curl -s "http://127.0.0.1:$PORT/api/health")"
  python3 - "${body}" <<'PY'
import json, sys
body = sys.argv[1]
d = json.loads(body)
assert d.get('code') == 200, body
assert 'API is healthy' in d.get('message', ''), body
PY
}

root_returns_404() {
  local status
  status="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/")"
  [[ "$status" == "404" ]]
}

admin_ui_disabled() {
  local status
  status="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/_/")"
  [[ "$status" == "404" ]]
}

admin_ui_enabled() {
  local status
  status="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/_/")"
  [[ "$status" == "200" ]]
}

pbvex_call_returns_validation_error() {
  local body
  body="$(curl -s -X POST -H 'Content-Type: application/json' -d '{}' \
    "http://127.0.0.1:$PORT/api/pbvex/call")"
  python3 - "$body" <<'PY'
import json, sys
body = sys.argv[1]
d = json.loads(body)
assert d.get('error') is True, body
assert d.get('code') == 'bad_request', body
assert 'Missing function name' in d.get('message', ''), body
assert d.get('requestId'), body
PY
}

pbvex_deployments_requires_auth() {
  local status
  status="$(curl -s -o /dev/null -w '%{http_code}' -X POST \
    "http://127.0.0.1:$PORT/api/pbvex/deployments")"
  [[ "$status" == "401" ]]
}

version_ok() {
  local output
  output="$("$BIN_DIR/pbvex" --version)"
  if [[ "$output" == *"untracked"* ]]; then
    echo "Version output is untracked: $output" >&2
    return 1
  fi
  if [[ -n "$EXPECTED_VERSION" ]] && [[ "$output" != *"$EXPECTED_VERSION"* ]]; then
    echo "Expected version $EXPECTED_VERSION, got $output" >&2
    return 1
  fi
  echo "Version output: $output"
}

default_data_dir_works() {
  local run_dir="$TMP_DIR/run_default"
  echo "=== Scenario: default data directory (no --dir) ==="
  start_server "$run_dir" "$TMP_DIR/pbvex_default.log" ""
  wait_for_health

  api_health_ok
  root_returns_404
  admin_ui_disabled
  pbvex_call_returns_validation_error
  pbvex_deployments_requires_auth

  if [[ ! -e "$run_dir/pb_data/data.db" ]]; then
    echo "Default data directory was not created in cwd: $run_dir/pb_data" >&2
    return 1
  fi
  if [[ -e "$BIN_DIR/pb_data" ]]; then
    echo "Data directory was created next to the executable" >&2
    return 1
  fi

  stop_server
  assert_isolated "$run_dir"
  echo "Default data directory scenario passed."
}

explicit_data_dir_works() {
  local run_dir="$TMP_DIR/run_explicit"
  local data_dir="$TMP_DIR/data_explicit"
  echo "=== Scenario: explicit --dir data directory ==="
  create_superuser "$data_dir"
  start_server "$run_dir" "$TMP_DIR/pbvex_explicit.log" "$data_dir"
  wait_for_health

  api_health_ok
  root_returns_404
  admin_ui_disabled
  pbvex_call_returns_validation_error
  pbvex_deployments_requires_auth
  authenticate_superuser
  deploy_and_activate
  create_message
  assert_message_list
  create_component_entry
  assert_component_list
  assert_realtime_initial_result

  if [[ ! -e "$data_dir/data.db" ]]; then
    echo "Explicit data directory was not used for persistence" >&2
    return 1
  fi
  if [[ -e "$run_dir/pb_data" ]]; then
    echo "Data directory was created in cwd despite --dir" >&2
    return 1
  fi
  if [[ -e "$BIN_DIR/pb_data" ]]; then
    echo "Data directory was created next to the executable" >&2
    return 1
  fi

  stop_server

  echo "=== Scenario: restart with the same data directory ==="
  start_server "$run_dir" "$TMP_DIR/pbvex_restart.log" "$data_dir"
  wait_for_health
  assert_message_list
  assert_component_list
  assert_realtime_initial_result
  stop_server

  assert_isolated "$run_dir"
  echo "Explicit data directory scenario passed."
}

admin_ui_opt_in_works() {
  local run_dir="$TMP_DIR/run_admin_ui"
  echo "=== Scenario: opt-in admin UI ==="
  start_server "$run_dir" "$TMP_DIR/pbvex_admin_ui.log" "" true
  wait_for_health

  api_health_ok
  root_returns_404
  admin_ui_enabled

  stop_server
  assert_isolated "$run_dir"
  echo "Opt-in admin UI scenario passed."
}

# Verify the binary advertises a cwd-relative default data directory.
if ! "$BIN_DIR/pbvex" --help | grep -F 'default "./pb_data"' >/dev/null; then
  echo "Default data directory is not working-directory-relative" >&2
  exit 1
fi

# Verify the upstream PocketBase self-update command is not exposed.
if "$BIN_DIR/pbvex" --help | grep -F 'update' >/dev/null; then
  echo "The update command is exposed; it could self-update from upstream PocketBase" >&2
  exit 1
fi

assert_static_linkage "$BIN_DIR/pbvex"
assert_tools_hidden
prepare_artifact

version_ok
default_data_dir_works
explicit_data_dir_works
admin_ui_opt_in_works

echo "Smoke test passed."
