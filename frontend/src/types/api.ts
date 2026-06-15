export type MediaType = 'image' | 'video';
export type AssetKind = 'all' | MediaType;
export type OrientationFilter = 'all' | 'landscape' | 'portrait';
export type SortKey =
  | 'timeline_desc'
  | 'timeline_asc'
  | 'imported_desc'
  | 'imported_asc'
  | 'filename'
  | 'filename_asc'
  | 'filename_desc'
  | 'size'
  | 'size_desc'
  | 'size_asc';

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

export interface SearchAssetsParams {
  q?: string;
  nfo?: string;
  type?: AssetKind;
  sort?: SortKey;
  from?: number;
  to?: number;
  widthMin?: number;
  widthMax?: number;
  heightMin?: number;
  heightMax?: number;
  durationMin?: number;
  durationMax?: number;
  sizeMin?: number;
  sizeMax?: number;
  orientation?: OrientationFilter;
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
  phase: string;
  roots: string[];
  currentRoot: string;
  currentRelPath: string;
  totalFiles: number;
  scannedFiles: number;
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
  imageQueued: number;
  imageCap: number;
  thumbQueued: number;
  thumbCap: number;
  previewQueued: number;
  previewCap: number;
  videoPosterQueued: number;
  videoPosterCap: number;
  videoProxyQueued: number;
  videoProxyCap: number;
  videoQueued: number;
  videoCap: number;
  activeThumb: number;
  activeTranscode: number;
}

export interface CacheStats {
  sizeBytes: number;
  fileCount: number;
  updatedAt: number;
  refreshing: boolean;
}

export interface ProcessingProgress {
  assetTotal: number;
  imageTotal: number;
  videoTotal: number;
  thumb: WorkStatusCounts;
  transcode: WorkStatusCounts;
  preview: WorkStatusCounts;
  videoPoster: WorkStatusCounts;
  videoProxy: WorkStatusCounts;
  queue: QueueStats;
  cache: CacheStats;
  active: boolean;
  updatedAt: number;
  refreshing: boolean;
}

export interface CleanupStatus {
  running: boolean;
  status: string;
  lastError: string;
  updatedAt: number;
}

export interface SettingsActivity {
  scan: ScanStatus;
  progress: ProcessingProgress;
  cleanup: CleanupStatus;
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
  progress: ScanLibraryProgress;
}

export interface ScanLibraryProgress {
  assetTotal: number;
  scannedFiles: number;
  unscannedFiles: number;
  thumb: WorkStatusCounts;
  transcode: WorkStatusCounts;
  active: boolean;
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

export interface LibraryAnchorsResponse {
  items: LibraryAnchor[];
  total: number;
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

export interface AlbumGroup {
  id: number;
  name: string;
  createdAt: number;
  updatedAt: number;
}

export interface Album {
  id: number;
  name: string;
  groupId: number | null;
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

export interface AlbumsResponse {
  items: Album[];
  groups: AlbumGroup[];
}

export interface AssetSidecars {
  nfo: NFOInfo | null;
  subtitles: SubtitleInfo[];
  defaultSubtitleId: string | null;
}

export interface NFOInfo {
  filename: string;
  fields: Record<string, string>;
  groups: NFOGroup[];
  text: string;
}

export interface NFOGroup {
  title: string;
  items: NFOField[];
}

export interface NFOField {
  key: string;
  label: string;
  value: string;
  copyable: boolean;
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
