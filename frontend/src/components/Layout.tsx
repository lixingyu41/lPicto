import { useCallback, useEffect, useState, type CSSProperties, type ReactNode } from 'react';
import { useLocation, type Location } from 'react-router-dom';
import DesignGridOverlay from './DesignGridOverlay';
import Sidebar from './Sidebar';
import { SidebarPanelProvider, type SidebarPanelTarget } from './SidebarContext';
import {
  isPrimarySidebarPanelTarget,
  loadSidebarCollapsed,
  loadSidebarSecondaryExpanded,
  loadSidebarWidths,
  normalizeSidebarWidths,
  primaryTargetForPath,
  saveSidebarCollapsed,
  saveSidebarSecondaryExpanded,
  saveSidebarWidths,
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
  const [sidebarWidths, setSidebarWidths] = useState<SidebarWidths>(() => loadSidebarWidths());
  const [routeEntering, setRouteEntering] = useState(false);
  const routeTarget = primaryTargetForPath(effectivePathname);
  const [sidebarExpanded, setSidebarExpandedState] = useState<SidebarPanelTarget | null>(() =>
    routeTarget && loadSidebarSecondaryExpanded() ? routeTarget : null,
  );
  const viewerActive = overlay !== null || effectivePathname.startsWith('/viewer/');
  const sidebarPanelOpen = (sidebarExpanded !== null && sidebarExpanded === routeTarget) || viewerActive;
  const shellClass = [
    'app-shell',
    sidebarCollapsed ? 'sidebar-primary-collapsed' : 'sidebar-primary-open',
    sidebarCollapsed ? 'sidebar-primary-icon-only' : '',
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
    setRouteEntering(false);
    const frame = window.requestAnimationFrame(() => setRouteEntering(true));
    const timer = window.setTimeout(() => setRouteEntering(false), 180);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timer);
    };
  }, [effectivePathname]);

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
            expanded={sidebarExpanded}
            routePathname={effectivePathname}
            secondaryWidth={sidebarWidths.secondary}
            onTogglePrimary={togglePrimarySidebar}
            onToggleExpanded={setSidebarExpanded}
            onSecondaryWidthChange={(width) => updateSidebarWidth('secondary', width)}
          />
        </aside>
        <main className={routeEntering ? 'main-panel route-entering' : 'main-panel'}>{children}</main>
        {overlay && <div className="viewer-shell-overlay">{overlay}</div>}
        <DesignGridOverlay />
      </div>
    </SidebarPanelProvider>
  );
}
