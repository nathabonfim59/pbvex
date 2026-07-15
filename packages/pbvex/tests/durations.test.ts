import { describe, expect, it } from 'vitest';
import {
  DAY_MS,
  HOUR_MS,
  MINUTE_MS,
  SECOND_MS,
  WEEK_MS,
} from '../src/runtime/server.js';

describe('duration constants', () => {
  it('exports readable millisecond units', () => {
    expect(SECOND_MS).toBe(1_000);
    expect(MINUTE_MS).toBe(60_000);
    expect(HOUR_MS).toBe(3_600_000);
    expect(DAY_MS).toBe(86_400_000);
    expect(WEEK_MS).toBe(604_800_000);
  });
});
