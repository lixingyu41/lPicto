import { type ReactNode, useEffect, useState } from 'react';
import { ChevronDown, FolderTree, Images, Library, Settings } from 'lucide-react';
import { NavLink } from 'react-router-dom';
import { useSidebarPanelValue, type SidebarPanelTarget } from './SidebarContext';

export default function Sidebar() {
  const panels = useSidebarPanelValue();
  const [expanded, setExpanded] = useState<SidebarPanelTarget | null>(null);

  useEffect(() => {
    if (expanded && !panels[expanded]) {
      setExpanded(null);
    }
  }, [expanded, panels]);

  const renderPanel = (target: SidebarPanelTarget) =>
    panels[target] && expanded === target ? <div className={`sidebar-panel sidebar-panel-${target}`}>{panels[target]}</div> : null;
  const renderBottomPanel = (target: SidebarPanelTarget) =>
    panels[target] ? <div className={`sidebar-panel sidebar-panel-${target}`}>{panels[target]}</div> : null;

  return (
    <nav className="nav">
      <div className="brand">LPicto</div>
      <div className="nav-main">
        <SidebarItem
          expanded={expanded === 'library'}
          hasPanel={Boolean(panels.library)}
          icon={<Library size={18} />}
          label="图库"
          target="library"
          to="/library"
          onToggle={setExpanded}
        />
        {renderPanel('library')}
        <SidebarItem
          expanded={expanded === 'albums'}
          hasPanel={Boolean(panels.albums)}
          icon={<Images size={18} />}
          label="相册"
          target="albums"
          to="/albums"
          onToggle={setExpanded}
        />
        {renderPanel('albums')}
        <SidebarItem
          expanded={expanded === 'folders'}
          hasPanel={Boolean(panels.folders)}
          icon={<FolderTree size={18} />}
          label="文件夹"
          target="folders"
          to="/folders"
          onToggle={setExpanded}
        />
        {renderPanel('folders')}
      </div>
      <div className="nav-bottom">
        {renderBottomPanel('viewer')}
        <SidebarItem
          expanded={expanded === 'settings'}
          hasPanel={Boolean(panels.settings)}
          icon={<Settings size={18} />}
          label="设置"
          target="settings"
          to="/settings"
          onToggle={setExpanded}
        />
        {renderPanel('settings')}
      </div>
    </nav>
  );
}

function SidebarItem({
  expanded,
  hasPanel,
  icon,
  label,
  target,
  to,
  onToggle,
}: {
  expanded: boolean;
  hasPanel: boolean;
  icon: ReactNode;
  label: string;
  target: SidebarPanelTarget;
  to: string;
  onToggle: (target: SidebarPanelTarget | null) => void;
}) {
  return (
    <div className={hasPanel ? 'nav-item has-expand' : 'nav-item'}>
      <NavLink to={to} className="nav-link">
        {icon}
        <span>{label}</span>
      </NavLink>
      {hasPanel && (
        <button
          className={expanded ? 'nav-expand-button expanded' : 'nav-expand-button'}
          type="button"
          title={expanded ? '收起' : '展开'}
          onClick={() => onToggle(expanded ? null : target)}
        >
          <ChevronDown size={15} />
        </button>
      )}
    </div>
  );
}
