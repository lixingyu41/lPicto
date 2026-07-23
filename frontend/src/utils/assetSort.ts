import type { Asset, SortKey } from '../types/api';
import { assetGroupLabel, type AssetGroupMode } from './assetGrouping';

interface MergeWindowOptions {
  hasMore: boolean;
  loadedStartIndex?: number;
  groupMode?: AssetGroupMode;
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
      next = sortAssets(
        next.map((item, index) => (index === existingIndex ? asset : item)),
        sort,
        options?.groupMode,
      );
      return;
    }
    if (!shouldInsertIntoWindow(next, asset, sort, options)) return;
    next = sortAssets([...next, asset], sort, options?.groupMode);
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
      return filenameAsc(a, b) || asc(a.id, b.id);
    case 'filename_desc':
      return filenameDesc(a, b) || desc(a.id, b.id);
    case 'size':
    case 'size_desc':
      return desc(a.size, b.size) || desc(a.id, b.id);
    case 'size_asc':
      return asc(a.size, b.size) || asc(a.id, b.id);
    case 'imported_asc':
      return asc(a.importedAt, b.importedAt) || asc(a.id, b.id);
    case 'imported_desc':
      return desc(a.importedAt, b.importedAt) || desc(a.id, b.id);
    case 'path_asc': return textAsc(a.relPath, b.relPath) || asc(a.id, b.id);
    case 'path_desc': return textDesc(a.relPath, b.relPath) || desc(a.id, b.id);
    case 'media_type_asc': return textAsc(a.mediaType, b.mediaType) || asc(a.id, b.id);
    case 'media_type_desc': return textDesc(a.mediaType, b.mediaType) || desc(a.id, b.id);
    case 'resolution_asc': return optionalAsc(pixelCount(a), pixelCount(b)) || asc(a.id, b.id);
    case 'resolution_desc': return optionalDesc(pixelCount(a), pixelCount(b)) || desc(a.id, b.id);
    case 'duration_asc': return optionalAsc(a.duration, b.duration) || asc(a.id, b.id);
    case 'duration_desc': return optionalDesc(a.duration, b.duration) || desc(a.id, b.id);
    case 'modified_asc': return asc(a.mtime, b.mtime) || asc(a.id, b.id);
    case 'modified_desc': return desc(a.mtime, b.mtime) || desc(a.id, b.id);
    case 'rating_asc': return asc(a.rating, b.rating) || asc(a.id, b.id);
    case 'rating_desc': return desc(a.rating, b.rating) || desc(a.id, b.id);
    case 'container_asc': return optionalTextAsc(a.container, b.container) || asc(a.id, b.id);
    case 'container_desc': return optionalTextDesc(a.container, b.container) || desc(a.id, b.id);
    case 'video_codec_asc': return optionalTextAsc(a.videoCodec, b.videoCodec) || asc(a.id, b.id);
    case 'video_codec_desc': return optionalTextDesc(a.videoCodec, b.videoCodec) || desc(a.id, b.id);
    case 'audio_codec_asc': return optionalTextAsc(a.audioCodec, b.audioCodec) || asc(a.id, b.id);
    case 'audio_codec_desc': return optionalTextDesc(a.audioCodec, b.audioCodec) || desc(a.id, b.id);
    case 'fps_asc': return optionalAsc(a.fps, b.fps) || asc(a.id, b.id);
    case 'fps_desc': return optionalDesc(a.fps, b.fps) || desc(a.id, b.id);
    case 'bitrate_asc': return optionalAsc(a.overallBitrate, b.overallBitrate) || asc(a.id, b.id);
    case 'bitrate_desc': return optionalDesc(a.overallBitrate, b.overallBitrate) || desc(a.id, b.id);
    case 'subtitle_asc': return asc(Number(a.hasSubtitle), Number(b.hasSubtitle)) || asc(a.id, b.id);
    case 'subtitle_desc': return desc(Number(a.hasSubtitle), Number(b.hasSubtitle)) || desc(a.id, b.id);
    case 'danmaku_asc': return asc(Number(a.hasDanmaku), Number(b.hasDanmaku)) || asc(a.id, b.id);
    case 'danmaku_desc': return desc(Number(a.hasDanmaku), Number(b.hasDanmaku)) || desc(a.id, b.id);
    case 'ai_description_asc': return optionalTextAsc(a.aiDescription, b.aiDescription) || asc(a.id, b.id);
    case 'ai_description_desc': return optionalTextDesc(a.aiDescription, b.aiDescription) || desc(a.id, b.id);
    case 'ai_tag_asc': return optionalTextAsc(topAITag(a), topAITag(b)) || asc(a.id, b.id);
    case 'ai_tag_desc': return optionalTextDesc(topAITag(a), topAITag(b)) || desc(a.id, b.id);
    default:
      return desc(a.timelineAt, b.timelineAt) || desc(a.id, b.id);
  }
}

function sortAssets(assets: Asset[], sort: SortKey, groupMode: AssetGroupMode = 'none') {
  if (groupMode === 'none') return [...assets].sort((a, b) => compareAssets(a, b, sort));
  const leaders = groupMode === 'folder' ? folderLeaders(assets, sort) : undefined;
  return [...assets].sort((a, b) => leaders
    ? compareFolderGroupedAssets(a, b, sort, leaders)
    : compareNativeGroupedAssets(a, b, sort, groupMode));
}

function compareFolderGroupedAssets(a: Asset, b: Asset, sort: SortKey, leaders: Map<string, Asset>) {
  if (a.parentRelPath === b.parentRelPath) {
    return compareAssets(a, b, sort);
  }
  const aLeader = leaders.get(a.parentRelPath) ?? a;
  const bLeader = leaders.get(b.parentRelPath) ?? b;
  return compareAssets(aLeader, bLeader, sort) || textAsc(a.parentRelPath, b.parentRelPath);
}

function folderLeaders(assets: Asset[], sort: SortKey) {
  const leaders = new Map<string, Asset>();
  assets.forEach((asset) => {
    const current = leaders.get(asset.parentRelPath);
    if (!current || compareAssets(asset, current, sort) < 0) {
      leaders.set(asset.parentRelPath, asset);
    }
  });
  return leaders;
}

function compareNativeGroupedAssets(a: Asset, b: Asset, sort: SortKey, groupMode: AssetGroupMode) {
  const aLabel = assetGroupLabel(a, groupMode, sort);
  const bLabel = assetGroupLabel(b, groupMode, sort);
  if (aLabel === bLabel) return compareAssets(a, b, sort);
  const direction = sort.endsWith('_asc') || sort === 'filename' ? 1 : -1;
  if (groupMode === 'size') return (sizeGroupRank(a.size) - sizeGroupRank(b.size)) * direction;
  return textAsc(aLabel, bLabel) * direction;
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

function filenameAsc(a: Asset, b: Asset) {
  return textAsc(filenameKey(a), filenameKey(b)) || textAsc(a.filename, b.filename);
}

function filenameDesc(a: Asset, b: Asset) {
  return textDesc(filenameKey(a), filenameKey(b)) || textDesc(a.filename, b.filename);
}

function filenameKey(asset: Asset) {
  return (asset.filenameSortKey || asset.filename).toLowerCase();
}

function shouldInsertIntoWindow(current: Asset[], asset: Asset, sort: SortKey, options?: MergeWindowOptions) {
  if (!options) return true;
  if (current.length === 0) return (options.loadedStartIndex ?? 0) === 0;
  const scope = [...current, asset];
  const leaders = options.groupMode === 'folder' ? folderLeaders(scope, sort) : undefined;
  const compare = (a: Asset, b: Asset) => leaders
    ? compareFolderGroupedAssets(a, b, sort, leaders)
    : options.groupMode && options.groupMode !== 'none'
      ? compareNativeGroupedAssets(a, b, sort, options.groupMode)
      : compareAssets(a, b, sort);
  if (compare(asset, current[0]) < 0) {
    return (options.loadedStartIndex ?? 0) === 0;
  }
  if (compare(asset, current[current.length - 1]) > 0) {
    return !options.hasMore;
  }
  return true;
}

function pixelCount(asset: Asset) {
  return asset.width && asset.height ? asset.width * asset.height : null;
}

function topAITag(asset: Asset) {
  return asset.aiTags?.[0]?.tag ?? null;
}

function optionalAsc(a: number | null | undefined, b: number | null | undefined) {
  if (a == null) return b == null ? 0 : 1;
  if (b == null) return -1;
  return asc(a, b);
}

function optionalDesc(a: number | null | undefined, b: number | null | undefined) {
  if (a == null) return b == null ? 0 : 1;
  if (b == null) return -1;
  return desc(a, b);
}

function optionalTextAsc(a: string | null | undefined, b: string | null | undefined) {
  if (!a) return !b ? 0 : 1;
  if (!b) return -1;
  return textAsc(a, b);
}

function optionalTextDesc(a: string | null | undefined, b: string | null | undefined) {
  if (!a) return !b ? 0 : 1;
  if (!b) return -1;
  return textDesc(a, b);
}

function sizeGroupRank(size: number) {
  const mb = 1024 * 1024;
  if (size >= 2000 * mb) return 6;
  if (size >= 1000 * mb) return 5;
  if (size >= 500 * mb) return 4;
  if (size >= 100 * mb) return 3;
  if (size >= 10 * mb) return 2;
  if (size >= mb) return 1;
  return 0;
}

function sameAsset(a: Asset, b: Asset) {
  return (
    a.id === b.id &&
    a.filename === b.filename &&
    a.filenameSortKey === b.filenameSortKey &&
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
    a.rotation === b.rotation &&
    a.rating === b.rating
  );
}
