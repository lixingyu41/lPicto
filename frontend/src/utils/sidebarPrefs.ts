import type { SidebarPanelTarget } from '../components/SidebarContext';

export type PrimarySidebarPanelTarget = 'library' | 'ratings' | 'search' | 'albums' | 'folders' | 'settings';

const sidebarSecondaryKey = 'lpicto.sidebarSecondaryExpanded';
const sidebarWidthsKey = 'lpicto.sidebarWidths';
const sidebarCollapsedKey = 'lpicto.sidebarCollapsed';
export const sidebarPrimaryWidth = 122;
const sidebarWidthDefaults = { primary: sidebarPrimaryWidth, secondary: 260 };
const sidebarWidthLimits = {
  secondary: { max: 640, min: 180 },
};

export interface SidebarWidths {
  primary: number;
  secondary: number;
}

export function isPrimarySidebarPanelTarget(target: SidebarPanelTarget | null | undefined): target is PrimarySidebarPanelTarget {
  return target === 'library' || target === 'ratings' || target === 'search' || target === 'albums' || target === 'folders' || target === 'settings';
}

export function primaryTargetForPath(pathname: string): PrimarySidebarPanelTarget | null {
  if (pathname === '/library' || pathname.startsWith('/library/')) return 'library';
  if (pathname === '/ratings' || pathname.startsWith('/ratings/')) return 'ratings';
  if (pathname === '/search' || pathname.startsWith('/search/')) return 'search';
  if (pathname === '/albums' || pathname.startsWith('/albums/')) return 'albums';
  if (pathname === '/folders' || pathname.startsWith('/folders/')) return 'folders';
  if (pathname === '/settings' || pathname.startsWith('/settings/')) return 'settings';
  return null;
}

export function loadSidebarSecondaryExpanded() {
  try {
    const raw = window.localStorage.getItem(sidebarSecondaryKey);
    if (!raw) return false;
    const parsed = JSON.parse(raw);
    if (typeof parsed === 'boolean') return parsed;
    if (!parsed || typeof parsed !== 'object') return false;
    return (
      parsed.library === true ||
      parsed.ratings === true ||
      parsed.search === true ||
      parsed.albums === true ||
      parsed.folders === true ||
      parsed.settings === true
    );
  } catch {
    return false;
  }
}

export function saveSidebarSecondaryExpanded(expanded: boolean) {
  try {
    window.localStorage.setItem(sidebarSecondaryKey, JSON.stringify(expanded));
  } catch {
    // Ignore storage failures; the current session state still works.
  }
}

export function loadSidebarCollapsed() {
  try {
    return window.localStorage.getItem(sidebarCollapsedKey) === 'true';
  } catch {
    return false;
  }
}

export function saveSidebarCollapsed(collapsed: boolean) {
  try {
    window.localStorage.setItem(sidebarCollapsedKey, String(collapsed));
  } catch {
    // Ignore storage failures; the current session state still works.
  }
}

export function loadSidebarWidths(): SidebarWidths {
  try {
    const raw = window.localStorage.getItem(sidebarWidthsKey);
    if (!raw) return sidebarWidthDefaults;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return sidebarWidthDefaults;
    return normalizeSidebarWidths({
      primary: Number(parsed.primary),
      secondary: Number(parsed.secondary),
    });
  } catch {
    return sidebarWidthDefaults;
  }
}

export function normalizeSidebarWidths(widths: Partial<SidebarWidths>): SidebarWidths {
  return {
    primary: normalizePrimarySidebarWidth(widths.primary ?? sidebarWidthDefaults.primary),
    secondary: clampSecondarySidebarWidth(widths.secondary ?? sidebarWidthDefaults.secondary),
  };
}

export function saveSidebarWidths(widths: SidebarWidths) {
  try {
    window.localStorage.setItem(sidebarWidthsKey, JSON.stringify(normalizeSidebarWidths(widths)));
  } catch {
    // Ignore storage failures; the current session state still works.
  }
}

function normalizePrimarySidebarWidth(value: number) {
  return Number.isFinite(value) ? sidebarPrimaryWidth : sidebarWidthDefaults.primary;
}

function clampSecondarySidebarWidth(value: number) {
  const limits = sidebarWidthLimits.secondary;
  if (!Number.isFinite(value)) return sidebarWidthDefaults.secondary;
  return Math.min(limits.max, Math.max(limits.min, Math.round(value)));
}
