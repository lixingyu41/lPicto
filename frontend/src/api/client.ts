import type {
  Album,
  AlbumGroup,
  AlbumSourceInput,
  AlbumsResponse,
  Asset,
  AlbumAssetFilter,
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
  Page,
  PublicConfig,
  ProcessingProgress,
  ScanCommandResponse,
  ScanRun,
  SearchAssetsParams,
  ScanFolder,
  ScanLibrariesResponse,
  SettingsActivity,
  ScanFoldersResponse,
  ScanStatus,
  SortKey,
  SourceFoldersResponse,
} from '../types/api';

interface APIErrorBody {
  error?: {
    code: string;
    message: string;
  };
}

const requestTimeoutMs = 30_000;

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const controller = new AbortController();
  const upstreamSignal = init?.signal;
  let timedOut = false;
  const timeoutID = window.setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, requestTimeoutMs);
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

export const api = {
  health: () => request<{ status: string }>('/api/health'),
  publicConfig: () => request<PublicConfig>('/api/config/public'),
  triggerScan: () => request<ScanCommandResponse>('/api/scan', { method: 'POST' }),
  countScan: () => request<ScanCommandResponse>('/api/scan/count', { method: 'POST' }),
  metadataScan: () => request<ScanCommandResponse>('/api/scan/metadata', { method: 'POST' }),
  pauseScan: () => request<ScanCommandResponse>('/api/scan/pause', { method: 'POST' }),
  rebuildScan: () => request<ScanCommandResponse>('/api/scan/rebuild?force=1', { method: 'POST' }),
  rebuildThumbnails: () => request<ScanCommandResponse>('/api/scan/thumbnails/rebuild?force=1', { method: 'POST' }),
  scanStatus: () => request<ScanStatus>('/api/scan/status'),
  scanRuns: (page = 1, pageSize = 20) => request<Page<ScanRun>>(`/api/scan/runs${qs({ page, pageSize })}`),
  settingsProgress: () => request<ProcessingProgress>('/api/settings/progress'),
  settingsActivity: () => request<SettingsActivity>('/api/settings/activity'),
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
  albumAssets: (id: number, page: number, pageSize: number, sort: SortKey, q: string, group?: AssetServerGroup, rating?: AssetRating) =>
    request<Page<Asset>>(`/api/albums/${id}/assets${qs({ page, pageSize, sort, q, group, rating })}`),
  albumAnchors: (id: number, pageSize: number, sort: SortKey, q: string, group?: AssetServerGroup, rating?: AssetRating) =>
    request<LibraryAnchorsResponse>(`/api/albums/${id}/anchors${qs({ pageSize, sort, q, group, rating })}`),
  albumSourceFolders: (parentRelPath: string) =>
    request<SourceFoldersResponse>(`/api/albums/source-folders${qs({ parentRelPath })}`),
  libraryAssets: (
    page: number,
    pageSize: number,
    type: AssetKind,
    sort: SortKey,
    q: string,
    group?: AssetServerGroup,
    rating?: AssetRating,
    albumId?: number,
    albumFilter?: AlbumAssetFilter,
  ) => request<Page<Asset>>(`/api/library/assets${qs({ page, pageSize, type, sort, q, group, rating, albumId, albumFilter })}`),
  libraryAnchors: (
    pageSize: number,
    type: AssetKind,
    sort: SortKey,
    q: string,
    group?: AssetServerGroup,
    rating?: AssetRating,
    albumId?: number,
    albumFilter?: AlbumAssetFilter,
  ) => request<LibraryAnchorsResponse>(`/api/library/anchors${qs({ pageSize, type, sort, q, group, rating, albumId, albumFilter })}`),
  searchAssets: (page: number, pageSize: number, params: SearchAssetsParams) =>
    request<Page<Asset>>(`/api/search/assets${qs({ page, pageSize, ...params })}`),
  searchAnchors: (pageSize: number, params: SearchAssetsParams) =>
    request<LibraryAnchorsResponse>(`/api/search/anchors${qs({ pageSize, ...params })}`),
  searchNFOOptions: (field: NFOFilterField, q: string, signal?: AbortSignal) =>
    request<{ items: string[] }>(`/api/search/nfo-options${qs({ field, q, limit: 40 })}`, { signal }),
  folders: (parentId: number) => request<{ items: Folder[] }>(`/api/folders${qs({ parentId })}`),
  folderTree: () => request<{ items: Folder[] }>('/api/folders/tree'),
  folder: (id: number) => request<Folder>(`/api/folders/${id}`),
  folderAssets: (id: number, page: number, pageSize: number, sort: SortKey, q: string, recursive: boolean, group?: AssetServerGroup, rating?: AssetRating) =>
    request<Page<Asset>>(`/api/folders/${id}/assets${qs({ page, pageSize, sort, q, recursive: recursive ? 1 : 0, group, rating })}`),
  folderAnchors: (id: number, pageSize: number, sort: SortKey, q: string, recursive: boolean, group?: AssetServerGroup, rating?: AssetRating) =>
    request<LibraryAnchorsResponse>(`/api/folders/${id}/anchors${qs({ pageSize, sort, q, recursive: recursive ? 1 : 0, group, rating })}`),
  asset: (id: number) => request<Asset>(`/api/assets/${id}`),
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
  neighbors: (id: number, params: Record<string, string | number | undefined | null>, signal?: AbortSignal) =>
    request<Neighbors>(`/api/assets/${id}/neighbors${qs(params)}`, { signal }),
  assetPosition: (id: number, params: Record<string, string | number | undefined | null>) =>
    request<AssetPosition>(`/api/assets/${id}/position${qs(params)}`),
};

export function assetThumbUrl(asset: Asset): string {
  return `/api/cache/thumbs/${asset.cacheKey}.webp`;
}

export function assetPreviewUrl(asset: Asset): string {
  if (asset.mediaType === 'video') {
    return assetThumbUrl(asset);
  }
  if (asset.browserPlayable) {
    return assetOriginalUrl(asset);
  }
  return `/api/assets/${asset.id}/preview?v=${asset.cacheKey}`;
}

export function assetOriginalUrl(asset: Asset): string {
  return `/api/assets/${asset.id}/original?v=${asset.cacheKey}`;
}

export function assetVideoUrl(asset: Asset): string {
  return `/api/assets/${asset.id}/video?v=${asset.cacheKey}`;
}

export function assetVideoProxyUrl(asset: Asset): string {
  return `/api/assets/${asset.id}/video-proxy?v=${asset.cacheKey}`;
}

export function assetSubtitleUrl(asset: Asset, subtitleId: string): string {
  return `/api/assets/${asset.id}/subtitles/${encodeURIComponent(subtitleId)}?v=${asset.cacheKey}`;
}
