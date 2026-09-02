import { api } from '../api/client';
import type { AITagTreeNode } from '../types/api';

interface TagTreeCacheEntry {
  savedAt: number;
  tree: AITagTreeNode[];
}

interface StoredTagTreeCache {
  version: 1;
  entries: Record<string, TagTreeCacheEntry>;
}

const storageKey = 'lpicto.ai-tag-tree-cache.v1';
export const aiTagTreeInvalidatedEvent = 'lpicto:ai-tag-tree-invalidated';
const freshForMs = 60_000;
const retainForMs = 30 * 24 * 60 * 60 * 1000;
const maxEntries = 24;
const memory = new Map<string, TagTreeCacheEntry>();
const inFlight = new Map<string, Promise<AITagTreeNode[]>>();
let storageLoaded = false;
let cacheGeneration = 0;

function cacheKey(selected: string[]) {
  return [...new Set(selected.map((value) => value.trim()).filter(Boolean))].sort().join('\u001f');
}

function loadStorage() {
  if (storageLoaded || typeof window === 'undefined') return;
  storageLoaded = true;
  try {
    const parsed = JSON.parse(window.localStorage.getItem(storageKey) ?? '') as StoredTagTreeCache;
    if (parsed.version !== 1 || !parsed.entries) return;
    const oldest = Date.now() - retainForMs;
    for (const [key, entry] of Object.entries(parsed.entries)) {
      if (entry.savedAt >= oldest && Array.isArray(entry.tree)) memory.set(key, entry);
    }
  } catch {
    // Restricted storage or an old malformed entry must not delay the picker.
  }
}

function persistStorage() {
  if (typeof window === 'undefined') return;
  const entries = [...memory.entries()]
    .sort((left, right) => right[1].savedAt - left[1].savedAt)
    .slice(0, maxEntries);
  memory.clear();
  entries.forEach(([key, value]) => memory.set(key, value));
  try {
    window.localStorage.setItem(storageKey, JSON.stringify({ version: 1, entries: Object.fromEntries(entries) } satisfies StoredTagTreeCache));
  } catch {
    // The in-memory cache remains available when persistent storage is full or unavailable.
  }
}

export function readAITagTreeCache(selected: string[]) {
  loadStorage();
  return memory.get(cacheKey(selected))?.tree ?? null;
}

export function readAITagTreeFallback(selected: string[]) {
  return readAITagTreeCache(selected) ?? readAITagTreeCache([]);
}

export function loadAITagTree(selected: string[], refresh = false): Promise<AITagTreeNode[]> {
  loadStorage();
  const key = cacheKey(selected);
  const cached = memory.get(key);
  if (!refresh && cached && Date.now() - cached.savedAt < freshForMs) return Promise.resolve(cached.tree);
  const pending = inFlight.get(key);
  if (pending) return pending;
  const requestGeneration = cacheGeneration;
  const request: Promise<AITagTreeNode[]> = api.aiTags('', selected).then((result) => {
    if (requestGeneration !== cacheGeneration) {
      inFlight.delete(key);
      return loadAITagTree(selected, true);
    }
    const tree = result.tree ?? [];
    memory.set(key, { savedAt: Date.now(), tree });
    persistStorage();
    return tree;
  }).finally(() => {
    if (inFlight.get(key) === request) inFlight.delete(key);
  });
  inFlight.set(key, request);
  return request;
}

export function preloadAITagTree() {
  return loadAITagTree([]).catch(() => readAITagTreeCache([]) ?? []);
}

export function invalidateAITagTreeCache() {
  cacheGeneration += 1;
  memory.clear();
  inFlight.clear();
  storageLoaded = true;
  try {
    window.localStorage.removeItem(storageKey);
  } catch {
    // Restricted storage does not affect the in-memory invalidation.
  }
}

if (typeof window !== 'undefined') {
  window.addEventListener(aiTagTreeInvalidatedEvent, invalidateAITagTreeCache);
}
