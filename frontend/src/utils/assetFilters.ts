import type { Album, AlbumSource, Asset, AssetKind, AssetRating, OrientationFilter } from '../types/api';

export function assetMatchesLibrary(asset: Asset, type: AssetKind, query: string, rating?: AssetRating, orientation?: OrientationFilter) {
  return (
    asset.thumbStatus === 'ready' &&
    matchesType(asset, type) &&
    matchesQuery(asset, query) &&
    matchesRating(asset, rating) &&
    matchesOrientationFilter(asset, orientation)
  );
}

export function assetMatchesRating(asset: Asset, rating: AssetRating, type: AssetKind, query: string, orientation?: OrientationFilter) {
  return assetMatchesLibrary(asset, type, query, rating, orientation);
}

export function assetMatchesFolder(
  asset: Asset,
  folderRelPath: string,
  recursive: boolean,
  query: string,
  rating?: AssetRating,
  orientation?: OrientationFilter,
) {
  if (asset.thumbStatus !== 'ready' || !matchesQuery(asset, query) || !matchesRating(asset, rating) || !matchesOrientationFilter(asset, orientation)) return false;
  if (recursive) {
    return folderRelPath === '' || asset.parentRelPath === folderRelPath || asset.parentRelPath.startsWith(`${folderRelPath}/`);
  }
  return asset.parentRelPath === folderRelPath;
}

export function assetMatchesAlbum(asset: Asset, album: Album | null, query: string, rating?: AssetRating, orientation?: OrientationFilter, type?: AssetKind) {
  if (
    !album ||
    asset.thumbStatus !== 'ready' ||
    !matchesType(asset, type ?? 'all') ||
    !matchesQuery(asset, query) ||
    !matchesRating(asset, rating) ||
    !matchesOrientationFilter(asset, orientation)
  ) {
    return false;
  }
  if (album.mediaTypeFilter !== 'all' && asset.mediaType !== album.mediaTypeFilter) return false;
  if (!matchesOrientation(asset, album.orientationFilter)) return false;
  return album.sources.some((source) => assetMatchesAlbumSource(asset, source));
}

export function assetMatchesAnyAlbum(asset: Asset, albums: Album[]) {
  return albums.some((album) => assetMatchesAlbum(asset, album, ''));
}

function assetMatchesAlbumSource(asset: Asset, source: AlbumSource) {
  const inFolder = source.recursive
    ? source.relPath === '' || asset.parentRelPath === source.relPath || asset.parentRelPath.startsWith(`${source.relPath}/`)
    : asset.parentRelPath === source.relPath;
  if (!inFolder) return false;
  if (source.mediaTypeFilter !== 'all' && asset.mediaType !== source.mediaTypeFilter) return false;
  return matchesOrientation(asset, source.orientationFilter);
}

function matchesOrientation(asset: Asset, orientation: Album['orientationFilter']) {
  if (orientation === 'all') return true;
  const width = effectiveWidth(asset);
  const height = effectiveHeight(asset);
  if (!width || !height) return false;
  return orientation === 'landscape' ? width >= height : height > width;
}

function matchesOrientationFilter(asset: Asset, orientation: OrientationFilter | undefined) {
  if (!orientation || orientation === 'all') return true;
  const width = effectiveWidth(asset);
  const height = effectiveHeight(asset);
  if (!width || !height) return false;
  return orientation === 'landscape' ? width > height : height > width;
}

function matchesType(asset: Asset, type: AssetKind) {
  return type === 'all' || asset.mediaType === type;
}

function matchesQuery(asset: Asset, query: string) {
  const normalized = query.trim().toLowerCase();
  return normalized === '' || asset.filename.toLowerCase().includes(normalized);
}

function matchesRating(asset: Asset, rating: AssetRating | undefined) {
  return rating === undefined || asset.rating === rating;
}

function effectiveWidth(asset: Asset) {
  if (asset.rotation === 90 || asset.rotation === 270) return asset.height;
  return asset.width;
}

function effectiveHeight(asset: Asset) {
  if (asset.rotation === 90 || asset.rotation === 270) return asset.width;
  return asset.height;
}
