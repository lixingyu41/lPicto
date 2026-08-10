import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';

export type SidebarPanelTarget = 'library' | 'recent' | 'albums' | 'folders' | 'collections' | 'viewer' | 'settings';

type SidebarPanels = Partial<Record<SidebarPanelTarget, ReactNode>>;

export interface SidebarReturnState {
  sidebarExpanded: SidebarPanelTarget | null;
}

interface SidebarPanelContextValue {
  panels: SidebarPanels;
  sidebarState: SidebarReturnState;
  viewerActive: boolean;
  viewerInfoVisible: boolean;
  setPanel: (target: SidebarPanelTarget, content: ReactNode | null) => void;
  setSidebarExpanded: (target: SidebarPanelTarget | null) => void;
  setViewerInfoVisible: (visible: boolean) => void;
}

const SidebarPanelContext = createContext<SidebarPanelContextValue | null>(null);

export function SidebarPanelProvider({
  children,
  sidebarExpanded,
  viewerActive,
  viewerInfoVisible,
  setSidebarExpanded,
  setViewerInfoVisible,
}: {
  children: ReactNode;
  sidebarExpanded: SidebarPanelTarget | null;
  viewerActive: boolean;
  viewerInfoVisible: boolean;
  setSidebarExpanded: (target: SidebarPanelTarget | null) => void;
  setViewerInfoVisible: (visible: boolean) => void;
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
    () => ({ sidebarExpanded }),
    [sidebarExpanded],
  );
  const value = useMemo(
    () => ({ panels, setPanel, setSidebarExpanded, setViewerInfoVisible, sidebarState, viewerActive, viewerInfoVisible }),
    [panels, setPanel, setSidebarExpanded, setViewerInfoVisible, sidebarState, viewerActive, viewerInfoVisible],
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

export function useViewerInfoPanel() {
  const context = useContext(SidebarPanelContext);
  if (!context) {
    throw new Error('useViewerInfoPanel must be used inside SidebarPanelProvider');
  }
  return {
    active: context.viewerActive,
    visible: context.viewerInfoVisible,
    setVisible: context.setViewerInfoVisible,
  };
}

export function useRestoreSidebarState() {
  const context = useContext(SidebarPanelContext);
  if (!context) {
    throw new Error('useRestoreSidebarState must be used inside SidebarPanelProvider');
  }
  const { setSidebarExpanded } = context;
  return useCallback(
    (state: Partial<SidebarReturnState>) => {
      if (state.sidebarExpanded === null || isSidebarPanelTarget(state.sidebarExpanded)) {
        setSidebarExpanded(state.sidebarExpanded);
      }
    },
    [setSidebarExpanded],
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
  }, [setPanel, target, ...deps]);
  useEffect(
    () => () => {
      setPanel(target, null);
    },
    [setPanel, target],
  );
}

function isSidebarPanelTarget(value: unknown): value is SidebarPanelTarget {
  return (
    value === 'library' ||
    value === 'recent' ||
    value === 'albums' ||
    value === 'folders' ||
    value === 'collections' ||
    value === 'viewer' ||
    value === 'settings'
  );
}
