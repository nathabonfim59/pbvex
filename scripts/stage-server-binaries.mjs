import { createHash } from 'node:crypto';
import { chmod, copyFile, mkdir, readFile, rm, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const artifactsPath = path.resolve(process.argv[2] ?? path.join(repoRoot, 'dist', 'artifacts.json'));
const outputDir = path.join(repoRoot, 'packages', 'server', 'bin');

const platformNames = { linux: 'linux', darwin: 'darwin', windows: 'win32' };
const archNames = { amd64: 'x64', arm64: 'arm64', arm: 'arm', s390x: 's390x', ppc64le: 'ppc64' };
const expectedTargets = new Set([
  'linux-x64',
  'linux-arm64',
  'linux-arm',
  'linux-s390x',
  'linux-ppc64',
  'darwin-x64',
  'darwin-arm64',
  'win32-x64',
  'win32-arm64',
]);

function targetKey(artifact) {
  const platform = platformNames[artifact.goos];
  const arch = archNames[artifact.goarch];
  if (!platform || !arch) return undefined;
  if (artifact.goarch === 'arm' && String(artifact.goarm) !== '7') return undefined;
  return `${platform}-${arch}`;
}

async function sha256(file) {
  const contents = await readFile(file);
  return createHash('sha256').update(contents).digest('hex');
}

const artifacts = JSON.parse(await readFile(artifactsPath, 'utf8'));
const binaries = artifacts.filter((artifact) => artifact.type === 'Binary');
const staged = {};

for (const entry of await import('node:fs/promises').then(({ readdir }) => readdir(outputDir, { withFileTypes: true }))) {
  if (entry.isDirectory()) await rm(path.join(outputDir, entry.name), { recursive: true, force: true });
}
await rm(path.join(outputDir, 'manifest.json'), { force: true });

for (const artifact of binaries) {
  const key = targetKey(artifact);
  if (!key || !expectedTargets.has(key)) continue;
  if (staged[key]) throw new Error(`Duplicate GoReleaser binary for ${key}`);
  const source = path.resolve(artifact.path);
  const extension = artifact.goos === 'windows' ? '.exe' : '';
  const relative = `${key}/pbvex${extension}`;
  const destination = path.join(outputDir, relative);
  await mkdir(path.dirname(destination), { recursive: true });
  await copyFile(source, destination);
  if (artifact.goos !== 'windows') await chmod(destination, 0o755);
  const info = await stat(destination);
  staged[key] = {
    file: relative,
    goos: artifact.goos,
    goarch: artifact.goarch,
    ...(artifact.goarm ? { goarm: String(artifact.goarm) } : {}),
    sha256: await sha256(destination),
    size: info.size,
  };
}

const missing = [...expectedTargets].filter((key) => !staged[key]);
if (missing.length > 0) throw new Error(`GoReleaser did not produce required server targets: ${missing.join(', ')}`);

await writeFile(path.join(outputDir, 'manifest.json'), `${JSON.stringify({ version: 1, binaries: staged }, null, 2)}\n`);
console.log(`Staged ${Object.keys(staged).length} PBVex server binaries from ${artifactsPath}`);
