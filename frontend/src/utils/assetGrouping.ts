import type { Asset, AssetServerGroup, SortKey } from '../types/api';

export type AssetGroupMode = 'none' | 'day' | 'month' | 'year' | 'size' | 'letter' | 'folder';

const assetGroupModes: AssetGroupMode[] = ['none', 'day', 'month', 'year', 'size', 'letter', 'folder'];

export function assetGroupLabel(asset: Asset, mode: AssetGroupMode, sort: SortKey): string {
  const time = sort === 'imported_desc' || sort === 'imported_asc' ? asset.importedAt : asset.timelineAt;
  switch (mode) {
    case 'folder':
      return folderGroupLabel(asset.parentRelPath);
    case 'day':
      return formatDateGroup(time, 'day');
    case 'month':
      return formatDateGroup(time, 'month');
    case 'year':
      return formatDateGroup(time, 'year');
    case 'size':
      return sizeGroupLabel(asset.size);
    case 'letter':
      return firstLetterGroup(asset.filename);
    default:
      return '';
  }
}

export function serverGroupForMode(mode: AssetGroupMode): AssetServerGroup | undefined {
  return mode === 'folder' ? 'folder' : undefined;
}

export function parseAssetGroupMode(value: string | null, fallback: AssetGroupMode = 'none'): AssetGroupMode {
  return assetGroupModes.includes(value as AssetGroupMode) ? (value as AssetGroupMode) : fallback;
}

export function folderGroupLabel(parentRelPath: string): string {
  return parentRelPath ? `/${parentRelPath}` : '全部存储';
}

export function sizeGroupLabel(size: number): string {
  const mb = 1024 * 1024;
  if (size >= 2000 * mb) return '2000M+';
  if (size >= 1000 * mb) return '1000M+';
  if (size >= 500 * mb) return '500M+';
  if (size >= 100 * mb) return '100M+';
  if (size >= 10 * mb) return '10M+';
  if (size >= mb) return '1M+';
  return '<1M';
}

export function firstLetterGroup(filename: string): string {
  const trimmed = filename.trim();
  if (!trimmed) return '#';
  const letter = trimmed[0]?.toUpperCase() ?? '#';
  return /[A-Z0-9]/.test(letter) ? letter : letter;
}

function formatDateGroup(unix: number, unit: 'day' | 'month' | 'year'): string {
  const date = new Date(unix * 1000);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  if (unit === 'year') return String(year);
  if (unit === 'month') return `${year}-${month}`;
  return `${year}-${month}-${day}`;
}
