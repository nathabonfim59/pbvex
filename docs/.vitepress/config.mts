import { defineConfig } from 'vitepress';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { DefaultTheme } from 'vitepress';

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, '..');

function toPosix(p: string) {
  return p.replace(/\\/g, '/');
}

function titleFromFile(filePath: string) {
  try {
    const content = readFileSync(filePath, 'utf-8');
    const frontmatter = content.match(/^---\s*\n([\s\S]*?)\n---/);
    if (frontmatter) {
      const titleMatch = frontmatter[1].match(/^title:\s*(.+)$/m);
      if (titleMatch) return titleMatch[1].trim();
    }
    const h1 = content.match(/^#\s+(.+)$/m);
    if (h1) return h1[1].trim();
  } catch {
    // ignore
  }
  return '';
}

function titleFromName(name: string) {
  return name
    .replace(/\.md$/, '')
    .replace(/-/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function pageText(filePath: string, fallback: string) {
  return titleFromFile(filePath) || titleFromName(fallback);
}

function pageLink(filePath: string) {
  const rel = toPosix(relative(root, filePath)).replace(/\.md$/, '');
  return '/' + rel;
}

function scanMarkdownFiles(dir: string, filter: (rel: string) => boolean = () => true) {
  const results: { filePath: string; rel: string }[] = [];
  if (!existsSync(dir)) return results;
  function walk(current: string) {
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      const entryPath = join(current, entry.name);
      if (entry.isDirectory()) {
        walk(entryPath);
      } else if (entry.isFile() && entry.name.endsWith('.md')) {
        const rel = toPosix(relative(dir, entryPath));
        if (filter(rel)) {
          results.push({ filePath: entryPath, rel });
        }
      }
    }
  }
  walk(dir);
  return results.sort((a, b) => a.rel.localeCompare(b.rel));
}

type MarkdownPage = ReturnType<typeof scanMarkdownFiles>[number];

function guideGroups(files: MarkdownPage[]): DefaultTheme.SidebarItem[] {
  const groups = new Map<string, MarkdownPage[]>();
  const category = (rel: string) => {
    if (rel.startsWith('guides/client/')) return 'Client SDK';
    if (rel.startsWith('guides/react/')) return 'React';
    if (rel.startsWith('guides/svelte/')) return 'Svelte';
    if (rel.startsWith('guides/')) return 'Backend guides';
    if (rel.startsWith('examples/')) return 'Examples';
    return 'Project';
  };

  for (const page of files) {
    const name = category(page.rel);
    const pages = groups.get(name) ?? [];
    pages.push(page);
    groups.set(name, pages);
  }

  const order = ['Project', 'Backend guides', 'Client SDK', 'React', 'Svelte', 'Examples'];
  return order.flatMap((name) => {
    const pages = groups.get(name);
    if (!pages?.length) return [];
    return [{
      text: name,
      collapsed: name === 'Examples',
      items: pages.map(({ filePath, rel }) => ({
        text: pageText(filePath, rel),
        link: pageLink(filePath),
      })),
    }];
  });
}

function typedocSidebarItems(pkgDir: string): DefaultTheme.SidebarItem[] {
  const sidebarFile = join(pkgDir, 'typedoc-sidebar.json');
  if (!existsSync(sidebarFile)) return [];
  const raw = JSON.parse(readFileSync(sidebarFile, 'utf-8')) as DefaultTheme.SidebarItem[];
  return raw.map((item) => normalizeTypedocSidebarItem(item));
}

function normalizeTypedocSidebarItem(item: DefaultTheme.SidebarItem): DefaultTheme.SidebarItem {
  const normalized: DefaultTheme.SidebarItem = {
    text: item.text,
    collapsed: item.collapsed,
    items: item.items ? item.items.map((child) => normalizeTypedocSidebarItem(child)) : undefined,
  };
  if (item.link) {
    normalized.link = item.link.replace(/\.md$/, '');
  }
  return normalized;
}

function goSidebarItems(): DefaultTheme.SidebarItem[] {
  const goDir = join(root, 'api-reference', 'go');
  if (!existsSync(goDir)) return [];
  const executable: DefaultTheme.SidebarItem[] = [];
  const internal: DefaultTheme.SidebarItem[] = [];
  const other: DefaultTheme.SidebarItem[] = [];

  function walk(current: string) {
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      const entryPath = join(current, entry.name);
      if (entry.isDirectory()) {
        walk(entryPath);
      } else if (entry.isFile() && entry.name === 'index.md') {
        const relDir = toPosix(relative(goDir, current));
        const text = pageText(entryPath, relDir);
        const link = '/api-reference/go/' + relDir + '/';
        const item = { text, link };
        if (relDir === 'backend/cmd/pbvex') {
          executable.push(item);
        } else if (relDir.includes('/internal/')) {
          internal.push(item);
        } else {
          other.push(item);
        }
      }
    }
  }
  walk(goDir);

  const items: DefaultTheme.SidebarItem[] = [];
  if (executable.length) {
    items.push({ text: 'Executable', items: executable });
  }
  if (internal.length) {
    items.push({ text: 'Internal packages', items: internal });
  }
  if (other.length) {
    items.push({ text: 'Other packages', items: other });
  }
  return items;
}

function buildSidebar(): DefaultTheme.SidebarItem[] {
  const sidebar: DefaultTheme.SidebarItem[] = [];

  // Guides: root .md files, excluding index.md and api-reference contents
  const guideFiles = scanMarkdownFiles(root, (rel) => {
    return (
      !rel.startsWith('.vitepress/') &&
      !rel.startsWith('api-reference/') &&
      !rel.startsWith('adr/') &&
      rel !== 'index.md'
    );
  });
  sidebar.push(...guideGroups(guideFiles));

  // ADRs
  const adrFiles = scanMarkdownFiles(root, (rel) => rel.startsWith('adr/') && rel.endsWith('.md'));
  if (adrFiles.length) {
    sidebar.push({
      text: 'ADRs',
      collapsed: true,
      items: adrFiles.map(({ filePath, rel }) => ({
        text: pageText(filePath, rel),
        link: pageLink(filePath),
      })),
    });
  }

  // API Reference
  const tsApiDir = join(root, 'api-reference', 'ts');
  const tsItems: DefaultTheme.SidebarItem[] = [];
  if (existsSync(tsApiDir)) {
    const tsDirs = readdirSync(tsApiDir)
      .filter((name) => statSync(join(tsApiDir, name)).isDirectory())
      .sort();
    for (const tsDir of tsDirs) {
      const pkgDir = join(tsApiDir, tsDir);
      const items = typedocSidebarItems(pkgDir);
      if (items.length) {
        tsItems.push({
          text: tsDir,
          collapsed: true,
          items,
        });
      } else {
        tsItems.push({
          text: tsDir,
          link: `/api-reference/ts/${tsDir}/`,
        });
      }
    }
  }

  const goItems = goSidebarItems();

  const apiItems: DefaultTheme.SidebarItem[] = [];
  apiItems.push({
    text: 'Overview',
    link: '/api-reference/',
  });
  if (tsItems.length) {
    apiItems.push({
      text: 'TypeScript',
      collapsed: true,
      items: [
        { text: 'Overview', link: '/api-reference/ts/' },
        ...tsItems,
      ],
    });
  }
  if (goItems.length) {
    apiItems.push({
      text: 'Go',
      collapsed: true,
      items: [
        { text: 'Overview', link: '/api-reference/go/' },
        ...goItems,
      ],
    });
  }

  sidebar.push({
    text: 'API Reference',
    collapsed: true,
    items: apiItems,
  });

  return sidebar;
}

function buildNav(): DefaultTheme.NavItem[] {
  const nav: DefaultTheme.NavItem[] = [
    { text: 'Client', link: '/guides/client/' },
    { text: 'React', link: '/guides/react/' },
    { text: 'Svelte', link: '/guides/svelte/' },
    { text: 'Examples', link: '/examples/client/' },
  ];
  nav.push({
    text: 'API Reference',
    link: '/api-reference/',
    activeMatch: '/api-reference/',
  });

  return nav;
}

export default defineConfig({
  title: 'PBVex',
  description: 'Convex-like TypeScript authoring on a PocketBase Go backend.',
  base: process.env.DOCS_BASE || '/',
  srcDir: '.',
  outDir: '.vitepress/dist',
  cacheDir: '.vitepress/cache',
  cleanUrls: false,
  ignoreDeadLinks: false,
  markdown: {
    lineNumbers: false,
  },
  themeConfig: {
    nav: buildNav(),
    sidebar: buildSidebar(),
    socialLinks: [
      { icon: 'github', link: 'https://github.com/nathabonfim59/pbvex' },
    ],
    footer: {
      message: 'Generated API reference. Source of truth is the codebase.',
      copyright: 'Copyright © 2026 PBVex authors',
    },
  },
});
