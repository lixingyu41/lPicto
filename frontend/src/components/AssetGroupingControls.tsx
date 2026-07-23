import { CalendarDays, CalendarRange, CalendarSync, FolderTree, Layers3, LetterText, PanelsTopLeft, type LucideIcon } from 'lucide-react';
import type { SortKey } from '../types/api';
import type { AssetGroupMode } from '../utils/assetGrouping';
import { SidebarSelect } from './SidebarControls';
import { CompactSidebarMenu } from './CompactSidebarMenu';

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

export function CompactAssetGroupingControls({
  groupMode,
  onChange,
  sort,
}: {
  groupMode: AssetGroupMode;
  onChange: (mode: AssetGroupMode) => void;
  sort: SortKey;
}) {
  const options = compactGroupOptions(sort);
  const active = options.find((option) => option.value === groupMode) ?? options[0];
  const ActiveIcon = active.icon;
  return (
    <CompactSidebarMenu
      ariaLabel={`分组: ${active.label}`}
      title={`分组: ${active.label}`}
      trigger={<ActiveIcon size={18} />}
      options={options.map((option) => {
        const Icon = option.icon;
        return {
          active: groupMode === option.value,
          icon: <Icon size={18} />,
          key: option.value,
          label: option.label,
          onSelect: () => onChange(option.value),
        };
      })}
    />
  );
}

export function normalizeAssetGroupModeForSort(mode: AssetGroupMode, sort: SortKey): AssetGroupMode {
  void sort;
  return mode;
}

function groupSectionForSort(sort: SortKey): { title: string; options: GroupOption[] } {
  void sort;
  return {
    title: '分组',
    options: [
      { value: 'day', label: '按日' },
      { value: 'month', label: '按月' },
      { value: 'year', label: '按年' },
      { value: 'size', label: '按大小' },
      { value: 'letter', label: '按首字母' },
    ],
  };
}

interface CompactGroupOption {
  value: AssetGroupMode;
  label: string;
  icon: LucideIcon;
}

function compactGroupOptions(sort: SortKey): CompactGroupOption[] {
  void sort;
  const none: CompactGroupOption = { value: 'none', label: '无分组', icon: Layers3 };
  const folder: CompactGroupOption = { value: 'folder', label: '按文件夹', icon: FolderTree };
  return [
    none,
    { value: 'day', label: '按日', icon: CalendarDays },
    { value: 'month', label: '按月', icon: CalendarRange },
    { value: 'year', label: '按年', icon: CalendarSync },
    { value: 'size', label: '按大小', icon: PanelsTopLeft },
    { value: 'letter', label: '按首字母', icon: LetterText },
    folder,
  ];
}
