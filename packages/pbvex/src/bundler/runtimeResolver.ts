import path from 'node:path';
import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export function resolveRuntimePath(moduleName: 'server' | 'values' | string): string {
  const tsPath = path.resolve(__dirname, `../runtime/${moduleName}.ts`);
  const jsPath = path.resolve(__dirname, `../runtime/${moduleName}.js`);
  if (existsSync(tsPath)) return tsPath;
  if (existsSync(jsPath)) return jsPath;
  throw new Error(`Cannot resolve pbvex/${moduleName} runtime module from ${__dirname}`);
}
