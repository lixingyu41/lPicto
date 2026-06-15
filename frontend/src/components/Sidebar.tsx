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

  if (collapsed) {
    return (
      <button className="sidebar-bubble" type="button" onClick={onToggleCollapsed} aria-label="展开侧栏">
        LPicto
      </button>
    );
  }

  return (
    <>
      <nav className="nav">
        <div className="brand-row">
          <button className="brand" type="button" onClick={onToggleCollapsed} aria-label="折叠侧栏">
            LPicto
          </button>
          <NavLink
            className={({ isActive }) => (isActive ? 'brand-settings-button active' : 'brand-settings-button')}
            to="/settings"
            title="设置"
            aria-label="设置"
          >
            <Settings size={18} />
          </NavLink>
        </div>
        <div className="nav-main">
          {navItems.map(({ Icon, label, target, to }) => (
            <SidebarItem
              expanded={expanded === target}
              icon={<Icon size={18} />}
              key={target}
              label={label}
              target={target}
              to={to}
              onToggle={onToggleExpanded}
              routeTarget={routeTarget}
            />
          ))}
        </div>
        <div className="nav-bottom">
          <div className="nav-bottom-panels">{renderBottomPanel('viewer')}</div>
        </div>
      </nav>
      {activeSecondaryTarget && (
        <aside className={`sidebar-secondary sidebar-secondary-${activeSecondaryTarget}`}>
          {secondaryPanel && <div className={`sidebar-panel sidebar-panel-${activeSecondaryTarget}`}>{secondaryPanel}</div>}
        </aside>
      )}
      <button
        aria-label="调整一级栏宽度"
        className="sidebar-resize-handle sidebar-resize-primary"
        title="调整一级栏宽度"
        type="button"
        onPointerDown={(event) => startResize('primary', event)}
      />
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
  expanded,
  icon,
  label,
  target,
  to,
  onToggle,
  routeTarget,
}: {
  expanded: boolean;
  icon: ReactNode;
  label: string;
  target: SidebarPanelTarget;
  to: string;
  onToggle: (target: SidebarPanelTarget | null) => void;
  routeTarget: SidebarPanelTarget | null;
}) {
  const isCurrentRoute = routeTarget === target;
  return (
    <div className="nav-item">
      <NavLink
        to={to}
        className="nav-link"
        aria-expanded={expanded}
        onClick={(event) => {
          if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
          if (isCurrentRoute) {
            event.preventDefault();
            onToggle(expanded ? null : target);
          } else if (expanded) {
            onToggle(null);
          }
        }}
      >
        {icon}
        <span>{label}</span>
      </NavLink>
    </div>
  );
}
