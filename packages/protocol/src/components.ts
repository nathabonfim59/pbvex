import type {
  ComponentDefinition,
  ComponentGraph,
  ComponentMount,
  FunctionDescriptor,
  JSONValue,
  ModuleSource,
} from './types.js';
import { canonicalHash, canonicalHashSync, hashSha256Bytes } from './canonical.js';
import {
  hasOnlyKeys,
  isBase64String,
  isComponentId,
  isComponentRelativeModulePath,
  isIdentifier,
  isJsonValue,
  isModulePath,
  isSafeFieldName,
  isSha256Hex,
  isValidatorDescriptor,
  validateValue,
} from './validators.js';

const PREFIX = 'pbvex/components/';
const MAX_DEFINITIONS = 1024;
const MAX_DEPTH = 32;

export function validateComponents(value: unknown): ComponentGraph | undefined {
  if (value === undefined) return undefined;
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('components must be an object');
  const raw = value as Record<string, unknown>;
  if (!hasOnlyKeys(raw, ['definitions', 'mounts'])) throw new Error('components has unknown fields');
  if (!Array.isArray(raw.definitions) || raw.definitions.length > MAX_DEFINITIONS) {
    throw new Error('components.definitions must be a bounded array');
  }
  if (!Array.isArray(raw.mounts)) throw new Error('components.mounts must be an array');

  const definitions = raw.definitions.map(validateDefinition);
  const byId = new Map<string, ComponentDefinition>();
  for (const definition of definitions) {
    if (byId.has(definition.componentId)) throw new Error(`duplicate componentId ${definition.componentId}`);
    byId.set(definition.componentId, definition);
  }
  validateDependencyGraph(definitions, byId);
  const seen = new Set<string>();
  const mounts = raw.mounts.map((mount) => validateMount(mount, '', byId, seen, 1));
  validateMountModuleOwnership(definitions, mounts);
  return definitions.length === 0 && mounts.length === 0 ? undefined : { definitions, mounts };
}

function validateMountModuleOwnership(definitions: ComponentDefinition[], mounts: ComponentMount[]): void {
  const byId = new Map(definitions.map((definition) => [definition.componentId, definition]));
  const visit = (mount: ComponentMount, parent: string): void => {
    const path = parent ? `${parent}/${mount.name}` : mount.name;
    const childNames = new Set((mount.children ?? []).map((child) => child.name));
    const definition = byId.get(mount.componentId)!;
    for (const relative of definition.modulePaths) {
      const firstSlash = relative.indexOf('/');
      const firstSegment = firstSlash < 0 ? relative : relative.slice(0, firstSlash);
      if (childNames.has(firstSegment)) {
        throw new Error(`component module ${path}/${relative} from mount ${path} collides with descendant mount ${path}/${firstSegment}`);
      }
    }
    for (const child of mount.children ?? []) visit(child, path);
  };
  for (const mount of mounts) visit(mount, '');
}

function validateDefinition(value: unknown, index: number): ComponentDefinition {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`components.definitions[${index}] must be an object`);
  const raw = value as Record<string, unknown>;
  if (!hasOnlyKeys(raw, ['componentId', 'modulePaths', 'moduleHashes', 'schema', 'args', 'env', 'dependencies'])) {
    throw new Error(`components.definitions[${index}] has unknown fields`);
  }
  if (!isComponentId(raw.componentId)) throw new Error(`components.definitions[${index}] componentId is invalid`);
  if (!Array.isArray(raw.modulePaths) || raw.modulePaths.length === 0) throw new Error(`components.definitions[${index}] modulePaths is invalid`);
  const modulePaths = raw.modulePaths.map((path) => {
    if (!isComponentRelativeModulePath(path)) throw new Error(`components.definitions[${index}] modulePath is invalid`);
    return path as string;
  });
  if (new Set(modulePaths).size !== modulePaths.length) throw new Error(`components.definitions[${index}] has duplicate modulePaths`);
  if (!raw.moduleHashes || typeof raw.moduleHashes !== 'object' || Array.isArray(raw.moduleHashes)) throw new Error(`components.definitions[${index}] moduleHashes is invalid`);
  const moduleHashes = raw.moduleHashes as Record<string, unknown>;
  if (Object.keys(moduleHashes).length !== modulePaths.length || modulePaths.some((path) => !isSha256Hex(moduleHashes[path]))) {
    throw new Error(`components.definitions[${index}] moduleHashes do not match modulePaths`);
  }
  if (raw.args !== undefined && !isValidatorDescriptor(raw.args)) throw new Error(`components.definitions[${index}] args is invalid`);
  const env = validateEnv(raw.env, index);
  const dependencies = raw.dependencies === undefined ? [] : raw.dependencies;
  if (!Array.isArray(dependencies) || dependencies.some((id) => !isComponentId(id)) || new Set(dependencies).size !== dependencies.length) {
    throw new Error(`components.definitions[${index}] dependencies are invalid`);
  }
  return {
    componentId: raw.componentId,
    modulePaths,
    moduleHashes: moduleHashes as Record<string, string>,
    schema: raw.schema as ComponentDefinition['schema'],
    args: raw.args as JSONValue | undefined,
    env,
    dependencies: dependencies as string[],
  };
}

function validateEnv(value: unknown, index: number): ComponentDefinition['env'] {
  if (value === undefined) return undefined;
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`components.definitions[${index}] env is invalid`);
  const out: NonNullable<ComponentDefinition['env']> = {};
  for (const [key, entry] of Object.entries(value)) {
    if (!isSafeFieldName(key) || !entry || typeof entry !== 'object' || Array.isArray(entry)) throw new Error(`components.definitions[${index}] env is invalid`);
    const raw = entry as Record<string, unknown>;
    if (!hasOnlyKeys(raw, ['type', 'value', 'name'])) throw new Error(`components.definitions[${index}] env is invalid`);
    if (raw.type === 'value' && typeof raw.value === 'string' && raw.value.length > 0 && raw.name === undefined) out[key] = { type: 'value', value: raw.value };
    else if (raw.type === 'envVar' && typeof raw.name === 'string' && raw.name.length > 0 && raw.value === undefined) out[key] = { type: 'envVar', name: raw.name };
    else throw new Error(`components.definitions[${index}] env is invalid`);
  }
  return out;
}

function validateDependencyGraph(definitions: ComponentDefinition[], byId: Map<string, ComponentDefinition>): void {
  const visit = (id: string, stack: Set<string>, depth: number): void => {
    if (depth > MAX_DEPTH || stack.has(id)) throw new Error('cyclic or too-deep component dependency');
    const definition = byId.get(id);
    if (!definition) throw new Error(`unknown component dependency ${id}`);
    stack.add(id);
    for (const dependency of definition.dependencies ?? []) visit(dependency, stack, depth + 1);
    stack.delete(id);
  };
  for (const definition of definitions) visit(definition.componentId, new Set(), 1);
}

function validateMount(value: unknown, parent: string, definitions: Map<string, ComponentDefinition>, seen: Set<string>, depth: number): ComponentMount {
  if (depth > MAX_DEPTH || !value || typeof value !== 'object' || Array.isArray(value)) throw new Error('component mount is invalid');
  const raw = value as Record<string, unknown>;
  if (!hasOnlyKeys(raw, ['name', 'componentId', 'args', 'children']) || !isIdentifier(raw.name) || !isComponentId(raw.componentId)) throw new Error('component mount is invalid');
  const path = parent ? `${parent}/${raw.name}` : raw.name as string;
  if (seen.has(path)) throw new Error(`duplicate component mount path ${path}`);
  seen.add(path);
  const definition = definitions.get(raw.componentId as string);
  if (!definition) throw new Error(`unknown component ${raw.componentId as string}`);
  const hasArgs = Object.prototype.hasOwnProperty.call(raw, 'args');
  if (hasArgs && definition.args === undefined) throw new Error(`mount ${path} does not accept args`);
  if (hasArgs && (!isJsonValue(raw.args) || !validateValue(definition.args!, raw.args, false))) throw new Error(`mount ${path} args are invalid`);
  if (!hasArgs && definition.args !== undefined && !descriptorAllowsMissing(definition.args)) throw new Error(`mount ${path} requires args`);
  const children = raw.children === undefined ? undefined : raw.children;
  if (children !== undefined && !Array.isArray(children)) throw new Error(`mount ${path} children must be an array`);
  return {
    name: raw.name as string,
    componentId: raw.componentId as string,
    ...(hasArgs ? { args: raw.args as JSONValue } : {}),
    ...(children ? { children: children.map((child) => validateMount(child, path, definitions, seen, depth + 1)) } : {}),
  };
}

function descriptorAllowsMissing(value: JSONValue): boolean {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const raw = value as Record<string, JSONValue>;
  if (raw.type === 'optional' || raw.type === 'defaulted') return true;
  if (raw.type === 'union' && Array.isArray(raw.validators)) return raw.validators.some(descriptorAllowsMissing);
  if (raw.type === 'object' && raw.shape && typeof raw.shape === 'object' && !Array.isArray(raw.shape)) return Object.values(raw.shape).every(descriptorAllowsMissing);
  return false;
}

export function validateComponentFunctionBinding(functions: FunctionDescriptor[], graph?: ComponentGraph): void {
  for (const fn of functions) {
    if (!fn.modulePath.startsWith(PREFIX)) continue;
    const owner = graph ? findMount(graph, fn.modulePath) : undefined;
    if (!owner) throw new Error(`function ${fn.name} does not match a component mount`);
    const definition = graph!.definitions.find((candidate) => candidate.componentId === owner.mount.componentId)!;
    const relative = fn.modulePath.slice(`${PREFIX}${owner.path}/`.length);
    if (!definition.modulePaths.includes(relative)) throw new Error(`function ${fn.name} module is not declared by component ${owner.path}`);
  }
}

function findMount(graph: ComponentGraph, modulePath: string): { path: string; mount: ComponentMount } | undefined {
  let best: { path: string; mount: ComponentMount } | undefined;
  const walk = (mounts: ComponentMount[], parent: string): void => {
    for (const mount of mounts) {
      const path = parent ? `${parent}/${mount.name}` : mount.name;
      if (modulePath.startsWith(`${PREFIX}${path}/`) && (!best || path.length > best.path.length)) best = { path, mount };
      walk(mount.children ?? [], path);
    }
  };
  walk(graph.mounts, '');
  return best;
}

export async function computeComponentId(definition: ComponentDefinition, bundleSha: string): Promise<string> {
  return `def_${await canonicalHash(componentHashInput(definition, bundleSha))}`;
}

export async function authenticateComponentIds(graph: ComponentGraph | undefined, bundleSha: string): Promise<void> {
  for (const definition of graph?.definitions ?? []) {
    if (definition.componentId !== await computeComponentId(definition, bundleSha)) throw new Error(`componentId ${definition.componentId} does not match content hash`);
  }
}

export function authenticateComponentIdsSync(graph: ComponentGraph | undefined, bundleSha: string): void {
  for (const definition of graph?.definitions ?? []) {
    const input = componentHashInput(definition, bundleSha);
    if (definition.componentId !== `def_${canonicalHashSync(input)}`) throw new Error(`componentId ${definition.componentId} does not match content hash`);
  }
}

function componentHashInput(definition: ComponentDefinition, bundleSha: string): Record<string, JSONValue> {
  const input: Record<string, JSONValue> = {
    modulePaths: definition.modulePaths,
    moduleHashes: definition.moduleHashes ?? {},
    dependencies: definition.dependencies ?? [],
    bundleSha,
  };
  if (definition.schema !== undefined) input.schema = definition.schema as JSONValue;
  if (definition.args !== undefined) input.args = definition.args;
  if (definition.env !== undefined) input.env = definition.env as JSONValue;
  return input;
}

export function validateModuleSources(value: unknown): ModuleSource[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > 16 * 1024) throw new Error('modules must be a bounded array');
  const seen = new Set<string>();
  return value.map((entry, index) => {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry) || !hasOnlyKeys(entry as Record<string, unknown>, ['path', 'bytes'])) throw new Error(`modules[${index}] is invalid`);
    const { path, bytes } = entry as Record<string, unknown>;
    if (!isModulePath(path) || seen.has(path) || !isBase64String(bytes)) throw new Error(`modules[${index}] is invalid`);
    seen.add(path);
    return { path, bytes };
  });
}

export async function verifyModuleSources(modules: ModuleSource[], graph?: ComponentGraph): Promise<void> {
  if (!graph) return;
  const hashes = new Map<string, string>();
  for (const module of modules) hashes.set(module.path, await hashSha256Bytes(base64(module.bytes)));
  const declared = new Set<string>();
  const walk = (mounts: ComponentMount[], parent: string): void => {
    for (const mount of mounts) {
      const path = parent ? `${parent}/${mount.name}` : mount.name;
      const definition = graph.definitions.find((candidate) => candidate.componentId === mount.componentId)!;
      for (const relative of definition.modulePaths) {
        const full = `${PREFIX}${path}/${relative}`;
        declared.add(full);
        if (hashes.get(full) !== definition.moduleHashes?.[relative]) throw new Error(`module ${full} is missing or has a hash mismatch`);
      }
      walk(mount.children ?? [], path);
    }
  };
  walk(graph.mounts, '');
  for (const path of hashes.keys()) if (path.startsWith(PREFIX) && !declared.has(path)) throw new Error(`module ${path} is not declared by a component mount`);
}

function base64(value: string): ArrayBuffer {
  const buffer = Buffer.from(value, 'base64');
  if (buffer.toString('base64') !== value) throw new Error('module bytes must be canonical base64');
  return buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength) as ArrayBuffer;
}
