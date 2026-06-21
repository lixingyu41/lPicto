import { assetOriginalUrl, assetPreviewUrl, assetThumbUrl } from '../api/client';
import type { Asset } from '../types/api';

type FetchPriority = 'high' | 'low' | 'auto';
type PriorityImage = HTMLImageElement & { fetchPriority?: FetchPriority };

const preloadedImages = new Map<string, HTMLImageElement>();
const maxPreloadedImages = 96;

export function viewerImageUrl(asset: Asset) {
  return asset.browserPlayable ? assetOriginalUrl(asset) : assetPreviewUrl(asset);
}

export function preloadViewerAsset(asset: Asset | undefined, priority: FetchPriority = 'auto') {
  if (!asset || asset.mediaType !== 'image') return;
  if (asset.thumbStatus === 'ready') {
    preloadImageUrl(assetThumbUrl(asset), 'low');
  }
  preloadImageUrl(viewerImageUrl(asset), priority);
}

export function preloadViewerAssets(assets: Array<Asset | undefined>, priority: FetchPriority = 'auto') {
  for (const asset of assets) {
    preloadViewerAsset(asset, priority);
  }
}

function preloadImageUrl(url: string, priority: FetchPriority) {
  if (!url || preloadedImages.has(url)) return;
  const image = new Image();
  image.decoding = 'async';
  (image as PriorityImage).fetchPriority = priority;
  image.src = url;
  preloadedImages.set(url, image);
  trimPreloadedImages();
}

function trimPreloadedImages() {
  while (preloadedImages.size > maxPreloadedImages) {
    const firstUrl = preloadedImages.keys().next().value as string | undefined;
    if (!firstUrl) return;
    preloadedImages.delete(firstUrl);
  }
}
