import type { SidebarPanelTarget } from '../components/SidebarContext';

export interface GridReturnState {
  loadedItemCount: number;
  loadedStartIndex: number;
  scrollRatio: number;
  scrollTop: number;
  sidebarCollapsed: boolean;
  sidebarExpanded: SidebarPanelTarget | null;
}

const pageStatePrefix = 'lpicto:page-state:';
const viewerReturnKey = 'lpicto:viewer-return-path';

export function loadPageState<T extends object>(key: string, fallback: T): T {
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = window.sessionStorage.getItem(pageStatePrefix + key);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return fallback;
    return { ...fallback, ...parsed };
  } catch {
    return fallback;
  }
}

export function savePageState<T extends object>(key: string, value: T) {
  if (typeof window === 'undefined') return;
  try {
    window.sessionStorage.setItem(pageStatePrefix + key, JSON.stringify(value));
  } catch {
    // Ignore storage failures; the viewer still has URL fallback.
  }
}

export function saveViewerReturnPath(path: string) {
  if (typeof window === 'undefined') return;
  try {
    window.sessionStorage.setItem(viewerReturnKey, path);
  } catch {
    // Ignore storage failures.
  }
}

export function loadViewerReturnPath() {
  if (typeof window === 'undefined') return '';
  try {
    return window.sessionStorage.getItem(viewerReturnKey) ?? '';
  } catch {
    return '';
  }
}

export function resetGridState(): GridReturnState {
  return { loadedItemCount: 0, loadedStartIndex: 0, scrollRatio: 0, scrollTop: 0, sidebarCollapsed: false, sidebarExpanded: null };
}

export function encodeReturnState<T extends object>(value: T) {
  return encodeURIComponent(JSON.stringify(value));
}

export function decodeReturnState<T extends object>(value: string | null, fallback: T): T {
  if (!value) return fallback;
  try {
    const parsed = JSON.parse(value);
    if (!parsed || typeof parsed !== 'object') return fallback;
    return { ...fallback, ...parsed };
  } catch {
    return fallback;
  }
}
