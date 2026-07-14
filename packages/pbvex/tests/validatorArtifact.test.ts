import { readFile } from 'node:fs/promises';
import { expect, test } from 'vitest';
import { createArtifact } from '../src/bundler/manifest.js';
import { v } from '../src/runtime/values.js';

test('TypeScript emits the checked-in deployable record/defaulted validator artifact', async () => {
  const fixture = JSON.parse(
    await readFile(new URL('../../../fixtures/validators/deployable-record-defaulted.json', import.meta.url), 'utf8'),
  ) as { descriptor: unknown };
  const args = v.object({
    labels: v.record(v.union(v.string(), v.literal('fixed')), v.defaulted(v.number(), 2)),
  });
  expect(args.toJSON()).toEqual(fixture.descriptor);

  const artifact = await createArtifact(
    'validator-artifact',
    'test',
    [
      {
        type: 'query',
        visibility: 'public',
        modulePath: 'roundtrip',
        exportName: 'default',
        args,
        returns: v.null(),
        handler: () => null,
      },
    ],
    [],
    undefined,
    undefined,
    [],
    '',
  );
  expect(artifact.manifest.functions[0].args).toEqual(fixture.descriptor);
});
