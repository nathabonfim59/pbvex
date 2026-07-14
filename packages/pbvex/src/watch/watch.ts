import { watch as chokidarWatch } from 'chokidar';
import { debounce } from './debounce.js';
import type { BundleResult } from '../bundler/bundler.js';
import type { ResolvedConfig } from '../config/config.js';

export interface WatchOptions {
  config: ResolvedConfig;
  build: () => Promise<BundleResult>;
  generateCodegen: (result: BundleResult) => Promise<void>;
  deploy: (result: BundleResult) => Promise<void>;
  onChange: (result: { ok: boolean; diagnostics: string[]; error?: string }) => void;
  debounceMs?: number;
}

export function watchPbvex(options: WatchOptions): { close: () => Promise<void> } {
  const watcher = chokidarWatch('pbvex/**/*.ts', {
    cwd: options.config.rootDir,
    ignored: ['**/pbvex/_generated/**', '**/node_modules/**'],
    persistent: true,
  });

  const debouncedRebuild = debounce(async () => {
    try {
      const result = await options.build();
      if (result.diagnostics.length > 0) {
        options.onChange({ ok: false, diagnostics: result.diagnostics });
        return;
      }
      await options.generateCodegen(result);
      await options.deploy(result);
      options.onChange({ ok: true, diagnostics: [] });
    } catch (err) {
      options.onChange({ ok: false, diagnostics: [], error: err instanceof Error ? err.message : String(err) });
    }
  }, options.debounceMs ?? 300);

  watcher
    .on('add', debouncedRebuild)
    .on('change', debouncedRebuild)
    .on('unlink', debouncedRebuild)
    .on('error', (err) => options.onChange({ ok: false, diagnostics: [], error: err.message }));

  return {
    close: async () => {
      await watcher.close();
    },
  };
}
