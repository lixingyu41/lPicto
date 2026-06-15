import type { Asset, SortKey } from '../types/api';

interface MergeWindowOptions {
  hasMore: boolean;
  loadedStartIndex?: number;
}

export function mergeSortedAssets(current: Asset[], incoming: Asset[], sort: SortKey, options?: MergeWindowOptions) {
  if (incoming.length === 0) return current;
  let next = current;
  const windowSize = current.length;
  const incomingById = new Map<number, Asset>();
  incoming.forEach((asset) => {
    if (asset.thumbStatus === 'ready') incomingById.set(asset.id, asset);
  });
  incomingById.forEach((asset) => {
    const existingIndex = next.findIndex((item) => item.id === asset.id);
    if (existingIndex >= 0) {
      if (sameAsset(next[existingIndex], asset)) return;
      next = next.map((item, index) => (index === existingIndex ? asset : item)).sort((a, b) => compareAssets(a, b, sort));
      return;
    }
    if (!shouldInsertIntoWindow(next, asset, sort, options)) return;
    next = [...next, asset].sort((a, b) => compareAssets(a, b, sort));
    if (options?.hasMore && windowSize > 0 && next.length > windowSize) {
      next = next.slice(0, windowSize);
    }
  });
  return next;
}

export function removeAssetById(current: Asset[], assetId: number) {
  return current.filter((asset) => asset.id !== assetId);
}

export function compareAssets(a: Asset, b: Asset, sort: SortKey) {
  switch (sort) {
    case 'timeline_asc':
      return asc(a.timelineAt, b.timelineAt) || asc(a.id, b.id);
    case 'filename':
    case 'filename_asc':
      return textAsc(a.filename, b.filename) || asc(a.id, b.id);
    case 'filename_desc':
      return textDesc(a.filename, b.filename) || desc(a.id, b.id);
    case 'size':
    case 'size_desc':
      return desc(a.size, b.size) || desc(a.id, b.id);
    case 'size_asc':
      return asc(a.size, b.size) || asc(a.id, b.id);
    case 'imported_asc':
      return asc(a.importedAt, b.importedAt) || asc(a.id, b.id);
    case 'imported_desc':
      return desc(a.importedAt, b.importedAt) || desc(a.id, b.id);
    default:
      return desc(a.timelineAt, b.timelineAt) || desc(a.id, b.id);
  }
}

function asc(a: number, b: number) {
  return a === b ? 0 : a < b ? -1 : 1;
}

function desc(a: number, b: number) {
  return a === b ? 0 : a > b ? -1 : 1;
}

function textAsc(a: string, b: string) {
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' });
}

function textDesc(a: string, b: string) {
  return b.localeCompare(a, undefined, { numeric: true, sensitivity: 'base' });
}

function shouldInsertIntoWindow(current: Asset[], asset: Asset, sort: SortKey, options?: MergeWindowOptions) {
  if (!options) return true;
  if (current.length === 0) return (options.loadedStartIndex ?? 0) === 0;
  if (compareAssets(asset, current[0], sort) < 0) {
    return (options.loadedStartIndex ?? 0) === 0;
  }
  if (compareAssets(asset, current[current.length - 1], sort) > 0) {
    return !options.hasMore;
  }
  return true;
}

function sameAsset(a: Asset, b: Asset) {
  return (
    a.id === b.id &&
    a.filename === b.filename &&
    a.relPath === b.relPath &&
    a.parentRelPath === b.parentRelPath &&
    a.mediaType === b.mediaType &&
    a.mimeType === b.mimeType &&
    a.size === b.size &&
    a.mtime === b.mtime &&
    a.width === b.width &&
    a.height === b.height &&
    a.duration === b.duration &&
    a.takenAt === b.takenAt &&
    a.timelineAt === b.timelineAt &&
    a.importedAt === b.importedAt &&
    a.cacheKey === b.cacheKey &&
    a.browserPlayable === b.browserPlayable &&
    a.thumbStatus === b.thumbStatus &&
    a.previewStatus === b.previewStatus &&
    a.videoPosterStatus === b.videoPosterStatus &&
    a.videoProxyStatus === b.videoProxyStatus &&
    a.rotation === b.rotation
  );
}
