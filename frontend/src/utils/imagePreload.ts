import { assetOriginalUrl, assetPreviewUrl } from '../api/client';
import type { Asset } from '../types/api';

type FetchPriority = 'high' | 'low' | 'auto';
type PriorityImage = HTMLImageElement & { fetchPriority?: FetchPriority };

const hoverPreloadedImages = new Map<string, HTMLImageElement>();
const maxHoverPreloadedImages = 8;

export function viewerImageUrl(asset: Asset, priority?: 'current' | 'preload') {
  return asset.browserPlayable ? assetOriginalUrl(asset, priority) : assetPreviewUrl(asset, priority);
}

export function preloadViewerAsset(asset: Asset | undefined, priority: FetchPriority = 'auto') {
  if (!asset || asset.mediaType !== 'image') return;
  const url = viewerImageUrl(asset);
  if (!url || hoverPreloadedImages.has(url)) return;
  const image = new Image();
  image.decoding = 'async';
  (image as PriorityImage).fetchPriority = priority;
  image.src = url;
  hoverPreloadedImages.set(url, image);
  while (hoverPreloadedImages.size > maxHoverPreloadedImages) {
    const firstURL = hoverPreloadedImages.keys().next().value as string | undefined;
    if (!firstURL) break;
    const stale = hoverPreloadedImages.get(firstURL);
    hoverPreloadedImages.delete(firstURL);
    if (stale) stale.src = '';
  }
}
