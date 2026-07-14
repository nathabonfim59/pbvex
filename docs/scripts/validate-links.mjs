#!/usr/bin/env node
/**
 * Validates local markdown links inside the owned docs directories.
 *
 * Run from the repository root:
 *   node docs/scripts/validate-links.mjs
 *
 * Checks:
 *   - Links in authored Markdown resolve to an existing VitePress page.
 *   - Generated API pages are checked by VitePress's Markdown parser during docs:build.
 *   - Anchors are stripped before path resolution.
 *   - External URLs and mailto: links are ignored.
 */

import { readFile, readdir, stat } from 'node:fs/promises';
import { dirname, join, resolve, relative, extname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const DOCS_ROOT = resolve(__dirname, '..');
const REPO_ROOT = resolve(DOCS_ROOT, '..');

const EXTENSIONS = new Set(['.md']);

async function* walk(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (path === join(DOCS_ROOT, 'api-reference')) continue;
      yield* walk(path);
    } else if (entry.isFile() && EXTENSIONS.has(extname(entry.name))) {
      yield path;
    }
  }
}

function findLinks(content) {
  const links = [];
  const inline = /\[([^\]]*)\]\(([^)]+)\)/g;
  const ref = /\[([^\]]*)\]:\s*(\S+)/g;

  let match;
  while ((match = inline.exec(content)) !== null) {
    links.push({ text: match[1], href: match[2] });
  }
  while ((match = ref.exec(content)) !== null) {
    links.push({ text: match[1], href: match[2] });
  }

  return links;
}

function isExternal(href) {
  const normalized = href.replace(/^<|>$/g, '');
  return /^(?:[a-z][a-z+.-]*:|#)/i.test(normalized);
}

function stripSuffix(href) {
  return href.replace(/^<|>$/g, '').split(/[?#]/, 1)[0];
}

async function resolvesToPage(path) {
  const candidates = [path];
  if (extname(path) === '') {
    candidates.push(`${path}.md`, join(path, 'index.md'));
  } else if (path.endsWith('.html')) {
    candidates.push(path.slice(0, -5) + '.md');
  }

  for (const candidate of candidates) {
    try {
      const candidateStat = await stat(candidate);
      if (candidateStat.isFile()) return true;
      if (candidateStat.isDirectory()) {
        const indexStat = await stat(join(candidate, 'index.md'));
        if (indexStat.isFile()) return true;
      }
    } catch {
      // Try the next supported VitePress path form.
    }
  }
  return false;
}

async function validateFile(filePath, broken) {
  const content = await readFile(filePath, 'utf-8');
  const links = findLinks(content);
  const baseDir = dirname(filePath);

  for (const link of links) {
    if (isExternal(link.href)) continue;

    const target = stripSuffix(link.href);
    if (target === '') continue;

    const resolved = target.startsWith('/')
      ? resolve(DOCS_ROOT, `.${target}`)
      : resolve(baseDir, target);

    if (!(await resolvesToPage(resolved))) {
      broken.push({ file: filePath, link: link.href, resolved });
    }
  }
}

async function main() {
  const broken = [];
  for await (const file of walk(DOCS_ROOT)) {
    await validateFile(file, broken);
  }

  const rel = (p) => relative(REPO_ROOT, p);

  if (broken.length > 0) {
    console.error('Broken local links:');
    for (const { file, link, resolved } of broken) {
      console.error(`  ${rel(file)} -> ${link} (resolved ${rel(resolved)})`);
    }
    process.exit(1);
  }

  console.log('All local links in docs/ are valid.');
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
