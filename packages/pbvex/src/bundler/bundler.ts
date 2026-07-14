import ts from 'typescript';
import { readdir, readFile } from 'node:fs/promises';
import path from 'node:path';
import { existsSync } from 'node:fs';
import { createContext, runInNewContext } from 'node:vm';
import type { DeploymentArtifact, ModuleEntry } from './manifest.js';
import { createArtifact } from './manifest.js';
import { validateImports, resolveRelativeModule } from './importValidator.js';
import { resolveRuntimePath } from './runtimeResolver.js';
import { DERIVE_FUNCTION_NAME_JS } from './functionName.js';
import type { FunctionDefinition, SchemaDefinition } from '../runtime/server.js';
import { isFunctionDefinition, isSchemaDefinition } from '../runtime/server.js';
import type { ComponentDefinitionWithKind, ComponentMountDefinition, AppDefinition } from '../runtime/component.js';
import { isComponentDefinition, isAppDefinition, buildComponentGraph } from '../runtime/component.js';

export interface BundleOptions {
  rootDir: string;
  project?: string;
  target: string;
  entryDir?: string;
}

export interface BundleResult {
  artifact: DeploymentArtifact;
  functions: FunctionDefinition<any, any, any>[];
  components: ComponentDefinitionWithKind[];
  app: AppDefinition | undefined;
  schema: SchemaDefinition | undefined;
  diagnostics: string[];
}

function codeUnitCompare(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

export async function discoverModules(rootDir: string, entryDir = 'pbvex'): Promise<string[]> {
  const dir = path.join(rootDir, entryDir);
  if (!existsSync(dir)) return [];
  const files: string[] = [];
  async function walk(current: string, prefix: string) {
    const entries = await readdir(current, { withFileTypes: true });
    for (const entry of entries) {
      const fullPath = path.join(current, entry.name);
      const relativePath = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (entry.isDirectory()) {
        if (entry.name === '_generated' || entry.name === 'node_modules') continue;
        await walk(fullPath, relativePath);
      } else if (entry.isFile() && entry.name.endsWith('.ts') && !entry.name.endsWith('.test.ts')) {
        if (relativePath === 'pbvex.config.ts') continue;
        files.push(path.join(entryDir, relativePath).replace(/\\/g, '/'));
      }
    }
  }
  await walk(dir, '');
  return files.sort();
}

export async function bundle(options: BundleOptions): Promise<BundleResult> {
  const modulePaths = await discoverModules(options.rootDir, options.entryDir);
  const diagnostics: string[] = [];
  const modules: ModuleEntry[] = [];

  for (const modulePath of modulePaths) {
    const absolutePath = path.join(options.rootDir, modulePath);
    const sourceText = await readFile(absolutePath, 'utf-8');
    const sourceFile = ts.createSourceFile(absolutePath, sourceText, ts.ScriptTarget.ES2022, true, ts.ScriptKind.TS);

    const { imports, errors } = validateImports(sourceFile, options.rootDir);
    if (errors.length > 0) {
      diagnostics.push(...errors.map((e) => e.message));
      continue;
    }

    const resolvedImports = imports.map((imp) => ({
      ...imp,
      resolvedSpecifier: imp.specifier.startsWith('.') ? resolveRelativeModule(absolutePath, imp.specifier, options.rootDir) : imp.specifier,
    }));

    const exports = findExportedNames(sourceFile);
    const { outputText } = ts.transpileModule(sourceText, {
      compilerOptions: {
        module: ts.ModuleKind.ESNext,
        target: ts.ScriptTarget.ES2020,
        moduleResolution: ts.ModuleResolutionKind.Bundler,
        esModuleInterop: true,
        skipLibCheck: true,
      },
    });

    const canonicalModulePath = modulePath.replace(/\\/g, '/');
    modules.push({
      path: canonicalModulePath,
      code: outputText,
      imports: resolvedImports,
      exports,
    });
  }

  if (diagnostics.length > 0) {
    return {
      artifact: await createArtifact(options.project, options.target, [], [], undefined, undefined, modules, ''),
      functions: [],
      components: [],
      app: undefined,
      schema: undefined,
      diagnostics,
    };
  }

  try {
    const { moduleBundle, modules: evaluatedModules } = await buildBundle(options.rootDir, options.entryDir ?? 'pbvex', modulePaths);

    const functions: FunctionDefinition<any, any, any>[] = [];
    const componentSources = new Map<ComponentDefinitionWithKind, string>();
    let app: AppDefinition | undefined;
    let schema: SchemaDefinition | undefined;

    for (const [modulePath, exports] of Object.entries(evaluatedModules)) {
      for (const [exportName, value] of Object.entries(exports)) {
        if (isSchemaDefinition(value) && !schema) {
          schema = value as SchemaDefinition;
        }
        if (isFunctionDefinition(value)) {
          const fn = value as FunctionDefinition<any, any, any>;
          fn.modulePath = modulePath;
          fn.exportName = exportName;
          functions.push(fn);
        }
        if (isComponentDefinition(value)) {
          const c = value as ComponentDefinitionWithKind;
          componentSources.set(c, modulePath);
        }
        if (isAppDefinition(value) && !app) {
          app = value as AppDefinition;
        }
      }
    }

    const normalized = new Map<ComponentDefinitionWithKind, ComponentDefinitionWithKind>();
    const normalizeComponent = (component: ComponentDefinitionWithKind): ComponentDefinitionWithKind => {
      const existing = normalized.get(component);
      if (existing) return existing;
      const sourceModulePath = componentSources.get(component) ?? component.sourceModulePath;
      const sourceRoot = componentRootDir(sourceModulePath ?? '');
      const inferredModulePaths = functions
        .filter((fn) => fn.component === component)
        .map((fn) => relativeModulePath(sourceRoot, fn.modulePath))
        .filter((modulePath) => modulePath.length > 0);
      const modulePaths = component.modulePaths.length > 0
        ? [...component.modulePaths]
        : Array.from(new Set(inferredModulePaths)).sort();
      const clone: ComponentDefinitionWithKind = {
        ...component,
        modulePaths,
        sourceModulePath,
        dependencies: undefined,
      };
      normalized.set(component, clone);
      Object.freeze(modulePaths);
      clone.dependencies = component.dependencies?.map(normalizeComponent);
      if (clone.dependencies) Object.freeze(clone.dependencies);
      return Object.freeze(clone);
    };
    const normalizeMount = (mount: ComponentMountDefinition): ComponentMountDefinition => Object.freeze({
      ...mount,
      component: normalizeComponent(mount.component),
      children: mount.children?.map(normalizeMount),
    });
    const components = Array.from(componentSources.keys(), normalizeComponent);
    for (const fn of functions) {
      if (fn.component) {
        const owner = normalized.get(fn.component as ComponentDefinitionWithKind);
        if (!owner) throw new Error(`Component function ${fn.modulePath}#${fn.exportName} has no exported component definition`);
        fn.component = owner as any;
      }
    }
    if (app) {
      app = Object.freeze({ ...app, components: app.components.map(normalizeMount) });
    }

    const materialized = materializeMountedComponents(functions, components, app, modules);
    const bundleCode = moduleBundle + '\n' + buildRegistrationCode(materialized.registrations);
    const componentSourceFunctionPaths = functions
      .filter((fn) => fn.component)
      .map((fn) => fn.modulePath);
    const artifact = await createArtifact(
      options.project,
      options.target,
      materialized.functions,
      components,
      app,
      schema,
      materialized.modules,
      bundleCode,
      componentSourceFunctionPaths,
      modules,
    );
    return { artifact, functions: materialized.functions, components, app, schema, diagnostics };
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return {
      artifact: await createArtifact(options.project, options.target, [], [], undefined, undefined, modules, ''),
      functions: [],
      components: [],
      app: undefined,
      schema: undefined,
      diagnostics: [message],
    };
  }
}

function findExportedNames(sourceFile: ts.SourceFile): string[] {
  const names: string[] = [];
  function visit(node: ts.Node) {
    if (ts.isExportDeclaration(node)) {
      if (node.exportClause && ts.isNamedExports(node.exportClause)) {
        for (const el of node.exportClause.elements) {
          names.push(el.name.text);
        }
      } else if (node.exportClause && ts.isNamespaceExport(node.exportClause)) {
        names.push(node.exportClause.name.text);
      }
    } else if (ts.isExportAssignment(node) && node.isExportEquals === false && ts.isIdentifier(node.expression)) {
      names.push('default');
    } else if (ts.isVariableStatement(node) && node.modifiers?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword)) {
      for (const decl of node.declarationList.declarations) {
        if (ts.isIdentifier(decl.name)) names.push(decl.name.text);
      }
    } else if (ts.isFunctionDeclaration(node) && node.modifiers?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword) && node.name) {
      names.push(node.name.text);
    } else if (ts.isClassDeclaration(node) && node.modifiers?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword) && node.name) {
      names.push(node.name.text);
    }
    ts.forEachChild(node, visit);
  }
  visit(sourceFile);
  return names;
}

interface FunctionRegistration {
  sourceModulePath: string;
  modulePath: string;
  exportName: string;
}

function componentRootDir(sourceModulePath: string): string {
  const lastSlash = sourceModulePath.lastIndexOf('/');
  return lastSlash > 0 ? sourceModulePath.slice(0, lastSlash) : '';
}

function relativeModulePath(rootDir: string, modulePath: string): string {
  const prefix = rootDir ? `${rootDir}/` : '';
  return modulePath.startsWith(prefix) ? modulePath.slice(prefix.length) : '';
}

function materializeMountedComponents(
  functions: FunctionDefinition<any, any, any>[],
  components: ComponentDefinitionWithKind[],
  app: AppDefinition | undefined,
  sourceModules: ModuleEntry[],
): { functions: FunctionDefinition<any, any, any>[]; modules: ModuleEntry[]; registrations: FunctionRegistration[] } {
  const mountPaths = new Map<ComponentDefinitionWithKind, string[]>();
  const childMountNames = new Map<string, Set<string>>();
  const addMount = (component: ComponentDefinitionWithKind, path: string): void => {
    const paths = mountPaths.get(component) ?? [];
    paths.push(path);
    mountPaths.set(component, paths);
  };
  const walkMounts = (mounts: ComponentMountDefinition[], parent: string): void => {
    for (const mount of mounts) {
      const path = parent ? `${parent}/${mount.name}` : mount.name;
      addMount(mount.component, path);
      childMountNames.set(path, new Set((mount.children ?? []).map((child) => child.name)));
      walkMounts(mount.children ?? [], path);
    }
  };
  if (app) {
    walkMounts(app.components, '');
  } else {
    for (const component of components) {
      const root = componentRootDir(component.sourceModulePath ?? '');
      const name = root.slice(root.lastIndexOf('/') + 1);
      if (name && component.modulePaths.length > 0) {
        addMount(component, name);
        childMountNames.set(name, new Set());
      }
    }
  }
  for (const paths of mountPaths.values()) paths.sort(codeUnitCompare);
  const assertMountOwner = (origin: string, relative: string, kind: string): void => {
    const firstSlash = relative.indexOf('/');
    const firstSegment = firstSlash < 0 ? relative : relative.slice(0, firstSlash);
    if (childMountNames.get(origin)?.has(firstSegment)) {
      throw new Error(`${kind} ${origin}/${relative} from component mount ${origin} collides with descendant mount ${origin}/${firstSegment}`);
    }
  };

  const materializedFunctions: FunctionDefinition<any, any, any>[] = [];
  const registrations: FunctionRegistration[] = [];
  const functionAliases = new Set<string>();
  const addFunction = (
    fn: FunctionDefinition<any, any, any>,
    sourceModulePath: string,
    modulePath: string,
  ): void => {
    const alias = `${modulePath}#${fn.exportName}`;
    if (functionAliases.has(alias)) throw new Error(`Function alias collision at ${alias}`);
    functionAliases.add(alias);
    materializedFunctions.push(modulePath === fn.modulePath ? fn : { ...fn, modulePath });
    registrations.push({ sourceModulePath, modulePath, exportName: fn.exportName });
  };
  for (const fn of functions) {
    const owner = fn.component as ComponentDefinitionWithKind | undefined;
    if (!owner) {
      addFunction(fn, fn.modulePath, fn.modulePath);
      continue;
    }
    const root = componentRootDir(owner.sourceModulePath ?? '');
    const relative = relativeModulePath(root, fn.modulePath);
    if (!relative || !owner.modulePaths.includes(relative)) {
      throw new Error(`Component function ${fn.modulePath}#${fn.exportName} is not declared in modulePaths`);
    }
    for (const mountPath of mountPaths.get(owner) ?? []) {
      assertMountOwner(mountPath, relative, 'Function module');
      const modulePath = `pbvex/components/${mountPath}/${relative}`;
      addFunction(fn, fn.modulePath, modulePath);
    }
  }

  const sourceByPath = new Map(sourceModules.map((module) => [module.path, module]));
  const materializedModules = new Map<string, ModuleEntry>();
  for (const component of components) {
    const root = componentRootDir(component.sourceModulePath ?? '');
    for (const mountPath of mountPaths.get(component) ?? []) {
      for (const relative of component.modulePaths) {
        assertMountOwner(mountPath, relative, 'Module');
        const sourcePath = root ? `${root}/${relative}` : relative;
        const source = sourceByPath.get(sourcePath);
        if (!source) throw new Error(`Module ${sourcePath} not found for mounted component ${mountPath}`);
        const path = `pbvex/components/${mountPath}/${relative}`;
        if (materializedModules.has(path)) throw new Error(`Module alias collision at ${path}`);
        materializedModules.set(path, { ...source, path });
      }
    }
  }

  materializedFunctions.sort((a, b) => codeUnitCompare(`${a.modulePath}:${a.exportName}`, `${b.modulePath}:${b.exportName}`));
  registrations.sort((a, b) => codeUnitCompare(`${a.modulePath}:${a.exportName}`, `${b.modulePath}:${b.exportName}`));
  const modules = components.length === 0
    ? [...sourceModules].sort((a, b) => codeUnitCompare(a.path, b.path))
    : Array.from(materializedModules.values()).sort((a, b) => codeUnitCompare(a.path, b.path));
  return { functions: materializedFunctions, modules, registrations };
}

function buildRegistrationCode(registrations: FunctionRegistration[]): string {
  const serialized = JSON.stringify(registrations);
  return `
(function(__pbvex) {
  if (!__pbvex || typeof __pbvex.registerFunction !== 'function') return;
  ${DERIVE_FUNCTION_NAME_JS}
  var modules = (typeof globalThis !== 'undefined' && globalThis.PBVEX_MODULES) ? globalThis.PBVEX_MODULES : {};
  var registrations = ${serialized};
  for (var i = 0; i < registrations.length; i++) {
    var registration = registrations[i];
    var mod = modules[registration.sourceModulePath];
    var fn = mod && mod[registration.exportName];
    if (!fn || typeof fn.handler !== 'function') continue;
    var name = pbvexDeriveFunctionName(registration.modulePath, registration.exportName);
    var descriptor = { name: name, modulePath: registration.modulePath, exportName: registration.exportName, type: fn.type, visibility: fn.visibility };
    if (fn.args && typeof fn.args.toJSON === 'function') descriptor.args = fn.args.toJSON();
    if (fn.returns && typeof fn.returns.toJSON === 'function') descriptor.returns = fn.returns.toJSON();
    if (fn.route) descriptor.route = fn.route;
    __pbvex.registerFunction(descriptor, fn.handler);
  }
})(globalThis.__pbvex);
`;
}

async function buildBundle(
  rootDir: string,
  _entryDir: string,
  modulePaths: string[],
): Promise<{ moduleBundle: string; modules: Record<string, Record<string, unknown>> }> {
  if (modulePaths.length === 0) {
    return { moduleBundle: '', modules: {} };
  }

  const { build } = await import('esbuild');

  const imports: string[] = [];
  const entries: string[] = [];
  for (let i = 0; i < modulePaths.length; i++) {
    const modulePath = modulePaths[i].replace(/\\/g, '/');
    imports.push(`import * as __mod_${i} from './${modulePath}';`);
    entries.push(`  '${modulePath}': __mod_${i},`);
  }

  const entryCode = `${imports.join('\n')}\n\nconst __PBVEX_MODULES = {\n${entries.join('\n')}\n};\n\nglobalThis.PBVEX_MODULES = __PBVEX_MODULES;\n`;

  const result = await build({
    stdin: {
      contents: entryCode,
      resolveDir: rootDir,
      sourcefile: 'pbvex-entry.ts',
      loader: 'ts',
    },
    bundle: true,
    write: false,
    format: 'iife',
    platform: 'browser',
    target: 'es2020',
    external: [],
    plugins: [
      {
        name: 'pbvex-resolver',
        setup(build) {
          build.onResolve({ filter: /.*/ }, (args) => {
            if (args.path === 'pbvex/server') {
              return { path: resolveRuntimePath('server') };
            }
            if (args.path === 'pbvex/values') {
              return { path: resolveRuntimePath('values') };
            }
            return undefined;
          });
        },
      },
    ],
  });

  const moduleBundle = result.outputFiles[0].text;
  const context = createContext({ console });
  runInNewContext(moduleBundle, context);
  const modules = (context.PBVEX_MODULES as Record<string, Record<string, unknown>>) ?? {};

  return { moduleBundle, modules };
}
