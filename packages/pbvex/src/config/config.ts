import { z } from 'zod';
import { readFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import path from 'node:path';
import ts from 'typescript';
import { validateImports } from '../bundler/importValidator.js';

const targetSchema = z.object({
  url: z.string().url(),
  metadata: z.record(z.unknown()).default({}),
});

export const configSchema = z.object({
  project: z.string().optional(),
  defaultTarget: z.string().default('local'),
  targets: z.record(targetSchema).default({}),
});

export type Target = z.infer<typeof targetSchema>;
export type PbvexConfig = z.infer<typeof configSchema>;

export interface ResolvedConfig extends PbvexConfig {
  target: string;
  url: string;
  token: string | undefined;
  rootDir: string;
}

const CREDENTIALS_FILE = '.pbvex/credentials.json';
const configPathLabel = 'pbvex.config.ts';

export async function loadConfig(cwd: string, options: { target?: string; url?: string; token?: string } = {}): Promise<ResolvedConfig> {
  const configPath = path.join(cwd, 'pbvex', 'pbvex.config.ts');
  const jsConfigPath = path.join(cwd, 'pbvex', 'pbvex.config.js');
  let config: PbvexConfig;
  if (existsSync(configPath)) {
    config = await importConfig(configPath);
  } else if (existsSync(jsConfigPath)) {
    config = await importConfig(jsConfigPath);
  } else {
    config = configSchema.parse({});
  }

  const targetName = options.target ?? config.defaultTarget ?? 'local';
  const target = config.targets[targetName];
  if (!target) {
    throw new Error(`Target "${targetName}" not found in pbvex.config.ts`);
  }

  const url = options.url ?? target.url;
  const token = options.token ?? (await resolveToken(cwd, targetName));

  return {
    ...config,
    target: targetName,
    url,
    token,
    rootDir: cwd,
  };
}

function validateConfigAst(sourceFile: ts.SourceFile): Error[] {
  const errors: Error[] = [];
  let foundExport = false;

  function error(node: ts.Node, message: string) {
    const { line, character } = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile));
    errors.push(new Error(`${configPathLabel}:${line + 1}:${character + 1}: ${message}`));
  }

  function isAllowedValue(node: ts.Node, allowDefineConfig: boolean): boolean {
    if (
      ts.isStringLiteral(node) ||
      ts.isNumericLiteral(node) ||
      node.kind === ts.SyntaxKind.TrueKeyword ||
      node.kind === ts.SyntaxKind.FalseKeyword ||
      node.kind === ts.SyntaxKind.NullKeyword ||
      node.kind === ts.SyntaxKind.NoSubstitutionTemplateLiteral
    ) {
      return true;
    }
    if (ts.isIdentifier(node)) {
      const name = (node as ts.Identifier).text;
      if (name === 'undefined') return true;
      if (allowDefineConfig && name === 'defineConfig') return true;
      error(node, `Identifier '${name}' is not allowed in config values`);
      return false;
    }
    if (ts.isArrayLiteralExpression(node)) {
      return node.elements.every((el) => {
        if (ts.isSpreadElement(el)) {
          error(el, 'Spread elements are not allowed in config values');
          return false;
        }
        return isAllowedValue(el, false);
      });
    }
    if (ts.isObjectLiteralExpression(node)) {
      return node.properties.every((prop) => {
        if (ts.isPropertyAssignment(prop)) {
          return isAllowedValue(prop.initializer, false);
        }
        if (ts.isShorthandPropertyAssignment(prop)) {
          error(prop, 'Shorthand properties are not allowed in config values');
          return false;
        }
        if (ts.isMethodDeclaration(prop) || ts.isGetAccessorDeclaration(prop) || ts.isSetAccessorDeclaration(prop)) {
          error(prop, 'Methods/getters/setters are not allowed in config values');
          return false;
        }
        if (ts.isSpreadAssignment(prop)) {
          error(prop, 'Spread assignments are not allowed in config values');
          return false;
        }
        return true;
      });
    }
    if (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isTypeAssertionExpression(node)) {
      return isAllowedValue((node as ts.ParenthesizedExpression | ts.AsExpression | ts.TypeAssertion).expression, false);
    }
    if (ts.isSatisfiesExpression(node)) {
      return isAllowedValue((node as ts.SatisfiesExpression).expression, false);
    }
    error(node, 'Expression type is not allowed in config values');
    return false;
  }

  function visitStatement(node: ts.Node) {
    if (ts.isImportDeclaration(node)) {
      if (node.importClause?.isTypeOnly) return;
      error(node, 'Only type-only imports are allowed in pbvex.config.ts');
      return;
    }
    if (ts.isExportAssignment(node) && !node.isExportEquals && !foundExport) {
      foundExport = true;
      const expression = node.expression;
      if (ts.isCallExpression(expression)) {
        const fn = expression.expression;
        if (ts.isIdentifier(fn) && fn.text === 'defineConfig' && expression.arguments.length === 1) {
          isAllowedValue(expression.arguments[0], false);
          return;
        }
        error(expression, 'Only defineConfig({...}) or a JSON-like object literal may be exported from pbvex.config.ts');
        return;
      }
      if (isAllowedValue(expression, false)) return;
      return;
    }
    if (ts.isExportAssignment(node) && node.isExportEquals) {
      error(node, 'export = is not allowed in pbvex.config.ts');
      return;
    }
    if (ts.isFunctionDeclaration(node) || ts.isClassDeclaration(node) || ts.isClassExpression(node)) {
      error(node, 'Function and class declarations are not allowed in pbvex.config.ts');
      return;
    }
    if (ts.isVariableStatement(node)) {
      error(node, 'Variable declarations are not allowed in pbvex.config.ts');
      return;
    }
    if (
      ts.isExpressionStatement(node) ||
      ts.isIfStatement(node) ||
      ts.isWhileStatement(node) ||
      ts.isDoStatement(node) ||
      ts.isForStatement(node) ||
      ts.isForInStatement(node) ||
      ts.isForOfStatement(node) ||
      ts.isSwitchStatement(node) ||
      ts.isTryStatement(node) ||
      ts.isThrowStatement(node) ||
      ts.isReturnStatement(node) ||
      ts.isBreakOrContinueStatement(node) ||
      ts.isWithStatement(node) ||
      ts.isDebuggerStatement(node) ||
      ts.isLabeledStatement(node) ||
      ts.isBlock(node)
    ) {
      error(node, 'This statement is not allowed in pbvex.config.ts');
      return;
    }
    ts.forEachChild(node, visitStatement);
  }

  visitStatement(sourceFile);
  if (!foundExport) {
    errors.push(new Error(`${configPathLabel}: Missing export default in pbvex.config.ts`));
  }
  return errors;
}

async function importConfig(configPath: string): Promise<PbvexConfig> {
  const sourceText = await readFile(configPath, 'utf-8');
  const sourceFile = ts.createSourceFile(configPath, sourceText, ts.ScriptTarget.ES2020, true, ts.ScriptKind.TS);
  const { errors: importErrors } = validateImports(sourceFile, path.dirname(configPath), new Set(), false);
  const astErrors = validateConfigAst(sourceFile);
  const errors = [...importErrors, ...astErrors];
  if (errors.length > 0) {
    throw new Error(`Invalid pbvex.config.ts: ${errors.map((e) => e.message).join('; ')}`);
  }

  const { outputText } = ts.transpileModule(sourceText, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
      moduleResolution: ts.ModuleResolutionKind.Bundler,
      esModuleInterop: true,
      skipLibCheck: true,
    },
  });

  const defineConfig = (c: unknown) => c;
  const mod: { exports?: { default?: PbvexConfig; [key: string]: unknown } } = { exports: {} };
  const fn = new Function('module', 'exports', 'require', '__dirname', '__filename', 'defineConfig', outputText);
  fn(mod, mod.exports, undefined, path.dirname(configPath), configPath, defineConfig);

  const exported = mod.exports?.default ?? mod.exports;
  return configSchema.parse(exported);
}

export async function resolveToken(cwd: string, targetName: string): Promise<string | undefined> {
  const envToken = process.env[`PBVEX_${targetName.toUpperCase()}_TOKEN`] ?? process.env.PBVEX_TOKEN;
  if (envToken) return envToken;

  const credentialsPath = path.join(cwd, CREDENTIALS_FILE);
  if (existsSync(credentialsPath)) {
    const contents = await readFile(credentialsPath, 'utf-8');
    const parsed = JSON.parse(contents);
    return parsed[targetName]?.token ?? parsed.token;
  }

  return undefined;
}
