export interface ViewerPrefs {
  videoAutoplay: boolean;
  zoomMode: ViewerZoomMode;
  zoomScale: number;
  zoomPixelArea: number;
}

export type ViewerZoomMode = 'scale' | 'pixels';

const zoomModeKey = 'lpicto.zoomMode';
const zoomScaleKey = 'lpicto.zoomScale';
const zoomPixelAreaKey = 'lpicto.zoomPixelArea';
const videoAutoplayKey = 'lpicto.videoAutoplay';
export const viewerPrefsChanged = 'lpicto-prefs-changed';

export function loadViewerPrefs(): ViewerPrefs {
  return {
    videoAutoplay: localStorage.getItem(videoAutoplayKey) === 'true',
    zoomMode: loadZoomMode(),
    zoomScale: loadNumber(zoomScaleKey, 1.5, 8, 2.6),
    zoomPixelArea: loadNumber(zoomPixelAreaKey, 50, 2000, 300),
  };
}

export function saveViewerPrefs(prefs: ViewerPrefs) {
  localStorage.setItem(videoAutoplayKey, String(prefs.videoAutoplay));
  localStorage.setItem(zoomModeKey, prefs.zoomMode);
  localStorage.setItem(zoomScaleKey, String(clampNumber(prefs.zoomScale, 1.5, 8, 2.6)));
  localStorage.setItem(zoomPixelAreaKey, String(clampNumber(prefs.zoomPixelArea, 50, 2000, 300)));
  window.dispatchEvent(new Event(viewerPrefsChanged));
}

function loadZoomMode(): ViewerZoomMode {
  return localStorage.getItem(zoomModeKey) === 'pixels' ? 'pixels' : 'scale';
}

function loadNumber(key: string, min: number, max: number, fallback: number) {
  const raw = localStorage.getItem(key);
  if (raw === null) return fallback;
  return clampNumber(Number(raw), min, max, fallback);
}

function clampNumber(value: number, min: number, max: number, fallback: number) {
  if (!Number.isFinite(value)) return fallback;
  return Math.min(max, Math.max(min, value));
}
