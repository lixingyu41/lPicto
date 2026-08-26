const mediaZoomOwners = new Set<object>();

function syncMediaZoomClass() {
  if (typeof document === 'undefined') return;
  document.documentElement.classList.toggle('viewer-media-zoom-active', mediaZoomOwners.size > 0);
}

export function setViewerMediaZoomActive(owner: object, active: boolean) {
  if (active) mediaZoomOwners.add(owner);
  else mediaZoomOwners.delete(owner);
  syncMediaZoomClass();
}

export function isViewerMediaZoomActive() {
  if (mediaZoomOwners.size > 0) return true;
  if (typeof document === 'undefined') return false;
  return Boolean(document.querySelector('.image-stage.zooming, .video-stage.video-hold-zoom-active'));
}
