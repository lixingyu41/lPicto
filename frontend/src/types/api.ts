export type MediaType = 'image' | 'video' | 'audio';
export type AssetKind = 'all' | MediaType;
export type OrientationFilter = 'all' | 'landscape' | 'portrait';
export type NFOFilterField = 'actor' | 'id' | 'tag' | 'title' | 'year';
export type AssetServerGroup = 'day' | 'month' | 'year' | 'size' | 'letter' | 'folder';
export type AssetRating = 0 | 1 | 2 | 3 | 4 | 5;
export type AlbumAssetFilter = 'none';
export type SortField =
  | 'timeline'
  | 'imported'
  | 'filename'
  | 'path'
  | 'media_type'
  | 'resolution'
  | 'duration'
  | 'modified'
  | 'size'
  | 'rating'
  | 'container'
  | 'video_codec'
  | 'audio_codec'
  | 'fps'
  | 'bitrate'
  | 'subtitle'
  | 'danmaku'
  | 'ai_description'
  | 'ai_tag';
export type SortKey = 'filename' | 'size' | `${SortField}_${'asc' | 'desc'}`;

export interface Asset {
  id: number;
  filename: string;
  filenameSortKey: string;
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
  rating: AssetRating;
  hidden: boolean;
  sha256: string | null;
  hasSubtitle: boolean;
  hasDanmaku: boolean;
  fps: number | null;
  videoCodec: string | null;
  audioCodec: string | null;
  container: string | null;
  videoBitrate: number | null;
  audioBitrate: number | null;
  overallBitrate: number | null;
  aiDescription?: string | null;
  aiTags?: AssetAITag[];
  manualTags?: AssetTag[];
  palette?: AIColor[];
}

export interface VideoProxyRuntime {
  assetId: number;
  required: boolean;
  cached: boolean;
  transcoding: boolean;
  queued: boolean;
  active: boolean;
  status: string;
  progress: number;
  secondsDone: number;
  duration: number;
  bytes: number;
  expiresAt: number;
  error: string;
  updatedAt: number;
  leaseUntil: number;
  cacheTtl: number;
  keepaliveTtl: number;
  runtimeKey: string;
  startSeconds: number;
  clientId: string;
  sessionId: string;
  sessionState: string;
  activeUsers: number;
  playingUsers: number;
  command: string;
  message: string;
  serverTime: number;
}

export interface AudioProxyRuntime {
  assetId: number;
  required: boolean;
  cached: boolean;
  transcoding: boolean;
  queued: boolean;
  status: 'idle' | 'queued' | 'processing' | 'ready' | 'error';
  progress: number;
  bytes: number;
  error: string;
  updatedAt: number;
}

export interface VideoSegmentStatus {
  assetId: number;
  sessionId: string;
  segmentIndex: number;
  state: string;
  status: string;
  cached: boolean;
  transcoding: boolean;
  queued: boolean;
  progress: number;
  secondsDone: number;
  duration: number;
  bytes: number;
  cachedBytes: number;
  cachedSegments: number;
  segmentCount: number;
  estimatedTotalBytes: number;
  sourceBytes: number;
  error: string;
  message: string;
  updatedAt: number;
  serverTime: number;
}

export interface VideoProxyHeartbeat {
  clientId: string;
  sessionId: string;
  state: 'preparing' | 'playing' | 'paused' | 'stopped';
  currentTime: number;
  playbackRate: number;
  wantsStream: boolean;
  hidden: boolean;
}

export interface VideoProxySettings {
  cacheTtlSeconds: number;
  maxCacheBytes: number;
}

export interface AssetTag {
  assetId: number;
  tag: string;
  createdAt: number;
}

export interface AITagFacet {
  facetKey: string;
  nodeId: string;
  nodeIds: string[];
  labels: string[];
}
export interface AssetAITag {
  tag: string;
  confidence: number;
  categoryKey?: string;
  categoryLabel?: string;
  subjectKey?: string;
  subjectLabel?: string;
  facets?: AITagFacet[];
}
export interface AIColor { hex: string; weight: number; }
export interface AssetAIResult {
  assetId: number;
  inputCacheKey: string;
  status: 'pending' | 'processing' | 'ready' | 'failed';
  description: string;
  tags: AssetAITag[];
  palette: AIColor[];
  attempts: number;
  error?: string;
  sampledFrames: Array<{ ratio: number }>;
  updatedAt?: string;
}
export interface AITagSummary { tag: string; count: number; aiCount: number; manualCount: number; manualAdded: boolean; manualTagId?: number; }
export interface AITagTreeNode {
  id: string;
  parentId?: string;
  label: string;
  depth: number;
  count: number;
  facetKey: string;
  source: 'ai' | 'manual';
}
export interface AIStatus {
  total: number; pending: number; processing: number; ready: number; failed: number; stale: number;
  queued: number; active: number; perMinute: number; etaSeconds: number | null;
  staged: number; stagedBytes: number; sourceReading: boolean; pausedReason: string;
}
export interface AISettings { autoAnalyze: boolean; manualRun: boolean; }
export interface SourceHealth { rootId: string; available: boolean; message?: string; checkedAt: number; }
export interface StorageStatus { available: boolean; message: string; roots: SourceHealth[]; }

export interface SystemTask {
  id: string;
  name: string;
  description: string;
  schedule: string;
  status: 'never' | 'pending' | 'running' | 'success' | 'warning' | 'failed' | 'stopped' | 'interrupted' | 'skipped';
  succeeded: boolean | null;
  lastStartedAt: number | null;
  lastFinishedAt: number | null;
  nextRunAt: number | null;
  durationSeconds: number | null;
  averageSecondsPerItem: number | null;
  message: string;
  blockedReason?: string;
  lastError: string;
  processed: number;
  failedCount: number;
  canRetry: boolean;
  supportsScope: boolean;
  progress: null | { total: number; completed: number; queued: number; pending: number; processing: number; failed: number };
  actions: Array<{ id: string; label: string; kind: 'primary' | 'secondary' | 'danger'; enabled: boolean; requiresConfirmation: boolean }>;
  failures: Array<{ assetId?: number; path: string; reason: string }>;
}

export interface TagSummary {
  id: number;
  name: string;
  assetCount: number;
  createdAt: number;
}

export type SystemCollectionKind = 'unclassified' | 'unrated' | 'untagged' | 'with_danmaku' | 'with_subtitles' | 'needs_transcode' | 'duplicates' | 'missing' | 'hidden' | 'ai_pending' | 'ai_ready' | 'ai_failed';

export interface CollectionRule extends LibraryFilterParams {
  name?: string;
}

export interface Collection {
  id: string;
  name: string;
  kind: 'system' | 'smart';
  systemKind?: SystemCollectionKind;
  description?: string;
  assetCount?: number;
  rule?: CollectionRule;
  createdAt?: number;
  updatedAt?: number;
}

export interface BatchOperationFailure {
  relPath?: string;
  assetId?: number;
  message: string;
}

export interface BatchOperationResult {
  updatedAssetIds: number[];
  deletedAssetIds?: number[];
  failures?: BatchOperationFailure[];
}

export interface DuplicateGroup {
  key: string;
  sha256: string;
  size: number;
  items: Asset[];
}

export interface AssetDeletedEvent {
  id: number;
  relPath: string;
  cacheKey: string;
}

export interface AssetDeleteEntry {
  relPath: string;
  name: string;
  kind: 'file' | 'folder' | 'symlink';
  size: number;
  reason: string;
  isMedia: boolean;
}

export interface AssetDeletePlan {
  asset: Asset;
  mode: 'files' | 'folder';
  token: string;
  canDelete: boolean;
  files: AssetDeleteEntry[];
  folder: AssetDeleteEntry | null;
  folderContents: AssetDeleteEntry[];
  warnings: string[];
  blockers: string[];
}

export interface AssetDeleteFailure {
  relPath: string;
  message: string;
}

export interface AssetDeleteResult {
  deleted: boolean;
  deletedAssetIds: number[];
  failures: AssetDeleteFailure[];
  plan?: AssetDeletePlan;
  stale?: boolean;
}

export interface LibraryFilterParams {
  q?: string;
  visible?: 'all';
  combinedQuery?: string;
  nfo?: string;
  nfoActor?: string;
  nfoId?: string;
  nfoTag?: string;
  manualTag?: string;
  combinedTag?: string;
  combinedTags?: string;
  tagNodes?: string;
  aiDescription?: string;
  aiTag?: string;
  nfoTitle?: string;
  nfoYear?: string;
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
  dimensionMode?: 'both';
  group?: AssetServerGroup;
  rating?: AssetRating;
  albumFilter?: AlbumAssetFilter;
  albumIds?: string;
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
  state: string;
  requestedAction: string;
  task: string;
  reason: string;
  phase: string;
  roots: string[];
  currentRoot: string;
  currentRelPath: string;
  discoveredFiles: number;
  totalFiles: number;
  scannedFiles: number;
  totalSeen: number;
  assetsAdded: number;
  assetsUpdated: number;
  assetsDeleted: number;
  errors: number;
}

export interface ScanCommandResponse {
  accepted: boolean;
  started: boolean;
  paused: boolean;
  state: string;
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
  activePreview: number;
  activeVideoPoster: number;
  activeTranscode: number;
}

export interface CacheStats {
  sizeBytes: number;
  cacheBytes: number;
  databaseBytes: number;
  fileCount: number;
  updatedAt: number;
  refreshing: boolean;
  maxBytes: number;
  minFreeBytes: number;
  freeBytes: number;
  reclaimableBytes: number;
  byKind: Record<string, number>;
}

export interface ProcessingProgress {
  assetTotal: number;
  imageTotal: number;
  videoTotal: number;
  audioTotal: number;
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

export interface MediaLibraryResetResult {
  reset: boolean;
  deletedAssets: number;
  deletedFiles: number;
  releasedBytes: number;
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
  aiFocus: string;
  folders: ScanFolder[];
  exists: boolean;
  progress: ScanLibraryProgress;
}

export interface ScanLibraryProgress {
  assetTotal: number;
  discoveredFiles: number;
  discoveredAt: number | null;
  scannedFiles: number;
  unscannedFiles: number;
  thumb: WorkStatusCounts;
  transcode: WorkStatusCounts;
  videoProxy: WorkStatusCounts;
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
  kind: 'year' | 'month' | 'day' | 'letter' | 'size' | 'folder';
  page: number;
  position: number;
  value: number;
}

export interface LibraryAnchorsResponse {
  items: LibraryAnchor[];
  total: number;
}

export interface AssetPosition {
  index: number;
  page: number;
  position: number;
  total: number;
}

export type AlbumMediaFilter = 'all' | MediaType;
export type AlbumOrientationFilter = 'all' | 'landscape' | 'portrait';

export interface AssetPreference {
  assetId: number;
  rotation: number;
  rating: AssetRating;
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
  liveVideoProxyMaxActive: number;
  videoProxyMaxHeight: number;
  videoSegmentSeconds: number;
  videoPreloadSegments: number;
}
