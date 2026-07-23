import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import Hls, { type Fragment } from 'hls.js';
import { ChevronDown, Maximize2, Minimize2, Pause, Play, RotateCw, Settings, Trash2, Volume2, VolumeX } from 'lucide-react';
import type { Asset, SubtitleInfo, VideoProxyHeartbeat, VideoProxyRuntime, VideoSegmentStatus } from '../types/api';
import {
  api,
  assetPreviewUrl,
  assetSubtitleUrl,
  assetVideoHlsPlaylistUrl,
  assetVideoHlsSegmentUrl,
  assetVideoUrl,
} from '../api/client';
import { formatDuration } from '../utils/format';
import { normalizeRotation, rotatedContainStyle } from '../utils/rotation';
import {
  playbackRates,
  playbackModeOptions,
  type ViewerPlaybackMode,
  type ViewerPrefs,
} from '../utils/viewerPrefs';
import DanmakuLayer from './DanmakuLayer';
import type { ViewerMediaLayerMode } from './mediaLayer';

export interface VideoPlaybackInfo {
  browserCachedBytes: number;
  bufferedEnd: number;
  buffering: boolean;
  canPlay: boolean;
  currentTime: number;
  currentSegmentBytes: number;
  currentSegmentTotalBytes: number;
  decodedHeight: number;
  decodedWidth: number;
  displayHeight: number;
  displayWidth: number;
  droppedFrames: number;
  duration: number;
  ended: boolean;
  hasPlaybackStarted: boolean;
  networkState: number;
  networkBytesPerSecond: number;
  notPlayingDetail: string | null;
  notPlayingReason: string | null;
  paused: boolean;
  playError: string;
  playbackStateLabel: string;
  playbackRate: number;
  playerHeight: number;
  playerWidth: number;
  readyState: number;
  seeking: boolean;
  sourceFailed: boolean;
  totalFrames: number;
}

interface Props {
  asset: Asset;
  fullscreen: boolean;
  playbackRate: number;
  viewerPrefs: ViewerPrefs;
  selectedSubtitleId: string;
  subtitles: SubtitleInfo[];
  subtitlesEnabled: boolean;
  deleting: boolean;
  layerMode: ViewerMediaLayerMode;
  preloadEnabled: boolean;
  onDanmakuPrefChange: (key: DanmakuPrefKey, value: number) => void;
  onDelete: () => void;
  onMediaError: (assetId: number, cacheKey: string, message: string) => void;
  onMediaReady: (assetId: number, cacheKey: string) => void;
  onPriorityPreloadComplete: (assetId: number, cacheKey: string) => void;
  onPlaybackInfoChange?: (info: VideoPlaybackInfo | null) => void;
  onPlaybackEnded: () => void;
  onPlaybackModeChange: (value: ViewerPlaybackMode) => void;
  onPlaybackRateChange: (value: number) => void;
  onRotate: () => void;
  onSelectedSubtitleChange: (value: string) => void;
  onSubtitlesEnabledChange: (value: boolean) => void;
  onToggleFullscreen: () => void;
  onProxyRuntimeChange?: (runtime: VideoSegmentStatus | null) => void;
}

type DanmakuPrefKey = 'danmakuDensity' | 'danmakuFontScale' | 'danmakuOpacity' | 'danmakuSpeed';

const autoplayDelayMs = 800;
const proxyPollMs = 3000;
const proxyKeepaliveMs = 15000;
const hlsSegmentSeconds = 10;
const hlsPreloadSegments = 5;
const hlsCriticalPreloadSegments = 3;
const volumeStep = 0.05;
const danmakuDensitySteps = [0.25, 0.5, 0.75, 1, 1.25, 1.5] as const;
const danmakuOpacitySteps = [0.15, 0.35, 0.55, 0.75, 0.95, 1] as const;
const danmakuFontScaleSteps = [0.75, 1, 1.25, 1.5] as const;
const danmakuSpeedSteps = [0.5, 0.75, 1, 1.25, 1.5, 2] as const;
const videoAudioStorageKey = 'lpicto-video-audio';
const videoProxyClientStorageKey = 'lpicto-video-proxy-client';

interface VideoAudioPreference {
  lastVolume: number;
  muted: boolean;
  version: number;
  volume: number;
}

interface VideoNetworkMetrics {
  browserCachedBytes: number;
  currentSegmentBytes: number;
  currentSegmentTotalBytes: number;
  networkBytesPerSecond: number;
}

type VideoRuntimeStatus = VideoProxyRuntime | VideoSegmentStatus;

let sharedVideoAudio: VideoAudioPreference | null = null;

export default function VideoViewer({
  asset,
  fullscreen,
  playbackRate,
  viewerPrefs,
  selectedSubtitleId,
  subtitles,
  subtitlesEnabled,
  deleting,
  layerMode,
  preloadEnabled,
  onDanmakuPrefChange,
  onDelete,
  onMediaError,
  onMediaReady,
  onPriorityPreloadComplete,
  onPlaybackInfoChange,
  onPlaybackEnded,
  onPlaybackModeChange,
  onPlaybackRateChange,
  onRotate,
  onSelectedSubtitleChange,
  onSubtitlesEnabledChange,
  onToggleFullscreen,
  onProxyRuntimeChange,
}: Props) {
  const ref = useRef<HTMLVideoElement | null>(null);
  const frameRef = useRef<HTMLDivElement | null>(null);
  const mediaReadyKeyRef = useRef('');
  const settingsRef = useRef<HTMLDivElement | null>(null);
  const autoplayTimer = useRef<number | null>(null);
  const resumeTimer = useRef<number | null>(null);
  const proxyPollTimer = useRef<number | null>(null);
  const proxyKeepaliveTimer = useRef<number | null>(null);
  const proxyPlayPending = useRef(false);
  const proxyClientId = useRef(loadVideoProxyClientId());
  const hlsRef = useRef<Hls | null>(null);
  const layerModeRef = useRef(layerMode);
  const preloadSegmentReadyRef = useRef(false);
  const priorityPreloadKeyRef = useRef('');
  const hlsLoadStopped = useRef(false);
  const skipNextAudioPreferenceSave = useRef(false);
  const wantsPlaying = useRef(false);
  const resumeAttempts = useRef(0);
  const currentTimeRef = useRef(0);
  const networkResourceBytes = useRef(new Map<string, number>());
  const networkIdleTimer = useRef<number | null>(null);
  const networkProgressTimer = useRef<number | null>(null);
  const activeNetworkFragment = useRef<Fragment | null>(null);
  const [liveAsset, setLiveAsset] = useState(asset);
  const [audio, setAudio] = useState<VideoAudioPreference>(() => loadVideoAudioPreference());
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [playbackOptionsOpen, setPlaybackOptionsOpen] = useState(false);
  const [danmakuOptionsOpen, setDanmakuOptionsOpen] = useState(false);
  const [proxyFailed, setProxyFailed] = useState(false);
  const [browserDirectFailed, setBrowserDirectFailed] = useState(false);
  const [sourceFailed, setSourceFailed] = useState(false);
  const [firstFrameReady, setFirstFrameReady] = useState(false);
  const [preloadSegmentReady, setPreloadSegmentReady] = useState(false);
  const [hasPlaybackStarted, setHasPlaybackStarted] = useState(false);
  const [playRequested, setPlayRequested] = useState(false);
  const [autoplayPending, setAutoplayPending] = useState(false);
  const [playError, setPlayError] = useState('');
  const [paused, setPaused] = useState(true);
  const [buffering, setBuffering] = useState(false);
  const [seeking, setSeeking] = useState(false);
  const [ended, setEnded] = useState(false);
  const [waitingTrigger, setWaitingTrigger] = useState<'none' | 'seek' | 'buffer' | 'network'>('none');
  const [hlsPhase, setHlsPhase] = useState<'idle' | 'manifest' | 'segment' | 'ready' | 'error' | 'direct'>('idle');
  const [bufferedEnd, setBufferedEnd] = useState(0);
  const [nativeHlsSource, setNativeHlsSource] = useState('');
  const [proxyStreamEnabled, setProxyStreamEnabled] = useState(false);
  const [proxySessionId] = useState(() => createVideoProxySessionId());
  const [proxyStartTime, setProxyStartTime] = useState(0);
  const [proxyRuntime, setProxyRuntime] = useState<VideoSegmentStatus | null>(null);
  const [duration, setDuration] = useState(asset.duration ?? 0);
  const [currentTime, setCurrentTime] = useState(0);
  const [scrubTime, setScrubTime] = useState<number | null>(null);
  const [frameSize, setFrameSize] = useState({ width: 0, height: 0 });
  const [videoMetrics, setVideoMetrics] = useState({
    decodedHeight: 0,
    decodedWidth: 0,
    displayHeight: 0,
    displayWidth: 0,
    droppedFrames: 0,
    networkState: 0,
    readyState: 0,
    totalFrames: 0,
  });
  const [networkMetrics, setNetworkMetrics] = useState<VideoNetworkMetrics>({
    browserCachedBytes: 0,
    currentSegmentBytes: 0,
    currentSegmentTotalBytes: 0,
    networkBytesPerSecond: 0,
  });
  const playbackAsset = liveAsset.id === asset.id && liveAsset.cacheKey === asset.cacheKey ? liveAsset : asset;
  const browserFirst = viewerPrefs.videoProcessingMode === 'browser';
  const usesProxy = !playbackAsset.browserPlayable && (!browserFirst || browserDirectFailed) && !proxyFailed;
  const mediaLoadEnabled = layerMode === 'active' || (preloadEnabled && !usesProxy);
  const source = useMemo(() => {
    if (!mediaLoadEnabled) return '';
    if (usesProxy) {
      return assetVideoHlsPlaylistUrl(playbackAsset, { clientId: proxyClientId.current, sessionId: proxySessionId });
    }
    return assetVideoUrl(playbackAsset);
  }, [mediaLoadEnabled, playbackAsset, proxySessionId, usesProxy]);

  const subtitleSource = subtitlesEnabled && selectedSubtitleId ? assetSubtitleUrl(asset, selectedSubtitleId) : '';
  const canPlay = !sourceFailed && Boolean(source);
  const posterSource = asset.thumbStatus === 'ready' ? assetPreviewUrl(asset) : '';
  const showPosterLayer = Boolean(posterSource) && (!canPlay || !firstFrameReady);
  const statusLabel = videoStatusLabel(playbackAsset, sourceFailed, proxyRuntime);
  const displayedTime = scrubTime ?? currentTime;
  const selectedSubtitle = useMemo(
    () => subtitles.find((subtitle) => subtitle.id === selectedSubtitleId),
    [selectedSubtitleId, subtitles],
  );
  const hasSubtitles = subtitles.length > 0;
  layerModeRef.current = layerMode;

  function notifyPreparedMedia() {
    const key = `${asset.id}:${asset.cacheKey}`;
    if (mediaReadyKeyRef.current === key) return;
    mediaReadyKeyRef.current = key;
    onMediaReady(asset.id, asset.cacheKey);
  }

  function notifyMediaReady() {
    setFirstFrameReady(true);
    notifyPreparedMedia();
    if (!usesProxy) onPriorityPreloadComplete(asset.id, asset.cacheKey);
  }

  function updateDirectPreloadProgress(video: HTMLVideoElement) {
    if (layerModeRef.current === 'active' || usesProxy || preloadSegmentReadyRef.current) return;
    const mediaDuration = video.duration || asset.duration || 0;
    const target = Math.min(10, mediaDuration > 0 ? mediaDuration : 10);
    let bufferedThrough = 0;
    for (let index = 0; index < video.buffered.length; index += 1) {
      if (video.buffered.start(index) <= 0.25) bufferedThrough = Math.max(bufferedThrough, video.buffered.end(index));
    }
    if (bufferedThrough + 0.25 < target) {
      const probeTime = Math.min(Math.max(0, target - 0.05), Math.max(0, bufferedThrough - 0.05));
      if (!video.seeking && probeTime > 0 && Math.abs(video.currentTime - probeTime) > 0.2) video.currentTime = probeTime;
      return;
    }
    video.pause();
    video.currentTime = 0;
    currentTimeRef.current = 0;
    setCurrentTime(0);
    preloadSegmentReadyRef.current = true;
    setPreloadSegmentReady(true);
    notifyPreparedMedia();
  }

  useLayoutEffect(() => {
    mediaReadyKeyRef.current = '';
    preloadSegmentReadyRef.current = false;
    priorityPreloadKeyRef.current = '';
    setFirstFrameReady(false);
    setPreloadSegmentReady(false);
  }, [asset.id, asset.cacheKey]);

  useEffect(() => {
    if (layerMode !== 'active' || !usesProxy) return undefined;
    const key = `${asset.id}:${asset.cacheKey}`;
    if (priorityPreloadKeyRef.current === key) return undefined;
    const controller = new AbortController();
    const session = { clientId: proxyClientId.current, sessionId: proxySessionId };
    void (async () => {
      await api.prewarmVideoSegments(asset.id, 0, hlsCriticalPreloadSegments, 'critical', session, controller.signal);
      if (controller.signal.aborted || layerModeRef.current !== 'active') return;
      onPriorityPreloadComplete(asset.id, asset.cacheKey);
      await api.prewarmVideoSegments(
        asset.id,
        hlsCriticalPreloadSegments,
        hlsPreloadSegments - hlsCriticalPreloadSegments,
        'balanced',
        session,
        controller.signal,
      );
      if (controller.signal.aborted || layerModeRef.current !== 'active') return;
      priorityPreloadKeyRef.current = key;
    })().catch((err: unknown) => {
      if (controller.signal.aborted) return;
      onMediaError(asset.id, asset.cacheKey, err instanceof Error ? `当前视频预缓存失败：${err.message}` : '当前视频预缓存失败');
    });
    return () => controller.abort();
  }, [asset.cacheKey, asset.id, layerMode, onMediaError, onPriorityPreloadComplete, proxySessionId, usesProxy]);

  const stopHLSSession = () => {
    if (!usesProxy) return;
    void api.stopVideoSegmentSession(asset.id, {
      clientId: proxyClientId.current,
      sessionId: proxySessionId,
    }).catch(() => undefined);
  };

  const previousLayerMode = useRef(layerMode);
  useEffect(() => {
    if (previousLayerMode.current === 'active' && layerMode !== 'active') stopHLSSession();
    previousLayerMode.current = layerMode;
  }, [asset.id, layerMode, proxySessionId, usesProxy]);

  useEffect(() => {
    if (layerMode === 'prepare' && !preloadEnabled) stopHLSSession();
  }, [asset.id, layerMode, preloadEnabled, proxySessionId, usesProxy]);

  useEffect(() => () => stopHLSSession(), [asset.id, proxySessionId, usesProxy]);

  const closeSettings = () => {
    setSettingsOpen(false);
    setPlaybackOptionsOpen(false);
    setDanmakuOptionsOpen(false);
  };

  const mediaStyle = useMemo(() => {
    const rotation = normalizeRotation(asset.rotation);
    if (rotation === 0) return undefined;
    return { ...rotatedContainStyle(asset, frameSize), bottom: 'auto', right: 'auto' };
  }, [asset, frameSize.height, frameSize.width]);

  const startProxyPlayback = () => {
    wantsPlaying.current = true;
    setPlayRequested(true);
    proxyPlayPending.current = true;
    resumeAttempts.current = 0;
    setPlayError('');
    setSourceFailed(false);
    setProxyStreamEnabled(true);
  };

  const proxyHeartbeat = (state: VideoProxyHeartbeat['state'], wantsStream: boolean): VideoProxyHeartbeat => ({
    clientId: proxyClientId.current,
    sessionId: proxySessionId,
    state,
    currentTime: currentTimeRef.current,
    playbackRate,
    wantsStream,
    hidden: document.hidden,
  });

  const sendProxyHeartbeat = (state: VideoProxyHeartbeat['state'], wantsStream: boolean) => {
    if (!usesProxy) return;
    void api.keepVideoProxyAlive(asset.id, proxyStartTime, proxyHeartbeat(state, wantsStream)).catch(() => undefined);
  };

  const stopProxySession = (sessionId: string, startSeconds: number, stoppedAt: number) => {
    void api
      .keepVideoProxyAlive(asset.id, startSeconds, {
        clientId: proxyClientId.current,
        sessionId,
        state: 'stopped',
        currentTime: stoppedAt,
        playbackRate,
        wantsStream: false,
        hidden: document.hidden,
      })
      .catch(() => undefined);
  };

  const togglePlay = () => {
    if (layerMode !== 'active' || !canPlay) return;
    const video = ref.current;
    if (!video) return;
    if (video.paused) {
      wantsPlaying.current = true;
      setPlayRequested(true);
      setEnded(false);
      resumeAttempts.current = 0;
      void startPlayback(video);
    } else {
      wantsPlaying.current = false;
      setPlayRequested(false);
      clearResumeTimer();
      video.pause();
    }
  };

  const toggleMute = () => {
    const video = ref.current;
    if (!video) return;
    if (video.muted || video.volume === 0) {
      video.volume = audio.lastVolume || 1;
      video.muted = false;
      return;
    }
    video.muted = true;
  };

  const adjustVolume = (delta: number) => {
    const video = ref.current;
    if (!video) return;
    const audibleVolume = video.muted ? 0 : video.volume;
    const next = clampVolume(Number((Math.round((audibleVolume + delta) / volumeStep) * volumeStep).toFixed(2)));
    video.volume = next;
    video.muted = next === 0;
  };

  const recordNetworkProgress = (key: string, loaded: number, total: number, startedAt: number, transferred = loaded) => {
    const safeLoaded = Math.max(0, Number.isFinite(loaded) ? loaded : 0);
    const safeTotal = Math.max(safeLoaded, Number.isFinite(total) ? total : 0);
    const previous = networkResourceBytes.current.get(key) ?? 0;
    const added = Math.max(0, safeLoaded - previous);
    networkResourceBytes.current.set(key, Math.max(previous, safeLoaded));
    const elapsedMs = Math.max(1, performance.now() - startedAt);
    const safeTransferred = Math.max(0, Number.isFinite(transferred) ? transferred : 0);
    const speed = safeTransferred > 0 ? safeTransferred * 1000 / elapsedMs : 0;
    setNetworkMetrics((current) => ({
      browserCachedBytes: current.browserCachedBytes + added,
      currentSegmentBytes: safeLoaded,
      currentSegmentTotalBytes: safeTotal,
      networkBytesPerSecond: speed || current.networkBytesPerSecond,
    }));
    if (networkIdleTimer.current !== null) window.clearTimeout(networkIdleTimer.current);
    networkIdleTimer.current = window.setTimeout(() => {
      networkIdleTimer.current = null;
      setNetworkMetrics((current) => ({ ...current, networkBytesPerSecond: 0 }));
    }, 2500);
  };

  const stopFragmentNetworkProgress = () => {
    activeNetworkFragment.current = null;
    if (networkProgressTimer.current === null) return;
    window.clearInterval(networkProgressTimer.current);
    networkProgressTimer.current = null;
  };

  const startFragmentNetworkProgress = (fragment: Fragment) => {
    stopFragmentNetworkProgress();
    activeNetworkFragment.current = fragment;
    const update = () => {
      const current = activeNetworkFragment.current;
      if (!current) return;
      const stats = current.stats;
      recordNetworkProgress(current.url, stats.loaded, stats.total, stats.loading.start);
    };
    update();
    networkProgressTimer.current = window.setInterval(update, 250);
  };

  async function startPlayback(video: HTMLVideoElement) {
    wantsPlaying.current = true;
    setPlayRequested(true);
    setEnded(false);
    setPlayError('');
    try {
      await video.play();
      resumeAttempts.current = 0;
      return;
    } catch (err) {
      let failure: unknown = err;
      if (!video.muted) {
        skipNextAudioPreferenceSave.current = true;
        video.muted = true;
        setAudio({ ...loadVideoAudioPreference(), muted: true, volume: video.volume });
        try {
          await video.play();
          resumeAttempts.current = 0;
          return;
        } catch (retryErr) {
          failure = retryErr;
        }
      }
      wantsPlaying.current = false;
      setPlayRequested(false);
      setPlayError(playbackFailureMessage(failure));
    }
  }

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (layerMode !== 'active') return;
      if (event.code !== 'Space') return;
      if (event.target instanceof Element && event.target.closest('button, input, select')) return;
      event.preventDefault();
      togglePlay();
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [layerMode]);

  useLayoutEffect(() => {
    const video = ref.current;
    if (!video) return;
    if (layerMode === 'active') {
      const savedAudio = loadVideoAudioPreference();
      video.volume = savedAudio.volume;
      video.muted = savedAudio.muted;
      if (hlsRef.current) {
        hlsRef.current.config.maxBufferLength = hlsSegmentSeconds * hlsCriticalPreloadSegments;
        hlsRef.current.config.maxMaxBufferLength = hlsSegmentSeconds * hlsCriticalPreloadSegments;
        hlsRef.current.startLoad(currentTimeRef.current);
        hlsLoadStopped.current = false;
      }
      return;
    }
    wantsPlaying.current = false;
    setPlayRequested(false);
    clearAutoplayTimer();
    clearResumeTimer();
    video.pause();
    video.muted = true;
    if (layerMode === 'hold' && hlsRef.current) {
      hlsRef.current.stopLoad();
      hlsLoadStopped.current = true;
    }
    closeSettings();
  }, [layerMode]);

  useEffect(() => {
    if (!settingsOpen) return undefined;
    function onPointerDown(event: PointerEvent) {
      if (event.target instanceof Node && settingsRef.current?.contains(event.target)) return;
      closeSettings();
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      event.stopPropagation();
      closeSettings();
    }
    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true);
      document.removeEventListener('keydown', onKeyDown, true);
    };
  }, [settingsOpen]);

  useEffect(() => {
    if (ref.current) {
      ref.current.playbackRate = playbackRate;
    }
  }, [playbackRate, source]);

  useEffect(() => {
    currentTimeRef.current = currentTime;
  }, [currentTime]);

  useEffect(() => {
    if (layerMode === 'active' || !preloadEnabled || !usesProxy || preloadSegmentReadyRef.current) return undefined;
    const controller = new AbortController();
    const segmentUrl = assetVideoHlsSegmentUrl(playbackAsset, 0, {
      clientId: proxyClientId.current,
      sessionId: proxySessionId,
    });
    void fetch(segmentUrl, { cache: 'default', signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        await response.arrayBuffer();
        if (controller.signal.aborted) return;
        preloadSegmentReadyRef.current = true;
        setPreloadSegmentReady(true);
        notifyPreparedMedia();
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        onMediaError(asset.id, asset.cacheKey, err instanceof Error ? `视频首个分片预加载失败：${err.message}` : '视频首个分片预加载失败');
      });
    return () => controller.abort();
  }, [asset.cacheKey, asset.id, layerMode, onMediaError, playbackAsset, preloadEnabled, proxySessionId, usesProxy]);

  useEffect(() => {
    const video = ref.current;
    if (!video || !source) return undefined;
    hlsRef.current?.destroy();
    hlsRef.current = null;
    hlsLoadStopped.current = false;
    setNativeHlsSource('');
    setHlsPhase('idle');
    setSourceFailed(false);
    setPlayError('');
    if (!usesProxy) {
      setHlsPhase('direct');
      video.src = source;
      setNativeHlsSource(source);
      video.load();
      return () => {
        setNativeHlsSource('');
        video.removeAttribute('src');
        video.load();
      };
    }
    const nativeHls = canPlayNativeHls(video);
    if (nativeHls) {
      setHlsPhase('manifest');
      video.src = source;
      setNativeHlsSource(source);
      video.load();
      return () => {
        setNativeHlsSource('');
        video.removeAttribute('src');
        video.load();
      };
    }
    if (!Hls.isSupported()) {
      setHlsPhase('error');
      setSourceFailed(true);
      onMediaError(asset.id, asset.cacheKey, '当前浏览器不支持 HLS 分片播放');
      setPlayError('当前浏览器不支持 HLS 分片播放');
      return undefined;
    }
    const hls = new Hls({
      backBufferLength: hlsSegmentSeconds,
      fragLoadingMaxRetry: 2,
      lowLatencyMode: false,
      maxBufferLength: hlsSegmentSeconds * hlsCriticalPreloadSegments,
      maxMaxBufferLength: hlsSegmentSeconds * hlsCriticalPreloadSegments,
      startFragPrefetch: false,
    });
    hlsRef.current = hls;
    hls.on(Hls.Events.MEDIA_ATTACHED, () => {
      setHlsPhase('manifest');
      hls.loadSource(source);
    });
    hls.on(Hls.Events.MANIFEST_LOADING, () => {
      setHlsPhase('manifest');
    });
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      setHlsPhase('ready');
    });
    hls.on(Hls.Events.FRAG_LOADING, (_event, data) => {
      setHlsPhase('segment');
      setBuffering(true);
      startFragmentNetworkProgress(data.frag);
    });
    hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
      setHlsPhase('ready');
      setBuffering(false);
      const stats = data.frag.stats;
      const payloadBytes = data.payload.byteLength;
      const loaded = Math.max(stats.loaded, payloadBytes);
      recordNetworkProgress(data.frag.url, loaded, Math.max(stats.total, loaded), stats.loading.start, stats.loaded);
      stopFragmentNetworkProgress();
      void pollVideoSegmentStatus(asset.id, proxySessionId);
    });
    hls.on(Hls.Events.ERROR, (_event, data) => {
      if (!data.fatal) return;
      setHlsPhase('error');
      setSourceFailed(true);
      setBuffering(false);
      setPlayError(hlsFailureMessage(data));
      onMediaError(asset.id, asset.cacheKey, hlsFailureMessage(data));
    });
    hls.attachMedia(video);
    return () => {
      stopFragmentNetworkProgress();
      hls.destroy();
      if (hlsRef.current === hls) hlsRef.current = null;
      hlsLoadStopped.current = false;
      setNativeHlsSource('');
      video.removeAttribute('src');
      video.load();
    };
  }, [asset.id, onMediaError, proxySessionId, source, usesProxy]);

  useEffect(() => {
    networkResourceBytes.current.clear();
    setNetworkMetrics({ browserCachedBytes: 0, currentSegmentBytes: 0, currentSegmentTotalBytes: 0, networkBytesPerSecond: 0 });
    if (typeof PerformanceObserver === 'undefined') return undefined;
    const startedAt = performance.now();
    const pathMarkers = usesProxy
      ? [`/api/assets/${asset.id}/hls/segments/`, `/api/assets/${asset.id}/hls/playlist.m3u8`]
      : [`/api/assets/${asset.id}/video`];
    const observer = new PerformanceObserver((list) => {
      list.getEntries().forEach((entry) => {
        if (!(entry instanceof PerformanceResourceTiming) || entry.startTime < startedAt || !pathMarkers.some((marker) => entry.name.includes(marker))) return;
        const bytes = entry.decodedBodySize || entry.encodedBodySize || entry.transferSize || 0;
        const key = usesProxy ? entry.name : `${entry.name}:${entry.startTime}`;
        recordNetworkProgress(key, bytes, bytes, entry.startTime, entry.transferSize);
      });
    });
    observer.observe({ type: 'resource', buffered: true });
    return () => observer.disconnect();
  }, [asset.id, usesProxy]);

  useEffect(() => {
    if (!usesProxy || !proxyStreamEnabled || !proxyPlayPending.current || !source) return;
    const video = ref.current;
    if (!video) return;
    proxyPlayPending.current = false;
    video.playbackRate = playbackRate;
    void startPlayback(video);
  }, [playbackRate, proxyStreamEnabled, source, usesProxy]);

  const playbackDiagnosis = useMemo(
    () => diagnoseVideoPlayback({
      autoplayEnabled: viewerPrefs.videoAutoplay,
      autoplayPending,
      bufferedEnd,
      buffering,
      canPlay,
      currentTime,
      ended,
      hasPlaybackStarted,
      hlsPhase,
      networkState: videoMetrics.networkState,
      paused,
      playError,
      playRequested,
      proxyRuntime,
      proxyStreamEnabled,
      readyState: videoMetrics.readyState,
      seeking,
      sourceFailed,
      usesProxy,
      waitingTrigger,
    }),
    [
      autoplayPending, bufferedEnd, buffering, canPlay, currentTime, ended, hasPlaybackStarted, hlsPhase,
      paused, playError, playRequested, proxyRuntime, proxyStreamEnabled, seeking, sourceFailed, usesProxy,
      videoMetrics.networkState, videoMetrics.readyState, viewerPrefs.videoAutoplay, waitingTrigger,
    ],
  );

  const playbackInfo = useMemo<VideoPlaybackInfo>(
    () => ({
      browserCachedBytes: networkMetrics.browserCachedBytes,
      bufferedEnd,
      buffering,
      canPlay,
      currentTime,
      currentSegmentBytes: networkMetrics.currentSegmentBytes,
      currentSegmentTotalBytes: networkMetrics.currentSegmentTotalBytes,
      decodedHeight: videoMetrics.decodedHeight,
      decodedWidth: videoMetrics.decodedWidth,
      displayHeight: videoMetrics.displayHeight,
      displayWidth: videoMetrics.displayWidth,
      droppedFrames: videoMetrics.droppedFrames,
      duration: duration || 0,
      ended,
      hasPlaybackStarted,
      networkState: videoMetrics.networkState,
      networkBytesPerSecond: networkMetrics.networkBytesPerSecond,
      notPlayingDetail: playbackDiagnosis.detail,
      notPlayingReason: playbackDiagnosis.reason,
      paused,
      playError,
      playbackStateLabel: playbackDiagnosis.label,
      playbackRate,
      playerHeight: frameSize.height,
      playerWidth: frameSize.width,
      readyState: videoMetrics.readyState,
      seeking,
      sourceFailed,
      totalFrames: videoMetrics.totalFrames,
    }),
    [bufferedEnd, buffering, canPlay, currentTime, duration, ended, frameSize, hasPlaybackStarted, networkMetrics, paused, playError, playbackDiagnosis, playbackRate, seeking, sourceFailed, videoMetrics],
  );

  useEffect(() => {
    if (layerMode === 'active') onPlaybackInfoChange?.(playbackInfo);
  }, [layerMode, onPlaybackInfoChange, playbackInfo]);

  useEffect(() => () => {
    if (layerModeRef.current === 'active') onPlaybackInfoChange?.(null);
  }, [onPlaybackInfoChange]);

  useEffect(() => {
    clearAutoplayTimer();
    setAutoplayPending(false);
    if (layerMode !== 'active' || !canPlay) return clearAutoplayTimer;
    const video = ref.current;
    if (!video) return clearAutoplayTimer;
    if (!viewerPrefs.videoAutoplay && viewerPrefs.playbackMode !== 'continuous') {
      video.pause();
      return clearAutoplayTimer;
    }
    setAutoplayPending(true);
    autoplayTimer.current = window.setTimeout(() => {
      setAutoplayPending(false);
      if (ref.current) {
        wantsPlaying.current = true;
        setPlayRequested(true);
        ref.current.playbackRate = playbackRate;
        void ref.current.play().catch(() => undefined);
      }
      autoplayTimer.current = null;
    }, autoplayDelayMs);
    return clearAutoplayTimer;
  }, [asset.id, canPlay, layerMode, playbackRate, source, viewerPrefs.playbackMode, viewerPrefs.videoAutoplay]);

  useEffect(() => {
    const frame = frameRef.current;
    if (!frame) return;
    const update = () => {
      const rect = frame.getBoundingClientRect();
      setFrameSize({ width: rect.width, height: rect.height });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(frame);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (ref.current) updateVideoMetrics(ref.current);
  }, [frameSize.height, frameSize.width]);

  const updateVideoMetrics = (video: HTMLVideoElement) => {
    const rect = video.getBoundingClientRect();
    const quality = typeof video.getVideoPlaybackQuality === 'function' ? video.getVideoPlaybackQuality() : null;
    const next = {
      decodedHeight: video.videoHeight || 0,
      decodedWidth: video.videoWidth || 0,
      displayHeight: Math.round(rect.height),
      displayWidth: Math.round(rect.width),
      droppedFrames: quality?.droppedVideoFrames ?? 0,
      networkState: video.networkState,
      readyState: video.readyState,
      totalFrames: quality?.totalVideoFrames ?? 0,
    };
    setVideoMetrics((current) => videoMetricsEqual(current, next) ? current : next);
  };

  useEffect(() => {
    setSourceFailed(false);
    setFirstFrameReady(false);
    setHasPlaybackStarted(false);
    setPlayRequested(false);
    setAutoplayPending(false);
    setPlayError('');
    setPaused(true);
    setBuffering(false);
    setSeeking(false);
    setEnded(false);
    setWaitingTrigger('none');
    setHlsPhase('idle');
    setBufferedEnd(0);
    setCurrentTime(0);
    currentTimeRef.current = 0;
    setScrubTime(null);
    setDuration(asset.duration ?? 0);
    setVideoMetrics({
      decodedHeight: 0,
      decodedWidth: 0,
      displayHeight: 0,
      displayWidth: 0,
      droppedFrames: 0,
      networkState: 0,
      readyState: 0,
      totalFrames: 0,
    });
    setLiveAsset(asset);
    setProxyFailed(false);
    setBrowserDirectFailed(false);
    setProxyStreamEnabled(false);
    setProxyStartTime(0);
    setProxyRuntime(null);
    proxyPlayPending.current = false;
    wantsPlaying.current = false;
    resumeAttempts.current = 0;
    clearResumeTimer();
  }, [asset.id, viewerPrefs.videoProcessingMode]);

  useEffect(() => {
    setSourceFailed(false);
    setPlayError('');
  }, [source]);

  useEffect(() => {
    clearProxyPollTimer();
    if (layerMode !== 'active' || !usesProxy || !source) {
      setProxyRuntime(null);
      return clearProxyPollTimer;
    }
    let active = true;
    const pollAssetId = asset.id;
    const pollSessionId = proxySessionId;
    const poll = async () => {
      try {
        const runtime = await api.videoSegmentStatus(pollAssetId, {
          clientId: proxyClientId.current,
          sessionId: pollSessionId,
        });
        if (!active || !videoSegmentStatusMatches(runtime, pollAssetId, pollSessionId)) return;
        setProxyRuntime(runtime);
      } catch {
        // Playback owns segment requests; status polling should not interrupt it.
      } finally {
        if (active) proxyPollTimer.current = window.setTimeout(poll, proxyPollMs);
      }
    };
    void poll();
    return () => {
      active = false;
      clearProxyPollTimer();
    };
  }, [asset.id, layerMode, proxySessionId, source, usesProxy]);

  useEffect(() => {
    const visibleRuntime = usesProxy && proxyRuntime && videoSegmentStatusMatches(proxyRuntime, asset.id, proxySessionId) ? proxyRuntime : null;
    if (layerMode === 'active') onProxyRuntimeChange?.(visibleRuntime);
  }, [asset.id, layerMode, onProxyRuntimeChange, proxyRuntime, proxySessionId, usesProxy]);

  useEffect(() => {
    if (!usesProxy || !proxyStreamEnabled) return undefined;
    const releaseAssetID = asset.id;
    const releaseStartTime = proxyStartTime;
    const releaseSessionID = proxySessionId;
    return () => {
      void api
        .keepVideoProxyAlive(releaseAssetID, releaseStartTime, {
          clientId: proxyClientId.current,
          sessionId: releaseSessionID,
          state: 'stopped',
          currentTime: currentTimeRef.current,
          playbackRate,
          wantsStream: false,
          hidden: document.hidden,
        })
        .catch(() => undefined);
    };
  }, [asset.id, proxySessionId, proxyStartTime, proxyStreamEnabled, usesProxy]);

  useEffect(() => {
    clearProxyKeepaliveTimer();
    if (!usesProxy || sourceFailed || !proxyStreamEnabled) {
      return clearProxyKeepaliveTimer;
    }
    const shouldSync = wantsPlaying.current || proxyPlayPending.current || !paused || hasPlaybackStarted;
    if (!shouldSync) {
      return clearProxyKeepaliveTimer;
    }
    let active = true;
    const keepaliveAssetId = asset.id;
    const keepaliveSessionId = proxySessionId;
    const keepaliveStartTime = proxyStartTime;
    const keepalive = async () => {
      const state: VideoProxyHeartbeat['state'] = !paused && hasPlaybackStarted ? 'playing' : 'preparing';
      try {
        const runtime = await api.keepVideoProxyAlive(keepaliveAssetId, keepaliveStartTime, {
          ...proxyHeartbeat(state, true),
          sessionId: keepaliveSessionId,
        });
        if (!active || !videoProxyRuntimeMatches(runtime, keepaliveAssetId, keepaliveSessionId, keepaliveStartTime)) return;
        if (runtime.command === 'start_stream' && wantsPlaying.current && !source) {
          startProxyPlayback();
        }
      } catch {
        // Playback owns the main request; keepalive failure should not interrupt it.
      } finally {
        if (active) proxyKeepaliveTimer.current = window.setTimeout(keepalive, proxyKeepaliveMs);
      }
    };
    void keepalive();
    return () => {
      active = false;
      clearProxyKeepaliveTimer();
    };
  }, [asset.id, hasPlaybackStarted, paused, playbackRate, proxySessionId, proxyStartTime, proxyStreamEnabled, source, sourceFailed, usesProxy]);

  useEffect(() => () => {
    clearResumeTimer();
    clearProxyPollTimer();
    clearProxyKeepaliveTimer();
    if (networkIdleTimer.current !== null) window.clearTimeout(networkIdleTimer.current);
    stopFragmentNetworkProgress();
  }, []);

  function clearAutoplayTimer() {
    if (autoplayTimer.current === null) return;
    window.clearTimeout(autoplayTimer.current);
    autoplayTimer.current = null;
  }

  function clearResumeTimer() {
    if (resumeTimer.current === null) return;
    window.clearTimeout(resumeTimer.current);
    resumeTimer.current = null;
  }

  function clearProxyPollTimer() {
    if (proxyPollTimer.current === null) return;
    window.clearTimeout(proxyPollTimer.current);
    proxyPollTimer.current = null;
  }

  function clearProxyKeepaliveTimer() {
    if (proxyKeepaliveTimer.current === null) return;
    window.clearTimeout(proxyKeepaliveTimer.current);
    proxyKeepaliveTimer.current = null;
  }

  async function pollVideoSegmentStatus(assetId: number, sessionId: string) {
    try {
      const runtime = await api.videoSegmentStatus(assetId, { clientId: proxyClientId.current, sessionId });
      if (!videoSegmentStatusMatches(runtime, assetId, sessionId)) return;
      setProxyRuntime(runtime);
    } catch {
      // Segment status is advisory; playback errors come from the media pipeline.
    }
  }

  function commitSeek(value = scrubTime) {
    if (value === null || !ref.current) return;
    const next = clampTime(value, duration || ref.current.duration || value);
    ref.current.currentTime = next;
    setCurrentTime(next);
    currentTimeRef.current = next;
    setScrubTime(null);
  }

  function updateBufferedRange(video: HTMLVideoElement) {
    let next = 0;
    for (let index = 0; index < video.buffered.length; index++) {
      const start = video.buffered.start(index);
      const end = video.buffered.end(index);
      if (start <= video.currentTime && end >= video.currentTime) {
        next = end;
        break;
      }
      next = Math.max(next, end);
    }
    setBufferedEnd(clampTime(next, duration || asset.duration || next));
    const hls = hlsRef.current;
    if (!hls || layerModeRef.current !== 'active') return;
    const forwardSeconds = Math.max(0, next - video.currentTime);
    if (forwardSeconds >= hlsSegmentSeconds * hlsCriticalPreloadSegments && !hlsLoadStopped.current) {
      hls.stopLoad();
      hlsLoadStopped.current = true;
    } else if (forwardSeconds < hlsSegmentSeconds * Math.max(1, hlsCriticalPreloadSegments - 1) && hlsLoadStopped.current) {
      hls.startLoad();
      hlsLoadStopped.current = false;
    }
  }

  function scheduleResumeAfterUnexpectedPause(video: HTMLVideoElement) {
    if (!wantsPlaying.current || video.ended || resumeAttempts.current >= 2) return;
    clearResumeTimer();
    resumeTimer.current = window.setTimeout(() => {
      resumeTimer.current = null;
      const current = ref.current;
      if (!current || !wantsPlaying.current || !current.paused || current.ended) return;
      resumeAttempts.current += 1;
      void startPlayback(current);
    }, 80);
  }

  return (
    <div className={canPlay ? 'video-stage' : 'video-stage video-stage-pending'}>
      <div className="video-frame" ref={frameRef}>
        {canPlay && (
          <video
            ref={ref}
            className="viewer-video"
            src={nativeHlsSource || undefined}
            poster={firstFrameReady ? undefined : posterSource || undefined}
            preload={layerMode === 'prepare' && !preloadSegmentReady ? 'auto' : 'metadata'}
            playsInline
            loop={viewerPrefs.playbackMode === 'single'}
            style={mediaStyle}
            onClick={togglePlay}
            onDurationChange={(event) => {
              setDuration(event.currentTarget.duration || asset.duration || 0);
              updateDirectPreloadProgress(event.currentTarget);
            }}
            onLoadedData={(event) => {
              setFirstFrameReady(true);
              if (layerModeRef.current === 'active') notifyPreparedMedia();
              else updateDirectPreloadProgress(event.currentTarget);
            }}
            onError={() => {
              if (!usesProxy && browserFirst && !playbackAsset.browserPlayable && !browserDirectFailed) {
                setBrowserDirectFailed(true);
                setHlsPhase('idle');
                setSourceFailed(false);
                setBuffering(false);
                setPlayError('');
                return;
              }
              setHlsPhase('error');
              setSourceFailed(true);
              setBuffering(false);
              onMediaError(asset.id, asset.cacheKey, '视频加载失败');
            }}
            onLoadedMetadata={(event) => {
              const savedAudio = loadVideoAudioPreference();
              event.currentTarget.volume = savedAudio.volume;
              event.currentTarget.muted = layerMode === 'active' ? savedAudio.muted : true;
              setAudio(savedAudio);
              setDuration(event.currentTarget.duration || asset.duration || 0);
              setPaused(event.currentTarget.paused);
              event.currentTarget.playbackRate = playbackRate;
              updateBufferedRange(event.currentTarget);
              updateVideoMetrics(event.currentTarget);
              updateDirectPreloadProgress(event.currentTarget);
            }}
            onPause={(event) => {
              setPaused(true);
              setEnded(event.currentTarget.ended);
              scheduleResumeAfterUnexpectedPause(event.currentTarget);
            }}
            onPlaying={(event) => {
              setBuffering(false);
              setSeeking(false);
              setEnded(false);
              setWaitingTrigger('none');
              updateBufferedRange(event.currentTarget);
            }}
            onPlay={() => {
              wantsPlaying.current = true;
              setPlayRequested(true);
              setAutoplayPending(false);
              setPlayError('');
              clearResumeTimer();
              setPaused(false);
              setHasPlaybackStarted(true);
            }}
            onProgress={(event) => {
              updateBufferedRange(event.currentTarget);
              updateDirectPreloadProgress(event.currentTarget);
            }}
            onStalled={() => {
              setBuffering(true);
              setWaitingTrigger('network');
            }}
            onWaiting={(event) => {
              setBuffering(true);
              setWaitingTrigger(event.currentTarget.seeking ? 'seek' : 'buffer');
            }}
            onCanPlay={(event) => {
              setBuffering(false);
              setWaitingTrigger('none');
              updateBufferedRange(event.currentTarget);
              if (layerModeRef.current === 'active') notifyMediaReady();
              else updateDirectPreloadProgress(event.currentTarget);
            }}
            onSeeked={(event) => {
              setBuffering(false);
              setSeeking(false);
              setWaitingTrigger('none');
              updateBufferedRange(event.currentTarget);
              updateDirectPreloadProgress(event.currentTarget);
            }}
            onSeeking={() => {
              setSeeking(true);
              setBuffering(true);
              setWaitingTrigger('seek');
            }}
            onEnded={() => {
              wantsPlaying.current = false;
              setPlayRequested(false);
              setPaused(true);
              setBuffering(false);
              setSeeking(false);
              setEnded(true);
              setWaitingTrigger('none');
              if (layerMode === 'active' && viewerPrefs.playbackMode === 'continuous') onPlaybackEnded();
            }}
            onTimeUpdate={(event) => {
              const next = clampTime(event.currentTarget.currentTime, duration || asset.duration || 0);
              currentTimeRef.current = next;
              if (scrubTime === null) {
                setCurrentTime(next);
              }
              if (next > 0.01) setHasPlaybackStarted(true);
              updateVideoMetrics(event.currentTarget);
            }}
            onVolumeChange={(event) => {
              const video = event.currentTarget;
              if (layerMode !== 'active') return;
              if (skipNextAudioPreferenceSave.current) {
                skipNextAudioPreferenceSave.current = false;
                setAudio({ ...loadVideoAudioPreference(), muted: video.muted, volume: video.volume });
                return;
              }
              setAudio(saveVideoAudioPreference(video.volume, video.muted));
            }}
          />
        )}
        <DanmakuLayer
          currentTime={displayedTime}
          density={viewerPrefs.danmakuDensity}
          enabled={Boolean(subtitleSource) && canPlay && !showPosterLayer}
          format={selectedSubtitle?.format ?? ''}
          frameHeight={frameSize.height}
          frameWidth={frameSize.width}
          fontScale={viewerPrefs.danmakuFontScale}
          opacity={viewerPrefs.danmakuOpacity}
          paused={paused || scrubTime !== null}
          playbackRate={playbackRate}
          source={subtitleSource}
          speed={viewerPrefs.danmakuSpeed}
        />
        {showPosterLayer && (
          <button
            className={canPlay ? 'video-poster-layer playable' : 'video-poster-layer'}
            type="button"
            disabled={!canPlay}
            onClick={togglePlay}
          >
            <img src={posterSource} alt={asset.filename} style={mediaStyle} />
            {canPlay ? (
              <>
                <span className="video-big-play">
                  <Play size={34} fill="currentColor" />
                </span>
                {playError && <span className="video-play-error">{playError}</span>}
              </>
            ) : (
              <span className="video-status-badge">{statusLabel}</span>
            )}
          </button>
        )}
        {!posterSource && !canPlay && <div className="video-pending">{statusLabel}</div>}
        {!posterSource && canPlay && usesProxy && !proxyStreamEnabled && (
          <button className="video-pending video-pending-button" type="button" onClick={togglePlay}>
            <Play size={34} fill="currentColor" />
            <span>点击播放开始转码</span>
          </button>
        )}
      </div>
      <div
        className={settingsOpen ? 'video-control-zone settings-open' : 'video-control-zone'}
        onClick={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <div className="video-controls">
          <button type="button" disabled={!canPlay} onClick={togglePlay} title={paused ? '播放' : '暂停'}>
            {paused ? <Play size={18} /> : <Pause size={18} />}
          </button>
          <span className="video-time">{formatDuration(displayedTime)}</span>
          <input
            className="video-progress-slider"
            aria-label="播放进度"
            max={duration || 0}
            min={0}
            step={0.01}
            disabled={!canPlay}
            type="range"
            value={Math.min(displayedTime, duration || displayedTime)}
            onBlur={(event) => commitSeek(Number(event.currentTarget.value))}
            onChange={(event) => {
              const next = Number(event.target.value);
              setScrubTime(next);
            }}
            onKeyUp={(event) => commitSeek(Number(event.currentTarget.value))}
            onPointerCancel={(event) => commitSeek(Number(event.currentTarget.value))}
            onPointerDown={(event) => setScrubTime(Number(event.currentTarget.value))}
            onPointerUp={(event) => commitSeek(Number(event.currentTarget.value))}
          />
          <span className="video-time">{formatDuration(duration)}</span>
          <div
            className="video-control-group video-audio-controls"
            data-viewer-wheel-control
            onWheel={(event) => {
              event.preventDefault();
              event.stopPropagation();
              if (event.deltaY === 0) return;
              adjustVolume(event.deltaY < 0 ? volumeStep : -volumeStep);
            }}
          >
            <button
              type="button"
              disabled={!canPlay}
              onClick={toggleMute}
              title={audio.muted ? '取消静音' : '静音'}
              aria-label={audio.muted ? '取消静音' : '静音'}
              aria-pressed={audio.muted}
            >
              {audio.muted ? <VolumeX size={18} /> : <Volume2 size={18} />}
            </button>
            <div className="video-volume-popover">
              <input
                className="video-volume-slider"
                aria-label="音量"
                aria-valuetext={`${Math.round((audio.muted ? 0 : audio.volume) * 100)}%`}
                max={1}
                min={0}
                step={volumeStep}
                disabled={!canPlay}
                type="range"
                value={audio.muted ? 0 : audio.volume}
                onChange={(event) => {
                  const next = clampVolume(Number(event.target.value));
                  if (!ref.current) return;
                  ref.current.volume = next;
                  ref.current.muted = next === 0;
                }}
              />
            </div>
          </div>
          <div className="video-control-group video-settings-wrap" data-viewer-wheel-control ref={settingsRef}>
            <button
              className={settingsOpen ? 'active' : undefined}
              type="button"
              title="视频设置"
              aria-label="视频设置"
              aria-expanded={settingsOpen}
              aria-haspopup="dialog"
              onClick={() => {
                if (settingsOpen) {
                  closeSettings();
                } else {
                  setPlaybackOptionsOpen(false);
                  setDanmakuOptionsOpen(false);
                  setSettingsOpen(true);
                }
              }}
            >
              <Settings size={18} />
            </button>
            {settingsOpen && (
              <div className="video-settings-popover" role="dialog" aria-label="视频设置">
                <label className="video-settings-row">
                  <span>播放模式</span>
                  <select
                    aria-label="播放模式"
                    value={viewerPrefs.playbackMode}
                    onChange={(event) => onPlaybackModeChange(event.currentTarget.value as ViewerPlaybackMode)}
                  >
                    {playbackModeOptions.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
                <button
                  className="video-settings-expand"
                  type="button"
                  aria-expanded={playbackOptionsOpen}
                  onClick={() => setPlaybackOptionsOpen((value) => !value)}
                >
                  <span>播放速度</span>
                  <span className="video-settings-expand-value">
                    <output>{formatDiscreteValue(playbackRate)}</output>
                    <ChevronDown className={playbackOptionsOpen ? 'expanded' : undefined} size={16} />
                  </span>
                </button>
                {playbackOptionsOpen && (
                  <div className="video-settings-expanded compact">
                    <DiscreteRangeControl
                      ariaLabel="播放速度"
                      label="倍速"
                      values={playbackRates}
                      value={playbackRate}
                      onChange={onPlaybackRateChange}
                    />
                  </div>
                )}
                <label className={hasSubtitles ? 'video-settings-row video-settings-toggle-row' : 'video-settings-row video-settings-toggle-row disabled'}>
                  <span>弹幕</span>
                  <span className="video-settings-switch">
                    <input
                      aria-label="弹幕开关"
                      type="checkbox"
                      checked={subtitlesEnabled && hasSubtitles}
                      disabled={!hasSubtitles}
                      onChange={(event) => onSubtitlesEnabledChange(event.currentTarget.checked)}
                    />
                    <span aria-hidden="true" />
                  </span>
                </label>
                {hasSubtitles && (
                  <label className="video-settings-row">
                    <span>弹幕列表</span>
                    <select
                      aria-label="弹幕列表"
                      value={selectedSubtitleId}
                      onChange={(event) => onSelectedSubtitleChange(event.currentTarget.value)}
                    >
                      {subtitles.map((subtitle) => (
                        <option key={subtitle.id} value={subtitle.id}>{subtitle.filename}</option>
                      ))}
                    </select>
                  </label>
                )}
                <button
                  className="video-settings-expand"
                  type="button"
                  disabled={!hasSubtitles}
                  aria-expanded={danmakuOptionsOpen}
                  onClick={() => setDanmakuOptionsOpen((value) => !value)}
                >
                  <span>弹幕设置</span>
                  <ChevronDown className={danmakuOptionsOpen ? 'expanded' : undefined} size={16} />
                </button>
                {danmakuOptionsOpen && hasSubtitles && (
                  <div className="video-settings-expanded">
                    <DiscreteRangeControl
                      ariaLabel="弹幕密度"
                      label="密度"
                      values={danmakuDensitySteps}
                      value={viewerPrefs.danmakuDensity}
                      onChange={(value) => onDanmakuPrefChange('danmakuDensity', value)}
                    />
                    <DiscreteRangeControl
                      ariaLabel="弹幕透明度"
                      label="透明"
                      values={danmakuOpacitySteps}
                      value={viewerPrefs.danmakuOpacity}
                      onChange={(value) => onDanmakuPrefChange('danmakuOpacity', value)}
                    />
                    <DiscreteRangeControl
                      ariaLabel="弹幕字号"
                      label="字号"
                      values={danmakuFontScaleSteps}
                      value={viewerPrefs.danmakuFontScale}
                      onChange={(value) => onDanmakuPrefChange('danmakuFontScale', value)}
                    />
                    <DiscreteRangeControl
                      ariaLabel="弹幕速度"
                      label="速度"
                      values={danmakuSpeedSteps}
                      value={viewerPrefs.danmakuSpeed}
                      onChange={(value) => onDanmakuPrefChange('danmakuSpeed', value)}
                    />
                  </div>
                )}
                <button className="video-settings-action" type="button" onClick={onRotate}>
                  <span>视频旋转</span>
                  <span><output>{asset.rotation || 0}°</output><RotateCw size={16} /></span>
                </button>
                <button
                  className="video-settings-action danger"
                  type="button"
                  disabled={deleting}
                  onClick={() => {
                    closeSettings();
                    onDelete();
                  }}
                >
                  <span>删除媒体</span>
                  <Trash2 size={16} />
                </button>
              </div>
            )}
          </div>
          <div className="video-control-group video-option-controls">
            <button
              type="button"
              title={fullscreen ? '退出全屏' : '全屏'}
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                onToggleFullscreen();
              }}
            >
              {fullscreen ? <Minimize2 size={18} /> : <Maximize2 size={18} />}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function DiscreteRangeControl({
  ariaLabel,
  label,
  onChange,
  value,
  values,
}: {
  ariaLabel: string;
  label: string;
  onChange: (value: number) => void;
  value: number;
  values: readonly number[];
}) {
  const selectedIndex = nearestStepIndex(value, values);
  const selectedValue = values[selectedIndex] ?? values[0] ?? 1;
  return (
    <label className="video-danmaku-range">
      <span>{label}</span>
      <input
        aria-label={ariaLabel}
        aria-valuetext={formatDiscreteValue(selectedValue)}
        max={Math.max(0, values.length - 1)}
        min={0}
        step={1}
        type="range"
        value={selectedIndex}
        onChange={(event) => {
          const nextValue = values[Number(event.currentTarget.value)];
          if (nextValue !== undefined) onChange(nextValue);
        }}
      />
      <output>{formatDiscreteValue(selectedValue)}</output>
    </label>
  );
}

function nearestStepIndex(value: number, values: readonly number[]) {
  if (values.length === 0) return 0;
  return values.reduce((nearest, current, index) => (
    Math.abs(current - value) < Math.abs((values[nearest] ?? current) - value) ? index : nearest
  ), 0);
}

function formatDiscreteValue(value: number) {
  if (!Number.isFinite(value)) return '1.00x';
  return `${value.toFixed(2)}x`;
}

function videoProxyRuntimeLabel(runtime: VideoRuntimeStatus) {
  if ('message' in runtime && runtime.message) return runtime.message;
  if (runtime.status === 'cached' || runtime.cached) return '已缓存';
  if (runtime.status === 'error') return '转码失败';
  if (runtime.status === 'queued' || runtime.queued) return '等待转码槽位';
  if (runtime.transcoding) return `实时转码 ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`;
  return '准备转码';
}

function videoProxyRuntimeMatches(runtime: VideoProxyRuntime, assetId: number, sessionId: string, startSeconds: number) {
  return (
    runtime.assetId === assetId &&
    runtime.sessionId === sessionId &&
    Math.abs((runtime.startSeconds || 0) - startSeconds) < 0.01
  );
}

function videoSegmentStatusMatches(runtime: VideoSegmentStatus, assetId: number, sessionId: string) {
  return runtime.assetId === assetId && runtime.sessionId === sessionId;
}

function videoMetricsEqual(
  left: {
    decodedHeight: number;
    decodedWidth: number;
    displayHeight: number;
    displayWidth: number;
    droppedFrames: number;
    networkState: number;
    readyState: number;
    totalFrames: number;
  },
  right: typeof left,
) {
  return Object.keys(left).every((key) => left[key as keyof typeof left] === right[key as keyof typeof right]);
}

interface PlaybackDiagnosisInput {
  autoplayEnabled: boolean;
  autoplayPending: boolean;
  bufferedEnd: number;
  buffering: boolean;
  canPlay: boolean;
  currentTime: number;
  ended: boolean;
  hasPlaybackStarted: boolean;
  hlsPhase: 'idle' | 'manifest' | 'segment' | 'ready' | 'error' | 'direct';
  networkState: number;
  paused: boolean;
  playError: string;
  playRequested: boolean;
  proxyRuntime: VideoSegmentStatus | null;
  proxyStreamEnabled: boolean;
  readyState: number;
  seeking: boolean;
  sourceFailed: boolean;
  usesProxy: boolean;
  waitingTrigger: 'none' | 'seek' | 'buffer' | 'network';
}

function diagnoseVideoPlayback(input: PlaybackDiagnosisInput): { detail: string | null; label: string; reason: string | null } {
  const runtime = input.usesProxy ? input.proxyRuntime : null;
  const bufferedAhead = Math.max(0, input.bufferedEnd - input.currentTime);
  if (input.sourceFailed || input.hlsPhase === 'error' || input.playError) {
    return {
      label: '加载失败',
      reason: '播放源或分片加载失败',
      detail: input.playError || runtime?.error || '浏览器报告媒体源加载失败',
    };
  }
  if (runtime?.status === 'error' || runtime?.error) {
    return {
      label: '转码失败',
      reason: '当前分片转码失败',
      detail: runtime.error || runtime.message || `切片 ${runtime.segmentIndex + 1} 无法生成`,
    };
  }
  if (input.ended) {
    return { label: '播放结束', reason: '视频已经播放到结尾', detail: `当前时间 ${formatDuration(input.currentTime)}` };
  }
  if (!input.canPlay) {
    return { label: '加载中', reason: '播放地址尚未准备完成', detail: '播放器正在等待媒体源初始化' };
  }
  if (input.seeking || input.waitingTrigger === 'seek') {
    if (runtime?.queued) {
      return { label: '跳转等待', reason: '目标切片正在等待转码槽位', detail: runtime.message || `等待生成切片 ${runtime.segmentIndex + 1}` };
    }
    if (runtime?.transcoding) {
      return {
        label: '跳转等待',
        reason: '目标切片正在实时转码',
        detail: `${runtime.message || `切片 ${runtime.segmentIndex + 1}`}，进度 ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`,
      };
    }
    if (input.hlsPhase === 'manifest') {
      return { label: '跳转等待', reason: '正在读取分片索引', detail: `目标时间 ${formatDuration(input.currentTime)}` };
    }
    return {
      label: '跳转等待',
      reason: input.usesProxy ? '正在请求目标时间对应的分片' : '正在请求目标位置的数据',
      detail: `目标时间 ${formatDuration(input.currentTime)}，当前前向缓存 ${bufferedAhead.toFixed(1)} 秒`,
    };
  }
  if (!input.hasPlaybackStarted) {
    if (input.autoplayPending) {
      return { label: '等待播放', reason: '等待自动播放计时', detail: `自动播放将在 ${autoplayDelayMs} 毫秒延迟后尝试` };
    }
    if (!input.playRequested) {
      return {
        label: '尚未开始',
        reason: '尚未发起播放',
        detail: bufferedAhead > 0
          ? `媒体已准备 ${bufferedAhead.toFixed(1)} 秒前向缓存，等待用户点击播放`
          : input.autoplayEnabled ? '等待自动播放或用户点击播放' : '自动播放已关闭，等待用户点击播放',
      };
    }
    if (input.usesProxy && runtime?.queued) {
      return {
        label: '等待转码',
        reason: '首个播放分片正在等待转码槽位',
        detail: runtime.message || `等待生成切片 ${runtime.segmentIndex + 1}`,
      };
    }
    if (input.usesProxy && runtime?.transcoding) {
      return {
        label: '转码中',
        reason: '首个播放分片正在实时转码',
        detail: `${runtime.message || `切片 ${runtime.segmentIndex + 1}`}，进度 ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`,
      };
    }
    if (input.hlsPhase === 'manifest') {
      return { label: '加载中', reason: '正在读取播放分片索引', detail: '索引就绪后将请求首个播放分片' };
    }
    if (bufferedAhead <= 0) {
      return {
        label: '等待播放',
        reason: input.usesProxy ? '正在等待首个播放分片' : '正在等待首帧媒体数据',
        detail: input.usesProxy && !input.proxyStreamEnabled ? '播放请求已发出，分片会话正在启动' : '播放请求已发出，当前还没有可播放数据',
      };
    }
    return { label: '等待播放', reason: '播放请求已发出，但尚未输出首帧', detail: `已有 ${bufferedAhead.toFixed(1)} 秒前向缓存，浏览器正在启动解码与渲染` };
  }
  if (input.paused && input.hasPlaybackStarted && !input.playRequested) {
    return { label: '暂停', reason: '用户暂停了视频', detail: `暂停位置 ${formatDuration(input.currentTime)}` };
  }
  if ((input.paused || input.buffering) && runtime?.queued) {
    return { label: '等待转码', reason: '当前分片正在等待转码槽位', detail: runtime.message || `等待生成切片 ${runtime.segmentIndex + 1}` };
  }
  if ((input.paused || input.buffering) && runtime?.transcoding) {
    return {
      label: '转码中',
      reason: '当前播放位置的分片尚未转码完成',
      detail: `${runtime.message || `切片 ${runtime.segmentIndex + 1}`}，进度 ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`,
    };
  }
  if (input.hlsPhase === 'manifest' && (input.paused || input.buffering)) {
    return { label: '加载中', reason: '正在读取 HLS 分片索引', detail: '索引就绪后才能确定目标切片地址' };
  }
  if (input.buffering) {
    if (input.readyState <= 1) {
      return { label: '缓冲中', reason: '媒体数据尚未到达', detail: '播放器目前只有元数据，尚无可解码的视频帧' };
    }
    if (input.waitingTrigger === 'network' || input.networkState === 2) {
      return { label: '缓冲中', reason: '网络数据到达速度不足', detail: `当前前向缓存 ${bufferedAhead.toFixed(1)} 秒，浏览器仍在加载数据` };
    }
    if (input.hlsPhase === 'segment') {
      return { label: '缓冲中', reason: '正在下载当前或下一个分片', detail: `当前前向缓存 ${bufferedAhead.toFixed(1)} 秒` };
    }
    return { label: '缓冲中', reason: '可连续播放的数据不足', detail: `当前前向缓存 ${bufferedAhead.toFixed(1)} 秒` };
  }
  if (input.paused && input.playRequested) {
    return { label: '等待播放', reason: '已请求播放，但浏览器尚未开始输出画面', detail: '播放器可能正在等待首帧、解码器或媒体数据' };
  }
  if (input.paused) {
    return { label: '暂停', reason: '播放器处于暂停状态', detail: `暂停位置 ${formatDuration(input.currentTime)}` };
  }
  return { label: '播放中', reason: null, detail: null };
}

function videoStatusLabel(asset: Asset, sourceFailed: boolean, runtime: VideoRuntimeStatus | null) {
  if (sourceFailed) return '视频加载失败';
  if (runtime) return videoProxyRuntimeLabel(runtime);
  if (!asset.browserPlayable) return '准备分片';
  return '等待媒体';
}

function canPlayNativeHls(video: HTMLVideoElement) {
  return video.canPlayType('application/vnd.apple.mpegurl') !== '' || video.canPlayType('application/x-mpegURL') !== '';
}

function hlsFailureMessage(data: { details?: string; type?: string }) {
  const detail = data.details || data.type || '';
  return detail ? `分片播放失败：${detail}` : '分片播放失败';
}

function playbackFailureMessage(err: unknown) {
  if (err instanceof DOMException && err.message) return `播放失败：${err.message}`;
  if (err instanceof Error && err.message) return `播放失败：${err.message}`;
  return '播放失败';
}

function loadVideoAudioPreference(): VideoAudioPreference {
  if (sharedVideoAudio) return sharedVideoAudio;
  let parsed: Partial<VideoAudioPreference> | null = null;
  try {
    const raw = window.localStorage.getItem(videoAudioStorageKey);
    parsed = raw ? (JSON.parse(raw) as Partial<VideoAudioPreference>) : null;
  } catch {
    parsed = null;
  }
  const volume = clampVolume(parsed?.volume ?? 1);
  const lastVolume = clampVolume(parsed?.lastVolume ?? (volume > 0 ? volume : 1));
  const version = parsed?.version === 2 ? 2 : 1;
  sharedVideoAudio = {
    lastVolume: lastVolume > 0 ? lastVolume : 1,
    muted: version === 2 ? Boolean(parsed?.muted) : false,
    version: 2,
    volume,
  };
  if (version !== 2) {
    try {
      window.localStorage.setItem(videoAudioStorageKey, JSON.stringify(sharedVideoAudio));
    } catch {
      // localStorage can be unavailable in private or restricted browser contexts.
    }
  }
  return sharedVideoAudio;
}

function saveVideoAudioPreference(volumeValue: number, muted: boolean): VideoAudioPreference {
  const previous = loadVideoAudioPreference();
  const volume = clampVolume(volumeValue);
  const lastVolume = !muted && volume > 0 ? volume : previous.lastVolume;
  const next = {
    lastVolume: lastVolume > 0 ? lastVolume : 1,
    muted: muted || volume === 0,
    version: 2,
    volume,
  };
  sharedVideoAudio = next;
  try {
    window.localStorage.setItem(videoAudioStorageKey, JSON.stringify(next));
  } catch {
    // localStorage can be unavailable in private or restricted browser contexts.
  }
  return next;
}

function loadVideoProxyClientId() {
  try {
    const existing = window.sessionStorage.getItem(videoProxyClientStorageKey);
    if (existing) return existing;
    const next = createVideoProxySessionId();
    window.sessionStorage.setItem(videoProxyClientStorageKey, next);
    return next;
  } catch {
    return createVideoProxySessionId();
  }
}

function createVideoProxySessionId() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function clampVolume(value: number) {
  if (!Number.isFinite(value)) return 1;
  return Math.min(1, Math.max(0, value));
}

function clampTime(value: number, max: number) {
  if (!Number.isFinite(value)) return 0;
  if (!Number.isFinite(max) || max <= 0) return Math.max(0, value);
  return Math.min(max, Math.max(0, value));
}
