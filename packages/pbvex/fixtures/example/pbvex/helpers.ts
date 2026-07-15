export function truncate(input: string, max: number): string {
  return input.length > max ? input.slice(0, max) + '...' : input;
}
