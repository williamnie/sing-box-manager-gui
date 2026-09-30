export function parseList(text: string): string[] {
  return text.split(/[\n,]+/).map((value) => value.trim()).filter(Boolean);
}
