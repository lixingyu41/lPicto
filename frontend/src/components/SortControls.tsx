import type { SortKey } from '../types/api';
import { SidebarButtonGroup, SidebarSelect } from './SidebarControls';

export type SortField = 'timeline' | 'imported' | 'size' | 'filename';
export type SortDirection = 'asc' | 'desc';

const sortFields: Array<{ value: SortField; label: string }> = [
  { value: 'timeline', label: '时间' },
  { value: 'imported', label: '导入时间' },
  { value: 'size', label: '大小' },
  { value: 'filename', label: '文件名' },
];

const sortDirections: Array<{ value: SortDirection; label: string }> = [
  { value: 'desc', label: '倒序' },
  { value: 'asc', label: '正序' },
];

const sortKeys: SortKey[] = [
  'timeline_desc',
  'timeline_asc',
  'imported_desc',
  'imported_asc',
  'filename',
  'filename_asc',
  'filename_desc',
  'size',
  'size_desc',
  'size_asc',
];

export default function SortControls({ sort, onChange }: { sort: SortKey; onChange: (sort: SortKey) => void }) {
  const parts = sortPartsFromKey(sort);
  return (
    <>
      <SidebarSelect label="排序" value={parts.field} options={sortFields} onChange={(value) => onChange(sortKeyFromParts(value as SortField, parts.direction))} />
      <SidebarButtonGroup
        columns={2}
        label="顺序"
        value={parts.direction}
        options={sortDirections}
        onChange={(value) => onChange(sortKeyFromParts(parts.field, value))}
      />
    </>
  );
}

export function isSortKey(value: string | null): value is SortKey {
  return sortKeys.includes(value as SortKey);
}

export function sortPartsFromKey(sort: SortKey): { field: SortField; direction: SortDirection } {
  switch (sort) {
    case 'timeline_asc':
      return { field: 'timeline', direction: 'asc' };
    case 'imported_asc':
      return { field: 'imported', direction: 'asc' };
    case 'imported_desc':
      return { field: 'imported', direction: 'desc' };
    case 'filename':
    case 'filename_asc':
      return { field: 'filename', direction: 'asc' };
    case 'filename_desc':
      return { field: 'filename', direction: 'desc' };
    case 'size_asc':
      return { field: 'size', direction: 'asc' };
    case 'size':
    case 'size_desc':
      return { field: 'size', direction: 'desc' };
    default:
      return { field: 'timeline', direction: 'desc' };
  }
}

export function sortKeyFromParts(field: SortField, direction: SortDirection): SortKey {
  if (field === 'timeline') return direction === 'asc' ? 'timeline_asc' : 'timeline_desc';
  if (field === 'imported') return direction === 'asc' ? 'imported_asc' : 'imported_desc';
  if (field === 'filename') return direction === 'asc' ? 'filename_asc' : 'filename_desc';
  return direction === 'asc' ? 'size_asc' : 'size_desc';
}
