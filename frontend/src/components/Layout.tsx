import { useCallback, useEffect, useState, type CSSProperties, type ReactNode } from 'react';
import { useLocation, type Location } from 'react-router-dom';
import Sidebar from './Sidebar';
import { api } from '../api/client';
import type { StorageStatus } from '../types/api';
import { SidebarPanelProvider, type SidebarPanelTarget } from './SidebarContext';
import {
  isPrimarySidebarPanelTarget,
  loadCollapsedSidebarContent,
  loadSidebarCollapsed,
  loadSidebarSecondaryExpanded,
  loadSidebarWidths,
  normalizeSidebarWidths,
  primaryTargetForPath,
  saveSidebarCollapsed,
  saveSidebarSecondaryExpanded,
  saveSidebarWidths,
  sidebarAppearanceChanged,
  type CollapsedSidebarContent,
  type SidebarWidths,
} from '../utils/sidebarPrefs';

interface Props {
  children: ReactNode;
  overlay?: ReactNode;
  routeLocation?: Location;
}

export default function Layout({ children, overlay = null, routeLocation }: Props) {
  const location = useLocation();
  const effectivePathname = routeLocation?.pathname ?? location.pathname;
  const [sidebarCollapsed, setSidebarCollapsedState] = useState(() => loadSidebarCollapsed());
  const [collapsedSidebarContent, setCollapsedSidebarContent] = useState<CollapsedSidebarContent>(() => loadCollapsedSidebarContent());
  const [sidebarWidths, setSidebarWidths] = useState<SidebarWidths>(() => loadSidebarWidths());
  const [routeEntering, setRouteEntering] = useState(false);
  const [storageStatus, setStorageStatus] = useState<StorageStatus | null>(null);
  const routeTarget = primaryTargetForPath(effectivePathname);
  const [sidebarExpanded, setSidebarExpandedState] = useState<SidebarPanelTarget | null>(() =>
    routeTarget && loadSidebarSecondaryExpanded() ? routeTarget : null,
  );
  const sidebarPanelOpen = sidebarExpanded !== null && sidebarExpanded === routeTarget;
  const shellClass = [
    'app-shell',
    sidebarCollapsed ? 'sidebar-primary-collapsed' : 'sidebar-primary-open',
    sidebarCollapsed ? 'sidebar-primary-icon-only' : '',
    sidebarCollapsed && collapsedSidebarContent === 'character' ? 'sidebar-primary-character-only' : '',
    sidebarPanelOpen ? 'sidebar-panel-open' : 'sidebar-panel-closed',
  ]
    .filter(Boolean)
    .join(' ');
  const setSidebarExpanded = useCallback(
    (target: SidebarPanelTarget | null) => {
      if (target === null) {
        saveSidebarSecondaryExpanded(false);
        setSidebarExpandedState(null);
        return;
      }
      if (isPrimarySidebarPanelTarget(target)) {
        saveSidebarSecondaryExpanded(true);
      }
      setSidebarExpandedState(target);
    },
    [],
  );
  const setSidebarCollapsed = useCallback((collapsed: boolean) => {
    saveSidebarCollapsed(collapsed);
    setSidebarCollapsedState(collapsed);
  }, []);
  useEffect(() => {
    setSidebarExpandedState(routeTarget && loadSidebarSecondaryExpanded() ? routeTarget : null);
  }, [routeTarget]);

  useEffect(() => {
    const refresh = () => setCollapsedSidebarContent(loadCollapsedSidebarContent());
    window.addEventListener(sidebarAppearanceChanged, refresh);
    return () => window.removeEventListener(sidebarAppearanceChanged, refresh);
  }, []);

  useEffect(() => {
    setRouteEntering(false);
    const frame = window.requestAnimationFrame(() => setRouteEntering(true));
    const timer = window.setTimeout(() => setRouteEntering(false), 180);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timer);
    };
  }, [effectivePathname]);

  useEffect(() => {
    let disposed = false;
    const refresh = () => {
      api.storageStatus().then((status) => {
        if (!disposed) setStorageStatus(status);
      }).catch(() => undefined);
    };
    refresh();
    const timer = window.setInterval(refresh, 15_000);
    return () => {
      disposed = true;
      window.clearInterval(timer);
    };
  }, []);

  const updateSidebarWidth = useCallback((kind: keyof SidebarWidths, width: number) => {
    setSidebarWidths((current) => {
      const next = normalizeSidebarWidths({ ...current, [kind]: width });
      saveSidebarWidths(next);
      return next;
    });
  }, []);

  const togglePrimarySidebar = useCallback(() => {
    setSidebarCollapsed(!sidebarCollapsed);
  }, [setSidebarCollapsed, sidebarCollapsed]);

  const shellStyle = {
    '--sidebar-primary-width': `${sidebarWidths.primary}px`,
    '--sidebar-secondary-width': `${sidebarWidths.secondary}px`,
  } as CSSProperties;

  return (
    <SidebarPanelProvider
      sidebarCollapsed={sidebarCollapsed}
      sidebarExpanded={sidebarExpanded}
      setSidebarCollapsed={setSidebarCollapsed}
      setSidebarExpanded={setSidebarExpanded}
    >
      <div className={shellClass} style={shellStyle}>
        <aside className={sidebarCollapsed ? 'sidebar is-primary-collapsed' : 'sidebar'}>
          <Sidebar
            collapsed={sidebarCollapsed}
            collapsedContent={collapsedSidebarContent}
            expanded={sidebarExpanded}
            routePathname={effectivePathname}
            secondaryWidth={sidebarWidths.secondary}
            onTogglePrimary={togglePrimarySidebar}
            onToggleExpanded={setSidebarExpanded}
            onSecondaryWidthChange={(width) => updateSidebarWidth('secondary', width)}
          />
        </aside>
        <main className={[
          'main-panel',
          routeEntering ? 'route-entering' : '',
          storageStatus && !storageStatus.available ? 'storage-unavailable' : '',
        ].filter(Boolean).join(' ')}>
          {storageStatus && !storageStatus.available && <div className="storage-unavailable-banner" role="status">{storageStatus.message}</div>}
          {children}
        </main>
        {overlay && <div className="viewer-shell-overlay">{overlay}</div>}
      </div>
    </SidebarPanelProvider>
  );
}
