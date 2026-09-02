export const assetPosterUpdatedEvent = 'lpicto:asset-poster-updated';

const revisions = new Map<number, number>();
const listeners = new Set<(assetId: number, revision: number) => void>();

function handlePosterUpdated(event: Event) {
  const detail = (event as CustomEvent<{ assetId: number; revision: number }>).detail;
  if (!detail || !Number.isFinite(detail.assetId) || !Number.isFinite(detail.revision)) return;
  listeners.forEach((listener) => listener(detail.assetId, detail.revision));
}

export function assetPosterRevision(assetId: number) {
  return revisions.get(assetId) ?? 0;
}

export function markAssetPosterUpdated(assetId: number) {
  const revision = Date.now();
  revisions.set(assetId, revision);
  window.dispatchEvent(new CustomEvent(assetPosterUpdatedEvent, { detail: { assetId, revision } }));
  return revision;
}

export function subscribeAssetPosterUpdates(listener: (assetId: number, revision: number) => void) {
  if (listeners.size === 0) window.addEventListener(assetPosterUpdatedEvent, handlePosterUpdated);
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) window.removeEventListener(assetPosterUpdatedEvent, handlePosterUpdated);
  };
}
