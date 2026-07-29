import {
  Activity,
  ArrowDown,
  ArrowDownAZ,
  ArrowUp,
  ArrowUpAZ,
  CalendarDays,
  Database,
  FileClock,
  FileText,
  FolderInput,
  Gauge,
  ScanLine,
  Star,
  Timer,
  type LucideIcon,
} from 'lucide-react';
import type { SortField, SortKey } from '../types/api';
import { CompactSidebarMenu } from './CompactSidebarMenu';

export type SortDirection = 'asc' | 'desc';

const sortFields: Array<{ value: SortField; label: string; icon: LucideIcon }> = [
  { value: 'timeline', label: '时间', icon: CalendarDays },
  { value: 'imported', label: '导入时间', icon: FolderInput },
  { value: 'modified', label: '修改时间', icon: FileClock },
  { value: 'size', label: '大小', icon: Database },
  { value: 'filename', label: '文件名', icon: FileText },
  { value: 'resolution', label: '分辨率', icon: ScanLine },
  { value: 'duration', label: '时长', icon: Timer },
  { value: 'rating', label: '评分', icon: Star },
  { value: 'fps', label: '帧率', icon: Gauge },
  { value: 'bitrate', label: '码率', icon: Activity },
];

const compactSortFields = sortFields;
const sortFieldValues = new Set(sortFields.map((field) => field.value));

export default function SortControls({ sort, onChange }: { sort: SortKey; onChange: (sort: SortKey) => void }) {
  const parts = sortPartsFromKey(sort);
  const nextDirection: SortDirection = parts.direction === 'asc' ? 'desc' : 'asc';
  return (
    <label className="sidebar-field sidebar-sort-field">
      <span>排序</span>
      <div className="sidebar-sort-row">
        <select value={parts.field} onChange={(event) => onChange(sortKeyFromParts(event.target.value as SortField, parts.direction))}>
          {sortFields.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          title={parts.direction === 'asc' ? '正序，点击切换为倒序' : '倒序，点击切换为正序'}
          aria-label={parts.direction === 'asc' ? '当前正序，点击切换为倒序' : '当前倒序，点击切换为正序'}
          onClick={() => onChange(sortKeyFromParts(parts.field, nextDirection))}
        >
          {parts.direction === 'asc' ? <ArrowUpAZ size={16} /> : <ArrowDownAZ size={16} />}
        </button>
      </div>
    </label>
  );
}

export function CompactSortControls({ sort, onChange }: { sort: SortKey; onChange: (sort: SortKey) => void }) {
  const parts = sortPartsFromKey(sort);
  const active = compactSortFields.find((field) => field.value === parts.field) ?? compactSortFields[0];
  const nextDirection: SortDirection = parts.direction === 'asc' ? 'desc' : 'asc';
  const ActiveIcon = active.icon;
  const DirectionIcon = parts.direction === 'asc' ? ArrowUp : ArrowDown;
  return (
    <CompactSidebarMenu
      ariaLabel={`${active.label}排序，点击设置排序`}
      title={`${active.label} · ${parts.direction === 'asc' ? '正序' : '倒序'}`}
      trigger={
        <span className="sidebar-sort-trigger-icon">
          <ActiveIcon size={18} />
          <DirectionIcon className="sidebar-sort-direction-icon" size={11} strokeWidth={2.6} />
        </span>
      }
      options={[
        {
          active: parts.direction === 'desc',
          closeOnSelect: false,
          icon: parts.direction === 'asc' ? <ArrowUpAZ size={18} /> : <ArrowDownAZ size={18} />,
          key: 'direction',
          label: parts.direction === 'asc' ? '正序' : '倒序',
          onSelect: () => onChange(sortKeyFromParts(parts.field, nextDirection)),
          trailing: <span className={parts.direction === 'desc' ? 'sidebar-inline-switch checked' : 'sidebar-inline-switch'}><i /></span>,
        },
        ...compactSortFields.map((field) => {
          const Icon = field.icon;
          return {
            active: parts.field === field.value,
            icon: <Icon size={18} />,
            key: field.value,
            label: field.label,
            onSelect: () => onChange(sortKeyFromParts(field.value, parts.direction)),
          };
        }),
      ]}
    />
  );
}

export function isSortKey(value: string | null): value is SortKey {
  if (value === 'filename' || value === 'size') return true;
  const match = value?.match(/^(.+)_(asc|desc)$/);
  return Boolean(match && sortFieldValues.has(match[1] as SortField));
}

export function sortPartsFromKey(sort: SortKey): { field: SortField; direction: SortDirection } {
  if (sort === 'filename') return { field: 'filename', direction: 'asc' };
  if (sort === 'size') return { field: 'size', direction: 'desc' };
  const match = sort.match(/^(.+)_(asc|desc)$/);
  if (match && sortFieldValues.has(match[1] as SortField)) {
    return { field: match[1] as SortField, direction: match[2] as SortDirection };
  }
  switch (sort) {
    default:
      return { field: 'timeline', direction: 'desc' };
  }
}

export function sortKeyFromParts(field: SortField, direction: SortDirection): SortKey {
  return `${field}_${direction}` as SortKey;
}
