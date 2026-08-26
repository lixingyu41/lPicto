import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { AssetKind, AssetRatingFilter, OrientationFilter, SortKey } from '../types/api';
import type { AssetGroupMode } from '../utils/assetGrouping';

export type SidebarPanelTarget = 'library' | 'recent' | 'albums' | 'folders' | 'collections' | 'viewer' | 'settings';

type SidebarPanels = Partial<Record<SidebarPanelTarget, ReactNode>>;

export interface SidebarReturnState {
  sidebarExpanded: SidebarPanelTarget | null;
}

export interface BrowseQueryChip {
  id: string;
  label: string;
  onRemove: () => void;
}

export type BrowsePanelMode = 'albums' | 'search' | 'filters';

export interface BrowseTools {
  albumFilterLabel?: string;
  albumFilterActive?: boolean;
  groupMode: AssetGroupMode;
  onGroupChange: (value: AssetGroupMode) => void;
  onOrientationChange: (value: OrientationFilter) => void;
  onRatingChange: (value: AssetRatingFilter) => void;
  onSortChange: (value: SortKey) => void;
  onTagFilterChange: (value: string[]) => void;
  onTypeChange: (value: AssetKind) => void;
  orientation: OrientationFilter;
  panelModes: BrowsePanelMode[];
  rating: AssetRatingFilter;
  sort: SortKey;
  tagFilters: string[];
  type: AssetKind;
}

interface SidebarPanelContextValue {
  panels: SidebarPanels;
  browseTools: Partial<Record<SidebarPanelTarget, BrowseTools>>;
  queryChips: Partial<Record<SidebarPanelTarget, BrowseQueryChip[]>>;
  scopeTitles: Partial<Record<SidebarPanelTarget, string>>;
  sidebarState: SidebarReturnState;
  viewerActive: boolean;
  viewerInfoVisible: boolean;
  setPanel: (target: SidebarPanelTarget, content: ReactNode | null) => void;
  setBrowseTools: (target: SidebarPanelTarget, tools: BrowseTools | null) => void;
  setQueryChips: (target: SidebarPanelTarget, chips: BrowseQueryChip[] | null) => void;
  setScopeTitle: (target: SidebarPanelTarget, title: string | null) => void;
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
  const [browseTools, setBrowseToolsState] = useState<Partial<Record<SidebarPanelTarget, BrowseTools>>>({});
  const [queryChips, setQueryChipsState] = useState<Partial<Record<SidebarPanelTarget, BrowseQueryChip[]>>>({});
  const [scopeTitles, setScopeTitles] = useState<Partial<Record<SidebarPanelTarget, string>>>({});
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
  const setBrowseTools = useCallback((target: SidebarPanelTarget, tools: BrowseTools | null) => {
    setBrowseToolsState((current) => {
      const next = { ...current };
      if (tools === null) delete next[target];
      else next[target] = tools;
      return next;
    });
  }, []);
  const setQueryChips = useCallback((target: SidebarPanelTarget, chips: BrowseQueryChip[] | null) => {
    setQueryChipsState((current) => {
      const next = { ...current };
      if (chips === null) delete next[target];
      else next[target] = chips;
      return next;
    });
  }, []);
  const setScopeTitle = useCallback((target: SidebarPanelTarget, title: string | null) => {
    setScopeTitles((current) => {
      const next = { ...current };
      if (title === null) delete next[target];
      else next[target] = title;
      return next;
    });
  }, []);
  const sidebarState = useMemo(
    () => ({ sidebarExpanded }),
    [sidebarExpanded],
  );
  const value = useMemo(
    () => ({ panels, browseTools, queryChips, scopeTitles, setBrowseTools, setPanel, setQueryChips, setScopeTitle, setSidebarExpanded, setViewerInfoVisible, sidebarState, viewerActive, viewerInfoVisible }),
    [panels, browseTools, queryChips, scopeTitles, setBrowseTools, setPanel, setQueryChips, setScopeTitle, setSidebarExpanded, setViewerInfoVisible, sidebarState, viewerActive, viewerInfoVisible],
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

export function useSidebarBrowseToolsValue() {
  const context = useContext(SidebarPanelContext);
  if (!context) throw new Error('useSidebarBrowseToolsValue must be used inside SidebarPanelProvider');
  return context.browseTools;
}

export function useSidebarScopeTitles() {
  const context = useContext(SidebarPanelContext);
  if (!context) throw new Error('useSidebarScopeTitles must be used inside SidebarPanelProvider');
  return context.scopeTitles;
}

export function useSidebarQueryChipsValue() {
  const context = useContext(SidebarPanelContext);
  if (!context) throw new Error('useSidebarQueryChipsValue must be used inside SidebarPanelProvider');
  return context.queryChips;
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

export function useSidebarScopeTitle(target: SidebarPanelTarget, title: string, deps: readonly unknown[]) {
  const context = useContext(SidebarPanelContext);
  if (!context) throw new Error('useSidebarScopeTitle must be used inside SidebarPanelProvider');
  const { setScopeTitle } = context;
  useEffect(() => {
    setScopeTitle(target, title);
  }, [setScopeTitle, target, title, ...deps]);
  useEffect(() => () => setScopeTitle(target, null), [setScopeTitle, target]);
}

export function useSidebarQueryChips(target: SidebarPanelTarget, chips: BrowseQueryChip[], deps: readonly unknown[]) {
  const context = useContext(SidebarPanelContext);
  if (!context) throw new Error('useSidebarQueryChips must be used inside SidebarPanelProvider');
  const { setQueryChips } = context;
  useEffect(() => setQueryChips(target, chips), [setQueryChips, target, ...deps]);
  useEffect(() => () => setQueryChips(target, null), [setQueryChips, target]);
}

export function useSidebarBrowseTools(target: SidebarPanelTarget, tools: BrowseTools, deps: readonly unknown[]) {
  const context = useContext(SidebarPanelContext);
  if (!context) throw new Error('useSidebarBrowseTools must be used inside SidebarPanelProvider');
  const { setBrowseTools } = context;
  useEffect(() => setBrowseTools(target, tools), [setBrowseTools, target, ...deps]);
  useEffect(() => () => setBrowseTools(target, null), [setBrowseTools, target]);
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
