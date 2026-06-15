import type { SidebarPanelTarget } from '../components/SidebarContext';

export type PrimarySidebarPanelTarget = 'library' | 'albums' | 'folders';

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
  return target === 'library' || target === 'albums' || target === 'folders';
}

export function loadSidebarSecondaryExpanded(target: PrimarySidebarPanelTarget) {
  return loadSidebarSecondaryState()[target] === true;
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
      albums: parsed.albums === true,
      folders: parsed.folders === true,
    };
  } catch {
    return {};
  }
}
