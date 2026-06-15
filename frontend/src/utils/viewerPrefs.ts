export interface ViewerPrefs {
  playbackRate: number;
  subtitlesEnabled: boolean;
  videoAutoplay: boolean;
  zoomMode: ViewerZoomMode;
  zoomScale: number;
  zoomPixelArea: number;
}

export type ViewerZoomMode = 'scale' | 'pixels';

export const playbackRates = [0.5, 1, 1.5, 2, 3] as const;

const playbackRateKey = 'lpicto.playbackRate';
const subtitlesEnabledKey = 'lpicto.subtitlesEnabled';
const zoomModeKey = 'lpicto.zoomMode';
const zoomScaleKey = 'lpicto.zoomScale';
const zoomPixelAreaKey = 'lpicto.zoomPixelArea';
const videoAutoplayKey = 'lpicto.videoAutoplay';
export const viewerPrefsChanged = 'lpicto-prefs-changed';

export function loadViewerPrefs(): ViewerPrefs {
  return {
    playbackRate: loadPlaybackRate(),
    subtitlesEnabled: loadBoolean(subtitlesEnabledKey, true),
    videoAutoplay: localStorage.getItem(videoAutoplayKey) === 'true',
    zoomMode: loadZoomMode(),
    zoomScale: loadNumber(zoomScaleKey, 1.5, 8, 2.6),
    zoomPixelArea: loadNumber(zoomPixelAreaKey, 50, 2000, 300),
  };
}

export function saveViewerPrefs(prefs: ViewerPrefs) {
  localStorage.setItem(playbackRateKey, String(normalizePlaybackRate(prefs.playbackRate)));
  localStorage.setItem(subtitlesEnabledKey, String(prefs.subtitlesEnabled));
  localStorage.setItem(videoAutoplayKey, String(prefs.videoAutoplay));
  localStorage.setItem(zoomModeKey, prefs.zoomMode);
  localStorage.setItem(zoomScaleKey, String(clampNumber(prefs.zoomScale, 1.5, 8, 2.6)));
  localStorage.setItem(zoomPixelAreaKey, String(clampNumber(prefs.zoomPixelArea, 50, 2000, 300)));
  window.dispatchEvent(new Event(viewerPrefsChanged));
}

export function nextPlaybackRate(current: number) {
  const normalized = normalizePlaybackRate(current);
  const index = playbackRates.findIndex((value) => value === normalized);
  return playbackRates[(index + 1) % playbackRates.length] ?? 1;
}

export function normalizePlaybackRate(value: number) {
  return playbackRates.reduce((nearest, rate) => (Math.abs(rate - value) < Math.abs(nearest - value) ? rate : nearest), 1);
}

function loadZoomMode(): ViewerZoomMode {
  return localStorage.getItem(zoomModeKey) === 'pixels' ? 'pixels' : 'scale';
}

function loadPlaybackRate() {
  return normalizePlaybackRate(loadNumber(playbackRateKey, 0.5, 3, 1));
}

function loadBoolean(key: string, fallback: boolean) {
  const raw = localStorage.getItem(key);
  if (raw === null) return fallback;
  return raw === 'true';
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
