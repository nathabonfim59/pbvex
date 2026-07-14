import { readFile } from 'node:fs/promises';

const expected = process.argv[2];
if (!expected || expected.startsWith('v')) {
  throw new Error(`Expected a version normalized by validate-tag.sh, got ${JSON.stringify(expected)}`);
}

const manifests = [
  'packages/protocol/package.json',
  'packages/pbvex/package.json',
  'packages/sdk-core/package.json',
  'packages/sdk-react/package.json',
  'packages/sdk-svelte/package.json',
];

for (const file of manifests) {
  const manifest = JSON.parse(await readFile(new URL(`../${file}`, import.meta.url), 'utf8'));
  if (manifest.private === true) throw new Error(`${file} is private and cannot be published`);
  if (manifest.version !== expected) {
    throw new Error(`${file} version ${manifest.version} does not match release ${expected}`);
  }
  if (manifest.publishConfig?.provenance !== true) {
    throw new Error(`${file} must enable publishConfig.provenance`);
  }
}

console.log(`All publishable package versions match ${expected}.`);
