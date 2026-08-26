export type ImmersiveChromeSize = 1 | 2 | 3 | 4 | 5;
export type ImmersivePinnedPanelMode = 'scope' | 'albums' | 'query';
export type ImmersivePinnedToolMenu = 'tags' | 'type' | 'orientation' | 'rating' | 'sort' | 'group' | 'layout';
export type ImmersivePinnedMenuTarget = 'library' | 'recent' | 'albums' | 'folders' | 'collections' | 'viewer' | 'settings';
export type ImmersivePinnedMenu =
  | { kind: 'navigation' }
  | { kind: 'panel'; mode: ImmersivePinnedPanelMode; target: ImmersivePinnedMenuTarget }
  | { kind: 'tool'; menu: ImmersivePinnedToolMenu; target: ImmersivePinnedMenuTarget };

const storageKey = 'lpicto.immersiveChrome.size';
const pinnedMenuWidthStorageKey = 'lpicto.immersiveChrome.pinnedMenuWidth';
const pinnedMenuStorageKey = 'lpicto.immersiveChrome.pinnedMenu.v1';
export const defaultPinnedMenuWidth = 340;
export const minPinnedMenuWidth = 240;
export const maxPinnedMenuWidth = 680;
export const immersiveChromeSizeChanged = 'lpicto:immersive-chrome-size-changed';

export function loadImmersiveChromeSize(): ImmersiveChromeSize {
  try {
    const value = Number(window.localStorage.getItem(storageKey));
    if (value >= 1 && value <= 5 && Number.isInteger(value)) return value as ImmersiveChromeSize;
  } catch {
    // Use the current visual size when browser storage is unavailable.
  }
  return 2;
}

export function saveImmersiveChromeSize(size: ImmersiveChromeSize) {
  try {
    window.localStorage.setItem(storageKey, String(size));
  } catch {
    // The live event still updates this tab.
  }
  window.dispatchEvent(new CustomEvent<ImmersiveChromeSize>(immersiveChromeSizeChanged, { detail: size }));
}

export function loadPinnedMenuWidth() {
  try {
    const value = Number(window.localStorage.getItem(pinnedMenuWidthStorageKey));
    if (Number.isFinite(value)) return Math.round(Math.min(maxPinnedMenuWidth, Math.max(minPinnedMenuWidth, value)));
  } catch {
    // Keep the default width when browser storage is unavailable.
  }
  return defaultPinnedMenuWidth;
}

export function savePinnedMenuWidth(width: number) {
  const normalized = Math.round(Math.min(maxPinnedMenuWidth, Math.max(minPinnedMenuWidth, width)));
  try {
    window.localStorage.setItem(pinnedMenuWidthStorageKey, String(normalized));
  } catch {
    // The current tab still keeps the live width.
  }
  return normalized;
}

export function loadPinnedMenu(): ImmersivePinnedMenu | null {
  try {
    const raw = window.localStorage.getItem(pinnedMenuStorageKey);
    if (!raw) return null;
    const value = JSON.parse(raw) as unknown;
    return isImmersivePinnedMenu(value) ? value : null;
  } catch {
    return null;
  }
}

export function savePinnedMenu(menu: ImmersivePinnedMenu | null) {
  try {
    if (menu === null) window.localStorage.removeItem(pinnedMenuStorageKey);
    else window.localStorage.setItem(pinnedMenuStorageKey, JSON.stringify(menu));
  } catch {
    // The current tab still keeps the live pinned menu.
  }
}

function isImmersivePinnedMenu(value: unknown): value is ImmersivePinnedMenu {
  if (!value || typeof value !== 'object') return false;
  const candidate = value as Record<string, unknown>;
  if (candidate.kind === 'navigation') return true;
  if (!isPinnedMenuTarget(candidate.target)) return false;
  if (candidate.kind === 'panel') return candidate.mode === 'scope' || candidate.mode === 'albums' || candidate.mode === 'query';
  if (candidate.kind === 'tool') {
    return candidate.menu === 'tags' || candidate.menu === 'type' || candidate.menu === 'orientation' || candidate.menu === 'rating' || candidate.menu === 'sort' || candidate.menu === 'group' || candidate.menu === 'layout';
  }
  return false;
}

function isPinnedMenuTarget(value: unknown): value is ImmersivePinnedMenuTarget {
  return value === 'library' || value === 'recent' || value === 'albums' || value === 'folders' || value === 'collections' || value === 'viewer' || value === 'settings';
}
