import { decodeValue } from '@pbvex/protocol';

interface ScopeBinding {
  name: string;
  alias: string;
}

export interface RecursiveDeclaration {
  alias: string;
  name: string;
  inner: unknown;
  enclosingScope: ScopeBinding[];
}

export class RecursiveRegistry {
  private declarations: RecursiveDeclaration[] = [];
  private byInner = new WeakMap<object, RecursiveDeclaration>();
  private allocated = new Set<string>();
  private taken: Set<string>;

  constructor(taken: Set<string> = new Set()) {
    this.taken = taken;
  }

  allocate(name: string, inner: unknown, enclosingScope: ScopeBinding[]): RecursiveDeclaration {
    const alias = this.uniqueAlias(name);
    const decl: RecursiveDeclaration = { alias, name, inner, enclosingScope: [...enclosingScope] };
    this.declarations.push(decl);
    if (inner !== null && typeof inner === 'object') {
      this.byInner.set(inner as object, decl);
    }
    return decl;
  }

  resolveByInner(inner: unknown): RecursiveDeclaration | undefined {
    if (inner !== null && typeof inner === 'object') {
      return this.byInner.get(inner as object);
    }
    return undefined;
  }

  getDeclarations(): readonly RecursiveDeclaration[] {
    return this.declarations;
  }

  private uniqueAlias(name: string): string {
    const base = 'PBVexRecursive_' + sanitizeNamePart(name);
    let candidate = base;
    let i = 2;
    while (this.allocated.has(candidate) || this.taken.has(candidate)) {
      candidate = `${base}_${i}`;
      i++;
    }
    this.allocated.add(candidate);
    return candidate;
  }
}

function sanitizeNamePart(name: string): string {
  let s = name.replace(/[^a-zA-Z0-9_$]/g, '_');
  if (s === '' || /^[0-9]/.test(s)) {
    s = '_' + s;
  }
  return s;
}

function descriptorShape(o: Record<string, unknown>): Record<string, unknown> | undefined {
  const shape = o.shape;
  if (shape !== undefined && shape !== null && typeof shape === 'object' && !Array.isArray(shape)) {
    return shape as Record<string, unknown>;
  }
  const fields = o.fields;
  if (fields !== undefined && fields !== null && typeof fields === 'object' && !Array.isArray(fields)) {
    return fields as Record<string, unknown>;
  }
  return undefined;
}

export function collectRecursive(
  schema: unknown,
  registry: RecursiveRegistry,
  scope: ScopeBinding[],
): void {
  if (schema === null || typeof schema !== 'object') return;
  const s = schema as Record<string, unknown>;
  switch (s.type) {
    case 'object': {
      const shape = descriptorShape(s);
      if (shape) {
        for (const [, v] of Object.entries(shape).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)) {
          collectRecursive(v, registry, scope);
        }
      }
      return;
    }
    case 'array':
      collectRecursive(s.item, registry, scope);
      return;
    case 'record':
      collectRecursive(s.key, registry, scope);
      collectRecursive(s.value, registry, scope);
      return;
    case 'union':
      if (Array.isArray(s.validators)) {
        for (const v of s.validators) {
          collectRecursive(v, registry, scope);
        }
      }
      return;
    case 'optional':
    case 'defaulted':
      collectRecursive(s.validator, registry, scope);
      return;
    case 'recursive': {
      const name = s.name;
      if (typeof name !== 'string') return;
      const decl = registry.allocate(name, s.validator, scope);
      collectRecursive(s.validator, registry, [...scope, { name, alias: decl.alias }]);
      return;
    }
    default:
      return;
  }
}

export function validatorToTypeString(
  schema: unknown,
  registry?: RecursiveRegistry,
  scope?: ScopeBinding[],
): string {
  if (schema === null || typeof schema !== 'object') return 'unknown';
  const s = schema as Record<string, unknown>;
  switch (s.type) {
    case 'id':
      return `Id<${JSON.stringify(s.tableName as string)}>`;
    case 'string':
      return 'string';
    case 'number':
    case 'float64':
      return 'number';
    case 'int64':
      return 'bigint';
    case 'bytes':
      return 'ArrayBuffer';
    case 'boolean':
      return 'boolean';
    case 'any':
      return 'any';
    case 'null':
      return 'null';
    case 'literal': {
      const decoded = decodeValue(s.value as any);
      return typeof decoded === 'bigint' ? `${String(decoded)}n` : JSON.stringify(decoded);
    }
    case 'object': {
      const shape = descriptorShape(s);
      if (shape) {
        const entries = Object.entries(shape).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0);
        if (entries.length === 0) {
          return 'Record<string, never>';
        }
        const fields = entries
          .map(([key, value]) => {
            const optional = isOptional(value);
            return `${JSON.stringify(key)}${optional ? '?' : ''}: ${validatorToTypeString(value, registry, scope)}`;
          })
          .join('; ');
        return `{ ${fields} }`;
      }
      return 'Record<string, unknown>';
    }
    case 'array':
      return `Array<${validatorToTypeString(s.item, registry, scope)}>`;
    case 'record': {
      const keyType = s.key && typeof s.key === 'object' ? validatorToTypeString(s.key, registry, scope) : 'string';
      return `Record<${keyType}, ${validatorToTypeString(s.value, registry, scope)}>`;
    }
    case 'union':
      if (Array.isArray(s.validators)) {
        return s.validators.map((v) => validatorToTypeString(v, registry, scope)).join(' | ');
      }
      return 'unknown';
    case 'optional':
      return `${validatorToTypeString(s.validator, registry, scope)} | undefined`;
    case 'defaulted':
      return validatorToTypeString(s.validator, registry, scope);
    case 'recursive': {
      if (registry) {
        const decl = registry.resolveByInner(s.validator);
        if (decl) return decl.alias;
      }
      return 'unknown';
    }
    case 'ref': {
      if (registry && scope) {
        for (let i = scope.length - 1; i >= 0; i--) {
          if (scope[i]!.name === s.name) return scope[i]!.alias;
        }
      }
      return 'unknown';
    }
    default:
      return 'unknown';
  }
}

export function emitRecursiveAliases(registry: RecursiveRegistry): string[] {
  return registry.getDeclarations().map((decl) => {
    const scope = [...decl.enclosingScope, { name: decl.name, alias: decl.alias }];
    return `export type ${decl.alias} = ${validatorToTypeString(decl.inner, registry, scope)};`;
  });
}

function isOptional(schema: unknown): boolean {
  if (schema === null || typeof schema !== 'object') return false;
  const s = schema as Record<string, unknown>;
  return s.type === 'optional' || s.type === 'defaulted';
}

export function isEmptyArgsDescriptor(descriptor: unknown): boolean {
  if (descriptor === null || typeof descriptor !== 'object') return false;
  const s = descriptor as Record<string, unknown>;
  if (s.type !== 'object') return false;
  const shape = descriptorShape(s);
  if (!shape) return false;
  return Object.keys(shape).length === 0;
}
