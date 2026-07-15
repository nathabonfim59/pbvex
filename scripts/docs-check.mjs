#!/usr/bin/env node
// @ts-check
/**
 * Checks whether docs/api-reference/ is up to date with the source.
 * Run with `pnpm run build && node scripts/docs-check.mjs` (or `pnpm docs:check`).
 */
import { execFileSync } from 'node:child_process';
import { cpSync, existsSync, readFileSync, readdirSync, rmSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = fileURLToPath(new URL('.', import.meta.url));
const root = resolve(__dirname, '..');
const apiRefDir = join(root, 'docs', 'api-reference');
const backupDir = join(root, 'docs', '.vitepress', '.api-check');

function toPosix(p) {
  return p.replace(/\\/g, '/');
}

function compareDirectories(left, right, leftBase, rightBase, diffs = []) {
  const leftRel = toPosix(relative(leftBase, left));
  const rightRel = toPosix(relative(rightBase, right));
  if (!existsSync(left) && !existsSync(right)) return diffs;
  if (!existsSync(left)) {
    diffs.push(`missing in generated: ${rightRel}`);
    return diffs;
  }
  if (!existsSync(right)) {
    diffs.push(`missing in original: ${leftRel}`);
    return diffs;
  }
  const leftStat = statSync(left);
  const rightStat = statSync(right);
  if (leftStat.isDirectory() !== rightStat.isDirectory()) {
    diffs.push(`type mismatch: ${leftRel}`);
    return diffs;
  }
  if (leftStat.isDirectory()) {
    const leftNames = new Set(readdirSync(left));
    const rightNames = new Set(readdirSync(right));
    const allNames = new Set([...leftNames, ...rightNames]);
    for (const name of allNames) {
      compareDirectories(join(left, name), join(right, name), leftBase, rightBase, diffs);
    }
  } else {
    const leftContent = readFileSync(left);
    const rightContent = readFileSync(right);
    if (leftContent.length !== rightContent.length || !leftContent.equals(rightContent)) {
      diffs.push(`content differs: ${leftRel}`);
    }
  }
  return diffs;
}

function backup() {
  rmSync(backupDir, { recursive: true, force: true });
  if (existsSync(apiRefDir)) {
    cpSync(apiRefDir, backupDir, { recursive: true });
  } else {
    // Empty backup means docs/api-reference was missing.
  }
}

function restore() {
  rmSync(apiRefDir, { recursive: true, force: true });
  if (existsSync(backupDir)) {
    cpSync(backupDir, apiRefDir, { recursive: true });
  }
  rmSync(backupDir, { recursive: true, force: true });
}

function main() {
  backup();
  try {
    console.log('Regenerating API docs to check staleness...');
    execFileSync('node', [join(root, 'scripts', 'docs-api.mjs')], {
      stdio: 'inherit',
      cwd: root,
    });

    if (!existsSync(apiRefDir)) {
      restore();
      console.error('docs/api-reference was not generated');
      process.exit(1);
    }

    const diffs = compareDirectories(apiRefDir, backupDir, root, root);
    if (diffs.length > 0) {
      restore();
      console.error('docs/api-reference is stale. Run pnpm docs:api to regenerate.');
      console.error('Differences:');
      for (const diff of diffs) {
        console.error(`  - ${diff}`);
      }
      process.exit(1);
    }

    rmSync(backupDir, { recursive: true, force: true });
    console.log('docs/api-reference is up to date.');
  } catch (err) {
    restore();
    throw err;
  }
}

main();
