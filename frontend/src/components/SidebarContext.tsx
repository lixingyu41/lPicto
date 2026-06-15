import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';

export type SidebarPanelTarget = 'library' | 'search' | 'albums' | 'folders' | 'viewer' | 'settings';

type SidebarPanels = Partial<Record<SidebarPanelTarget, ReactNode>>;

export interface SidebarReturnState {
  sidebarCollapsed: boolean;
  sidebarExpanded: SidebarPanelTarget | null;
}

interface SidebarPanelContextValue {
  panels: SidebarPanels;
  sidebarState: SidebarReturnState;
  setPanel: (target: SidebarPanelTarget, content: ReactNode | null) => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setSidebarExpanded: (target: SidebarPanelTarget | null) => void;
}

const SidebarPanelContext = createContext<SidebarPanelContextValue | null>(null);

export function SidebarPanelProvider({
  children,
  sidebarCollapsed,
  sidebarExpanded,
  setSidebarCollapsed,
  setSidebarExpanded,
}: {
  children: ReactNode;
  sidebarCollapsed: boolean;
  sidebarExpanded: SidebarPanelTarget | null;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setSidebarExpanded: (target: SidebarPanelTarget | null) => void;
}) {
  const [panels, setPanels] = useState<SidebarPanels>({});
  const setPanel = useCallback((target: SidebarPanelTarget, content: ReactNode | null) => {
    setPanels((current) => {
      const next = { ...current };
      if (content === null) {
        delete next[target];
      } else {
        next[target] = content;
      }
      return next;
    });
  }, []);
  const sidebarState = useMemo(
    () => ({ sidebarCollapsed, sidebarExpanded }),
    [sidebarCollapsed, sidebarExpanded],
  );
  const value = useMemo(
    () => ({ panels, setPanel, setSidebarCollapsed, setSidebarExpanded, sidebarState }),
    [panels, setPanel, setSidebarCollapsed, setSidebarExpanded, sidebarState],
  );
  return <SidebarPanelContext.Provider value={value}>{children}</SidebarPanelContext.Provider>;
}

export function useSidebarPanelValue() {
  const context = useContext(SidebarPanelContext);
  if (!context) {
    throw new Error('useSidebarPanelValue must be used inside SidebarPanelProvider');
  }
  return context.panels;
}

export function useSidebarReturnState() {
  const context = useContext(SidebarPanelContext);
  if (!context) {
    throw new Error('useSidebarReturnState must be used inside SidebarPanelProvider');
  }
  return context.sidebarState;
}

export function useRestoreSidebarState() {
  const context = useContext(SidebarPanelContext);
  if (!context) {
    throw new Error('useRestoreSidebarState must be used inside SidebarPanelProvider');
  }
  const { setSidebarCollapsed, setSidebarExpanded } = context;
  return useCallback(
    (state: Partial<SidebarReturnState>) => {
      if (typeof state.sidebarCollapsed === 'boolean') {
        setSidebarCollapsed(state.sidebarCollapsed);
      }
      if (state.sidebarExpanded === null || isSidebarPanelTarget(state.sidebarExpanded)) {
        setSidebarExpanded(state.sidebarExpanded);
      }
    },
    [setSidebarCollapsed, setSidebarExpanded],
  );
}

export function useSidebarPanel(target: SidebarPanelTarget, content: ReactNode, deps: readonly unknown[]) {
  const context = useContext(SidebarPanelContext);
  if (!context) {
    throw new Error('useSidebarPanel must be used inside SidebarPanelProvider');
  }
  const { setPanel } = context;
  useEffect(() => {
    setPanel(target, content);
    return () => {
      setPanel(target, null);
    };
  }, [setPanel, target, ...deps]);
}

function isSidebarPanelTarget(value: unknown): value is SidebarPanelTarget {
  return value === 'library' || value === 'search' || value === 'albums' || value === 'folders' || value === 'viewer' || value === 'settings';
}
