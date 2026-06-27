import type { SortKey } from '../types/api';
import type { AssetGroupMode } from '../utils/assetGrouping';

interface GroupOption {
  label: string;
  value: AssetGroupMode;
}

export default function AssetGroupingControls({
  groupMode,
  onChange,
  sort,
}: {
  groupMode: AssetGroupMode;
  onChange: (mode: AssetGroupMode) => void;
  sort: SortKey;
}) {
  const section = groupSectionForSort(sort);
  return (
    <>
      <div className="sidebar-control-title">分组</div>
      <div className="sidebar-list">
        <GroupButton active={groupMode === 'none'} label="无分组" onClick={() => onChange('none')} />
      </div>
      <div className="sidebar-group-section">
        <div className="sidebar-control-subtitle">{section.title}</div>
        <div className="sidebar-list">
          {section.options.map((option) => (
            <GroupButton active={groupMode === option.value} key={option.value} label={option.label} onClick={() => onChange(option.value)} />
          ))}
        </div>
      </div>
      <div className="sidebar-group-section">
        <div className="sidebar-control-subtitle">文件夹分组</div>
        <div className="sidebar-list">
          <GroupButton active={groupMode === 'folder'} label="按文件夹" onClick={() => onChange(groupMode === 'folder' ? 'none' : 'folder')} />
        </div>
      </div>
    </>
  );
}

export function normalizeAssetGroupModeForSort(mode: AssetGroupMode, sort: SortKey): AssetGroupMode {
  if (mode === 'none' || mode === 'folder') return mode;
  return groupSectionForSort(sort).options.some((option) => option.value === mode) ? mode : 'none';
}

function GroupButton({ active, label, onClick }: { active: boolean; label: string; onClick: () => void }) {
  return (
    <button className={active ? 'sidebar-list-row active' : 'sidebar-list-row'} type="button" onClick={onClick}>
      <span className="sidebar-list-marker" aria-hidden="true" />
      <span>{label}</span>
    </button>
  );
}

function groupSectionForSort(sort: SortKey): { title: string; options: GroupOption[] } {
  if (sort === 'filename' || sort === 'filename_asc' || sort === 'filename_desc') {
    return { title: '首字母分组', options: [{ value: 'letter', label: '按首字母' }] };
  }
  if (sort === 'size' || sort === 'size_asc' || sort === 'size_desc') {
    return { title: '大小分组', options: [{ value: 'size', label: '按大小' }] };
  }
  return {
    title: '时间分组',
    options: [
      { value: 'day', label: '按日' },
      { value: 'month', label: '按月' },
      { value: 'year', label: '按年' },
    ],
  };
}
