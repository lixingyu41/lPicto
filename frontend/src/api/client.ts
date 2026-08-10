import type {
  Album,
  AlbumGroup,
  AlbumSourceInput,
  AlbumsResponse,
  Asset,
  BatchOperationResult,
  Collection,
  CollectionRule,
  DuplicateGroup,
  AssetDeletePlan,
  AssetDeleteResult,
  AlbumAssetFilter,
  AssetTag,
	AssetAIResult,
	AITagSummary,
	AITagTreeNode,
	AIStatus,
	AISettings,
  AssetRating,
  AssetServerGroup,
  AssetKind,
  AssetPosition,
  AssetPreference,
  AssetSidecars,
  Folder,
  LibraryAnchorsResponse,
  Neighbors,
  NFOFilterField,
  OrientationFilter,
  Page,
  PublicConfig,
  ProcessingProgress,
  ScanCommandResponse,
  ScanRun,
  ScanLibrary,
  LibraryFilterParams,
  MediaLibraryResetResult,
  ScanFolder,
  ScanLibrariesResponse,
  SettingsActivity,
  ScanFoldersResponse,
  ScanStatus,
  SortKey,
  SourceFoldersResponse,
  SystemTask,
  TagSummary,
  VideoProxyHeartbeat,
  VideoProxyRuntime,
  VideoProxySettings,
  VideoSegmentStatus,
  VideoStoryboard,
  AudioProxyRuntime,
} from '../types/api';
import { loadMediaViewPreferences } from '../utils/mediaViewPrefs';
import audioCoverUrl from '../assets/audio-cover.png';

interface APIErrorBody {
  error?: {
    code: string;
    message: string;
  };
}

const requestTimeoutMs = 30_000;

export interface VideoProxySessionContext {
  clientId?: string;
  sessionId?: string;
  priority?: 'playback' | 'preload';
}

async function request<T>(url: string, init?: RequestInit, timeoutMs = requestTimeoutMs): Promise<T> {
  const controller = new AbortController();
  const upstreamSignal = init?.signal;
  let timedOut = false;
  const timeoutID = window.setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);
  const abortFromUpstream = () => controller.abort();
  if (upstreamSignal?.aborted) {
    controller.abort();
  } else {
    upstreamSignal?.addEventListener('abort', abortFromUpstream, { once: true });
  }
  try {
    const response = await fetch(url, {
      headers: { Accept: 'application/json' },
      ...init,
      signal: controller.signal,
    });
    if (!response.ok) {
      let message = '请求失败';
      try {
        const body = (await response.json()) as APIErrorBody;
        message = body.error?.message ?? message;
      } catch {
        message = response.statusText || message;
      }
      throw new Error(message);
    }
    return (await response.json()) as T;
  } catch (err) {
    if (timedOut) {
      throw new Error('请求超时');
    }
    throw err;
  } finally {
    window.clearTimeout(timeoutID);
    upstreamSignal?.removeEventListener('abort', abortFromUpstream);
  }
}

async function requestDeleteAsset(url: string, token: string): Promise<AssetDeleteResult> {
  const controller = new AbortController();
  const timeoutID = window.setTimeout(() => controller.abort(), 120_000);
  try {
    const response = await fetch(url, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
      signal: controller.signal,
    });
    if (response.status === 409) {
      const body = (await response.json()) as { stale: boolean; plan: AssetDeletePlan };
      return { deleted: false, deletedAssetIds: [], failures: [], stale: true, plan: body.plan };
    }
    if (!response.ok) {
      let message = '删除失败';
      try {
        const body = (await response.json()) as APIErrorBody;
        message = body.error?.message ?? message;
      } catch {
        message = response.statusText || message;
      }
      throw new Error(message);
    }
    return (await response.json()) as AssetDeleteResult;
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw new Error('删除超时');
    }
    throw err;
  } finally {
    window.clearTimeout(timeoutID);
  }
}

function qs(params: Record<string, string | number | undefined | null>): string {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      search.set(key, String(value));
    }
  });
  const text = search.toString();
  return text ? `?${text}` : '';
}

function includeAISummary() {
  return loadMediaViewPreferences().mode === 'list' ? 1 : undefined;
}

export const api = {
  health: () => request<{ status: string }>('/api/health'),
  storageStatus: () => request<import('../types/api').StorageStatus>('/api/storage/status'),
  publicConfig: () => request<PublicConfig>('/api/config/public'),
  triggerScan: () => request<ScanCommandResponse>('/api/scan', { method: 'POST' }),
  countScan: () => request<ScanCommandResponse>('/api/scan/count', { method: 'POST' }),
  metadataScan: () => request<ScanCommandResponse>('/api/scan/metadata', { method: 'POST' }),
  pauseScan: () => request<ScanCommandResponse>('/api/scan/pause', { method: 'POST' }),
  rebuildScan: () => request<ScanCommandResponse>('/api/scan/rebuild?force=1', { method: 'POST' }),
  rebuildThumbnails: () => request<ScanCommandResponse>('/api/scan/thumbnails/rebuild?force=1', { method: 'POST' }),
  continueThumbnails: () => request<ScanCommandResponse>('/api/scan/thumbnails/continue', { method: 'POST' }),
  scanStatus: () => request<ScanStatus>('/api/scan/status'),
  scanRuns: (page = 1, pageSize = 20) => request<Page<ScanRun>>(`/api/scan/runs${qs({ page, pageSize })}`),
  settingsProgress: () => request<ProcessingProgress>('/api/settings/progress'),
  settingsActivity: () => request<SettingsActivity>('/api/settings/activity'),
  resetMediaLibrary: (confirmation: string) =>
    request<MediaLibraryResetResult>('/api/settings/media-library/reset', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ confirmation }),
    }, 180_000),
  systemTasks: () => request<{ items: SystemTask[] }>('/api/settings/tasks'),
  runSystemTask: (id: string, action: string, libraryId: string | null) =>
    request<{ accepted: boolean; count?: number; state?: string }>(`/api/settings/tasks/${encodeURIComponent(id)}/run`, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, libraryId }),
    }),
  stopSystemTask: (id: string) =>
    request<{ accepted: boolean; state?: string }>(`/api/settings/tasks/${encodeURIComponent(id)}/stop`, { method: 'POST' }),
  videoProxySettings: () => request<VideoProxySettings>('/api/settings/video-proxy'),
  updateVideoProxySettings: (settings: VideoProxySettings) =>
    request<VideoProxySettings>('/api/settings/video-proxy', {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(settings),
    }),
  scanLibraries: () => request<ScanLibrariesResponse>('/api/settings/libraries'),
  createScanLibrary: (name: string, relPaths: string[]) =>
    request<ScanLibrariesResponse & { started: boolean }>('/api/settings/libraries', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, relPaths }),
    }),
  updateScanLibrary: (id: string, name: string, relPaths: string[]) =>
    request<ScanLibrariesResponse & { started: boolean }>(`/api/settings/libraries/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, relPaths }),
    }),
  removeScanLibrary: (id: string) =>
    request<ScanLibrariesResponse & { started: boolean; cleanupQueued?: boolean }>(`/api/settings/libraries/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  updateScanLibraryAIFocus: (id: string, focus: string) =>
    request<ScanLibrary>(`/api/settings/libraries/${encodeURIComponent(id)}/ai-focus`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ focus }),
    }),
  reindexScanLibraryAI: (id: string) =>
    request<{ accepted: boolean; count: number; libraryId: string }>(`/api/settings/libraries/${encodeURIComponent(id)}/ai/reindex`, { method: 'POST' }),
  scanLibrary: (id: string) =>
    request<ScanCommandResponse>(`/api/settings/libraries/${encodeURIComponent(id)}/scan`, { method: 'POST' }),
  countScanLibrary: (id: string) =>
    request<ScanCommandResponse>(`/api/settings/libraries/${encodeURIComponent(id)}/scan/count`, { method: 'POST' }),
  metadataScanLibrary: (id: string) =>
    request<ScanCommandResponse>(`/api/settings/libraries/${encodeURIComponent(id)}/scan/metadata`, { method: 'POST' }),
  rebuildLibraryThumbnails: (id: string) =>
    request<ScanCommandResponse>(`/api/settings/libraries/${encodeURIComponent(id)}/thumbnails/rebuild?force=1`, {
      method: 'POST',
    }),
  continueLibraryThumbnails: (id: string) =>
    request<ScanCommandResponse>(`/api/settings/libraries/${encodeURIComponent(id)}/thumbnails/continue`, {
      method: 'POST',
    }),
  scanFolders: () => request<ScanFoldersResponse>('/api/settings/scan-folders'),
  addScanFolder: (relPath: string) =>
    request<{ items: ScanFolder[] }>('/api/settings/scan-folders', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ relPath }),
    }),
  removeScanFolder: (relPath: string) =>
    request<{ items: ScanFolder[] }>(`/api/settings/scan-folders${qs({ relPath })}`, { method: 'DELETE' }),
  sourceFolders: (parentRelPath: string, excludeLibraryId?: string) =>
    request<SourceFoldersResponse>(`/api/source-folders${qs({ parentRelPath, excludeLibraryId })}`),
  albums: () => request<AlbumsResponse>('/api/albums'),
  album: (id: number) => request<Album>(`/api/albums/${id}`),
  albumGroups: () => request<{ items: AlbumGroup[] }>('/api/album-groups'),
  createAlbumGroup: (name: string) =>
    request<AlbumGroup>('/api/album-groups', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  createAlbum: (name: string, sources: AlbumSourceInput[], groupId: number | null) =>
    request<Album>('/api/albums', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, groupId, sources }),
    }),
  updateAlbum: (id: number, name: string, sources: AlbumSourceInput[], groupId: number | null) =>
    request<Album>(`/api/albums/${id}`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, groupId, sources }),
    }),
  deleteAlbum: (id: number) => request<{ deleted: boolean }>(`/api/albums/${id}`, { method: 'DELETE' }),
  refreshAlbum: (id: number) => request<ScanCommandResponse>(`/api/albums/${id}/refresh`, { method: 'POST' }),
  albumAssets: (
    id: number,
    page: number,
    pageSize: number,
    sort: SortKey,
    q: string,
    group?: AssetServerGroup,
    rating?: AssetRating,
    orientation?: OrientationFilter,
    type?: AssetKind,
    combinedTags?: string,
    tagNodes?: string,
  ) => request<Page<Asset>>(`/api/albums/${id}/assets${qs({ page, pageSize, sort, q, group, rating, orientation, type, combinedTags, tagNodes, includeAiSummary: includeAISummary() })}`),
  albumAnchors: (id: number, pageSize: number, sort: SortKey, q: string, group?: AssetServerGroup, rating?: AssetRating, orientation?: OrientationFilter, type?: AssetKind, combinedTags?: string, tagNodes?: string) =>
    request<LibraryAnchorsResponse>(`/api/albums/${id}/anchors${qs({ pageSize, sort, q, group, rating, orientation, type, combinedTags, tagNodes })}`),
  albumSourceFolders: (parentRelPath: string) =>
    request<SourceFoldersResponse>(`/api/albums/source-folders${qs({ parentRelPath })}`),
  libraryAssets: (page: number, pageSize: number, params: LibraryFilterParams) =>
    request<Page<Asset>>(`/api/library/assets${qs({ page, pageSize, ...params, includeAiSummary: includeAISummary() })}`),
  libraryAnchors: (pageSize: number, params: LibraryFilterParams) =>
    request<LibraryAnchorsResponse>(`/api/library/anchors${qs({ pageSize, ...params })}`),
  libraryNFOOptions: (field: NFOFilterField, q: string, signal?: AbortSignal) =>
    request<{ items: string[] }>(`/api/library/nfo-options${qs({ field, q, limit: 40 })}`, { signal }),
  folders: (parentId: number) => request<{ items: Folder[] }>(`/api/folders${qs({ parentId })}`),
  folderTree: () => request<{ items: Folder[] }>('/api/folders/tree'),
  folderByPath: (relPath: string) => request<Folder>(`/api/folders/by-path${qs({ relPath })}`),
  folder: (id: number) => request<Folder>(`/api/folders/${id}`),
  folderAssets: (id: number, page: number, pageSize: number, sort: SortKey, q: string, recursive: boolean, group?: AssetServerGroup, rating?: AssetRating, orientation?: OrientationFilter, type?: AssetKind, combinedTags?: string, tagNodes?: string) =>
    request<Page<Asset>>(`/api/folders/${id}/assets${qs({ page, pageSize, sort, q, recursive: recursive ? 1 : 0, group, rating, orientation, type, combinedTags, tagNodes, includeAiSummary: includeAISummary() })}`),
  folderAnchors: (id: number, pageSize: number, sort: SortKey, q: string, recursive: boolean, group?: AssetServerGroup, rating?: AssetRating, orientation?: OrientationFilter, type?: AssetKind, combinedTags?: string, tagNodes?: string) =>
    request<LibraryAnchorsResponse>(`/api/folders/${id}/anchors${qs({ pageSize, sort, q, recursive: recursive ? 1 : 0, group, rating, orientation, type, combinedTags, tagNodes })}`),
  tags: () => request<{ items: TagSummary[] }>('/api/tags'),
  createTag: (name: string) =>
    request<TagSummary>('/api/tags', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  updateTag: (id: number, name: string) =>
    request<TagSummary>(`/api/tags/${id}`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  deleteTag: (id: number) => request<{ deleted: boolean }>(`/api/tags/${id}`, { method: 'DELETE' }),
  mergeTags: (sourceTagIds: number[], targetTagId: number, targetName?: string) =>
    request<TagSummary>('/api/tags/merge', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ sourceTagIds, targetTagId, targetName }),
    }),
  collections: () => request<{ items: Collection[] }>('/api/collections'),
  createCollection: (name: string, rule: CollectionRule) =>
    request<Collection>('/api/collections', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, rule }),
    }),
  updateCollection: (id: string, patch: { name?: string; rule?: CollectionRule }) =>
    request<Collection>(`/api/collections/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    }),
  deleteCollection: (id: string) => request<{ deleted: boolean }>(`/api/collections/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  collectionAssets: (
    id: string,
    page: number,
    pageSize: number,
    sort: SortKey,
    q: string,
    group?: AssetServerGroup,
    rating?: AssetRating,
    orientation?: OrientationFilter,
    type?: AssetKind,
    combinedTags: string[] = [],
    tagNodes: string[] = [],
    filenameQuery = '',
  ) => request<Page<Asset>>(`/api/collections/${collectionPathID(id)}/assets${qs({ page, pageSize, sort, q: filenameQuery, combinedQuery: q, group, rating, orientation, type, combinedTags: combinedTags.length > 0 ? JSON.stringify(combinedTags) : undefined, tagNodes: tagNodes.length > 0 ? JSON.stringify(tagNodes) : undefined, includeAiSummary: includeAISummary() })}`),
  collectionAnchors: (id: string, pageSize: number, sort: SortKey, q: string, group?: AssetServerGroup, rating?: AssetRating, orientation?: OrientationFilter, type?: AssetKind, combinedTags: string[] = [], tagNodes: string[] = [], filenameQuery = '') =>
    request<LibraryAnchorsResponse>(`/api/collections/${collectionPathID(id)}/anchors${qs({ pageSize, sort, q: filenameQuery, combinedQuery: q, group, rating, orientation, type, combinedTags: combinedTags.length > 0 ? JSON.stringify(combinedTags) : undefined, tagNodes: tagNodes.length > 0 ? JSON.stringify(tagNodes) : undefined })}`),
  duplicates: () => request<{ items: DuplicateGroup[] }>('/api/duplicates'),
  duplicateSelection: () => request<{ assetIds: number[]; keepPolicy: 'oldest_imported' }>('/api/duplicates/selection'),
  addAlbumAssets: (id: number, assetIds: number[]) =>
    request<BatchOperationResult>(`/api/albums/${id}/assets`, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds }),
    }),
  removeAlbumAssets: (id: number, assetIds: number[]) =>
    request<BatchOperationResult>(`/api/albums/${id}/assets`, {
      method: 'DELETE',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds }),
    }),
  batchAddTags: (assetIds: number[], tags: string[]) =>
    request<BatchOperationResult>('/api/assets/batch/tags', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds, tags }),
    }),
  batchSetRating: (assetIds: number[], rating: AssetRating) =>
    request<BatchOperationResult>('/api/assets/batch/rating', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds, rating }),
    }),
  batchAddToAlbum: (assetIds: number[], albumId: number) =>
    request<BatchOperationResult>('/api/assets/batch/album', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds, albumId }),
    }),
  batchRotate: (assetIds: number[], rotation: number) =>
    request<BatchOperationResult>('/api/assets/batch/rotation', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds, rotation }),
    }),
  batchHide: (assetIds: number[], hidden = true) =>
    request<BatchOperationResult>(hidden ? '/api/assets/batch/hide' : '/api/assets/batch/unhide', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds }),
    }),
  batchDelete: (assetIds: number[], purgeUnavailable = false, refreshCollectionCounts = false) =>
    request<BatchOperationResult>('/api/assets/batch/delete', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds, purgeUnavailable, refreshCollectionCounts }),
    }),
  batchDeleteRecords: (assetIds: number[], refreshCollectionCounts = false) =>
    request<BatchOperationResult>('/api/assets/batch/delete-records', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ assetIds, refreshCollectionCounts }),
    }),
  asset: (id: number) => request<Asset>(`/api/assets/${id}`),
  assetAI: (id: number) => request<AssetAIResult>(`/api/assets/${id}/ai`),
  reanalyzeAssetAI: (id: number) => request<{ accepted: boolean; assetId: number }>(`/api/assets/${id}/ai/reanalyze`, { method: 'POST' }),
  replaceAssetAITag: (id: number, payload: { previousTag?: string; tag: string; categoryKey: string; subjectKey: string }) =>
    request<AssetAIResult>(`/api/assets/${id}/ai/tags`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }),
  deleteAssetAITag: (id: number, tag: string) =>
    request<AssetAIResult>(`/api/assets/${id}/ai/tags${qs({ tag })}`, { method: 'DELETE' }),
  aiStatus: () => request<AIStatus>('/api/ai/status'),
  aiSettings: () => request<AISettings>('/api/ai/settings'),
  updateAISettings: (autoAnalyze: boolean) => request<AISettings>('/api/ai/settings', {
    method: 'PUT',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ autoAnalyze }),
  }),
  runAIManually: () => request<{ accepted: boolean; count: number; settings: AISettings }>('/api/ai/run', { method: 'POST' }),
  stopAIManually: () => request<AISettings>('/api/ai/stop', { method: 'POST' }),
  reindexAI: () => request<{ accepted: boolean; count: number }>('/api/ai/reindex', { method: 'POST' }),
  retryFailedAI: () => request<{ accepted: boolean; count: number }>('/api/ai/retry-failed', { method: 'POST' }),
  aiTags: (q = '', tagNodes: string[] = []) =>
    request<{ items: AITagSummary[]; tree: AITagTreeNode[] }>(`/api/ai/tags${qs({ q, tagNodes: tagNodes.length > 0 ? JSON.stringify(tagNodes) : undefined })}`),
  assetDeletePlan: (id: number) => request<AssetDeletePlan>(`/api/assets/${id}/delete-plan`),
  deleteAsset: (id: number, token: string) => requestDeleteAsset(`/api/assets/${id}/delete`, token),
  deleteAssetRecord: (id: number) => request<AssetDeleteResult>(`/api/assets/${id}/record`, { method: 'DELETE' }),
  markAssetPlayed: (id: number) =>
    request<{ recorded: boolean; lastPlayedAt: number }>(`/api/assets/${id}/played`, { method: 'POST' }),
  assetTags: (id: number) => request<{ items: AssetTag[] }>(`/api/assets/${id}/tags`),
  addAssetTag: (id: number, tag: string) =>
    request<{ items: AssetTag[] }>(`/api/assets/${id}/tags`, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ tag }),
    }),
  removeAssetTag: (id: number, tag: string) =>
    request<{ items: AssetTag[] }>(`/api/assets/${id}/tags${qs({ tag })}`, { method: 'DELETE' }),
  assetPreferences: (id: number) => request<AssetPreference>(`/api/assets/${id}/preferences`),
  assetSidecars: (id: number) => request<AssetSidecars>(`/api/assets/${id}/sidecars`),
  updateAssetPreferences: (id: number, rotation: number) =>
    request<AssetPreference>(`/api/assets/${id}/preferences`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ rotation }),
    }),
  updateAssetRating: (id: number, rating: AssetRating) =>
    request<AssetPreference>(`/api/assets/${id}/preferences`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ rating }),
    }),
  videoProxyStatus: (id: number, startSeconds = 0, session?: VideoProxySessionContext) =>
    request<VideoProxyRuntime>(`/api/assets/${id}/video-proxy/status${videoProxyQuery(startSeconds, session)}`),
  keepVideoProxyAlive: (id: number, startSeconds = 0, heartbeat?: VideoProxyHeartbeat) =>
    request<VideoProxyRuntime>(`/api/assets/${id}/video-proxy/keepalive${videoProxyQuery(startSeconds, heartbeat)}`, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: heartbeat ? JSON.stringify(heartbeat) : undefined,
    }),
  videoSegmentStatus: (id: number, session?: VideoProxySessionContext) =>
    request<VideoSegmentStatus>(`/api/assets/${id}/hls/status${videoSessionQuery(session)}`),
  prewarmVideoSegments: (id: number, from: number, count: number, priority: 'playback' | 'critical' | 'balanced', session?: VideoProxySessionContext, signal?: AbortSignal) =>
    request<{ cachedSegments: number; required: boolean }>(`/api/assets/${id}/hls/prewarm${qs({
      from: Math.max(0, Math.floor(from)),
      count: Math.max(1, Math.floor(count)),
      priority,
      clientId: session?.clientId,
      sessionId: session?.sessionId,
    })}`, { method: 'POST', signal }),
  stopVideoSegmentSession: (id: number, session?: VideoProxySessionContext) =>
    request<{ cancelled: number }>(`/api/assets/${id}/hls/session/stop${videoSessionQuery(session)}`, { method: 'POST' }),
  prewarmDirectVideo: (id: number, startSeconds = 0) =>
    request<{ accepted: boolean; chunks: number }>(`/api/assets/${id}/video/cache/prewarm?start=${Math.max(0, startSeconds)}`, { method: 'POST' }),
  startAudioProxy: (id: number, priority: 'current' | 'preload', signal?: AbortSignal) =>
    request<AudioProxyRuntime>(`/api/assets/${id}/audio-proxy?priority=${priority}`, { method: 'POST', signal }),
  audioProxyStatus: (id: number, signal?: AbortSignal) =>
    request<AudioProxyRuntime>(`/api/assets/${id}/audio-proxy/status`, { signal }),
  neighbors: (id: number, params: Record<string, string | number | undefined | null>, signal?: AbortSignal) =>
    request<Neighbors>(`/api/assets/${id}/neighbors${qs(params)}`, { signal }),
  assetPosition: (id: number, params: Record<string, string | number | undefined | null>) =>
    request<AssetPosition>(`/api/assets/${id}/position${qs(params)}`),
  assetStoryboard: (id: number, signal?: AbortSignal) =>
    request<VideoStoryboard>(`/api/assets/${id}/storyboard`, { signal }),
  generateAssetStoryboard: (id: number, signal?: AbortSignal) =>
    request<{ accepted: boolean; state: string }>(`/api/assets/${id}/storyboard/generate`, { method: 'POST', signal }),
};

export function assetThumbUrl(asset: Asset): string {
  if (asset.mediaType === 'audio') return audioCoverUrl;
  return `/api/cache/thumbs/${asset.cacheKey}.webp`;
}

export function assetPreviewUrl(asset: Asset): string {
  if (asset.mediaType === 'audio') return audioCoverUrl;
  if (asset.mediaType === 'video') {
    return assetThumbUrl(asset);
  }
  if (asset.browserPlayable) {
    return assetOriginalUrl(asset);
  }
  return `/api/assets/${asset.id}/preview?v=${asset.cacheKey}`;
}

export function assetAudioCoverUrl(): string {
  return audioCoverUrl;
}

export function assetAudioUrl(asset: Asset): string {
  return `/api/assets/${asset.id}/audio?v=${asset.cacheKey}`;
}

export function assetOriginalUrl(asset: Asset): string {
  return `/api/assets/${asset.id}/original?v=${asset.cacheKey}`;
}

export function assetVideoUrl(asset: Asset): string {
  return `/api/assets/${asset.id}/video?v=${asset.cacheKey}#t=0.001`;
}

export function assetStoryboardSheetUrl(asset: Asset, sheet: number): string {
  return `/api/assets/${asset.id}/storyboard/${Math.max(0, Math.floor(sheet))}?v=${asset.cacheKey}`;
}

export function assetVideoProxyUrl(asset: Asset, startSeconds = 0, session?: VideoProxySessionContext): string {
  const query = new URLSearchParams({ v: asset.cacheKey, play: '1' });
  if (Number.isFinite(startSeconds) && startSeconds > 0) {
    query.set('start', Math.max(0, startSeconds).toFixed(2));
  }
  if (session?.clientId) {
    query.set('clientId', session.clientId);
  }
  if (session?.sessionId) {
    query.set('sessionId', session.sessionId);
  }
  return `/api/assets/${asset.id}/video-proxy?${query.toString()}`;
}

export function assetVideoHlsPlaylistUrl(asset: Asset, session?: VideoProxySessionContext): string {
  const query = new URLSearchParams({ v: asset.cacheKey });
  if (session?.clientId) {
    query.set('clientId', session.clientId);
  }
  if (session?.sessionId) {
    query.set('sessionId', session.sessionId);
  }
  query.set('priority', session?.priority ?? 'preload');
  return `/api/assets/${asset.id}/hls/playlist.m3u8?${query.toString()}`;
}

export function assetVideoHlsSegmentUrl(asset: Asset, segmentIndex: number, session?: VideoProxySessionContext): string {
  const query = new URLSearchParams({ v: asset.cacheKey, priority: 'neighbor' });
  if (session?.clientId) {
    query.set('clientId', session.clientId);
  }
  if (session?.sessionId) {
    query.set('sessionId', session.sessionId);
  }
  return `/api/assets/${asset.id}/hls/segments/${Math.max(0, Math.floor(segmentIndex))}.ts?${query.toString()}`;
}

function videoProxyQuery(startSeconds: number, session?: VideoProxySessionContext) {
  return qs({
    start: Number.isFinite(startSeconds) && startSeconds > 0 ? Math.max(0, startSeconds).toFixed(2) : undefined,
    clientId: session?.clientId,
    sessionId: session?.sessionId,
  });
}

function videoSessionQuery(session?: VideoProxySessionContext) {
  return qs({
    clientId: session?.clientId,
    sessionId: session?.sessionId,
  });
}

function collectionPathID(id: string) {
  const encoded = encodeURIComponent(id);
  if (id.startsWith('ai-tag:')) return encoded.replace(/^ai-tag%3A/i, 'ai-tag:');
  return id.startsWith('tag:') ? encoded.replace(/^tag%3A/i, 'tag:') : encoded;
}

export function assetSubtitleUrl(asset: Asset, subtitleId: string): string {
  return `/api/assets/${asset.id}/subtitles/${encodeURIComponent(subtitleId)}?v=${asset.cacheKey}`;
}
