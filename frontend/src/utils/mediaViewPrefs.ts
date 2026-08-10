export type MediaViewMode = 'waterfall' | 'list' | 'grid';

export type MediaColumnId =
  | 'media'
  | 'path'
  | 'mediaType'
  | 'resolution'
  | 'duration'
  | 'timeline'
  | 'imported'
  | 'modified'
  | 'size'
  | 'rating'
  | 'container'
  | 'videoCodec'
  | 'audioCodec'
  | 'fps'
  | 'bitrate'
  | 'subtitle'
  | 'danmaku'
  | 'aiDescription'
  | 'palette'
  | 'aiTags';

export interface MediaViewPreferences {
  version: number;
  mode: MediaViewMode;
  videoHoverPreview: boolean;
  visibleColumns: MediaColumnId[];
  columnOrder: MediaColumnId[];
  columnWidths: Partial<Record<MediaColumnId, number>>;
}

export interface MediaColumnDefinition {
  id: MediaColumnId;
  label: string;
  defaultWidth: number;
  minWidth: number;
  maxWidth: number;
}

export interface MediaLayoutDefinition {
  id: MediaViewMode;
  label: string;
  description: string;
  configurableColumns: boolean;
}

export const mediaViewPreferencesChanged = 'lpicto-media-view-preferences-changed';

export const mediaLayoutDefinitions: MediaLayoutDefinition[] = [
  {
    id: 'waterfall',
    label: '瀑布流',
    description: '按媒体比例紧凑排列',
    configurableColumns: false,
  },
  {
    id: 'list',
    label: '列表',
    description: '每行一个媒体并显示可配置字段',
    configurableColumns: true,
  },
  {
    id: 'grid',
    label: '网格',
    description: '固定尺寸卡片按行列排列',
    configurableColumns: false,
  },
];

export const mediaColumnDefinitions: MediaColumnDefinition[] = [
  { id: 'media', label: '媒体', defaultWidth: 300, minWidth: 220, maxWidth: 640 },
  { id: 'path', label: '路径', defaultWidth: 320, minWidth: 160, maxWidth: 640 },
  { id: 'mediaType', label: '类型', defaultWidth: 110, minWidth: 72, maxWidth: 240 },
  { id: 'resolution', label: '分辨率', defaultWidth: 120, minWidth: 92, maxWidth: 240 },
  { id: 'duration', label: '时长', defaultWidth: 110, minWidth: 80, maxWidth: 220 },
  { id: 'timeline', label: '拍摄时间', defaultWidth: 180, minWidth: 140, maxWidth: 280 },
  { id: 'imported', label: '导入时间', defaultWidth: 180, minWidth: 140, maxWidth: 280 },
  { id: 'modified', label: '修改时间', defaultWidth: 180, minWidth: 140, maxWidth: 280 },
  { id: 'size', label: '大小', defaultWidth: 110, minWidth: 84, maxWidth: 220 },
  { id: 'rating', label: '评分', defaultWidth: 110, minWidth: 90, maxWidth: 180 },
  { id: 'container', label: '容器', defaultWidth: 120, minWidth: 84, maxWidth: 240 },
  { id: 'videoCodec', label: '视频编码', defaultWidth: 120, minWidth: 90, maxWidth: 280 },
  { id: 'audioCodec', label: '音频编码', defaultWidth: 120, minWidth: 90, maxWidth: 280 },
  { id: 'fps', label: '帧率', defaultWidth: 110, minWidth: 80, maxWidth: 180 },
  { id: 'bitrate', label: '码率', defaultWidth: 120, minWidth: 90, maxWidth: 220 },
  { id: 'subtitle', label: '字幕', defaultWidth: 100, minWidth: 72, maxWidth: 160 },
  { id: 'danmaku', label: '弹幕', defaultWidth: 100, minWidth: 72, maxWidth: 160 },
  { id: 'aiDescription', label: 'AI 描述', defaultWidth: 360, minWidth: 180, maxWidth: 640 },
  { id: 'palette', label: '配色', defaultWidth: 150, minWidth: 110, maxWidth: 240 },
  { id: 'aiTags', label: '标签', defaultWidth: 320, minWidth: 180, maxWidth: 640 },
];

const storageKey = 'lpicto.mediaViewPreferences.v1';
const defaultOrder = mediaColumnDefinitions.map((column) => column.id);
const defaultVisible: MediaColumnId[] = ['media', 'timeline', 'size', 'rating'];

export const defaultMediaViewPreferences: MediaViewPreferences = {
  version: 4,
  mode: 'waterfall',
  videoHoverPreview: true,
  visibleColumns: defaultVisible,
  columnOrder: defaultOrder,
  columnWidths: {},
};

export function loadMediaViewPreferences(): MediaViewPreferences {
  try {
    const parsed = JSON.parse(localStorage.getItem(storageKey) ?? '') as Partial<MediaViewPreferences>;
    return normalizeMediaViewPreferences(parsed);
  } catch {
    return cloneDefaults();
  }
}

export function saveMediaViewPreferences(preferences: MediaViewPreferences) {
  const normalized = normalizeMediaViewPreferences(preferences);
  localStorage.setItem(storageKey, JSON.stringify(normalized));
  window.dispatchEvent(new CustomEvent(mediaViewPreferencesChanged, { detail: normalized }));
}

export function normalizeMediaViewPreferences(value: Partial<MediaViewPreferences> | null | undefined): MediaViewPreferences {
  const known = new Set<MediaColumnId>(defaultOrder);
  const requestedOrder = Array.isArray(value?.columnOrder) ? value.columnOrder.filter((id): id is MediaColumnId => known.has(id as MediaColumnId)) : [];
  const columnOrder = [...requestedOrder, ...defaultOrder.filter((id) => !requestedOrder.includes(id))];
  const requestedVisible = Array.isArray(value?.visibleColumns) ? value.visibleColumns.filter((id): id is MediaColumnId => known.has(id as MediaColumnId)) : [];
  if (value?.version !== 3 && value?.version !== 4) {
    if (!requestedVisible.includes('aiTags')) requestedVisible.push('aiTags');
    if (!requestedVisible.includes('palette')) requestedVisible.push('palette');
  }
  const visibleColumns = Array.from(new Set<MediaColumnId>(['media', ...(requestedVisible.length > 0 ? requestedVisible : defaultVisible)]));
  const columnWidths: Partial<Record<MediaColumnId, number>> = {};
  mediaColumnDefinitions.forEach((definition) => {
    const width = Number(value?.columnWidths?.[definition.id]);
    if (Number.isFinite(width)) {
      columnWidths[definition.id] = Math.round(Math.min(definition.maxWidth, Math.max(definition.minWidth, width)));
    }
  });
  return {
    version: 4,
    mode: value?.mode === 'list' || value?.mode === 'grid' ? value.mode : 'waterfall',
    videoHoverPreview: value?.videoHoverPreview !== false,
    visibleColumns,
    columnOrder,
    columnWidths,
  };
}

export function orderedVisibleColumns(preferences: MediaViewPreferences) {
  const visible = new Set(preferences.visibleColumns);
  return preferences.columnOrder.filter((id) => visible.has(id));
}

export function mediaColumnDefinition(id: MediaColumnId) {
  return mediaColumnDefinitions.find((column) => column.id === id) ?? mediaColumnDefinitions[0];
}

export function mediaColumnWidth(preferences: MediaViewPreferences, id: MediaColumnId) {
  return preferences.columnWidths[id] ?? mediaColumnDefinition(id).defaultWidth;
}

export function mediaLayoutDefinition(id: MediaViewMode) {
  return mediaLayoutDefinitions.find((layout) => layout.id === id) ?? mediaLayoutDefinitions[0];
}

function cloneDefaults(): MediaViewPreferences {
  return {
    ...defaultMediaViewPreferences,
    visibleColumns: [...defaultMediaViewPreferences.visibleColumns],
    columnOrder: [...defaultMediaViewPreferences.columnOrder],
    columnWidths: {},
  };
}
