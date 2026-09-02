import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { useLocation, type Location } from 'react-router-dom';
import ImmersiveTopbar from './ImmersiveTopbar';
import { api } from '../api/client';
import type { StorageStatus } from '../types/api';
import { SidebarPanelProvider, type SidebarPanelTarget } from './SidebarContext';
import {
  isPrimarySidebarPanelTarget,
  primaryTargetForPath,
} from '../utils/sidebarPrefs';
import {
  immersiveChromePinnedChanged,
  isImmersiveChromePinnedStorageEvent,
  loadImmersiveChromePinned,
  saveImmersiveChromePinned,
} from '../utils/immersiveChromePrefs';

interface Props {
  children: ReactNode;
  overlay?: ReactNode;
  routeLocation?: Location;
}

export default function Layout({ children, overlay = null, routeLocation }: Props) {
  const location = useLocation();
  const effectivePathname = routeLocation?.pathname ?? location.pathname;
  const [routeEntering, setRouteEntering] = useState(false);
  const [storageStatus, setStorageStatus] = useState<StorageStatus | null>(null);
  const [viewerInfoVisible, setViewerInfoVisible] = useState(true);
  const [topbarPinned, setTopbarPinnedState] = useState(() => loadImmersiveChromePinned());
  const [pinnedMenuOpen, setPinnedMenuOpen] = useState(false);
  const viewerActive = Boolean(overlay) || location.pathname.startsWith('/viewer/');
  const routeTarget = primaryTargetForPath(effectivePathname);
  const [sidebarExpanded, setSidebarExpandedState] = useState<SidebarPanelTarget | null>(null);
  const sidebarPanelOpen = sidebarExpanded !== null && sidebarExpanded === routeTarget;
  const shellClass = ['app-shell', routeTarget === 'settings' ? 'standard-shell' : 'media-browse-shell', sidebarPanelOpen ? 'browse-panel-open' : 'browse-panel-closed', viewerActive ? 'viewer-active' : '', topbarPinned ? 'topbar-pinned' : '', pinnedMenuOpen ? 'pinned-menu-open' : '']
    .filter(Boolean)
    .join(' ');
  const setSidebarExpanded = useCallback(
    (target: SidebarPanelTarget | null) => {
      if (target === null) {
        setSidebarExpandedState(null);
        return;
      }
      if (!isPrimarySidebarPanelTarget(target) && target !== 'viewer') return;
      setSidebarExpandedState(target);
    },
    [],
  );
  const setTopbarPinned = useCallback((pinned: boolean) => {
    setTopbarPinnedState(pinned);
    saveImmersiveChromePinned(pinned);
  }, []);

  useEffect(() => {
    const syncPinnedState = () => setTopbarPinnedState(loadImmersiveChromePinned());
    const handleStorage = (event: StorageEvent) => {
      if (isImmersiveChromePinnedStorageEvent(event)) syncPinnedState();
    };
    window.addEventListener(immersiveChromePinnedChanged, syncPinnedState);
    window.addEventListener('storage', handleStorage);
    return () => {
      window.removeEventListener(immersiveChromePinnedChanged, syncPinnedState);
      window.removeEventListener('storage', handleStorage);
    };
  }, []);
  useEffect(() => {
    setSidebarExpandedState(null);
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

  return (
    <SidebarPanelProvider
      sidebarExpanded={sidebarExpanded}
      viewerActive={viewerActive}
      viewerInfoVisible={viewerInfoVisible}
      setSidebarExpanded={setSidebarExpanded}
      setViewerInfoVisible={setViewerInfoVisible}
    >
      <div className={shellClass}>
        <ImmersiveTopbar
          expanded={sidebarExpanded}
          pinned={topbarPinned}
          routePathname={effectivePathname}
          onPinnedChange={setTopbarPinned}
          onPinnedMenuOpenChange={setPinnedMenuOpen}
          onToggleExpanded={setSidebarExpanded}
        />
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
