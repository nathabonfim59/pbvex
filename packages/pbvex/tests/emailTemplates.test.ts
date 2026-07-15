import { afterEach, describe, expect, it } from 'vitest';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { discoverEmailTemplates } from '../src/bundler/emailTemplates.js';
import { bundle } from '../src/bundler/bundler.js';
import { artifactToJson } from '../src/bundler/manifest.js';
import { canonicalHash, validateUploadRequest } from '@pbvex/protocol';
import { generateServerTs } from '../src/codegen/codegen.js';

const dirs: string[] = [];
afterEach(async () => { await Promise.all(dirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true }))); });

async function project() {
  const dir = await mkdtemp(path.join(os.tmpdir(), 'pbvex-email-')); dirs.push(dir);
  await mkdir(path.join(dir, 'pbvex', 'emails'), { recursive: true });
  await writeFile(path.join(dir, 'pbvex', 'noop.ts'), `import { action } from 'pbvex/server'; export const noop = action({ handler: async () => null });`);
  return dir;
}

describe('application email templates', () => {
  it('discovers sorted templates and authenticates the deploy roundtrip', async () => {
    const dir = await project();
    await writeFile(path.join(dir, 'pbvex', 'emails', 'welcome.json'), JSON.stringify({ subject: 'Welcome {{name}}', text: 'Hi {{name}}' }));
    await writeFile(path.join(dir, 'pbvex', 'emails', 'alert.json'), JSON.stringify({ subject: 'Alert', html: '<b>{{message}}</b>' }));
    expect((await discoverEmailTemplates(dir)).map((t) => t.name)).toEqual(['alert', 'welcome']);
    const result = await bundle({ rootDir: dir, target: 'test' });
    expect(result.diagnostics).toEqual([]);
    expect(result.artifact.manifest.emailTemplates?.entries.map((t) => t.name)).toEqual(['alert', 'welcome']);
    await expect(validateUploadRequest(JSON.parse(artifactToJson(result.artifact)))).resolves.toBeDefined();
    const tampered = JSON.parse(artifactToJson(result.artifact)); tampered.manifest.emailTemplates.entries[0].subject = 'Changed';
    await expect(validateUploadRequest(tampered)).rejects.toThrow(/emailTemplates hash/);
    const emptyMixedBody = JSON.parse(artifactToJson(result.artifact));
    emptyMixedBody.manifest.emailTemplates.entries[0].text = '';
    emptyMixedBody.manifest.emailTemplates.sha256 = await canonicalHash({ bundleSha256: emptyMixedBody.sha256, entries: emptyMixedBody.manifest.emailTemplates.entries });
    await expect(validateUploadRequest(emptyMixedBody)).rejects.toThrow(/non-empty text or html/);
  });

  it('rejects unsafe subjects and generates a template-name union', async () => {
    const dir = await project();
    await writeFile(path.join(dir, 'pbvex', 'emails', 'bad.json'), JSON.stringify({ subject: 'Hi\nBcc: x', text: 'x' }));
    await expect(discoverEmailTemplates(dir)).rejects.toThrow(/Invalid subject/);
    await writeFile(path.join(dir, 'pbvex', 'emails', 'bad.json'), JSON.stringify({ subject: 'Valid', text: '', html: '<p>x</p>' }));
    await expect(discoverEmailTemplates(dir)).rejects.toThrow(/non-empty text or html/);
    await writeFile(path.join(dir, 'pbvex', 'emails', 'bad.json'), JSON.stringify({ subject: 'é'.repeat(500), text: 'x' }));
    await expect(discoverEmailTemplates(dir)).rejects.toThrow(/Invalid subject/);
    expect(generateServerTs(['welcome', 'receipt'])).toContain('export type EmailTemplateName = "receipt" | "welcome";');
  });
});
