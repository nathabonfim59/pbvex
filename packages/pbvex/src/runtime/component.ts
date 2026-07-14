import type { ComponentDefinition, ComponentGraph, ComponentMount, SchemaDescriptor, JSONValue, PbvexValue } from '@pbvex/protocol';
import { canonicalHash, encodeValue, hashSha256 } from '@pbvex/protocol';
import type { SchemaDefinition } from '../schema/schema.js';
import { isSchemaDefinition } from '../schema/schema.js';
import type { Validator } from './values.js';
import { isValidator } from './values.js';

export const COMPONENT_KIND = 'component' as const;
export const APP_KIND = 'app' as const;

export type ComponentArgValidator = Validator<any, any>;

/** Env entry shape used in component declarations. */
export type EnvEntry = { type: 'value' | 'envVar'; value?: string; name?: string };

/** Resolve an env declaration to a typed key→string map (all env values are strings at runtime). */
export type ResolveEnv<E> = E extends Record<string, EnvEntry>
  ? { readonly [K in keyof E]: string }
  : Record<never, string>;

export interface ComponentOptions {
  modulePaths?: string[];
  schema?: SchemaDefinition;
  args?: ComponentArgValidator;
  env?: Record<string, EnvEntry>;
  dependencies?: ComponentDefinitionWithKind[];
}

export interface ComponentDefinitionWithKind extends Omit<ComponentDefinition, 'dependencies' | 'moduleHashes'> {
  kind: typeof COMPONENT_KIND;
  sourceModulePath?: string;
  dependencies?: ComponentDefinitionWithKind[];
}

/**
 * TypedComponent carries type parameters for compile-time inference.
 * OutArgs is the resolved type the handler sees (after defaults applied).
 * InArgs is the mount-input type (optional/defaulted fields may be omitted).
 * The phantom properties are set to null at runtime but typed for inference.
 */
export interface TypedComponent<
  OutArgs = undefined,
  Env extends Record<string, EnvEntry> | undefined = undefined,
  InArgs = OutArgs,
> extends ComponentDefinitionWithKind {
  /** @internal phantom — carries type info for inference (null at runtime) */
  readonly __pbvexArgs: { readonly _out: OutArgs; readonly _in: InArgs };
  /** @internal phantom — carries env type info (null at runtime) */
  readonly __pbvexEnv: { readonly _env: Env };
}

export interface AppDefinition {
  kind: typeof APP_KIND;
  components: ComponentMountDefinition[];
}

export interface ComponentMountDefinition {
  component: ComponentDefinitionWithKind;
  name: string;
  args?: JSONValue;
  children?: ComponentMountDefinition[];
}

/** Non-exported unique symbol — prevents direct construction of TypedMount. */
const __mountBrand: unique symbol = Symbol('__pbvexTypedMount');

/** A ComponentMountDefinition produced by mount(), carrying an opaque brand. */
export interface TypedMount extends ComponentMountDefinition {
  readonly [__mountBrand]: true;
}

export interface ComponentModule {
  path: string;
  code: string;
}

/**
 * defineComponent creates a typed component definition. OutArgs is inferred
 * from the args validator's output type; InArgs from the input type (which
 * marks optional/defaulted fields). The Env type parameter captures the
 * literal env key set for compile-time key checking.
 */
export function defineComponent<
  Args = undefined,
  const Env extends Record<string, EnvEntry> | undefined = undefined,
  InArgs = Args,
>(options: {
  modulePaths?: string[];
  schema?: SchemaDefinition;
  args?: Validator<Args, InArgs>;
  env?: Env;
  dependencies?: TypedComponent[];
}): TypedComponent<Args, Env, InArgs> {
  if (options.schema !== undefined && !isSchemaDefinition(options.schema)) {
    throw new Error('Component schema must be a SchemaDefinition');
  }
  if (options.args !== undefined && !isValidator(options.args)) {
    throw new Error('Component args must be a Validator');
  }
  const schema: SchemaDescriptor | undefined = options.schema
    ? (options.schema.toJSON() as unknown as SchemaDescriptor)
    : undefined;
  const args: JSONValue | undefined = options.args
    ? ((options.args as Validator<any>).toJSON() as JSONValue)
    : undefined;
  const modulePaths = options.modulePaths ? [...options.modulePaths] : [];
  const env = options.env ? { ...options.env } : undefined;
  const dependencies = options.dependencies ? [...options.dependencies] : undefined;

  const def = {
    kind: COMPONENT_KIND,
    sourceModulePath: undefined,
    componentId: '',
    modulePaths,
    schema,
    args,
    env,
    dependencies,
  };
  if (modulePaths.length) Object.freeze(modulePaths);
  if (dependencies) Object.freeze(dependencies);
  return Object.freeze(def) as unknown as TypedComponent<Args, Env, InArgs>;
}

/**
 * Conditional rest-tuple for mount options. Three levels:
 * 1. In = undefined (no args): options optional, args forbidden (args?: never)
 * 2. undefined extends In (top-level optional/defaulted): options optional
 * 3. {} extends In (all-optional object): options optional
 * 4. otherwise: options required with args
 */
type MountRest<In> =
  0 extends (1 & In)
    ? [options: { args: In; children?: TypedMount[] }]
    : [In] extends [undefined]
      ? [options?: { children?: TypedMount[] }]
      : undefined extends In
        ? [options?: { args?: In; children?: TypedMount[] }]
        : {} extends In
          ? [options?: { args?: In; children?: TypedMount[] }]
          : [options: { args: In; children?: TypedMount[] }];

/**
 * mount creates a TypedMount with compile-time args validation. The component
 * type C is matched exactly (not widened to a generic), and In is extracted
 * via conditional infer — so [In] extends [undefined] resolves correctly and
 * excess-property checks fire for no-args components.
 */
export function mount<C extends TypedComponent<any, any, any>>(
  component: C,
  name: string,
  ...rest: C extends TypedComponent<any, any, infer I> ? MountRest<I> : []
): TypedMount {
  const options = rest[0] as { args?: unknown; children?: TypedMount[] } | undefined;
  const args = options && 'args' in options && options.args !== undefined
    ? encodeValue(options.args as PbvexValue)
    : undefined;
  return {
    component: component as unknown as ComponentDefinitionWithKind,
    name,
    args,
    children: options?.children,
    [__mountBrand]: true as const,
  } as TypedMount;
}

/** defineApp accepts only TypedMount results from mount(), preventing bypass. */
export function defineApp(options: { components: TypedMount[] }): AppDefinition {
  return { kind: APP_KIND, components: options.components as ComponentMountDefinition[] };
}

export function isComponentDefinition(value: unknown): value is ComponentDefinitionWithKind {
  if (value === null || typeof value !== 'object') return false;
  const obj = value as Record<string, unknown>;
  return obj.kind === COMPONENT_KIND && typeof obj.componentId === 'string' && Array.isArray(obj.modulePaths);
}

export function isAppDefinition(value: unknown): value is AppDefinition {
  if (value === null || typeof value !== 'object') return false;
  const obj = value as Record<string, unknown>;
  return obj.kind === APP_KIND && Array.isArray(obj.components);
}

const MAX_COMPONENT_DEFINITIONS = 1024;
const MAX_COMPONENT_DEPTH = 32;

function componentRootDir(sourceModulePath: string): string {
  const lastSlash = sourceModulePath.lastIndexOf('/');
  return lastSlash > 0 ? sourceModulePath.slice(0, lastSlash) : '';
}

function relativeModulePath(rootDir: string, modulePath: string): string {
  const prefix = rootDir ? `${rootDir}/` : '';
  if (!modulePath.startsWith(prefix)) return '';
  return modulePath.slice(prefix.length);
}

export async function buildComponentGraph(
  components: ComponentDefinitionWithKind[],
  app: AppDefinition | undefined,
  functionModulePaths: string[],
  modules: ComponentModule[],
  bundleSha: string,
): Promise<ComponentGraph | undefined> {
  if (components.length > MAX_COMPONENT_DEFINITIONS) {
    throw new Error('Too many component definitions');
  }

  const modulesByPath = new Map<string, string>(modules.map((m) => [m.path, m.code]));
  const idMap = new Map<ComponentDefinitionWithKind, string>();
  const definitionMap = new Map<string, ComponentDefinition>();

  async function computeComponent(
    c: ComponentDefinitionWithKind,
    stack: ComponentDefinitionWithKind[],
  ): Promise<string> {
    if (idMap.has(c)) {
      return idMap.get(c)!;
    }
    if (stack.includes(c)) {
      throw new Error('Cyclic component dependency');
    }
    if (stack.length >= MAX_COMPONENT_DEPTH) {
      throw new Error('Component dependency depth exceeds 32');
    }
    if (definitionMap.size >= MAX_COMPONENT_DEFINITIONS) {
      throw new Error('Too many component definitions');
    }

    stack.push(c);
    try {
      const sourceModulePath = c.sourceModulePath;
      if (!sourceModulePath) {
        throw new Error('Component sourceModulePath is missing');
      }
      const rootDir = componentRootDir(sourceModulePath);

      let modulePaths: string[] = c.modulePaths.length > 0 ? [...c.modulePaths] : [];
      if (modulePaths.length === 0) {
        for (const functionModulePath of functionModulePaths) {
          const relative = relativeModulePath(rootDir, functionModulePath);
          if (relative) {
            modulePaths.push(relative);
          }
        }
        modulePaths = Array.from(new Set(modulePaths)).sort();
      }
      if (modulePaths.length === 0) {
        throw new Error(`Component ${sourceModulePath} has no modulePaths`);
      }

      const moduleHashes: Record<string, string> = {};
      for (const modulePath of modulePaths) {
        const fullPath = rootDir ? `${rootDir}/${modulePath}` : modulePath;
        const code = modulesByPath.get(fullPath);
        if (code === undefined) {
          throw new Error(`Module ${fullPath} not found for component ${sourceModulePath}`);
        }
        moduleHashes[modulePath] = await hashSha256(code);
      }

      const dependencyIds: string[] = [];
      if (c.dependencies) {
        for (const dep of c.dependencies) {
          dependencyIds.push(await computeComponent(dep, stack));
        }
      }
      const dependencies = Array.from(new Set(dependencyIds)).sort();

      const hashInput: Record<string, JSONValue> = { modulePaths, moduleHashes, dependencies, bundleSha };
      if (c.schema !== undefined) hashInput.schema = c.schema as JSONValue;
      if (c.args !== undefined) hashInput.args = c.args;
      if (c.env !== undefined) hashInput.env = c.env as JSONValue;

      const componentId = 'def_' + (await canonicalHash(hashInput));

      const definition: ComponentDefinition = {
        componentId,
        modulePaths,
        moduleHashes,
        schema: c.schema,
        args: c.args,
        env: c.env,
        dependencies,
      };
      Object.freeze(moduleHashes);
      Object.freeze(definition);

      idMap.set(c, componentId);
      definitionMap.set(componentId, definition);
      return componentId;
    } finally {
      stack.pop();
    }
  }

  for (const c of components) {
    await computeComponent(c, []);
  }

  const mounts: ComponentMount[] = [];
  if (app && app.components) {
    for (const mount of app.components) {
      mounts.push(await mountToProtocol(mount, []));
    }
  } else {
    for (const c of components) {
      if (!c.sourceModulePath) continue;
      const rootDir = componentRootDir(c.sourceModulePath);
      const modulePaths =
        c.modulePaths.length > 0
          ? c.modulePaths
          : functionModulePaths.filter((p) => p.startsWith(`${rootDir}/`)).map((p) => relativeModulePath(rootDir, p));
      if (modulePaths.length === 0) continue;
      const componentId = idMap.get(c);
      if (!componentId) continue;
      const name = rootDir.slice(rootDir.lastIndexOf('/') + 1);
      mounts.push({ name, componentId });
    }
  }

  const sortedMounts = sortMounts(mounts);
  checkMounts(sortedMounts, 1, '', new Set());

  const definitions = Array.from(definitionMap.values()).sort((a, b) =>
    a.componentId.localeCompare(b.componentId),
  );

  if (definitions.length === 0 && sortedMounts.length === 0) {
    return undefined;
  }

  if (definitions.length > MAX_COMPONENT_DEFINITIONS) {
    throw new Error('Too many component definitions');
  }

  Object.freeze(definitions);
  Object.freeze(sortedMounts);
  return Object.freeze({ definitions, mounts: sortedMounts }) as ComponentGraph;

  async function mountToProtocol(
    mount: ComponentMountDefinition,
    stack: ComponentDefinitionWithKind[],
  ): Promise<ComponentMount> {
    const componentId = await computeComponent(mount.component, stack);
    const children = mount.children
      ? await Promise.all(mount.children.map((child) => mountToProtocol(child, stack)))
      : undefined;
    return { name: mount.name, componentId, args: mount.args, children };
  }

  function sortMounts(m: ComponentMount[]): ComponentMount[] {
    const sorted = [...m].sort((a, b) => a.name.localeCompare(b.name));
    return sorted.map((mount) => ({
      name: mount.name,
      componentId: mount.componentId,
      args: mount.args,
      children: mount.children ? sortMounts(mount.children) : undefined,
    }));
  }

  function checkMounts(m: ComponentMount[], depth: number, prefix: string, seen: Set<string>): void {
    if (depth > MAX_COMPONENT_DEPTH) {
      throw new Error('Component mount depth exceeds 32');
    }
    for (const mount of m) {
      const path = prefix ? `${prefix}/${mount.name}` : mount.name;
      if (seen.has(path)) {
        throw new Error(`Duplicate component mount path ${path}`);
      }
      seen.add(path);
      if (mount.children) {
        checkMounts(mount.children, depth + 1, path, seen);
      }
    }
  }
}
