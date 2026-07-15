import { canonicalJson, encodeValue, type PbvexValue } from '@pbvex/protocol';
import type { Client, QueryResult, Unsubscribe, WatchOptions } from '@pbvex/client';

const LOADING = Object.freeze({ data: undefined, error: null, isLoading: true });
const SKIPPED = Object.freeze({ data: undefined, error: null, isLoading: false });

export function toCanonical(args: unknown): string {
  if (args === 'skip') return 'skip';
  const encoded = args === undefined ? {} : encodeValue(args as PbvexValue);
  return canonicalJson(encoded);
}

export class QueryStore<Args, Return> {
  private result: QueryResult<Return>;
  private serverSnapshot: QueryResult<Return>;
  private listeners = new Set<() => void>();
  private unsubscribe: Unsubscribe | null = null;
  private generation = 0;

  constructor(
    private readonly client: Client,
    private readonly path: string,
    private readonly args: Args | 'skip' | undefined,
  ) {
    const skip = args === 'skip';
    this.result = (skip ? SKIPPED : LOADING) as QueryResult<Return>;
    this.serverSnapshot = this.result;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    if (!this.unsubscribe && this.args !== 'skip') {
      this.generation += 1;
      const gen = this.generation;
      this.unsubscribe = this.client.watch(this.path, this.args, {
        onUpdate: (result) => {
          if (gen !== this.generation) return;
          this.handleUpdate(result as QueryResult<Return>);
        },
        onError: (error) => {
          if (gen !== this.generation) return;
          this.handleUpdate({ data: undefined, error, isLoading: false } as QueryResult<Return>);
        },
      } as WatchOptions<Return>);
    }
    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0 && this.unsubscribe) {
        // Invalidate any callbacks from the watch that is about to be torn down.
        this.generation += 1;
        this.unsubscribe();
        this.unsubscribe = null;
      }
    };
  }

  private handleUpdate(result: QueryResult<Return>): void {
    if (this.isSameResult(result)) return;
    this.result = result;
    for (const listener of this.listeners) {
      listener();
    }
  }

  private isSameResult(next: QueryResult<Return>): boolean {
    return (
      this.result.isLoading === next.isLoading &&
      this.result.error === next.error &&
      this.result.data === next.data
    );
  }

  getSnapshot(): QueryResult<Return> {
    return this.result;
  }

  getServerSnapshot(): QueryResult<Return> {
    return this.serverSnapshot;
  }
}
