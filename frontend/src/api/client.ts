import type {
  Album,
  AlbumGroup,
  AlbumSourceInput,
  AlbumsResponse,
  Asset,
  AssetKind,
  AssetPreference,
  AssetSidecars,
  Folder,
  LibraryAnchorsResponse,
  Neighbors,
  Page,
  PublicConfig,
  ProcessingProgress,
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

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    headers: { Accept: 'application/json' },
    ...init,
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
  triggerScan: () => request<{ started: boolean }>('/api/scan', { method: 'POST' }),
  pauseScan: () => request<{ paused: boolean }>('/api/scan/pause', { method: 'POST' }),
  rebuildScan: () => request<{ started: boolean }>('/api/scan/rebuild?force=1', { method: 'POST' }),
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
    request<{ started: boolean }>(`/api/settings/libraries/${encodeURIComponent(id)}/scan`, { method: 'POST' }),
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
  refreshAlbum: (id: number) => request<{ started: boolean }>(`/api/albums/${id}/refresh`, { method: 'POST' }),
  albumAssets: (id: number, page: number, pageSize: number, sort: SortKey, q: string) =>
    request<Page<Asset>>(`/api/albums/${id}/assets${qs({ page, pageSize, sort, q })}`),
  albumAnchors: (id: number, pageSize: number, sort: SortKey, q: string) =>
    request<LibraryAnchorsResponse>(`/api/albums/${id}/anchors${qs({ pageSize, sort, q })}`),
  albumSourceFolders: (parentRelPath: string) =>
    request<SourceFoldersResponse>(`/api/albums/source-folders${qs({ parentRelPath })}`),
  libraryAssets: (page: number, pageSize: number, type: AssetKind, sort: SortKey, q: string) =>
    request<Page<Asset>>(`/api/library/assets${qs({ page, pageSize, type, sort, q })}`),
  libraryAnchors: (pageSize: number, type: AssetKind, sort: SortKey, q: string) =>
    request<LibraryAnchorsResponse>(`/api/library/anchors${qs({ pageSize, type, sort, q })}`),
  searchAssets: (page: number, pageSize: number, params: SearchAssetsParams) =>
    request<Page<Asset>>(`/api/search/assets${qs({ page, pageSize, ...params })}`),
  folders: (parentId: number) => request<{ items: Folder[] }>(`/api/folders${qs({ parentId })}`),
  folderTree: () => request<{ items: Folder[] }>('/api/folders/tree'),
  folder: (id: number) => request<Folder>(`/api/folders/${id}`),
  folderAssets: (id: number, page: number, pageSize: number, sort: SortKey, q: string, recursive: boolean) =>
    request<Page<Asset>>(`/api/folders/${id}/assets${qs({ page, pageSize, sort, q, recursive: recursive ? 1 : 0 })}`),
  asset: (id: number) => request<Asset>(`/api/assets/${id}`),
  assetPreferences: (id: number) => request<AssetPreference>(`/api/assets/${id}/preferences`),
  assetSidecars: (id: number) => request<AssetSidecars>(`/api/assets/${id}/sidecars`),
  updateAssetPreferences: (id: number, rotation: number) =>
    request<AssetPreference>(`/api/assets/${id}/preferences`, {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ rotation }),
    }),
  neighbors: (id: number, params: Record<string, string | number | undefined | null>, signal?: AbortSignal) =>
    request<Neighbors>(`/api/assets/${id}/neighbors${qs(params)}`, { signal }),
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
