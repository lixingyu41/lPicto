export interface ViewerPrefs {
  danmakuDensity: number;
  danmakuFontScale: number;
  danmakuOpacity: number;
  danmakuSpeed: number;
  playbackRate: number;
  playbackMode: ViewerPlaybackMode;
  videoProcessingMode: VideoProcessingMode;
  videoServerTranscoder: VideoServerTranscoder;
  imageSlideshowSeconds: number;
  subtitlesEnabled: boolean;
  videoAutoplay: boolean;
  videoPlaybackDelaySeconds: number;
  zoomMode: ViewerZoomMode;
  zoomScale: number;
  zoomPixelArea: number;
}

export type ViewerZoomMode = 'scale' | 'pixels';
export type ViewerPlaybackMode = 'continuous' | 'single' | 'pause';
export type VideoProcessingMode = 'browser' | 'server';
export type VideoServerTranscoder = 'cpu' | 'gpu';

export const playbackRates = [0.5, 1, 1.5, 2, 3] as const;
export const playbackModeOptions: ReadonlyArray<{ label: string; value: ViewerPlaybackMode }> = [
  { value: 'continuous', label: '连续播放' },
  { value: 'single', label: '单个循环' },
  { value: 'pause', label: '播完暂停' },
];
export const danmakuDensityRange = { min: 0.25, max: 1.5, fallback: 1 } as const;
export const danmakuFontScaleRange = { min: 0.75, max: 1.5, fallback: 1 } as const;
export const danmakuOpacityRange = { min: 0.15, max: 1, fallback: 0.95 } as const;
export const danmakuSpeedRange = { min: 0.5, max: 2, fallback: 1 } as const;
export const zoomScaleRange = { min: 1.5, max: 8, fallback: 2.6 } as const;
export const zoomPixelAreaRange = { min: 50, max: 2000, fallback: 300 } as const;
export const zoomScaleStep = 0.1;
export const zoomPixelAreaStep = 10;
export const imageSlideshowSecondsRange = { min: 1, max: 60, fallback: 3 } as const;
export const videoPlaybackDelaySecondsRange = { min: 0, max: 5, fallback: 0 } as const;

const danmakuDensityKey = 'lpicto.danmakuDensity';
const danmakuFontScaleKey = 'lpicto.danmakuFontScale';
const danmakuOpacityKey = 'lpicto.danmakuOpacity';
const danmakuSpeedKey = 'lpicto.danmakuSpeed';
const playbackRateKey = 'lpicto.playbackRate';
const playbackModeKey = 'lpicto.playbackMode';
const videoProcessingModeKey = 'lpicto.videoProcessingMode';
const videoServerTranscoderKey = 'lpicto.videoServerTranscoder';
const imageSlideshowSecondsKey = 'lpicto.imageSlideshowSeconds';
const subtitlesEnabledKey = 'lpicto.subtitlesEnabled';
const zoomModeKey = 'lpicto.zoomMode';
const zoomScaleKey = 'lpicto.zoomScale';
const zoomPixelAreaKey = 'lpicto.zoomPixelArea';
const videoAutoplayKey = 'lpicto.videoAutoplay';
const videoPlaybackDelaySecondsKey = 'lpicto.videoPlaybackDelaySeconds';
export const viewerPrefsChanged = 'lpicto-prefs-changed';

export function loadViewerPrefs(): ViewerPrefs {
  return {
    danmakuDensity: loadNumber(danmakuDensityKey, danmakuDensityRange.min, danmakuDensityRange.max, danmakuDensityRange.fallback),
    danmakuFontScale: loadNumber(danmakuFontScaleKey, danmakuFontScaleRange.min, danmakuFontScaleRange.max, danmakuFontScaleRange.fallback),
    danmakuOpacity: loadNumber(danmakuOpacityKey, danmakuOpacityRange.min, danmakuOpacityRange.max, danmakuOpacityRange.fallback),
    danmakuSpeed: loadNumber(danmakuSpeedKey, danmakuSpeedRange.min, danmakuSpeedRange.max, danmakuSpeedRange.fallback),
    playbackRate: loadPlaybackRate(),
    playbackMode: loadPlaybackMode(),
    videoProcessingMode: loadVideoProcessingMode(),
    videoServerTranscoder: loadVideoServerTranscoder(),
    imageSlideshowSeconds: loadNumber(imageSlideshowSecondsKey, imageSlideshowSecondsRange.min, imageSlideshowSecondsRange.max, imageSlideshowSecondsRange.fallback),
    subtitlesEnabled: loadBoolean(subtitlesEnabledKey, true),
    videoAutoplay: localStorage.getItem(videoAutoplayKey) === 'true',
    videoPlaybackDelaySeconds: loadNumber(
      videoPlaybackDelaySecondsKey,
      videoPlaybackDelaySecondsRange.min,
      videoPlaybackDelaySecondsRange.max,
      videoPlaybackDelaySecondsRange.fallback,
    ),
    zoomMode: loadZoomMode(),
    zoomScale: loadNumber(zoomScaleKey, zoomScaleRange.min, zoomScaleRange.max, zoomScaleRange.fallback),
    zoomPixelArea: loadNumber(zoomPixelAreaKey, zoomPixelAreaRange.min, zoomPixelAreaRange.max, zoomPixelAreaRange.fallback),
  };
}

export function saveViewerPrefs(prefs: ViewerPrefs) {
  localStorage.setItem(danmakuDensityKey, String(clampNumber(prefs.danmakuDensity, danmakuDensityRange.min, danmakuDensityRange.max, danmakuDensityRange.fallback)));
  localStorage.setItem(
    danmakuFontScaleKey,
    String(clampNumber(prefs.danmakuFontScale, danmakuFontScaleRange.min, danmakuFontScaleRange.max, danmakuFontScaleRange.fallback)),
  );
  localStorage.setItem(danmakuOpacityKey, String(clampNumber(prefs.danmakuOpacity, danmakuOpacityRange.min, danmakuOpacityRange.max, danmakuOpacityRange.fallback)));
  localStorage.setItem(danmakuSpeedKey, String(clampNumber(prefs.danmakuSpeed, danmakuSpeedRange.min, danmakuSpeedRange.max, danmakuSpeedRange.fallback)));
  localStorage.setItem(playbackRateKey, String(normalizePlaybackRate(prefs.playbackRate)));
  localStorage.setItem(playbackModeKey, prefs.playbackMode);
  localStorage.setItem(videoProcessingModeKey, prefs.videoProcessingMode);
  localStorage.setItem(videoServerTranscoderKey, prefs.videoServerTranscoder);
  localStorage.setItem(imageSlideshowSecondsKey, String(clampNumber(prefs.imageSlideshowSeconds, imageSlideshowSecondsRange.min, imageSlideshowSecondsRange.max, imageSlideshowSecondsRange.fallback)));
  localStorage.setItem(subtitlesEnabledKey, String(prefs.subtitlesEnabled));
  localStorage.setItem(videoAutoplayKey, String(prefs.videoAutoplay));
  localStorage.setItem(
    videoPlaybackDelaySecondsKey,
    String(clampNumber(
      prefs.videoPlaybackDelaySeconds,
      videoPlaybackDelaySecondsRange.min,
      videoPlaybackDelaySecondsRange.max,
      videoPlaybackDelaySecondsRange.fallback,
    )),
  );
  localStorage.setItem(zoomModeKey, prefs.zoomMode);
  localStorage.setItem(zoomScaleKey, String(clampNumber(prefs.zoomScale, zoomScaleRange.min, zoomScaleRange.max, zoomScaleRange.fallback)));
  localStorage.setItem(
    zoomPixelAreaKey,
    String(clampNumber(prefs.zoomPixelArea, zoomPixelAreaRange.min, zoomPixelAreaRange.max, zoomPixelAreaRange.fallback)),
  );
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

export function stepViewerZoomPrefs(prefs: ViewerPrefs, direction: 1 | -1): ViewerPrefs {
  if (prefs.zoomMode === 'pixels') {
    return {
      ...prefs,
      zoomPixelArea: clampNumber(
        prefs.zoomPixelArea - direction * zoomPixelAreaStep,
        zoomPixelAreaRange.min,
        zoomPixelAreaRange.max,
        zoomPixelAreaRange.fallback,
      ),
    };
  }
  return {
    ...prefs,
    zoomScale: Math.round(clampNumber(
      prefs.zoomScale + direction * zoomScaleStep,
      zoomScaleRange.min,
      zoomScaleRange.max,
      zoomScaleRange.fallback,
    ) * 10) / 10,
  };
}

function loadZoomMode(): ViewerZoomMode {
  return localStorage.getItem(zoomModeKey) === 'pixels' ? 'pixels' : 'scale';
}

function loadPlaybackRate() {
  return normalizePlaybackRate(loadNumber(playbackRateKey, 0.5, 3, 1));
}

function loadPlaybackMode(): ViewerPlaybackMode {
  const value = localStorage.getItem(playbackModeKey);
  return value === 'continuous' || value === 'single' ? value : 'pause';
}

function loadVideoProcessingMode(): VideoProcessingMode {
  return localStorage.getItem(videoProcessingModeKey) === 'browser' ? 'browser' : 'server';
}

function loadVideoServerTranscoder(): VideoServerTranscoder {
  return localStorage.getItem(videoServerTranscoderKey) === 'gpu' ? 'gpu' : 'cpu';
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
