import { type PointerEvent, type ReactNode, useCallback, useEffect } from 'react';
import { FolderTree, Images, Library, Search, Settings } from 'lucide-react';
import { NavLink, useLocation } from 'react-router-dom';
import { useSidebarPanelValue, type SidebarPanelTarget } from './SidebarContext';
import { isPrimarySidebarPanelTarget, primaryTargetForPath, type PrimarySidebarPanelTarget } from '../utils/sidebarPrefs';

interface Props {
  collapsed: boolean;
  expanded: SidebarPanelTarget | null;
  primaryWidth: number;
  secondaryWidth: number;
  onPrimaryWidthChange: (width: number) => void;
  onSecondaryWidthChange: (width: number) => void;
  onToggleCollapsed: () => void;
  onToggleExpanded: (target: SidebarPanelTarget | null) => void;
}

const navItems: Array<{
  Icon: typeof Library;
  label: string;
  target: PrimarySidebarPanelTarget;
  to: string;
}> = [
  { Icon: Library, label: '图库', target: 'library', to: '/library' },
  { Icon: Search, label: '搜索', target: 'search', to: '/search' },
  { Icon: Images, label: '相册', target: 'albums', to: '/albums' },
  { Icon: FolderTree, label: '文件夹', target: 'folders', to: '/folders' },
  { Icon: Settings, label: '设置', target: 'settings', to: '/settings' },
];

export default function Sidebar({
  collapsed,
  expanded,
  primaryWidth,
  secondaryWidth,
  onPrimaryWidthChange,
  onSecondaryWidthChange,
  onToggleCollapsed,
  onToggleExpanded,
}: Props) {
  const location = useLocation();
  const panels = useSidebarPanelValue();
  const routeTarget = primaryTargetForPath(location.pathname);

  useEffect(() => {
    if (expanded && !isPrimarySidebarPanelTarget(expanded) && !panels[expanded]) {
      onToggleExpanded(null);
    }
  }, [expanded, onToggleExpanded, panels]);

  const renderBottomPanel = (target: SidebarPanelTarget) =>
    panels[target] ? <div className={`sidebar-panel sidebar-panel-${target}`}>{panels[target]}</div> : null;
  const activeSecondaryTarget = routeTarget && expanded === routeTarget ? routeTarget : null;
  const secondaryPanel = activeSecondaryTarget ? panels[activeSecondaryTarget] : null;
  const routeSecondaryLabel = routeTarget ? sidebarLabel(routeTarget) : '';
  const canToggleRouteSecondary = routeTarget !== null && Boolean(panels[routeTarget]);
  const routeSecondaryOpen = activeSecondaryTarget !== null;
  const routeSecondaryToggleLabel = `${routeSecondaryOpen ? '折叠' : '展开'}${routeSecondaryLabel}`;
  const showPrimarySecondaryButton = !collapsed && canToggleRouteSecondary && !routeSecondaryOpen;
  const showFloatingSecondaryButton = collapsed && canToggleRouteSecondary && !routeSecondaryOpen;
  const toggleRouteSecondary = () => {
    if (!routeTarget) return;
    onToggleExpanded(routeSecondaryOpen ? null : routeTarget);
  };
  const startResize = useCallback(
    (kind: 'primary' | 'secondary', event: PointerEvent<HTMLButtonElement>) => {
      if (event.button !== 0) return;
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = kind === 'primary' ? primaryWidth : secondaryWidth;
      const onWidthChange = kind === 'primary' ? onPrimaryWidthChange : onSecondaryWidthChange;
      document.body.classList.add('sidebar-resizing');
      const onPointerMove = (moveEvent: globalThis.PointerEvent) => {
        onWidthChange(startWidth + moveEvent.clientX - startX);
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
    [onPrimaryWidthChange, onSecondaryWidthChange, primaryWidth, secondaryWidth],
  );

  return (
    <>
      {collapsed && !routeSecondaryOpen && (
        <div className={showFloatingSecondaryButton ? 'sidebar-floating-controls is-joined' : 'sidebar-floating-controls'}>
          <button className="sidebar-floating-button sidebar-floating-primary" type="button" onClick={onToggleCollapsed} aria-label="展开一级侧栏">
            LPicto
          </button>
          {showFloatingSecondaryButton && routeTarget && (
            <button
              className="sidebar-floating-button sidebar-floating-secondary"
              type="button"
              title={routeSecondaryToggleLabel}
              aria-label={routeSecondaryToggleLabel}
              onClick={toggleRouteSecondary}
            >
              {routeSecondaryLabel}
            </button>
          )}
        </div>
      )}
      {!collapsed && (
        <nav className="nav">
          <div className={showPrimarySecondaryButton ? 'brand-row has-secondary-toggle' : 'brand-row'}>
            <button className="brand" type="button" onClick={onToggleCollapsed} aria-label="折叠一级侧栏">
              LPicto
            </button>
            {showPrimarySecondaryButton && routeTarget && (
              <button
                className="brand-secondary-button"
                type="button"
                title={routeSecondaryToggleLabel}
                aria-label={routeSecondaryToggleLabel}
                onClick={toggleRouteSecondary}
              >
                {routeSecondaryLabel}
              </button>
            )}
          </div>
          <div className="nav-main">
            {navItems.map(({ Icon, label, target, to }) => (
              <SidebarItem icon={<Icon size={18} />} key={target} label={label} to={to} />
            ))}
          </div>
          <div className="nav-bottom">
            <div className="nav-bottom-panels">{renderBottomPanel('viewer')}</div>
          </div>
        </nav>
      )}
      {activeSecondaryTarget && (
        <aside className={`sidebar-secondary sidebar-secondary-${activeSecondaryTarget}`}>
          <div className={collapsed ? 'sidebar-secondary-header has-primary-toggle' : 'sidebar-secondary-header'}>
            {collapsed && (
              <button className="sidebar-secondary-primary-button" type="button" onClick={onToggleCollapsed} aria-label="展开一级侧栏">
                LPicto
              </button>
            )}
            <button
              className="sidebar-secondary-title-button"
              type="button"
              title={routeSecondaryToggleLabel}
              aria-label={routeSecondaryToggleLabel}
              onClick={toggleRouteSecondary}
            >
              {routeSecondaryLabel}
            </button>
          </div>
          {secondaryPanel && <div className={`sidebar-panel sidebar-panel-${activeSecondaryTarget}`}>{secondaryPanel}</div>}
        </aside>
      )}
      {!collapsed && (
        <button
          aria-label="调整一级栏宽度"
          className="sidebar-resize-handle sidebar-resize-primary"
          title="调整一级栏宽度"
          type="button"
          onPointerDown={(event) => startResize('primary', event)}
        />
      )}
      {activeSecondaryTarget && (
        <button
          aria-label="调整二级栏宽度"
          className="sidebar-resize-handle sidebar-resize-secondary"
          title="调整二级栏宽度"
          type="button"
          onPointerDown={(event) => startResize('secondary', event)}
        />
      )}
    </>
  );
}

function SidebarItem({
  icon,
  label,
  to,
}: {
  icon: ReactNode;
  label: string;
  to: string;
}) {
  return (
    <div className="nav-item">
      <NavLink to={to} className="nav-link">
        {icon}
        <span>{label}</span>
      </NavLink>
    </div>
  );
}

function sidebarLabel(target: PrimarySidebarPanelTarget) {
  return navItems.find((item) => item.target === target)?.label ?? '';
}
