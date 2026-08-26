import type { SidebarPanelTarget } from '../components/SidebarContext';

export type PrimarySidebarPanelTarget = 'library' | 'recent' | 'albums' | 'folders' | 'collections' | 'settings';

export function isPrimarySidebarPanelTarget(target: SidebarPanelTarget | null | undefined): target is PrimarySidebarPanelTarget {
  return target === 'library' || target === 'recent' || target === 'albums' || target === 'folders' || target === 'collections' || target === 'settings';
}

export function primaryTargetForPath(pathname: string): PrimarySidebarPanelTarget | null {
  if (pathname === '/library' || pathname.startsWith('/library/')) return 'library';
  if (pathname === '/recent' || pathname.startsWith('/recent/')) return 'recent';
  if (pathname === '/albums' || pathname.startsWith('/albums/')) return 'albums';
  if (pathname === '/folders' || pathname.startsWith('/folders/')) return 'folders';
  if (pathname === '/collections' || pathname.startsWith('/collections/') || pathname === '/tags' || pathname.startsWith('/tags/')) return 'collections';
  if (pathname === '/settings' || pathname.startsWith('/settings/')) return 'settings';
  return null;
}
