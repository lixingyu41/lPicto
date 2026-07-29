const viewerPanelWidthKey = 'lpicto.viewerPanelWidth';

export const viewerPanelDefaultWidth = 340;
export const viewerPanelMinWidth = 280;
export const viewerPanelMaxWidth = 560;
export const viewerMediaMinWidth = 480;

export function loadViewerPanelWidth() {
  try {
    const stored = Number(window.localStorage.getItem(viewerPanelWidthKey));
    return normalizeViewerPanelWidth(stored || viewerPanelDefaultWidth);
  } catch {
    return viewerPanelDefaultWidth;
  }
}

export function saveViewerPanelWidth(width: number) {
  try {
    window.localStorage.setItem(viewerPanelWidthKey, String(normalizeViewerPanelWidth(width)));
  } catch {
    // The current session can still use the resized width when storage is unavailable.
  }
}

export function normalizeViewerPanelWidth(width: number) {
  if (!Number.isFinite(width)) return viewerPanelDefaultWidth;
  return Math.min(viewerPanelMaxWidth, Math.max(viewerPanelMinWidth, Math.round(width)));
}

export function fitViewerPanelWidth(width: number, containerWidth: number) {
  const preferred = normalizeViewerPanelWidth(width);
  if (!Number.isFinite(containerWidth) || containerWidth <= 0) return preferred;
  const available = Math.max(viewerPanelMinWidth, containerWidth - viewerMediaMinWidth);
  return Math.min(preferred, available);
}
