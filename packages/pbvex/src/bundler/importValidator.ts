import ts from 'typescript';
import path from 'node:path';
import type { ModuleImport, SourcePosition } from './manifest.js';
import { diagnostic } from './diagnostics.js';

const ALLOWED_EXTERNAL = new Set(['pbvex/server', 'pbvex/values']);

const NODE_BUILTINS = new Set([
  'assert', 'buffer', 'child_process', 'cluster', 'console', 'constants', 'crypto', 'dgram', 'diagnostics_channel',
  'dns', 'domain', 'events', 'fs', 'http', 'http2', 'https', 'inspector', 'module', 'net', 'os', 'path', 'perf_hooks',
  'process', 'punycode', 'querystring', 'readline', 'repl', 'stream', 'string_decoder', 'sys', 'timers', 'tls', 'trace_events',
  'tty', 'url', 'util', 'v8', 'vm', 'wasi', 'worker_threads', 'zlib',
]);

function isNodeBuiltin(specifier: string): boolean {
  if (specifier.startsWith('node:')) return true;
  const base = specifier.split('/')[0];
  return NODE_BUILTINS.has(base);
}

function isAsset(specifier: string): boolean {
  const ext = path.extname(specifier).toLowerCase();
  return ['.css', '.scss', '.sass', '.less', '.png', '.jpg', '.jpeg', '.gif', '.svg', '.ico', '.woff', '.woff2', '.ttf', '.otf', '.eot', '.mp3', '.mp4', '.webm', '.json', '.wasm'].includes(ext);
}

function getPosition(sourceFile: ts.SourceFile, node: ts.Node): SourcePosition {
  const { line, character } = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile));
  return { file: sourceFile.fileName, line: line + 1, column: character + 1 };
}

function extractImportNames(node: ts.ImportDeclaration | ts.ImportClause | ts.NamedImports | ts.NamespaceImport): string[] {
  const names: string[] = [];
  if (ts.isImportDeclaration(node) && node.importClause) {
    return extractImportNames(node.importClause);
  }
  if (ts.isImportClause(node)) {
    if (node.name) names.push(`default:${node.name.text}`);
    if (node.namedBindings) names.push(...extractImportNames(node.namedBindings));
    return names;
  }
  if (ts.isNamedImports(node)) {
    return node.elements.map((el) => {
      if (el.propertyName) return `${el.propertyName.text} as ${el.name.text}`;
      return el.name.text;
    });
  }
  if (ts.isNamespaceImport(node)) {
    return [`* as ${node.name.text}`];
  }
  return names;
}

export interface ImportValidationResult {
  imports: ModuleImport[];
  errors: Error[];
}

export function validateImports(
  sourceFile: ts.SourceFile,
  rootDir: string,
  allowedExternal: Set<string> = ALLOWED_EXTERNAL,
  allowRelative = true,
): ImportValidationResult {
  const imports: ModuleImport[] = [];
  const errors: Error[] = [];

  function visit(node: ts.Node) {
    if (ts.isImportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      const specifier = node.moduleSpecifier.text;
      const position = getPosition(sourceFile, node.moduleSpecifier);
      const isTypeOnly = node.importClause?.isTypeOnly;

      if (isTypeOnly) {
        imports.push({
          specifier,
          names: [],
          kind: 'type',
          position,
        });
        return;
      }

      const names = extractImportNames(node);
      imports.push({
        specifier,
        names,
        kind: 'value',
        position,
      });

      if (isAsset(specifier)) {
        errors.push(diagnostic('PBVEX_ASSET_IMPORT', 'Unsupported asset import', `${specifier} at ${formatPosition(position)}`));
      } else if (isNodeBuiltin(specifier)) {
        errors.push(diagnostic('PBVEX_NODE_BUILTIN', 'Node built-in imports are not allowed', `${specifier} at ${formatPosition(position)}`));
      } else if (specifier.startsWith('.')) {
        if (!allowRelative) {
          errors.push(diagnostic('PBVEX_RELATIVE_IMPORT', 'Relative imports are not allowed in this context', `${specifier} at ${formatPosition(position)}`));
        }
      } else if (allowedExternal.has(specifier)) {
        // allowed
      } else {
        errors.push(diagnostic('PBVEX_NON_RELATIVE_IMPORT', 'Non-relative import is not allowed', `${specifier} at ${formatPosition(position)}`));
      }
    }

    if (ts.isImportEqualsDeclaration(node)) {
      errors.push(diagnostic('PBVEX_IMPORT_EQUALS', 'Import equals declarations are not supported', `at ${formatPosition(getPosition(sourceFile, node))}`));
    }

    // Dynamic imports: CallExpression with import keyword
    if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
      errors.push(diagnostic('PBVEX_DYNAMIC_IMPORT', 'Dynamic imports are not supported', `at ${formatPosition(getPosition(sourceFile, node))}`));
    }

    // Reject require() calls
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'require') {
      errors.push(diagnostic('PBVEX_REQUIRE', 'CommonJS require() is not supported', `at ${formatPosition(getPosition(sourceFile, node))}`));
    }

    ts.forEachChild(node, visit);
  }

  visit(sourceFile);
  return { imports, errors };
}

function formatPosition(position: SourcePosition): string {
  return `${position.file}:${position.line}:${position.column}`;
}

export function resolveRelativeModule(basePath: string, specifier: string, rootDir: string): string | undefined {
  if (!specifier.startsWith('.')) return undefined;
  const resolved = path.resolve(path.dirname(basePath), specifier);
  const candidates = [resolved, `${resolved}.ts`, `${resolved}/index.ts`];
  for (const candidate of candidates) {
    if (candidate.endsWith('.ts') && ts.sys.fileExists(candidate)) {
      return path.relative(rootDir, candidate).replace(/\\/g, '/');
    }
  }
  return undefined;
}
