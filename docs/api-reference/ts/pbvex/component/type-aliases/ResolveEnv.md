[pbvex](../../index.md) / [component](../index.md) / ResolveEnv

# Type Alias: ResolveEnv\<E\>

> **ResolveEnv**\<`E`\> = `E` *extends* `Record`\<`string`, [`EnvEntry`](EnvEntry.md)\> ? `{ readonly [K in keyof E]: string }` : `Record`\<`never`, `string`\>

Resolve an env declaration to a typed key→string map (all env values are strings at runtime).

## Type Parameters

### E

`E`
