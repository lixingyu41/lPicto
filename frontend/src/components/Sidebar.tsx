import {
  type FocusEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from 'react';
import { FolderTree, History, Images, Layers, Library, PanelRightClose, PanelRightOpen, Settings } from 'lucide-react';
import { NavLink, useLocation } from 'react-router-dom';
import { useSidebarPanelValue, useViewerInfoPanel, type SidebarPanelTarget } from './SidebarContext';
import {
  isPrimarySidebarPanelTarget,
  primaryTargetForPath,
  type PrimarySidebarPanelTarget,
} from '../utils/sidebarPrefs';
import { loadSettingsSection, settingsSectionPath } from '../utils/settingsRoute';

interface Props {
  expanded: SidebarPanelTarget | null;
  secondaryWidth: number;
  routePathname?: string;
  onSecondaryWidthChange: (width: number) => void;
  onToggleExpanded: (target: SidebarPanelTarget | null) => void;
}

const navItems: Array<{
  Icon: typeof Library;
  label: string;
  target: PrimarySidebarPanelTarget;
  to: string;
}> = [
  { Icon: Library, label: '图库', target: 'library', to: '/library' },
  { Icon: History, label: '最近播放', target: 'recent', to: '/recent' },
  { Icon: Images, label: '相册', target: 'albums', to: '/albums' },
  { Icon: FolderTree, label: '文件夹', target: 'folders', to: '/folders' },
  { Icon: Layers, label: '智能', target: 'collections', to: '/collections' },
  { Icon: Settings, label: '设置', target: 'settings', to: '/settings' },
];

export default function Sidebar({
  expanded,
  secondaryWidth,
  routePathname,
  onSecondaryWidthChange,
  onToggleExpanded,
}: Props) {
  const location = useLocation();
  const effectivePathname = routePathname ?? location.pathname;
  const panels = useSidebarPanelValue();
  const viewerInfo = useViewerInfoPanel();
  const routeTarget = primaryTargetForPath(effectivePathname);
  const [primaryMenuOpen, setPrimaryMenuOpen] = useState(false);
  const primaryMenuHost = useRef<HTMLDivElement | null>(null);
  const primaryMenuCloseTimer = useRef<number | null>(null);
  const holdPrimaryMenuUntilPointerMove = useRef(false);

  const cancelPrimaryMenuClose = useCallback(() => {
    if (primaryMenuCloseTimer.current === null) return;
    window.clearTimeout(primaryMenuCloseTimer.current);
    primaryMenuCloseTimer.current = null;
  }, []);
  const openPrimaryMenu = useCallback(() => {
    holdPrimaryMenuUntilPointerMove.current = false;
    cancelPrimaryMenuClose();
    setPrimaryMenuOpen(true);
  }, [cancelPrimaryMenuClose]);
  const schedulePrimaryMenuClose = useCallback(() => {
    cancelPrimaryMenuClose();
    primaryMenuCloseTimer.current = window.setTimeout(() => {
      primaryMenuCloseTimer.current = null;
      setPrimaryMenuOpen(false);
    }, 500);
  }, [cancelPrimaryMenuClose]);

  useEffect(() => cancelPrimaryMenuClose, [cancelPrimaryMenuClose]);

  useEffect(() => {
    const handlePointerMove = (event: globalThis.PointerEvent) => {
      if (!holdPrimaryMenuUntilPointerMove.current) return;
      holdPrimaryMenuUntilPointerMove.current = false;
      const hoveredElement = document.elementFromPoint(event.clientX, event.clientY);
      if (hoveredElement && primaryMenuHost.current?.contains(hoveredElement)) {
        cancelPrimaryMenuClose();
        return;
      }
      schedulePrimaryMenuClose();
    };
    window.addEventListener('pointermove', handlePointerMove, true);
    return () => window.removeEventListener('pointermove', handlePointerMove, true);
  }, [cancelPrimaryMenuClose, schedulePrimaryMenuClose]);

  useEffect(() => {
    if (expanded && !isPrimarySidebarPanelTarget(expanded) && !panels[expanded]) {
      onToggleExpanded(null);
    }
  }, [expanded, onToggleExpanded, panels]);

  const activeRouteSecondaryTarget = routeTarget && expanded === routeTarget ? routeTarget : null;
  const secondaryPanel = activeRouteSecondaryTarget ? panels[activeRouteSecondaryTarget] : null;
  const routeSecondaryLabel = activeRouteSecondaryTarget ? sidebarLabel(activeRouteSecondaryTarget) : routeTarget ? sidebarLabel(routeTarget) : '';
  const toggleRouteSecondary = useCallback(() => {
    if (!routeTarget) return;
    onToggleExpanded(activeRouteSecondaryTarget ? null : routeTarget);
  }, [activeRouteSecondaryTarget, onToggleExpanded, routeTarget]);
  const openSecondaryForNav = useCallback(
    (target: PrimarySidebarPanelTarget) => {
      cancelPrimaryMenuClose();
      holdPrimaryMenuUntilPointerMove.current = true;
      setPrimaryMenuOpen(true);
      if (!panels[target]) return;
      onToggleExpanded(target);
    },
    [cancelPrimaryMenuClose, onToggleExpanded, panels],
  );
  const startResize = useCallback(
    (event: PointerEvent<HTMLButtonElement>) => {
      if (event.button !== 0) return;
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = secondaryWidth;
      document.body.classList.add('sidebar-resizing');
      const onPointerMove = (moveEvent: globalThis.PointerEvent) => {
        onSecondaryWidthChange(startWidth + moveEvent.clientX - startX);
      };
      const endResize = () => {
        document.body.classList.remove('sidebar-resizing');
        window.removeEventListener('pointermove', onPointerMove);
        window.removeEventListener('pointerup', endResize);
        window.removeEventListener('pointercancel', endResize);
      };
      window.addEventListener('pointermove', onPointerMove);
      window.addEventListener('pointerup', endResize);
      window.addEventListener('pointercancel', endResize);
    },
    [onSecondaryWidthChange, secondaryWidth],
  );
  const handleBrandBlur = useCallback((event: FocusEvent<HTMLDivElement>) => {
    if (event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget)) return;
    schedulePrimaryMenuClose();
  }, [schedulePrimaryMenuClose]);

  return (
    <>
      <div
        className="sidebar-brand-host"
        ref={primaryMenuHost}
        onBlur={handleBrandBlur}
        onFocus={openPrimaryMenu}
        onPointerEnter={(event) => {
          if (event.pointerType === 'mouse') openPrimaryMenu();
        }}
        onPointerLeave={(event) => {
          if (event.pointerType === 'mouse' && !holdPrimaryMenuUntilPointerMove.current) schedulePrimaryMenuClose();
        }}
      >
        <button
          aria-expanded={Boolean(activeRouteSecondaryTarget)}
          aria-haspopup="menu"
          aria-label={activeRouteSecondaryTarget ? '收回二级菜单' : '展开二级菜单'}
          className="brand"
          type="button"
          onClick={() => {
            cancelPrimaryMenuClose();
            toggleRouteSecondary();
          }}
        >
          <span className="brand-mark">LPicto</span>
        </button>
        {primaryMenuOpen && (
          <div aria-label="一级菜单" className="primary-menu-popover" role="menu">
            {navItems.map(({ Icon, label, target, to }) => (
              <SidebarItem
                active={routeTarget === target}
                icon={<Icon size={18} />}
                key={target}
                label={label}
                to={target === 'settings' ? settingsSectionPath(loadSettingsSection()) : to}
                onActivate={() => openSecondaryForNav(target)}
              />
            ))}
            {viewerInfo.active && (
              <button
                aria-pressed={viewerInfo.visible}
                className={viewerInfo.visible ? 'nav-link nav-viewer-info-toggle active' : 'nav-link nav-viewer-info-toggle'}
                role="menuitem"
                type="button"
                onClick={() => {
                  cancelPrimaryMenuClose();
                  viewerInfo.setVisible(!viewerInfo.visible);
                  setPrimaryMenuOpen(true);
                }}
              >
                <span className="nav-link-icon">
                  {viewerInfo.visible ? <PanelRightClose size={18} /> : <PanelRightOpen size={18} />}
                </span>
                <span className="nav-link-label">查看器信息</span>
              </button>
            )}
          </div>
        )}
      </div>
      {activeRouteSecondaryTarget && (
        <aside className={`sidebar-secondary sidebar-secondary-${activeRouteSecondaryTarget}`}>
          <div className="sidebar-secondary-main">
            <div className="sidebar-secondary-header">
              <span className="sidebar-secondary-title">{routeSecondaryLabel}</span>
            </div>
            {secondaryPanel}
          </div>
        </aside>
      )}
      {activeRouteSecondaryTarget && (
        <button
          aria-label="调整二级栏宽度"
          className="sidebar-resize-handle sidebar-resize-secondary"
          title="调整二级栏宽度"
          type="button"
          onPointerDown={startResize}
        />
      )}
    </>
  );
}

function SidebarItem({
  active,
  icon,
  label,
  onActivate,
  to,
}: {
  active: boolean;
  icon: ReactNode;
  label: string;
  onActivate: (event: MouseEvent<HTMLAnchorElement>) => void;
  to: string;
}) {
  return (
    <NavLink
      to={to}
      className={active ? 'nav-link active' : 'nav-link'}
      role="menuitem"
      title={label}
      onClick={(event) => {
        if (active) event.preventDefault();
        onActivate(event);
      }}
    >
      <span className="nav-link-icon">{icon}</span>
      <span className="nav-link-label">{label}</span>
    </NavLink>
  );
}

function sidebarLabel(target: PrimarySidebarPanelTarget) {
  return navItems.find((item) => item.target === target)?.label ?? '';
}
