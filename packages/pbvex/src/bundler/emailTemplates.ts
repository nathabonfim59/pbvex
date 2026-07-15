import { readdir, readFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import path from 'node:path';
import type { EmailTemplate } from '@pbvex/protocol';

export async function discoverEmailTemplates(rootDir: string, entryDir = 'pbvex'): Promise<EmailTemplate[]> {
  const dir = path.join(rootDir, entryDir, 'emails');
  if (!existsSync(dir)) return [];
  const entries = await readdir(dir, { withFileTypes: true });
  const unsupported = entries.find((entry) => !entry.isFile());
  if (unsupported) throw new Error(`Email template directory may contain only regular .json files: ${unsupported.name}`);
  const files = entries.map((e) => e.name).sort();
  const out: EmailTemplate[] = [];
  for (const file of files) {
    if (!file.endsWith('.json')) throw new Error(`Email template files must use .json: ${file}`);
    const name = file.slice(0, -5);
    if (!/^[a-z][a-z0-9_-]{0,63}$/.test(name)) throw new Error(`Invalid email template name: ${name}`);
    const raw = await readFile(path.join(dir, file), 'utf8');
    if (Buffer.byteLength(raw) > 128 * 1024) throw new Error(`Email template is too large: ${name}`);
    let parsed: any;
    try { parsed = JSON.parse(raw); } catch { throw new Error(`Invalid email template JSON: ${name}`); }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || Object.keys(parsed).some((k) => !['subject', 'text', 'html'].includes(k))) throw new Error(`Invalid email template object: ${name}`);
    if (typeof parsed.subject !== 'string' || !parsed.subject || Buffer.byteLength(parsed.subject) > 998 || /[\r\n]/.test(parsed.subject)) throw new Error(`Invalid subject in email template: ${name}`);
    if ((parsed.text !== undefined && (typeof parsed.text !== 'string' || parsed.text.length === 0)) || (parsed.html !== undefined && (typeof parsed.html !== 'string' || parsed.html.length === 0)) || (parsed.text === undefined && parsed.html === undefined)) throw new Error(`Email template ${name} needs non-empty text or html`);
    out.push({ name, subject: parsed.subject, ...(parsed.text ? { text: parsed.text } : {}), ...(parsed.html ? { html: parsed.html } : {}) });
  }
  if (out.length > 64) throw new Error('At most 64 email templates are allowed');
  if (out.reduce((n, t) => n + Buffer.byteLength(t.subject) + Buffer.byteLength(t.text ?? '') + Buffer.byteLength(t.html ?? ''), 0) > 512 * 1024) throw new Error('Email templates exceed 512 KiB');
  return out;
}
