import type { SidebarPanelTarget } from '../components/SidebarContext';

export interface GridReturnState {
  focusAssetId?: number | null;
  loadedItemCount: number;
  loadedStartIndex: number;
  scrollRatio: number;
  scrollTop: number;
  sidebarExpanded: SidebarPanelTarget | null;
}

const pageStatePrefix = 'lpicto:page-state:';
const viewerReturnKey = 'lpicto:viewer-return-path';
export const viewerOverlayAssetFocusChanged = 'lpicto:viewer-overlay-asset-focus';
export const assetRatingChanged = 'lpicto:asset-rating-changed';

function loadStoredValue(key: string) {
  const localValue = window.localStorage.getItem(key);
  if (localValue !== null) return localValue;
  return window.sessionStorage.getItem(key);
}

export function loadPageState<T extends object>(key: string, fallback: T): T {
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = loadStoredValue(pageStatePrefix + key);
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
    window.localStorage.setItem(pageStatePrefix + key, JSON.stringify(value));
  } catch {
    // Ignore storage failures; the viewer still has URL fallback.
  }
}

export function clearRestoreParamFromLocation() {
  if (typeof window === 'undefined') return;
  const url = new URL(window.location.href);
  if (!url.searchParams.has('restore')) return;
  url.searchParams.delete('restore');
  const next = `${url.pathname}${url.search}${url.hash}`;
  window.history.replaceState(window.history.state, '', next);
}

export function saveViewerReturnPath(path: string) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(viewerReturnKey, path);
  } catch {
    // Ignore storage failures.
  }
}

export function loadViewerReturnPath() {
  if (typeof window === 'undefined') return '';
  try {
    return loadStoredValue(viewerReturnKey) ?? '';
  } catch {
    return '';
  }
}

export function resetGridState(): GridReturnState {
  return {
    focusAssetId: null,
    loadedItemCount: 0,
    loadedStartIndex: 0,
    scrollRatio: 0,
    scrollTop: 0,
    sidebarExpanded: null,
  };
}

export function encodeReturnState<T extends object>(value: T) {
  return encodeURIComponent(JSON.stringify(value));
}

export function appendViewerReturnParams(url: string, returnPath: string, state: object) {
  const separator = url.includes('?') ? '&' : '?';
  return `${url}${separator}returnPath=${encodeURIComponent(returnPath)}&returnState=${encodeReturnState(state)}`;
}

export function emitViewerOverlayAssetFocus(assetId: number) {
  if (typeof window === 'undefined' || !Number.isFinite(assetId) || assetId <= 0) return;
  window.dispatchEvent(new CustomEvent(viewerOverlayAssetFocusChanged, { detail: { assetId } }));
}

export function emitAssetRatingChanged(assetId: number, rating: number) {
  if (typeof window === 'undefined' || !Number.isFinite(assetId) || assetId <= 0) return;
  if (!Number.isFinite(rating) || rating < 0 || rating > 5) return;
  window.dispatchEvent(new CustomEvent(assetRatingChanged, { detail: { assetId, rating } }));
}

export function viewerOverlayAssetFocusId(event: Event) {
  if (!(event instanceof CustomEvent)) return null;
  const assetId = Number((event.detail as { assetId?: unknown } | null)?.assetId);
  return Number.isFinite(assetId) && assetId > 0 ? assetId : null;
}

export function assetRatingChangeDetail(event: Event) {
  if (!(event instanceof CustomEvent)) return null;
  const assetId = Number((event.detail as { assetId?: unknown; rating?: unknown } | null)?.assetId);
  const rating = Number((event.detail as { assetId?: unknown; rating?: unknown } | null)?.rating);
  if (!Number.isFinite(assetId) || assetId <= 0 || !Number.isFinite(rating) || rating < 0 || rating > 5) return null;
  return { assetId, rating };
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
