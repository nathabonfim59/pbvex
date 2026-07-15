#!/usr/bin/env node
import { spawnServer } from './index.js';

let child;
try {
  child = spawnServer(process.argv.slice(2), { stdio: 'inherit' });
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}

child.once('error', (error) => {
  console.error(`Failed to start the PBVex backend: ${error.message}`);
  process.exitCode = 1;
});

child.once('exit', (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exitCode = code ?? 1;
});
