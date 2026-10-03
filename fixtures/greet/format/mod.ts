export function titleCase(value: string): string {
  return value.replace(/\b\w/g, (char) => char.toUpperCase());
}

export function trimAll(values: string[]): string[] {
  return values.map((value) => value.trim());
}
