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
  const rel = toPosix(relative(root, filePath));
  if (rel === 'index.md') return '/';
  if (rel.endsWith('/index.md')) return '/' + rel.slice(0, -'index.md'.length);
  return '/' + rel.replace(/\.md$/, '');
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
type PageIndex = Map<string, MarkdownPage>;

function indexPages(files: MarkdownPage[]) {
  return new Map(files.map((page) => [page.rel, page]));
}

function pagesFor(
  pages: PageIndex,
  paths: string[],
  used: Set<string>,
): DefaultTheme.SidebarItem[] {
  return paths.flatMap((path) => {
    const page = pages.get(path);
    if (!page || used.has(path)) return [];
    used.add(path);
    return [{ text: pageText(page.filePath, page.rel), link: pageLink(page.filePath) }];
  });
}

function remainingPages(
  pages: PageIndex,
  prefix: string,
  used: Set<string>,
): DefaultTheme.SidebarItem[] {
  return [...pages.values()]
    .filter((page) => page.rel.startsWith(prefix) && !used.has(page.rel))
    .map((page) => {
      used.add(page.rel);
      return { text: pageText(page.filePath, page.rel), link: pageLink(page.filePath) };
    });
}

function directChildPages(
  pages: PageIndex,
  prefix: string,
  used: Set<string>,
): DefaultTheme.SidebarItem[] {
  return [...pages.values()]
    .filter((page) => {
      const remainder = page.rel.slice(prefix.length);
      return page.rel.startsWith(prefix) && !remainder.includes('/') && !used.has(page.rel);
    })
    .map((page) => {
      used.add(page.rel);
      return { text: pageText(page.filePath, page.rel), link: pageLink(page.filePath) };
    });
}

function docsSidebarItems(): DefaultTheme.SidebarItem[] {
  const pages = indexPages(scanMarkdownFiles(root, (rel) => (
    !rel.startsWith('.vitepress/') &&
    !rel.startsWith('api-reference/') &&
    !rel.startsWith('adr/') &&
    rel !== 'operator.md'
  )));
  const used = new Set<string>();
  const sidebar: DefaultTheme.SidebarItem[] = [];

  const gettingStarted = pagesFor(pages, [
    'index.md',
    'quickstart.md',
    'concepts/backend-primitives.md',
    'concepts/how-it-works.md',
    'guides/index.md',
  ], used);
  if (gettingStarted.length) {
    sidebar.push({ text: 'Start here', items: gettingStarted });
  }

  const build = pagesFor(pages, [
    'guides/schema-and-database.md',
    'guides/querying-and-indexes.md',
    'guides/relationships-and-joins.md',
    'guides/data-types-and-validation.md',
    'guides/project-organization.md',
    'guides/functions.md',
    'guides/http-actions.md',
    'guides/outbound-http.md',
    'guides/components.md',
  ], used);
  if (build.length) sidebar.push({ text: 'Build your backend', items: build });

  const platform = pagesFor(pages, [
    'guides/auth.md',
    'guides/migrations.md',
    'guides/authorization.md',
    'guides/email-templates.md',
    'guides/storage.md',
    'guides/image-resizing.md',
    'guides/scheduling.md',
    'guides/cron-jobs.md',
    'guides/environment-variables.md',
  ], used);
  if (platform.length) sidebar.push({ text: 'Platform capabilities', items: platform });

  const messagingTutorial = pagesFor(pages, [
    'tutorials/messaging/index.md',
    'tutorials/messaging/data-model.md',
    'tutorials/messaging/authentication-and-profiles.md',
    'tutorials/messaging/contacts.md',
    'tutorials/messaging/conversations.md',
    'tutorials/messaging/messages-and-realtime.md',
    'tutorials/messaging/attachments.md',
  ], used);
  const paymentsTutorial = pagesFor(pages, [
    'tutorials/payments/index.md',
    'tutorials/payments/data-model.md',
    'tutorials/payments/checkout.md',
    'tutorials/payments/webhooks.md',
    'tutorials/payments/entitlements-and-production.md',
  ], used);
  if (messagingTutorial.length || paymentsTutorial.length) {
    const useCases: DefaultTheme.SidebarItem[] = [];
    if (messagingTutorial.length) useCases.push({
      text: 'Messaging app tutorial',
      collapsed: false,
      items: messagingTutorial,
    });
    if (paymentsTutorial.length) useCases.push({
      text: 'FakePayment tutorial',
      collapsed: false,
      items: paymentsTutorial,
    });
    sidebar.push({
      text: 'Use cases',
      collapsed: false,
      items: useCases,
    });
  }

  const client = pagesFor(pages, [
    'guides/client/index.md',
    'guides/client/installation.md',
    'guides/client/configuration.md',
    'guides/client/queries-mutations-actions.md',
    'guides/client/realtime.md',
    'guides/client/errors.md',
  ], used);
  client.push(...remainingPages(pages, 'guides/client/', used));

  if (client.length) sidebar.push({ text: 'Client SDK', items: client });

  const frameworkItems: DefaultTheme.SidebarItem[] = [];
  for (const [text, prefix] of [['React', 'guides/react/'], ['Svelte', 'guides/svelte/']] as const) {
    const items = remainingPages(pages, prefix, used);
    if (items.length) frameworkItems.push({ text, collapsed: true, items });
  }
  if (frameworkItems.length) {
    sidebar.push({ text: 'Frameworks', items: frameworkItems });
  }

  const operations = pagesFor(pages, [
    'guides/deployment.md',
    'self-hosting.md',
    'guides/going-to-production.md',
    'guides/testing.md',
    'guides/limits.md',
  ], used);
  if (operations.length) sidebar.push({ text: 'Deploy & operate', items: operations });

  const otherGuides = directChildPages(pages, 'guides/', used);
  if (otherGuides.length) {
    sidebar.push({ text: 'More guides', collapsed: true, items: otherGuides });
  }

  const exampleGroups: DefaultTheme.SidebarItem[] = [];
  for (const [text, prefix] of [['Client', 'examples/client/'], ['React', 'examples/react/'], ['Svelte', 'examples/svelte/']] as const) {
    const items = remainingPages(pages, prefix, used);
    if (items.length) exampleGroups.push({ text, collapsed: true, items });
  }
  const otherExamples = remainingPages(pages, 'examples/', used);
  if (otherExamples.length) exampleGroups.push({ text: 'More examples', collapsed: true, items: otherExamples });
  if (exampleGroups.length) sidebar.push({ text: 'Examples', collapsed: true, items: exampleGroups });

  const project = pagesFor(pages, [
    'getting-started/agent-skills.md',
    'releasing.md',
  ], used);
  project.push(...remainingPages(pages, '', used));
  if (project.length) sidebar.push({ text: 'Project', collapsed: true, items: project });

  return sidebar;
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
        const packageNames: Record<string, string> = {
          'backend/cmd/pbvex': 'PBVex CLI',
          'backend/internal/api': 'API server',
          'backend/internal/auth': 'Authentication',
          'backend/internal/deploy': 'Deployment',
          'backend/internal/pbvex': 'Backend core',
          'backend/internal/realtime': 'Realtime',
          'backend/internal/runtime': 'Function runtime',
          'backend/internal/scheduler': 'Scheduler',
          'backend/internal/schema': 'Schema',
          'backend/internal/storage': 'Storage',
        };
        const text = packageNames[relDir] ?? pageText(entryPath, relDir);
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
  const sidebar = docsSidebarItems();

  // ADRs
  const adrFiles = scanMarkdownFiles(root, (rel) => rel.startsWith('adr/') && rel.endsWith('.md'));
  if (adrFiles.length) {
    const adrGroup: DefaultTheme.SidebarItem = {
      text: 'Architecture decision records',
      collapsed: true,
      items: adrFiles.map(({ filePath, rel }) => ({
        text: pageText(filePath, rel),
        link: pageLink(filePath),
      })),
    };
    const project = sidebar.find((item) => item.text === 'Project');
    if (project?.items) {
      project.items.push(adrGroup);
    } else {
      sidebar.push({ text: 'Project', collapsed: true, items: [adrGroup] });
    }
  }

  // API Reference
  const tsApiDir = join(root, 'api-reference', 'ts');
  const tsItems: DefaultTheme.SidebarItem[] = [];
  if (existsSync(tsApiDir)) {
    const tsDirs = readdirSync(tsApiDir)
      .filter((name) => statSync(join(tsApiDir, name)).isDirectory())
      .sort();
    const packageNames: Record<string, string> = {
      pbvex: 'pbvex',
      protocol: '@pbvex/protocol',
      client: '@pbvex/client',
      react: '@pbvex/react',
      svelte: '@pbvex/svelte',
    };
    for (const tsDir of tsDirs) {
      const pkgDir = join(tsApiDir, tsDir);
      const items = typedocSidebarItems(pkgDir);
      if (items.length) {
        tsItems.push({
          text: packageNames[tsDir] ?? titleFromName(tsDir),
          collapsed: true,
          items,
        });
      } else {
        tsItems.push({
          text: packageNames[tsDir] ?? titleFromName(tsDir),
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
  const quickstart = ['quickstart.md', 'guides/quickstart.md']
    .map((rel) => join(root, rel))
    .find((filePath) => existsSync(filePath));
  return [
    { text: 'Quickstart', link: quickstart ? pageLink(quickstart) : '/' },
    {
      text: 'Guides',
      items: [
        { text: 'All guides', link: '/guides/' },
        { text: 'Build your backend', link: '/guides/functions' },
        { text: 'Authentication', link: '/guides/auth' },
        { text: 'PocketBase migrations', link: '/guides/migrations' },
        { text: 'Client SDK', link: '/guides/client/' },
        { text: 'React', link: '/guides/react/' },
        { text: 'Svelte', link: '/guides/svelte/' },
      ],
    },
    {
      text: 'Deploy',
      items: [
        { text: 'Deploy an application', link: '/guides/deployment' },
        { text: 'Self-host PBVex', link: '/self-hosting' },
        { text: 'Go to production', link: '/guides/going-to-production' },
      ],
    },
    {
      text: 'API',
    link: '/api-reference/',
    activeMatch: '/api-reference/',
    },
  ];
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
