import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';

export type SidebarPanelTarget = 'library' | 'albums' | 'folders' | 'viewer' | 'settings';

type SidebarPanels = Partial<Record<SidebarPanelTarget, ReactNode>>;

interface SidebarPanelContextValue {
  panels: SidebarPanels;
  setPanel: (target: SidebarPanelTarget, content: ReactNode | null) => void;
}

const SidebarPanelContext = createContext<SidebarPanelContextValue | null>(null);

export function SidebarPanelProvider({ children }: { children: ReactNode }) {
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
  const value = useMemo(() => ({ panels, setPanel }), [panels, setPanel]);
  return <SidebarPanelContext.Provider value={value}>{children}</SidebarPanelContext.Provider>;
}

export function useSidebarPanelValue() {
  const context = useContext(SidebarPanelContext);
  if (!context) {
    throw new Error('useSidebarPanelValue must be used inside SidebarPanelProvider');
  }
  return context.panels;
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
