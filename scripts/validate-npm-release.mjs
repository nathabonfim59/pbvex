import { readFile } from 'node:fs/promises';

const expected = process.argv[2];
if (!expected || expected.startsWith('v')) {
  throw new Error(`Expected a version normalized by validate-tag.sh, got ${JSON.stringify(expected)}`);
}

const manifests = new Map([
  ['packages/protocol/package.json', '@pbvex/protocol'],
  ['packages/server/package.json', '@pbvex/server'],
  ['packages/pbvex/package.json', 'pbvex'],
  ['packages/client/package.json', '@pbvex/client'],
  ['packages/react/package.json', '@pbvex/react'],
  ['packages/svelte/package.json', '@pbvex/svelte'],
]);

for (const [file, name] of manifests) {
  const manifest = JSON.parse(await readFile(new URL(`../${file}`, import.meta.url), 'utf8'));
  if (manifest.name !== name) {
    throw new Error(`${file} must publish as ${name}, got ${manifest.name}`);
  }
  if (manifest.private === true) throw new Error(`${file} is private and cannot be published`);
  if (manifest.version !== expected) {
    throw new Error(`${file} version ${manifest.version} does not match release ${expected}`);
  }
  if (manifest.publishConfig?.provenance !== true) {
    throw new Error(`${file} must enable publishConfig.provenance`);
  }
}

console.log(`All publishable package versions match ${expected}.`);
