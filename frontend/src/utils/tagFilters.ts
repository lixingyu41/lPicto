export function parseTagFilters(value: string | null | undefined): string[] {
  if (!value) return [];
  try {
    const parsed = JSON.parse(value);
    if (!Array.isArray(parsed)) return [];
    return normalizeTagFilters(parsed.map(String));
  } catch {
    return normalizeTagFilters(value.split(','));
  }
}

export function normalizeTagFilters(values: string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter((value) => value && [...value].length <= 80))].slice(0, 32);
}

export function serializeTagFilters(values: string[]) {
  const normalized = normalizeTagFilters(values);
  return normalized.length > 0 ? JSON.stringify(normalized) : undefined;
}
