export type MediaType = 'image' | 'video';
export type AssetKind = 'all' | MediaType;
export type SortKey = 'timeline_desc' | 'timeline_asc' | 'filename' | 'size' | 'imported_desc';

export interface Asset {
  id: number;
  filename: string;
  relPath: string;
  parentRelPath: string;
  mediaType: MediaType;
  mimeType: string | null;
  size: number;
  mtime: number;
  width: number | null;
  height: number | null;
  duration: number | null;
  takenAt: number | null;
  timelineAt: number;
  importedAt: number;
  cacheKey: string;
  browserPlayable: boolean;
  thumbStatus: string;
  previewStatus: string;
  videoPosterStatus: string;
  videoProxyStatus: string;
  rotation: number;
}

export interface Folder {
  id: number;
  relPath: string;
  name: string;
  parentRelPath: string | null;
  depth: number;
  assetCount: number;
  recursiveAssetCount: number;
  coverAssetId: number | null;
}

export interface Page<T> {
  items: T[];
  page: number;
  pageSize: number;
  hasMore: boolean;
}

export interface ScanRun {
  id: number;
  status: string;
  startedAt: number;
  finishedAt: number | null;
  totalSeen: number;
  assetsAdded: number;
  assetsUpdated: number;
  assetsDeleted: number;
  errors: number;
  lastError: string | null;
}

export interface ScanStatus {
  running: boolean;
  lastStart: number;
  lastRun: ScanRun | null;
  progress: ScanProgress;
}

export interface ScanProgress {
  reason: string;
  currentRoot: string;
  currentRelPath: string;
  totalSeen: number;
  assetsAdded: number;
  assetsUpdated: number;
  assetsDeleted: number;
  errors: number;
}

export interface WorkStatusCounts {
  total: number;
  ready: number;
  pending: number;
  processing: number;
  error: number;
  notRequired: number;
}

export interface QueueStats {
  thumbQueued: number;
  thumbCap: number;
  videoQueued: number;
  videoCap: number;
}

export interface ProcessingProgress {
  assetTotal: number;
  imageTotal: number;
  videoTotal: number;
  thumb: WorkStatusCounts;
  preview: WorkStatusCounts;
  videoPoster: WorkStatusCounts;
  videoProxy: WorkStatusCounts;
  queue: QueueStats;
}

export interface ScanFolder {
  relPath: string;
  name: string;
  parentRelPath: string | null;
  depth: number;
  exists: boolean;
}

export interface ScanLibrary {
  id: string;
  name: string;
  folders: ScanFolder[];
  exists: boolean;
}

export interface ScanFoldersResponse {
  configured: boolean;
  items: ScanFolder[];
}

export interface ScanLibrariesResponse {
  configured: boolean;
  items: ScanLibrary[];
}

export interface SourceFolder {
  relPath: string;
  name: string;
  parentRelPath: string | null;
  depth: number;
  selected: boolean;
  included: boolean;
}

export interface SourceFoldersResponse {
  current: SourceFolder;
  items: SourceFolder[];
}

export interface Neighbors {
  current: Asset;
  previous: Asset[];
  next: Asset[];
}

export interface LibraryAnchor {
  key: string;
  label: string;
  kind: 'year' | 'month' | 'day' | 'letter' | 'size';
  page: number;
  position: number;
  value: number;
}

export type AlbumMediaFilter = 'all' | MediaType;
export type AlbumOrientationFilter = 'all' | 'landscape' | 'portrait';

export interface AssetPreference {
  assetId: number;
  rotation: number;
  updatedAt: number;
}

export interface AlbumSource {
  id: number;
  sourceType: 'folder';
  relPath: string;
  recursive: boolean;
  mediaTypeFilter: AlbumMediaFilter;
  orientationFilter: AlbumOrientationFilter;
}

export interface Album {
  id: number;
  name: string;
  mediaTypeFilter: AlbumMediaFilter;
  orientationFilter: AlbumOrientationFilter;
  assetCount: number;
  coverAssetId: number | null;
  createdAt: number;
  updatedAt: number;
  sources: AlbumSource[];
}

export interface AlbumSourceInput {
  relPath: string;
  recursive: boolean;
  mediaTypeFilter: AlbumMediaFilter;
  orientationFilter: AlbumOrientationFilter;
}

export interface AssetSidecars {
  nfo: NFOInfo | null;
  subtitles: SubtitleInfo[];
  defaultSubtitleId: string | null;
}

export interface NFOInfo {
  filename: string;
  fields: Record<string, string>;
  text: string;
}

export interface SubtitleInfo {
  id: string;
  filename: string;
  format: string;
  default: boolean;
}

export interface PublicConfig {
  pageSizeDefault: number;
  pageSizeMax: number;
  thumbLongEdge: number;
  previewLongEdge: number;
  videoProxyEnabled: boolean;
  videoProxyMaxHeight: number;
}
