import { type KeyboardEvent, type MouseEvent, type PointerEvent, type ReactNode, useCallback, useEffect } from 'react';
import { FolderTree, Images, Layers, Library, Settings } from 'lucide-react';
import { NavLink, useLocation } from 'react-router-dom';
import { useSidebarPanelValue, type SidebarPanelTarget } from './SidebarContext';
import {
  isPrimarySidebarPanelTarget,
  primaryTargetForPath,
  type CollapsedSidebarContent,
  type PrimarySidebarPanelTarget,
} from '../utils/sidebarPrefs';
import { loadSettingsSection, settingsSectionPath } from '../utils/settingsRoute';

interface Props {
  collapsed: boolean;
  collapsedContent: CollapsedSidebarContent;
  expanded: SidebarPanelTarget | null;
  secondaryWidth: number;
  routePathname?: string;
  onSecondaryWidthChange: (width: number) => void;
  onTogglePrimary: () => void;
  onToggleExpanded: (target: SidebarPanelTarget | null) => void;
}

const navItems: Array<{
  Icon: typeof Library;
  label: string;
  compactLabel: string;
  target: PrimarySidebarPanelTarget;
  to: string;
}> = [
  { Icon: Library, compactLabel: '库', label: '图库', target: 'library', to: '/library' },
  { Icon: Images, compactLabel: '册', label: '相册', target: 'albums', to: '/albums' },
  { Icon: FolderTree, compactLabel: '夹', label: '文件夹', target: 'folders', to: '/folders' },
  { Icon: Layers, compactLabel: '智', label: '智能', target: 'collections', to: '/collections' },
  { Icon: Settings, compactLabel: '设', label: '设置', target: 'settings', to: '/settings' },
];

export default function Sidebar({
  collapsed,
  collapsedContent,
  expanded,
  secondaryWidth,
  routePathname,
  onSecondaryWidthChange,
  onTogglePrimary,
  onToggleExpanded,
}: Props) {
  const location = useLocation();
  const effectivePathname = routePathname ?? location.pathname;
  const panels = useSidebarPanelValue();
  const routeTarget = primaryTargetForPath(effectivePathname);

  useEffect(() => {
    if (expanded && !isPrimarySidebarPanelTarget(expanded) && !panels[expanded]) {
      onToggleExpanded(null);
    }
  }, [expanded, onToggleExpanded, panels]);

  const activeRouteSecondaryTarget = routeTarget && expanded === routeTarget ? routeTarget : null;
  const activeSecondaryTarget = activeRouteSecondaryTarget;
  const secondaryPanel = activeRouteSecondaryTarget ? panels[activeRouteSecondaryTarget] : null;
  const routeSecondaryLabel = activeRouteSecondaryTarget ? sidebarLabel(activeRouteSecondaryTarget) : routeTarget ? sidebarLabel(routeTarget) : '';
  const routeSecondaryOpen = activeSecondaryTarget !== null;
  const toggleRouteSecondary = useCallback(() => {
    if (!routeTarget) return;
    onToggleExpanded(routeSecondaryOpen ? null : routeTarget);
  }, [onToggleExpanded, routeSecondaryOpen, routeTarget]);
  const handleSecondaryHeaderKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if (event.key !== 'Enter' && event.key !== ' ') return;
      event.preventDefault();
      toggleRouteSecondary();
    },
    [toggleRouteSecondary],
  );
  const toggleSecondaryForNav = useCallback(
    (target: PrimarySidebarPanelTarget) => {
      if (!panels[target]) return;
      onToggleExpanded(target);
    },
    [onToggleExpanded, panels],
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

  return (
    <>
      <nav className="nav">
        <div className="brand-row">
          <button className="brand" type="button" title={collapsed ? '展开一级侧栏' : '折叠一级侧栏'} aria-label={collapsed ? '展开一级侧栏' : '折叠一级侧栏'} onClick={onTogglePrimary}>
            <span className="brand-mark brand-mark-full">LPicto</span>
            <span className="brand-mark brand-mark-compact">P</span>
          </button>
        </div>
        <div className="nav-main">
          {navItems.map(({ Icon, compactLabel, label, target, to }) => (
            <SidebarItem
              active={routeTarget === target}
              compactLabel={collapsedContent === 'character' ? compactLabel : ''}
              icon={<Icon size={18} />}
              key={target}
              label={label}
              to={target === 'settings' ? settingsSectionPath(loadSettingsSection()) : to}
              onActivate={() => toggleSecondaryForNav(target)}
            />
          ))}
        </div>
        <div className="nav-bottom">
          <div className="nav-bottom-panels" />
        </div>
      </nav>
      {activeSecondaryTarget && (
        <aside className={`sidebar-secondary sidebar-secondary-${activeSecondaryTarget}`}>
          {activeRouteSecondaryTarget && (
            <div className="sidebar-secondary-main">
              <div
                aria-label="折叠二级栏"
                className="sidebar-secondary-header"
                role="button"
                tabIndex={0}
                title="折叠二级栏"
                onClick={toggleRouteSecondary}
                onKeyDown={handleSecondaryHeaderKeyDown}
              >
                <span className="sidebar-secondary-title">{routeSecondaryLabel}</span>
              </div>
              {secondaryPanel}
            </div>
          )}
        </aside>
      )}
      <div aria-hidden="true" className="sidebar-resize-handle sidebar-resize-primary sidebar-resize-static" />
      {activeSecondaryTarget && (
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
  compactLabel,
  icon,
  label,
  onActivate,
  to,
}: {
  active: boolean;
  compactLabel: string;
  icon: ReactNode;
  label: string;
  onActivate: (event: MouseEvent<HTMLAnchorElement>) => void;
  to: string;
}) {
  return (
    <NavLink
      to={to}
      className={active ? 'nav-link active' : 'nav-link'}
      title={label}
      onClick={(event) => {
        if (active) {
          event.preventDefault();
        }
        onActivate(event);
      }}
    >
      <span className="nav-link-icon">{icon}</span>
      {compactLabel && <span className="nav-link-compact-label" aria-hidden="true">{compactLabel}</span>}
      <span className="nav-link-label">{label}</span>
    </NavLink>
  );
}

function sidebarLabel(target: PrimarySidebarPanelTarget) {
  return navItems.find((item) => item.target === target)?.label ?? '';
}
