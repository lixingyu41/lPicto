import { useCallback, useEffect, useState, type CSSProperties, type ReactNode } from 'react';
import { useLocation } from 'react-router-dom';
import Sidebar from './Sidebar';
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
}

export default function Layout({ children }: Props) {
  const location = useLocation();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [sidebarWidths, setSidebarWidths] = useState<SidebarWidths>(() => loadSidebarWidths());
  const routeTarget = primaryTargetForPath(location.pathname);
  const [sidebarExpanded, setSidebarExpandedState] = useState<SidebarPanelTarget | null>(() =>
    routeTarget && loadSidebarSecondaryExpanded(routeTarget) ? routeTarget : null,
  );
  const sidebarPanelOpen = sidebarExpanded !== null && sidebarExpanded === routeTarget;
  const shellClass = sidebarCollapsed ? 'app-shell sidebar-collapsed' : sidebarPanelOpen ? 'app-shell sidebar-panel-open' : 'app-shell';
  const setSidebarExpanded = useCallback(
    (target: SidebarPanelTarget | null) => {
      if (target === null) {
        if (routeTarget) {
          saveSidebarSecondaryExpanded(routeTarget, false);
        }
        setSidebarExpandedState(null);
        return;
      }
      if (isPrimarySidebarPanelTarget(target)) {
        saveSidebarSecondaryExpanded(target, true);
      }
      setSidebarExpandedState(target);
    },
    [routeTarget],
  );

  useEffect(() => {
    setSidebarExpandedState(routeTarget && loadSidebarSecondaryExpanded(routeTarget) ? routeTarget : null);
  }, [routeTarget]);

  const updateSidebarWidth = useCallback((kind: keyof SidebarWidths, width: number) => {
    setSidebarWidths((current) => {
      const next = normalizeSidebarWidths({ ...current, [kind]: width });
      saveSidebarWidths(next);
      return next;
    });
  }, []);

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
        <aside className={sidebarCollapsed ? 'sidebar is-collapsed' : 'sidebar'}>
          <Sidebar
            collapsed={sidebarCollapsed}
            expanded={sidebarExpanded}
            primaryWidth={sidebarWidths.primary}
            secondaryWidth={sidebarWidths.secondary}
            onToggleCollapsed={() => setSidebarCollapsed((value) => !value)}
            onToggleExpanded={setSidebarExpanded}
            onPrimaryWidthChange={(width) => updateSidebarWidth('primary', width)}
            onSecondaryWidthChange={(width) => updateSidebarWidth('secondary', width)}
          />
        </aside>
        <main className="main-panel">{children}</main>
      </div>
    </SidebarPanelProvider>
  );
}
