import type { SortKey } from '../types/api';
import type { AssetGroupMode } from '../utils/assetGrouping';
import { SidebarSelect } from './SidebarControls';

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
  const options = [
    { value: 'none', label: '无分组' },
    ...section.options,
    { value: 'folder', label: '按文件夹' },
  ];
  return (
    <SidebarSelect label="分组" value={groupMode} options={options} onChange={(value) => onChange(value as AssetGroupMode)} />
  );
}

export function normalizeAssetGroupModeForSort(mode: AssetGroupMode, sort: SortKey): AssetGroupMode {
  if (mode === 'none' || mode === 'folder') return mode;
  return groupSectionForSort(sort).options.some((option) => option.value === mode) ? mode : 'none';
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
