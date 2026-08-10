import { useCallback, useEffect, useState, type CSSProperties, type ReactNode } from 'react';
import { useLocation, type Location } from 'react-router-dom';
import Sidebar from './Sidebar';
import { api } from '../api/client';
import type { StorageStatus } from '../types/api';
import { SidebarPanelProvider, type SidebarPanelTarget } from './SidebarContext';
import {
  isPrimarySidebarPanelTarget,
  loadSidebarSecondaryExpanded,
  loadSidebarWidths,
  normalizeSidebarWidths,
  primaryTargetForPath,
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
  const [sidebarWidths, setSidebarWidths] = useState<SidebarWidths>(() => loadSidebarWidths());
  const [routeEntering, setRouteEntering] = useState(false);
  const [storageStatus, setStorageStatus] = useState<StorageStatus | null>(null);
  const [viewerInfoVisible, setViewerInfoVisible] = useState(true);
  const viewerActive = Boolean(overlay) || location.pathname.startsWith('/viewer/');
  const routeTarget = primaryTargetForPath(effectivePathname);
  const [sidebarExpanded, setSidebarExpandedState] = useState<SidebarPanelTarget | null>(() =>
    routeTarget && loadSidebarSecondaryExpanded() ? routeTarget : null,
  );
  const sidebarPanelOpen = sidebarExpanded !== null && sidebarExpanded === routeTarget;
  const shellClass = [
    'app-shell',
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

  const shellStyle = {
    '--sidebar-secondary-width': `${sidebarWidths.secondary}px`,
  } as CSSProperties;

  return (
    <SidebarPanelProvider
      sidebarExpanded={sidebarExpanded}
      viewerActive={viewerActive}
      viewerInfoVisible={viewerInfoVisible}
      setSidebarExpanded={setSidebarExpanded}
      setViewerInfoVisible={setViewerInfoVisible}
    >
      <div className={shellClass} style={shellStyle}>
        <aside className="sidebar">
          <Sidebar
            expanded={sidebarExpanded}
            routePathname={effectivePathname}
            secondaryWidth={sidebarWidths.secondary}
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
