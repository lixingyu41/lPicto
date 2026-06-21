import type { SidebarPanelTarget } from '../components/SidebarContext';

export type PrimarySidebarPanelTarget = 'library' | 'search' | 'albums' | 'folders' | 'settings';

const sidebarSecondaryKey = 'lpicto.sidebarSecondaryExpanded';
const sidebarWidthsKey = 'lpicto.sidebarWidths';
const sidebarWidthDefaults = { primary: 292, secondary: 260 };
const sidebarWidthLimits = {
  primary: { max: 520, min: 180 },
  secondary: { max: 640, min: 180 },
};

export interface SidebarWidths {
  primary: number;
  secondary: number;
}

export function isPrimarySidebarPanelTarget(target: SidebarPanelTarget | null | undefined): target is PrimarySidebarPanelTarget {
  return target === 'library' || target === 'search' || target === 'albums' || target === 'folders' || target === 'settings';
}

export function primaryTargetForPath(pathname: string): PrimarySidebarPanelTarget | null {
  if (pathname === '/library' || pathname.startsWith('/library/')) return 'library';
  if (pathname === '/search' || pathname.startsWith('/search/')) return 'search';
  if (pathname === '/albums' || pathname.startsWith('/albums/')) return 'albums';
  if (pathname === '/folders' || pathname.startsWith('/folders/')) return 'folders';
  if (pathname === '/settings' || pathname.startsWith('/settings/')) return 'settings';
  return null;
}

export function loadSidebarSecondaryExpanded(target: PrimarySidebarPanelTarget) {
  const state = loadSidebarSecondaryState();
  if (target === 'search' && state.search === undefined) {
    return true;
  }
  return state[target] === true;
}

export function saveSidebarSecondaryExpanded(target: PrimarySidebarPanelTarget, expanded: boolean) {
  const next = { ...loadSidebarSecondaryState(), [target]: expanded };
  try {
    window.localStorage.setItem(sidebarSecondaryKey, JSON.stringify(next));
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
    primary: clampSidebarWidth('primary', widths.primary ?? sidebarWidthDefaults.primary),
    secondary: clampSidebarWidth('secondary', widths.secondary ?? sidebarWidthDefaults.secondary),
  };
}

export function saveSidebarWidths(widths: SidebarWidths) {
  try {
    window.localStorage.setItem(sidebarWidthsKey, JSON.stringify(normalizeSidebarWidths(widths)));
  } catch {
    // Ignore storage failures; the current session state still works.
  }
}

function clampSidebarWidth(kind: keyof SidebarWidths, value: number) {
  const limits = sidebarWidthLimits[kind];
  if (!Number.isFinite(value)) return sidebarWidthDefaults[kind];
  return Math.min(limits.max, Math.max(limits.min, Math.round(value)));
}

function loadSidebarSecondaryState(): Partial<Record<PrimarySidebarPanelTarget, boolean>> {
  try {
    const raw = window.localStorage.getItem(sidebarSecondaryKey);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return {};
    return {
      library: parsed.library === true,
      search: parsed.search === true,
      albums: parsed.albums === true,
      folders: parsed.folders === true,
      settings: parsed.settings === true,
    };
  } catch {
    return {};
  }
}
