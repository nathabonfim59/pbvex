const CRON_MACROS = new Set([
  '@yearly',
  '@annually',
  '@monthly',
  '@weekly',
  '@daily',
  '@midnight',
  '@hourly',
]);

function parseInteger(value: string): number | undefined {
  if (!/^\d+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
}

function isCronSegment(segment: string, min: number, max: number): boolean {
  if (segment.length === 0) return false;
  for (const part of segment.split(',')) {
    const stepParts = part.split('/');
    if (stepParts.length > 2) return false;
    let step = 1;
    if (stepParts.length === 2) {
      const parsed = parseInteger(stepParts[1]!);
      if (parsed === undefined || parsed < 1 || parsed > max) return false;
      step = parsed;
    }

    const range = stepParts[0]!;
    if (range === '*') continue;
    const rangeParts = range.split('-');
    if (rangeParts.length === 1) {
      const value = parseInteger(rangeParts[0]!);
      if (step !== 1 || value === undefined || value < min || value > max) return false;
      continue;
    }
    if (rangeParts.length !== 2) return false;
    const from = parseInteger(rangeParts[0]!);
    const to = parseInteger(rangeParts[1]!);
    if (from === undefined || to === undefined || from < min || from > max || to < from || to > max) {
      return false;
    }
  }
  return true;
}

/** Returns whether a string is accepted by PocketBase's five-field cron parser. */
export function isCronExpression(value: unknown): value is string {
  if (typeof value !== 'string' || value.length === 0 || value.length > 128) return false;
  if (CRON_MACROS.has(value)) return true;
  const segments = value.split(' ');
  return segments.length === 5 &&
    isCronSegment(segments[0]!, 0, 59) &&
    isCronSegment(segments[1]!, 0, 23) &&
    isCronSegment(segments[2]!, 1, 31) &&
    isCronSegment(segments[3]!, 1, 12) &&
    isCronSegment(segments[4]!, 0, 6);
}
