export interface OpaqueIdEnvelope {
  version: 1 | 2;
  keyId: bigint;
  namespace: string;
  table: string;
  raw: string;
  mac: Uint8Array;
}

const IDENTIFIER = /^[A-Za-z][A-Za-z0-9_]*$/;
const COMPONENT_NAMESPACE = /^cmp_[a-z2-7]{52}$/;

export function parseOpaqueId(value: string, allowLegacyRoot = true): OpaqueIdEnvelope | undefined {
  if (value.length > 4096) return undefined;
  const parts = value.split('.');
  if (parts.length !== 4 || (parts[0] !== 'pbv1' && parts[0] !== 'pbv2')) return undefined;
  const keyText = parts[1]!;
  if (!/^[1-9]\d*$/.test(keyText)) return undefined;
  const keyBig = BigInt(keyText);
  if (keyBig > 9223372036854775807n) return undefined;
  const payload = decodeRawUrl(parts[2]!);
  const mac = decodeRawUrl(parts[3]!);
  if (!payload || !mac || payload.length === 0 || payload.length > 2048 || mac.length !== 32) return undefined;
  const payloadText = decodeUtf8(payload);
  if (payloadText === undefined) return undefined;
  let parsed: unknown;
  try { parsed = JSON.parse(payloadText); } catch { return undefined; }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return undefined;
  const o = parsed as Record<string, unknown>;
  if (Object.keys(o).length !== 5 || !['v', 'k', 'n', 't', 'r'].every((key) => key in o)) return undefined;
  const version = parts[0] === 'pbv2' ? 2 : 1;
  if (o.v !== version || typeof o.k !== 'number' || typeof o.n !== 'string' || typeof o.t !== 'string' || typeof o.r !== 'string') return undefined;
  if (!IDENTIFIER.test(o.t) || o.t.length > 1024 || new TextEncoder().encode(o.r).length !== 15) return undefined;
  if (version === 1) {
    if (!allowLegacyRoot || o.n.length === 0) return undefined;
  } else if (o.n !== 'root' && !COMPONENT_NAMESPACE.test(o.n)) {
    return undefined;
  }
  const canonical = canonicalPayload(version, keyBig, o.n, o.t, o.r);
  if (payloadText !== canonical) return undefined;
  return { version, keyId: keyBig, namespace: o.n, table: o.t, raw: o.r, mac };
}

/** Formats a structural ID envelope with an already-authenticated MAC. */
export function formatOpaqueId(
  envelope: Omit<OpaqueIdEnvelope, 'mac'>,
  mac: Uint8Array,
): string {
  if (mac.length !== 32) throw new Error('opaque id MAC must be 32 bytes');
  const prefix = envelope.version === 2 ? 'pbv2' : 'pbv1';
  const payload = new TextEncoder().encode(canonicalPayload(
    envelope.version,
    envelope.keyId,
    envelope.namespace,
    envelope.table,
    envelope.raw,
  ));
  const value = `${prefix}.${envelope.keyId}.${encodeRawUrl(payload)}.${encodeRawUrl(mac)}`;
  if (!parseOpaqueId(value, true)) throw new Error('invalid opaque id envelope');
  return value;
}

function canonicalPayload(version: 1 | 2, keyId: bigint, namespace: string, table: string, raw: string): string {
  return `{"v":${version},"k":${keyId},"n":${goJsonString(namespace)},"t":${goJsonString(table)},"r":${goJsonString(raw)}}`;
}

function decodeRawUrl(value: string): Uint8Array | undefined {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) return undefined;
  try {
    let standard = value.replace(/-/g, '+').replace(/_/g, '/');
    while (standard.length % 4) standard += '=';
    const binary = atob(standard);
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
    return encodeRawUrl(bytes) === value ? bytes : undefined;
  } catch { return undefined; }
}

function encodeRawUrl(value: Uint8Array): string {
  let binary = '';
  for (const byte of value) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function decodeUtf8(value: Uint8Array): string | undefined {
  try { return new TextDecoder('utf-8', { fatal: true }).decode(value); } catch { return undefined; }
}

function goJsonString(value: string): string {
  return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, (char) => {
		const code = char.charCodeAt(0);
		return code <= 0xff ? `\\u00${code.toString(16).padStart(2, '0')}` : `\\u${code.toString(16)}`;
	});
}
