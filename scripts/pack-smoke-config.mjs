import { writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const fileDependency = (archive) => `file:${archive}`;

export function createConsumerManifest({ protocol, server, pbvex, client, react, svelte }) {
  return {
    name: 'pbvex-packed-consumer',
    private: true,
    type: 'module',
    packageManager: 'pnpm@8.15.0',
    scripts: {
      typecheck: 'tsc --noEmit',
      smoke: 'node smoke.mjs',
    },
    dependencies: {
      '@pbvex/protocol': fileDependency(protocol),
      '@pbvex/server': fileDependency(server),
      pbvex: fileDependency(pbvex),
      '@pbvex/client': fileDependency(client),
      '@pbvex/react': fileDependency(react),
      '@pbvex/svelte': fileDependency(svelte),
      '@types/react': '^18.3.0',
      '@types/react-dom': '^18.3.0',
      react: '^18.3.1',
      'react-dom': '^18.3.1',
      svelte: '^5.0.0',
      typescript: '^5.0.0',
    },
    pnpm: {
      overrides: {
        '@pbvex/protocol': fileDependency(protocol),
        '@pbvex/server': fileDependency(server),
      },
    },
  };
}

async function main() {
  const [output, protocol, server, pbvex, client, react, svelte] = process.argv.slice(2);
  if (![output, protocol, server, pbvex, client, react, svelte].every(Boolean)) {
    throw new Error('Usage: pack-smoke-config.mjs <output> <protocol> <server> <pbvex> <client> <react> <svelte>');
  }
  const manifest = createConsumerManifest({ protocol, server, pbvex, client, react, svelte });
  await writeFile(output, `${JSON.stringify(manifest, null, 2)}\n`);
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
