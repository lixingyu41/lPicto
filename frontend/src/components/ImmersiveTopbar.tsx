import {
  Activity,
  ArrowDownAZ,
  ArrowUpAZ,
  CalendarDays,
  CalendarRange,
  CalendarSync,
  CircleX,
  Database,
  EyeOff,
  FileClock,
  FileText,
  FolderTree,
  FolderInput,
  Gauge,
  History,
  Image as ImageIcon,
  Images,
  Layers3,
  LayoutGrid,
  LetterText,
  Library,
  ListChecks,
  Menu,
  MonitorSmartphone,
  Music,
  PanelsTopLeft,
  Pin,
  PinOff,
  PanelRightClose,
  PanelRightOpen,
  RectangleHorizontal,
  RectangleVertical,
  RotateCw,
  ScanLine,
  Search,
  Settings,
  Star,
  StarOff,
  Sparkles,
  Tags,
  Timer,
  Trash2,
  Video,
  X,
  type LucideIcon,
} from 'lucide-react';
import { isViewerMediaZoomActive } from '../utils/viewerInteractionState';
import { useCallback, useEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { NavLink } from 'react-router-dom';
import { useSidebarBrowseToolsValue, useSidebarPanelValue, useSidebarQueryChipsValue, useSidebarScopeTitles, useViewerInfoPanel, type BrowseTools, type SidebarPanelTarget } from './SidebarContext';
import { primaryTargetForPath, type PrimarySidebarPanelTarget } from '../utils/sidebarPrefs';
import { loadSettingsSection, settingsSectionPath } from '../utils/settingsRoute';
import {
  assetGridBatchStateEvent,
  assetGridBatchStateRequestEvent,
  dispatchAssetGridBatchCommand,
  type AssetGridBatchCommand,
  type AssetGridBatchState,
} from '../utils/batchSelection';
import {
  loadMediaViewPreferences,
  mediaLayoutDefinitions,
  mediaViewPreferencesChanged,
  saveMediaViewPreferences,
  type MediaViewMode,
  type MediaViewPreferences,
} from '../utils/mediaViewPrefs';
import { sortKeyFromParts, sortPartsFromKey } from './SortControls';
import {
  immersiveChromeSizeChanged,
  loadImmersiveChromeSize,
  loadPinnedMenu,
  loadPinnedMenuWidth,
  maxPinnedMenuWidth,
  minPinnedMenuWidth,
  savePinnedMenu,
  savePinnedMenuWidth,
  type ImmersiveChromeSize,
  type ImmersivePinnedMenu,
} from '../utils/immersiveChromePrefs';
import type { AssetGroupMode } from '../utils/assetGrouping';
import type { AssetKind, AssetRatingFilter, OrientationFilter, SortField } from '../types/api';
import HierarchicalTagPicker from './HierarchicalTagPicker';

interface Props {
  expanded: SidebarPanelTarget | null;
  pinned: boolean;
  routePathname: string;
  onPinnedChange: (pinned: boolean) => void;
  onPinnedMenuOpenChange: (open: boolean) => void;
  onToggleExpanded: (target: SidebarPanelTarget | null) => void;
}

const navItems: Array<{ Icon: LucideIcon; label: string; target: PrimarySidebarPanelTarget; to: string }> = [
  { Icon: Library, label: '图库', target: 'library', to: '/library' },
  { Icon: History, label: '最近播放', target: 'recent', to: '/recent' },
  { Icon: Images, label: '相册', target: 'albums', to: '/albums' },
  { Icon: FolderTree, label: '文件夹', target: 'folders', to: '/folders' },
  { Icon: Layers3, label: '智能', target: 'collections', to: '/collections' },
  { Icon: Settings, label: '设置', target: 'settings', to: '/settings' },
];

const emptyBatchState: AssetGridBatchState = {
  available: false,
  busy: false,
  canAutoSelect: false,
  message: '',
  progress: null,
  selectedCount: 0,
  selectionMode: false,
};

type PanelMode = 'scope' | 'albums' | 'query';
type ToolMenu = 'tags' | 'type' | 'orientation' | 'rating' | 'sort' | 'group' | 'layout';
type PinnedMenu = ImmersivePinnedMenu;
const hoverIntentDelay = 120;
const menuDismissDelay = 500;

export default function ImmersiveTopbar({ expanded, pinned: chromePinned, routePathname, onPinnedChange, onPinnedMenuOpenChange, onToggleExpanded }: Props) {
  const panels = useSidebarPanelValue();
  const browseToolsByTarget = useSidebarBrowseToolsValue();
  const queryChipsByTarget = useSidebarQueryChipsValue();
  const scopeTitles = useSidebarScopeTitles();
  const viewerInfo = useViewerInfoPanel();
  const routeTarget = primaryTargetForPath(routePathname);
  const mediaRoute = routeTarget !== 'settings' && routeTarget !== null;
  const panelOpen = routeTarget !== null && expanded === routeTarget;
  const panel = routeTarget ? panels[routeTarget] : null;
  const browseTools = routeTarget ? browseToolsByTarget[routeTarget] : undefined;
  const fallbackScope = navItems.find((item) => item.target === routeTarget)?.label ?? 'lPicto';
  const scopeTitle = routeTarget ? scopeTitles[routeTarget] ?? fallbackScope : fallbackScope;
  const [navOpen, setNavOpen] = useState(false);
  const [panelMode, setPanelMode] = useState<PanelMode>('scope');
  const [toolMenu, setToolMenu] = useState<ToolMenu | null>(null);
  const [toolAnchorLeft, setToolAnchorLeft] = useState<number | null>(null);
  const [visible, setVisible] = useState(true);
  const [interactionPinned, setInteractionPinned] = useState(false);
  const [batchState, setBatchState] = useState<AssetGridBatchState>(emptyBatchState);
  const [viewPreferences, setViewPreferences] = useState<MediaViewPreferences>(() => loadMediaViewPreferences());
  const [chromeSize, setChromeSize] = useState<ImmersiveChromeSize>(() => loadImmersiveChromeSize());
  const [pinnedMenu, setPinnedMenu] = useState<PinnedMenu | null>(() => loadPinnedMenu());
  const [pinnedMenuWidth, setPinnedMenuWidth] = useState(() => loadPinnedMenuWidth());
  const hideTimer = useRef<number | null>(null);
  const dismissTimer = useRef<number | null>(null);
  const revealTimer = useRef<number | null>(null);
  const menuHoverTimer = useRef<number | null>(null);
  const chromeRef = useRef<HTMLDivElement | null>(null);
  const mobileTriggerRef = useRef<HTMLButtonElement | null>(null);
  const interactionOpen = chromePinned || interactionPinned || navOpen || toolMenu !== null || panelOpen || (mediaRoute && batchState.selectionMode);
  const queryChips = routeTarget ? queryChipsByTarget[routeTarget] ?? [] : [];
  const queryCount = queryChips.length;
  const scopeDetail = scopeTitle.includes('/') ? scopeTitle.slice(scopeTitle.lastIndexOf('/') + 1).trim() : undefined;
  const revealZoneHeight = [48, 54, 58, 64, 70][chromeSize - 1] ?? 54;
  const pinnedNavigation = pinnedMenu?.kind === 'navigation';
  const pinnedPanel = pinnedMenu?.kind === 'panel' && pinnedMenu.target === routeTarget ? pinnedMenu : null;
  const pinnedTool = pinnedMenu?.kind === 'tool' && pinnedMenu.target === routeTarget ? pinnedMenu : null;
  const pinnedMenuActive = pinnedNavigation || pinnedPanel !== null || pinnedTool !== null;

  const cancelHide = useCallback(() => {
    if (hideTimer.current === null) return;
    window.clearTimeout(hideTimer.current);
    hideTimer.current = null;
  }, []);
  const cancelDismiss = useCallback(() => {
    if (dismissTimer.current === null) return;
    window.clearTimeout(dismissTimer.current);
    dismissTimer.current = null;
  }, []);
  const cancelReveal = useCallback(() => {
    if (revealTimer.current === null) return;
    window.clearTimeout(revealTimer.current);
    revealTimer.current = null;
  }, []);
  const cancelMenuHover = useCallback(() => {
    if (menuHoverTimer.current === null) return;
    window.clearTimeout(menuHoverTimer.current);
    menuHoverTimer.current = null;
  }, []);
  const show = useCallback(() => {
    if (isViewerMediaZoomActive()) return;
    cancelHide();
    cancelReveal();
    setVisible(true);
  }, [cancelHide, cancelReveal]);
  const scheduleReveal = useCallback(() => {
    if (isViewerMediaZoomActive()) {
      cancelReveal();
      return;
    }
    if (visible || revealTimer.current !== null) return;
    revealTimer.current = window.setTimeout(() => {
      revealTimer.current = null;
      show();
    }, hoverIntentDelay);
  }, [show, visible]);
  const scheduleMenuHover = useCallback((action: () => void) => {
    if (isViewerMediaZoomActive()) {
      cancelMenuHover();
      return;
    }
    if (interactionPinned) return;
    cancelMenuHover();
    menuHoverTimer.current = window.setTimeout(() => {
      menuHoverTimer.current = null;
      action();
    }, hoverIntentDelay);
  }, [cancelMenuHover, interactionPinned]);
  const scheduleHide = useCallback((delay = menuDismissDelay) => {
    cancelHide();
    if (!mediaRoute || interactionOpen) return;
    hideTimer.current = window.setTimeout(() => {
      hideTimer.current = null;
      setVisible(false);
    }, delay);
  }, [cancelHide, interactionOpen, mediaRoute]);
  const scheduleDismiss = useCallback((delay = menuDismissDelay) => {
    cancelDismiss();
    if (interactionPinned) return;
    dismissTimer.current = window.setTimeout(() => {
      dismissTimer.current = null;
      const activeElement = document.activeElement;
      if (activeElement && chromeRef.current?.contains(activeElement) && isEditableTarget(activeElement)) return;
      setNavOpen(false);
      setToolMenu(null);
      onToggleExpanded(null);
      if (mediaRoute && !chromePinned) setVisible(false);
    }, delay);
  }, [cancelDismiss, chromePinned, interactionPinned, mediaRoute, onToggleExpanded]);

  useEffect(() => {
    if (interactionOpen || !mediaRoute) show();
    else scheduleHide(1200);
  }, [interactionOpen, mediaRoute, scheduleHide, show]);
  useEffect(() => cancelHide, [cancelHide]);
  useEffect(() => cancelDismiss, [cancelDismiss]);
  useEffect(() => cancelReveal, [cancelReveal]);
  useEffect(() => cancelMenuHover, [cancelMenuHover]);
  useEffect(() => {
    setNavOpen(false);
    setToolMenu(null);
    setInteractionPinned(false);
    onToggleExpanded(null);
    setVisible(true);
  }, [routePathname]);
  useEffect(() => onPinnedMenuOpenChange(pinnedMenuActive), [onPinnedMenuOpenChange, pinnedMenuActive]);
  useEffect(() => () => onPinnedMenuOpenChange(false), [onPinnedMenuOpenChange]);
  useEffect(() => {
    const handlePointerMove = (event: PointerEvent) => {
      if (isViewerMediaZoomActive()) {
        cancelReveal();
        cancelMenuHover();
        cancelDismiss();
        setNavOpen(false);
        setToolMenu(null);
        onToggleExpanded(null);
        if (mediaRoute) setVisible(false);
        return;
      }
      if (chromeRef.current?.contains(event.target as Node)) {
        cancelReveal();
        return;
      }
      if (event.clientY <= revealZoneHeight) scheduleReveal();
      else {
        cancelReveal();
        if (visible) scheduleHide();
      }
    };
    const handleScroll = () => scheduleHide(280);
    const handlePointerDown = (event: PointerEvent) => {
      if (chromeRef.current?.contains(event.target as Node) || mobileTriggerRef.current?.contains(event.target as Node)) return;
      cancelDismiss();
      cancelMenuHover();
      setInteractionPinned(false);
      setNavOpen(false);
      setToolMenu(null);
      onToggleExpanded(null);
      if (mediaRoute && !batchState.selectionMode && !chromePinned) setVisible(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === '/' && !isEditableTarget(event.target)) {
        event.preventDefault();
        const queryButton = chromeRef.current?.querySelector<HTMLButtonElement>('[data-topbar-query]');
        if (queryButton) {
          setInteractionPinned(false);
          showPanel('query', queryButton);
          window.setTimeout(() => focusPanelControl('query'), 30);
        }
      }
      if (event.key === 'Escape') {
        setNavOpen(false);
        setToolMenu(null);
        setInteractionPinned(false);
        onToggleExpanded(null);
        scheduleHide(210);
      }
    };
    window.addEventListener('pointermove', handlePointerMove, true);
    window.addEventListener('pointerdown', handlePointerDown, true);
    window.addEventListener('scroll', handleScroll, true);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('pointermove', handlePointerMove, true);
      window.removeEventListener('pointerdown', handlePointerDown, true);
      window.removeEventListener('scroll', handleScroll, true);
      window.removeEventListener('keydown', handleKeyDown);
    };
  });
  useEffect(() => {
    const handleBatchState = (event: Event) => setBatchState((event as CustomEvent<AssetGridBatchState>).detail ?? emptyBatchState);
    window.addEventListener(assetGridBatchStateEvent, handleBatchState);
    window.dispatchEvent(new Event(assetGridBatchStateRequestEvent));
    return () => window.removeEventListener(assetGridBatchStateEvent, handleBatchState);
  }, [routePathname]);
  useEffect(() => {
    const handlePreferences = (event: Event) => {
      setViewPreferences((event as CustomEvent<MediaViewPreferences>).detail ?? loadMediaViewPreferences());
    };
    window.addEventListener(mediaViewPreferencesChanged, handlePreferences);
    return () => window.removeEventListener(mediaViewPreferencesChanged, handlePreferences);
  }, []);
  useEffect(() => {
    const handleChromeSize = (event: Event) => setChromeSize((event as CustomEvent<ImmersiveChromeSize>).detail ?? loadImmersiveChromeSize());
    window.addEventListener(immersiveChromeSizeChanged, handleChromeSize);
    return () => window.removeEventListener(immersiveChromeSizeChanged, handleChromeSize);
  }, []);

  const setAnchorForTrigger = useCallback((trigger: HTMLButtonElement, width: number) => {
    const triggerRect = trigger.getBoundingClientRect();
    const chromeRect = chromeRef.current?.getBoundingClientRect();
    const halfWidth = width / 2;
    const viewportCenter = Math.min(window.innerWidth - halfWidth - 8, Math.max(halfWidth + 8, triggerRect.left + triggerRect.width / 2));
    setToolAnchorLeft(viewportCenter - (chromeRect?.left ?? 0));
  }, []);

  const showPanel = useCallback((focus: PanelMode, trigger: HTMLButtonElement) => {
    if (isViewerMediaZoomActive()) return;
    if (!routeTarget || !panel) return;
    cancelDismiss();
    show();
    setNavOpen(false);
    setToolMenu(null);
    setAnchorForTrigger(trigger, Math.min(560, window.innerWidth - 24));
    setPanelMode(focus);
    onToggleExpanded(routeTarget);
  }, [cancelDismiss, onToggleExpanded, panel, routeTarget, setAnchorForTrigger, show]);

  const togglePanel = useCallback((focus: PanelMode, trigger: HTMLButtonElement) => {
    if (panelOpen && panelMode === focus) {
      setInteractionPinned(false);
      onToggleExpanded(null);
      return;
    }
    showPanel(focus, trigger);
    setInteractionPinned(true);
  }, [onToggleExpanded, panelMode, panelOpen, showPanel]);

  const showToolMenu = useCallback((menu: ToolMenu, trigger: HTMLButtonElement) => {
    if (isViewerMediaZoomActive()) return;
    cancelDismiss();
    show();
    setNavOpen(false);
    onToggleExpanded(null);
    const sizeIndex = chromeSize - 1;
    const menuWidth = menu === 'tags'
      ? Math.min(620, window.innerWidth - 24)
      : menu === 'layout'
        ? [240, 260, 280, 300, 320][sizeIndex]
        : [210, 230, 250, 270, 290][sizeIndex];
    setAnchorForTrigger(trigger, menuWidth);
    setToolMenu(menu);
  }, [cancelDismiss, chromeSize, onToggleExpanded, setAnchorForTrigger, show]);

  const toggleToolMenu = useCallback((menu: ToolMenu, trigger: HTMLButtonElement) => {
    if (toolMenu === menu) {
      setInteractionPinned(false);
      setToolMenu(null);
      return;
    }
    showToolMenu(menu, trigger);
    setInteractionPinned(true);
  }, [showToolMenu, toolMenu]);

  const showNavigation = useCallback((trigger: HTMLButtonElement) => {
    if (isViewerMediaZoomActive()) return;
    cancelDismiss();
    show();
    setToolMenu(null);
    onToggleExpanded(null);
    setAnchorForTrigger(trigger, 210);
    setNavOpen(true);
  }, [cancelDismiss, onToggleExpanded, setAnchorForTrigger, show]);

  const closeOpenMenu = useCallback(() => {
    cancelDismiss();
    cancelMenuHover();
    setInteractionPinned(false);
    setNavOpen(false);
    setToolMenu(null);
    onToggleExpanded(null);
  }, [cancelDismiss, cancelMenuHover, onToggleExpanded]);

  const previewPanel = useCallback((focus: PanelMode, trigger: HTMLButtonElement) => {
    scheduleMenuHover(() => showPanel(focus, trigger));
  }, [scheduleMenuHover, showPanel]);
  const previewToolMenu = useCallback((menu: ToolMenu, trigger: HTMLButtonElement) => {
    scheduleMenuHover(() => showToolMenu(menu, trigger));
  }, [scheduleMenuHover, showToolMenu]);
  const previewNavigation = useCallback((trigger: HTMLButtonElement) => {
    scheduleMenuHover(() => showNavigation(trigger));
  }, [scheduleMenuHover, showNavigation]);
  const previewCloseOpenMenu = useCallback(() => {
    scheduleMenuHover(closeOpenMenu);
  }, [closeOpenMenu, scheduleMenuHover]);

  const toggleChromePinned = useCallback((trigger: HTMLButtonElement) => {
    cancelDismiss();
    cancelHide();
    cancelMenuHover();
    cancelReveal();
    if (chromePinned) {
      onPinnedChange(false);
      setInteractionPinned(false);
      setNavOpen(false);
      setToolMenu(null);
      onToggleExpanded(null);
      return;
    }
    onPinnedChange(true);
    setInteractionPinned(false);
    showNavigation(trigger);
  }, [cancelDismiss, cancelHide, cancelMenuHover, cancelReveal, chromePinned, onPinnedChange, onToggleExpanded, showNavigation]);

  const toggleMobileChrome = useCallback(() => {
    cancelDismiss();
    cancelHide();
    cancelMenuHover();
    cancelReveal();
    if (chromePinned || interactionPinned) {
      onPinnedChange(false);
      setInteractionPinned(false);
      setNavOpen(false);
      setToolMenu(null);
      onToggleExpanded(null);
      if (mediaRoute && !batchState.selectionMode) setVisible(false);
      return;
    }
    setInteractionPinned(true);
    setVisible(true);
  }, [batchState.selectionMode, cancelDismiss, cancelHide, cancelMenuHover, cancelReveal, chromePinned, interactionPinned, mediaRoute, onPinnedChange, onToggleExpanded]);

  const selectLayout = useCallback((mode: MediaViewMode) => {
    const next = { ...viewPreferences, mode };
    setViewPreferences(next);
    saveMediaViewPreferences(next);
  }, [viewPreferences]);

  const togglePinnedMenu = useCallback((next: PinnedMenu) => {
    setPinnedMenu((current) => {
      const updated = samePinnedMenu(current, next) ? null : next;
      savePinnedMenu(updated);
      return updated;
    });
    setInteractionPinned(false);
    setNavOpen(false);
    setToolMenu(null);
    onToggleExpanded(null);
  }, [onToggleExpanded]);

  const startPinnedMenuResize = useCallback((event: ReactPointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture?.(event.pointerId);
    const startX = event.clientX;
    const startWidth = pinnedMenuWidth;
    document.body.classList.add('immersive-pinned-menu-resizing');
    const handleMove = (moveEvent: PointerEvent) => {
      const viewportMaximum = Math.max(minPinnedMenuWidth, Math.min(maxPinnedMenuWidth, window.innerWidth - 320));
      setPinnedMenuWidth(Math.min(viewportMaximum, Math.max(minPinnedMenuWidth, startWidth + moveEvent.clientX - startX)));
    };
    const finish = () => {
      window.removeEventListener('pointermove', handleMove);
      window.removeEventListener('pointerup', finish);
      window.removeEventListener('pointercancel', finish);
      document.body.classList.remove('immersive-pinned-menu-resizing');
      setPinnedMenuWidth((current) => savePinnedMenuWidth(current));
    };
    window.addEventListener('pointermove', handleMove);
    window.addEventListener('pointerup', finish);
    window.addEventListener('pointercancel', finish);
  }, [pinnedMenuWidth]);

  const renderNavigationMenu = (isPinned: boolean) => (
    <nav className={`immersive-popover immersive-nav-popover${isPinned ? ' immersive-pinned-card' : ''}`} aria-label="主菜单" style={isPinned || toolAnchorLeft === null ? undefined : { left: toolAnchorLeft, right: 'auto' }}>
      <MenuCardHeading label="页面切换" pinned={isPinned} onPin={() => togglePinnedMenu({ kind: 'navigation' })} />
      {navItems.map(({ Icon, label, target, to }) => (
        <NavLink
          className={routeTarget === target ? 'active' : ''}
          key={target}
          to={target === 'settings' ? settingsSectionPath(loadSettingsSection()) : to}
          onClick={(event) => {
            setInteractionPinned(false);
            if (routeTarget === target) {
              event.preventDefault();
              if (!isPinned) setNavOpen(false);
            } else if (!isPinned) setNavOpen(false);
          }}
        >
          <Icon size={18} /><span>{label}</span>
        </NavLink>
      ))}
    </nav>
  );

  const renderLayoutMenu = (isPinned: boolean) => (
    <div className={`immersive-popover immersive-layout-popover${isPinned ? ' immersive-pinned-card' : ''}`} role="menu" aria-label="布局" style={isPinned || toolAnchorLeft === null ? undefined : { left: toolAnchorLeft, right: 'auto' }}>
      <MenuCardHeading label="布局" pinned={isPinned} onPin={() => routeTarget && togglePinnedMenu({ kind: 'tool', menu: 'layout', target: routeTarget })} />
      {mediaLayoutDefinitions.map((layout) => (
        <button className={viewPreferences.mode === layout.id ? 'active' : ''} key={layout.id} type="button" onClick={() => selectLayout(layout.id)}>
          <LayoutGrid size={18} /><span><strong>{layout.label}</strong><small>{layout.description}</small></span>
        </button>
      ))}
    </div>
  );

  const renderTagMenu = (isPinned: boolean) => browseTools && (
    <div className={`immersive-popover immersive-tag-popover${isPinned ? ' immersive-pinned-card' : ''}`} role="dialog" aria-label="标签筛选" style={isPinned || toolAnchorLeft === null ? undefined : { left: toolAnchorLeft, right: 'auto' }}>
      <MenuCardHeading label="标签筛选" pinned={isPinned} onPin={() => routeTarget && togglePinnedMenu({ kind: 'tool', menu: 'tags', target: routeTarget })} />
      <div className="immersive-pinned-card-content"><HierarchicalTagPicker inline selected={browseTools.tagFilters} onChange={browseTools.onTagFilterChange} /></div>
    </div>
  );

  const renderPanelMenu = (mode: PanelMode, isPinned: boolean) => panel && routeTarget && (
    <section className={`immersive-query-sheet target-${routeTarget} mode-${mode}${isPinned ? ' immersive-pinned-card' : ''}`} aria-label={`${scopeTitle}${panelModeLabel(mode)}`} style={isPinned || toolAnchorLeft === null ? undefined : { left: toolAnchorLeft, right: 'auto' }}>
      <div className="immersive-sheet-header">
        <div><small>{panelModeLabel(mode)}</small><strong>{scopeTitle}</strong></div>
        <div className="immersive-sheet-actions">
          {mode === 'query' && queryChips.length > 0 && <button className="immersive-clear-query" type="button" onClick={() => queryChips.forEach((chip) => chip.onRemove())}>清除全部</button>}
          <MenuPinButton label={panelModeLabel(mode)} pinned={isPinned} onClick={() => togglePinnedMenu({ kind: 'panel', mode, target: routeTarget })} />
        </div>
      </div>
      {mode === 'query' && queryChips.length > 0 && (
        <div className="immersive-sheet-query-chips" aria-label="当前筛选条件">
          {queryChips.map((chip) => (
            <button key={chip.id} type="button" title={`移除 ${chip.label}`} onClick={chip.onRemove}>
              <span>{chip.label}</span><X size={12} />
            </button>
          ))}
        </div>
      )}
      <div className="immersive-sheet-content">{panel}</div>
    </section>
  );

  return (
    <>
      <button
        ref={mobileTriggerRef}
        className={`immersive-mobile-trigger${chromePinned || interactionPinned ? ' active' : ''}`}
        type="button"
        aria-label={chromePinned || interactionPinned ? '收起顶部菜单' : '打开顶部菜单'}
        aria-expanded={chromePinned || interactionPinned}
        onClick={toggleMobileChrome}
      >
        {chromePinned || interactionPinned ? <X size={18} /> : <Menu size={18} />}
      </button>
      <div
        className={`immersive-chrome immersive-chrome-size-${chromeSize}${visible || interactionOpen ? ' visible' : ''}${interactionOpen ? ' locked' : ''}${chromePinned ? ' pinned' : ''}`}
        ref={chromeRef}
        style={!chromePinned && pinnedMenuActive ? { left: pinnedMenuWidth + 12 } : undefined}
        onPointerEnter={() => { cancelDismiss(); cancelMenuHover(); show(); }}
        onPointerLeave={() => { cancelMenuHover(); scheduleDismiss(menuDismissDelay); }}
        onBlurCapture={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) scheduleDismiss(menuDismissDelay);
        }}
      >
        <header className="immersive-topbar">
          <button
            className={`immersive-brand-scope${chromePinned ? ' pinned' : ''}`}
            aria-expanded={navOpen}
            aria-label={`${chromePinned ? '取消固定' : '固定'}顶部菜单，当前页面 ${scopeTitle}`}
            aria-pressed={chromePinned}
            type="button"
            onMouseEnter={(event) => previewNavigation(event.currentTarget)}
            onClick={(event) => toggleChromePinned(event.currentTarget)}
          >
            <span className="immersive-brand-name">lPicto</span>
            <span className="immersive-brand-separator" aria-hidden="true">-&gt;</span>
            <strong>{scopeTitle}</strong>
          </button>
          <span className="immersive-topbar-spacer" />
          {routeTarget === 'albums' && <TopbarButton icon={Images} label="相册" detail={scopeDetail} active={(panelOpen && panelMode === 'scope') || pinnedPanel?.mode === 'scope'} onHover={(trigger) => previewPanel('scope', trigger)} onClick={(event) => togglePanel('scope', event.currentTarget)} />}
          {routeTarget === 'folders' && <TopbarButton icon={FolderTree} label="文件夹" detail={scopeDetail} active={(panelOpen && panelMode === 'scope') || pinnedPanel?.mode === 'scope'} onHover={(trigger) => previewPanel('scope', trigger)} onClick={(event) => togglePanel('scope', event.currentTarget)} />}
          {routeTarget === 'collections' && <TopbarButton icon={Layers3} label="集合" detail={scopeDetail} active={(panelOpen && panelMode === 'scope') || pinnedPanel?.mode === 'scope'} onHover={(trigger) => previewPanel('scope', trigger)} onClick={(event) => togglePanel('scope', event.currentTarget)} />}
          {browseTools?.panelModes.includes('albums') && <TopbarButton icon={Images} label="相册" detail={browseTools.albumFilterLabel} active={(panelOpen && panelMode === 'albums') || pinnedPanel?.mode === 'albums' || browseTools.albumFilterActive} onHover={(trigger) => previewPanel('albums', trigger)} onClick={(event) => togglePanel('albums', event.currentTarget)} />}
          {browseTools && (browseTools.panelModes.includes('search') || browseTools.panelModes.includes('filters')) && (
            <TopbarButton icon={Search} label="搜索与筛选" detail={queryCount > 0 ? String(queryCount) : undefined} active={(panelOpen && panelMode === 'query') || pinnedPanel?.mode === 'query' || queryCount > 0} dataAttribute="query" onHover={(trigger) => previewPanel('query', trigger)} onClick={(event) => togglePanel('query', event.currentTarget)} />
          )}
          {browseTools && <TopbarButton icon={Tags} label="标签" detail={browseTools.tagFilters.length > 0 ? String(browseTools.tagFilters.length) : undefined} active={toolMenu === 'tags' || pinnedTool?.menu === 'tags' || browseTools.tagFilters.length > 0} onHover={(trigger) => previewToolMenu('tags', trigger)} onClick={(event) => toggleToolMenu('tags', event.currentTarget)} />}
          {browseTools && <TopbarButton icon={mediaTypeIcon(browseTools.type)} label="类型" detail={mediaTypeLabel(browseTools.type)} active={toolMenu === 'type' || pinnedTool?.menu === 'type' || browseTools.type !== 'all'} onHover={(trigger) => previewToolMenu('type', trigger)} onClick={(event) => toggleToolMenu('type', event.currentTarget)} />}
          {browseTools && <TopbarButton icon={orientationIcon(browseTools.orientation)} label="方向" detail={orientationLabel(browseTools.orientation)} active={toolMenu === 'orientation' || pinnedTool?.menu === 'orientation' || browseTools.orientation !== 'all'} onHover={(trigger) => previewToolMenu('orientation', trigger)} onClick={(event) => toggleToolMenu('orientation', event.currentTarget)} />}
          {browseTools && <TopbarButton icon={browseTools.rating === 'all' ? Star : browseTools.rating === 0 ? StarOff : Star} label="评分" detail={ratingLabel(browseTools.rating)} active={toolMenu === 'rating' || pinnedTool?.menu === 'rating' || browseTools.rating !== 'all'} onHover={(trigger) => previewToolMenu('rating', trigger)} onClick={(event) => toggleToolMenu('rating', event.currentTarget)} />}
          {browseTools && <TopbarButton icon={RotateCw} label="排序" active={toolMenu === 'sort' || pinnedTool?.menu === 'sort'} onHover={(trigger) => previewToolMenu('sort', trigger)} onClick={(event) => toggleToolMenu('sort', event.currentTarget)} />}
          {browseTools && <TopbarButton icon={Layers3} label="分组" active={toolMenu === 'group' || pinnedTool?.menu === 'group' || browseTools.groupMode !== 'none'} onHover={(trigger) => previewToolMenu('group', trigger)} onClick={(event) => toggleToolMenu('group', event.currentTarget)} />}
          {mediaRoute && <TopbarButton icon={LayoutGrid} label="布局" active={toolMenu === 'layout' || pinnedTool?.menu === 'layout'} onHover={(trigger) => previewToolMenu('layout', trigger)} onClick={(event) => toggleToolMenu('layout', event.currentTarget)} />}
          {mediaRoute && batchState.available && !batchState.selectionMode && <TopbarButton icon={ListChecks} label="多选" onHover={previewCloseOpenMenu} onClick={() => dispatchAssetGridBatchCommand('toggle-selection')} />}
          {viewerInfo.active && (
            <TopbarButton
              icon={viewerInfo.visible ? PanelRightClose : PanelRightOpen}
              label="信息"
              active={viewerInfo.visible}
              onHover={previewCloseOpenMenu}
              onClick={() => viewerInfo.setVisible(!viewerInfo.visible)}
            />
          )}
        </header>
        {navOpen && renderNavigationMenu(false)}
        {toolMenu === 'layout' && renderLayoutMenu(false)}
        {toolMenu === 'tags' && renderTagMenu(false)}
        {toolMenu && toolMenu !== 'layout' && toolMenu !== 'tags' && browseTools && routeTarget && <BrowseToolPopover anchorLeft={toolAnchorLeft} menu={toolMenu} pinned={false} tools={browseTools} onPin={() => togglePinnedMenu({ kind: 'tool', menu: toolMenu, target: routeTarget })} />}
        {panelOpen && renderPanelMenu(panelMode, false)}
      </div>
      {pinnedMenuActive && (
        <aside className="immersive-pinned-sidebar" style={{ width: pinnedMenuWidth }} aria-label="固定菜单">
          {pinnedNavigation && renderNavigationMenu(true)}
          {pinnedPanel && renderPanelMenu(pinnedPanel.mode, true)}
          {pinnedTool?.menu === 'layout' && renderLayoutMenu(true)}
          {pinnedTool?.menu === 'tags' && renderTagMenu(true)}
          {pinnedTool && pinnedTool.menu !== 'layout' && pinnedTool.menu !== 'tags' && browseTools && <BrowseToolPopover anchorLeft={null} menu={pinnedTool.menu} pinned tools={browseTools} onPin={() => togglePinnedMenu(pinnedTool)} />}
          <button className="immersive-pinned-resize-handle" type="button" aria-label="拖动调节固定菜单宽度" title="拖动调节宽度" onPointerDown={startPinnedMenuResize} />
        </aside>
      )}
      {mediaRoute && batchState.selectionMode && <SelectionActionBar state={batchState} />}
    </>
  );
}

function TopbarButton({ active = false, dataAttribute, detail, icon: Icon, label, onClick, onHover }: { active?: boolean; dataAttribute?: 'query'; detail?: string; icon: LucideIcon; label: string; onClick: (event: ReactMouseEvent<HTMLButtonElement>) => void; onHover?: (trigger: HTMLButtonElement) => void }) {
  const title = detail ? `${label}：${detail}` : label;
  return <button className={`immersive-topbar-button${active ? ' active' : ''}`} data-topbar-query={dataAttribute === 'query' ? '' : undefined} type="button" title={title} onMouseEnter={(event) => onHover?.(event.currentTarget)} onClick={onClick}><Icon size={17} /><span>{label}{detail && <strong>{detail}</strong>}</span></button>;
}

function SelectionActionBar({ state }: { state: AssetGridBatchState }) {
  const actions: Array<{ command: AssetGridBatchCommand; Icon: LucideIcon; label: string; danger?: boolean }> = [
    ...(state.canAutoSelect ? [{ command: 'auto-select' as const, Icon: Sparkles, label: '自动选择重复项' }] : []),
    { command: 'select-all', Icon: ListChecks, label: '全选已加载' },
    { command: 'clear', Icon: CircleX, label: '清空' },
    { command: 'add-tag', Icon: Tags, label: '标签' },
    { command: 'set-rating', Icon: Star, label: '评分' },
    { command: 'add-album', Icon: Images, label: '加入相册' },
    { command: 'rotate', Icon: RotateCw, label: '旋转' },
    { command: 'hide', Icon: EyeOff, label: '隐藏' },
    { command: 'delete', Icon: Trash2, label: '删除', danger: true },
    { command: 'delete-records', Icon: Database, label: '删除记录', danger: true },
  ];
  return (
    <div className="selection-action-bar" role="toolbar" aria-label="多选操作">
      <strong>已选 {state.selectedCount}</strong>
      {state.progress && (
        <span
          className="batch-progress"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={state.progress.total}
          aria-valuenow={state.progress.current}
          title={state.message}
        >
          <i style={{ width: `${state.progress.total > 0 ? (state.progress.current / state.progress.total) * 100 : 0}%` }} />
          <small>{state.progress.current}/{state.progress.total}</small>
        </span>
      )}
      {actions.map(({ command, Icon, label, danger }) => (
        <button className={danger ? 'danger' : ''} disabled={state.busy || (state.selectedCount === 0 && !['auto-select', 'select-all', 'clear'].includes(command))} key={command} type="button" title={label} onClick={() => dispatchAssetGridBatchCommand(command)}>
          <Icon size={17} /><span>{label}</span>
        </button>
      ))}
      <button type="button" title="退出多选" onClick={() => dispatchAssetGridBatchCommand('toggle-selection')}><X size={18} /><span>退出</span></button>
    </div>
  );
}

function BrowseToolPopover({ anchorLeft, menu, onPin, pinned, tools }: { anchorLeft: number | null; menu: Exclude<ToolMenu, 'layout' | 'tags'>; onPin: () => void; pinned: boolean; tools: BrowseTools }) {
  const style = pinned || anchorLeft === null ? undefined : { left: anchorLeft, right: 'auto' as const };
  if (menu === 'type') {
    const options: Array<{ Icon: LucideIcon; label: string; value: AssetKind }> = [
      { Icon: PanelsTopLeft, label: '全部媒体', value: 'all' },
      { Icon: ImageIcon, label: '图片', value: 'image' },
      { Icon: Video, label: '视频', value: 'video' },
      { Icon: Music, label: '音频', value: 'audio' },
    ];
    return <ToolOptionList label="媒体类型" onPin={onPin} options={options} pinned={pinned} style={style} value={tools.type} onChange={tools.onTypeChange} />;
  }
  if (menu === 'orientation') {
    const options: Array<{ Icon: LucideIcon; label: string; value: OrientationFilter }> = [
      { Icon: MonitorSmartphone, label: '全部方向', value: 'all' },
      { Icon: RectangleHorizontal, label: '横屏', value: 'landscape' },
      { Icon: RectangleVertical, label: '竖屏', value: 'portrait' },
    ];
    return <ToolOptionList label="画面方向" onPin={onPin} options={options} pinned={pinned} style={style} value={tools.orientation} onChange={tools.onOrientationChange} />;
  }
  if (menu === 'rating') {
    const options: Array<{ Icon: LucideIcon; label: string; value: AssetRatingFilter }> = [
      { Icon: Star, label: '全部评分', value: 'all' },
      { Icon: StarOff, label: '未评分', value: 0 },
      ...([1, 2, 3, 4, 5] as const).map((value) => ({ Icon: Star, label: `${value} 星`, value })),
    ];
    return <ToolOptionList label="评分" onPin={onPin} options={options} pinned={pinned} style={style} value={tools.rating} onChange={tools.onRatingChange} />;
  }
  if (menu === 'group') {
    const options: Array<{ Icon: LucideIcon; label: string; value: AssetGroupMode }> = [
      { Icon: PanelsTopLeft, label: '不分组', value: 'none' },
      { Icon: CalendarDays, label: '按日', value: 'day' },
      { Icon: CalendarRange, label: '按月', value: 'month' },
      { Icon: CalendarSync, label: '按年', value: 'year' },
      { Icon: Gauge, label: '按大小', value: 'size' },
      { Icon: LetterText, label: '按首字母', value: 'letter' },
      { Icon: FolderInput, label: '按文件夹', value: 'folder' },
    ];
    return <ToolOptionList label="分组" onPin={onPin} options={options} pinned={pinned} style={style} value={tools.groupMode} onChange={tools.onGroupChange} />;
  }
  const parts = sortPartsFromKey(tools.sort);
  const fields: Array<{ Icon: LucideIcon; label: string; value: SortField }> = [
    { Icon: CalendarDays, label: '时间', value: 'timeline' },
    { Icon: FolderInput, label: '导入时间', value: 'imported' },
    { Icon: History, label: '最近播放', value: 'last_played' },
    { Icon: FileClock, label: '修改时间', value: 'modified' },
    { Icon: Database, label: '大小', value: 'size' },
    { Icon: FileText, label: '文件名', value: 'filename' },
    { Icon: ScanLine, label: '分辨率', value: 'resolution' },
    { Icon: Timer, label: '时长', value: 'duration' },
    { Icon: Star, label: '评分', value: 'rating' },
    { Icon: Gauge, label: '帧率', value: 'fps' },
    { Icon: Activity, label: '码率', value: 'bitrate' },
  ];
  return (
    <div className={`immersive-popover immersive-tool-popover${pinned ? ' immersive-pinned-card' : ''}`} role="menu" aria-label="排序" style={style}>
      <MenuCardHeading label="排序" pinned={pinned} onPin={onPin} />
      <button className="immersive-tool-direction" type="button" onClick={() => tools.onSortChange(sortKeyFromParts(parts.field, parts.direction === 'asc' ? 'desc' : 'asc'))}>
        {parts.direction === 'asc' ? <ArrowUpAZ size={18} /> : <ArrowDownAZ size={18} />}
        <span>{parts.direction === 'asc' ? '正序' : '倒序'}</span>
        <small>点击切换</small>
      </button>
      <div className="immersive-tool-divider" />
      {fields.map(({ Icon, label, value }) => (
        <button className={parts.field === value ? 'active' : ''} key={value} type="button" onClick={() => tools.onSortChange(sortKeyFromParts(value, parts.direction))}>
          <Icon size={18} /><span>{label}</span>
        </button>
      ))}
    </div>
  );
}

function ToolOptionList<T extends string | number>({ label, onChange, onPin, options, pinned, style, value }: {
  label: string;
  onChange: (value: T) => void;
  onPin: () => void;
  options: Array<{ Icon: LucideIcon; label: string; value: T }>;
  pinned: boolean;
  style?: { left: number; right: 'auto' };
  value: T;
}) {
  return (
    <div className={`immersive-popover immersive-tool-popover${pinned ? ' immersive-pinned-card' : ''}`} role="menu" aria-label={label} style={style}>
      <MenuCardHeading label={label} pinned={pinned} onPin={onPin} />
      {options.map(({ Icon, label: optionLabel, value: optionValue }) => (
        <button className={value === optionValue ? 'active' : ''} key={optionValue} type="button" onClick={() => onChange(optionValue)}>
          <Icon size={18} /><span>{optionLabel}</span>
        </button>
      ))}
    </div>
  );
}

function MenuCardHeading({ label, onPin, pinned, suffix }: { label: string; onPin: () => void; pinned: boolean; suffix?: ReactNode }) {
  return (
    <div className="immersive-tool-heading immersive-card-heading">
      <span>{label}</span>
      <span className="immersive-card-heading-actions">{suffix}<MenuPinButton label={label} pinned={pinned} onClick={onPin} /></span>
    </div>
  );
}

function MenuPinButton({ label, onClick, pinned }: { label: string; onClick: () => void; pinned: boolean }) {
  const actionLabel = pinned ? `取消固定${label}` : `固定${label}到左侧`;
  return (
    <button className={`immersive-card-pin${pinned ? ' active' : ''}`} type="button" aria-label={actionLabel} aria-pressed={pinned} title={actionLabel} onClick={onClick}>
      {pinned ? <PinOff size={15} /> : <Pin size={15} />}
    </button>
  );
}

function samePinnedMenu(current: PinnedMenu | null, next: PinnedMenu) {
  if (!current || current.kind !== next.kind) return false;
  if (current.kind === 'navigation' && next.kind === 'navigation') return true;
  if (current.kind === 'panel' && next.kind === 'panel') return current.target === next.target && current.mode === next.mode;
  if (current.kind === 'tool' && next.kind === 'tool') return current.target === next.target && current.menu === next.menu;
  return false;
}

function mediaTypeIcon(type: AssetKind): LucideIcon {
  if (type === 'image') return ImageIcon;
  if (type === 'video') return Video;
  if (type === 'audio') return Music;
  return PanelsTopLeft;
}

function orientationIcon(orientation: OrientationFilter): LucideIcon {
  if (orientation === 'landscape') return RectangleHorizontal;
  if (orientation === 'portrait') return RectangleVertical;
  return MonitorSmartphone;
}

function mediaTypeLabel(type: AssetKind) {
  if (type === 'image') return '图片';
  if (type === 'video') return '视频';
  if (type === 'audio') return '音频';
  return undefined;
}

function orientationLabel(orientation: OrientationFilter) {
  if (orientation === 'landscape') return '横屏';
  if (orientation === 'portrait') return '竖屏';
  return undefined;
}

function ratingLabel(rating: AssetRatingFilter) {
  if (rating === 'all') return undefined;
  return rating === 0 ? '未评分' : `${rating} 星`;
}

function panelModeLabel(mode: PanelMode) {
  if (mode === 'albums') return '相册范围';
  if (mode === 'query') return '搜索与筛选';
  return '浏览范围';
}

function focusPanelControl(focus: PanelMode) {
  const sheet = document.querySelector<HTMLElement>('.immersive-query-sheet');
  if (!sheet) return;
  if (focus === 'query') {
    window.setTimeout(() => sheet.querySelector<HTMLInputElement>('.sidebar-search-card input, input[type="search"], input')?.focus(), 20);
    return;
  }
  sheet.querySelector<HTMLElement>('button, input')?.focus();
}

function isEditableTarget(target: EventTarget | null) {
  return target instanceof HTMLElement && (target.isContentEditable || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT');
}
