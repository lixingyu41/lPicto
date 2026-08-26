import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from 'react';
import Hls, { type Fragment } from 'hls.js';
import { Check, ChevronDown, Database, Maximize2, Minimize2, Pause, PictureInPicture2, Play, RotateCw, Settings, SkipBack, SkipForward, Trash2, Volume2, VolumeX } from 'lucide-react';
import type { Asset, SubtitleInfo, VideoProxyHeartbeat, VideoProxyRuntime, VideoSegmentStatus, VideoStoryboard } from '../types/api';
import {
  api,
  assetPreviewUrl,
  assetStoryboardSheetUrl,
  assetSubtitleUrl,
  assetVideoHlsPlaylistUrl,
  assetVideoUrl,
} from '../api/client';
import { formatDuration } from '../utils/format';
import { isSafariBrowser } from '../utils/browser';
import { normalizeRotation, rotatedContainStyle } from '../utils/rotation';
import { setViewerMediaZoomActive } from '../utils/viewerInteractionState';
import {
  playbackRates,
  playbackModeOptions,
  zoomScaleRange,
  type ViewerPlaybackMode,
  type ViewerPrefs,
} from '../utils/viewerPrefs';
import DanmakuLayer from './DanmakuLayer';
import { viewerAudioOutputBridge } from './audioOutputBridge';
import type { ViewerMediaLayerMode } from './mediaLayer';
import type { ViewerMediaPlaybackController } from './mediaPlaybackController';
import MediaProgressSlider, { mediaBufferedRangesEqual, readMediaBufferedRanges, type MediaBufferedRange } from './MediaProgressSlider';

export interface VideoPlaybackInfo {
  audioNetworkBytesPerSecond: number;
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
  separateAVTransfers: boolean;
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
  videoNetworkBytesPerSecond: number;
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
  onDanmakuPrefChange: (key: DanmakuPrefKey, value: number) => void;
  onDelete: () => void;
  onDeleteRecord: () => void;
  onMediaError: (assetId: number, cacheKey: string, message: string) => void;
  onMediaReady: (assetId: number, cacheKey: string) => void;
  onPosterReady: (assetId: number, cacheKey: string) => void;
  onPriorityPreloadComplete: (assetId: number, cacheKey: string) => void;
  onPlaybackInfoChange?: (info: VideoPlaybackInfo | null) => void;
  onPlaybackControllerChange?: (assetId: number, controller: ViewerMediaPlaybackController | null) => void;
  onPlaybackEnded: () => void;
  onPrevious: () => void;
  onNext: () => void;
  previousEnabled: boolean;
  nextEnabled: boolean;
  onPlaybackModeChange: (value: ViewerPlaybackMode) => void;
  onPlaybackRateChange: (value: number) => void;
  onRotate: () => void;
  onSelectedSubtitleChange: (value: string) => void;
  onSubtitlesEnabledChange: (value: boolean) => void;
  onToggleFullscreen: () => void;
  onProxyRuntimeChange?: (runtime: VideoSegmentStatus | null) => void;
}

type DanmakuPrefKey = 'danmakuDensity' | 'danmakuFontScale' | 'danmakuOpacity' | 'danmakuSpeed';

const proxyPollMs = 3000;
const proxyKeepaliveMs = 15000;
const hlsFallbackSegmentSeconds = 4;
const hlsFirstSegmentSeconds = 2;
const hlsAheadSegments = 5;
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
  audioNetworkBytesPerSecond: number;
  browserCachedBytes: number;
  currentSegmentBytes: number;
  currentSegmentTotalBytes: number;
  networkBytesPerSecond: number;
  separateAVTransfers: boolean;
  videoNetworkBytesPerSecond: number;
}

type NetworkStreamKind = 'combined' | 'video' | 'audio';

type VideoRuntimeStatus = VideoProxyRuntime | VideoSegmentStatus;

type SafariVideoPresentationMode = 'inline' | 'picture-in-picture' | 'fullscreen';

interface PresentationVideoElement extends HTMLVideoElement {
  webkitPresentationMode?: SafariVideoPresentationMode;
  webkitSetPresentationMode?: (mode: SafariVideoPresentationMode) => void;
  webkitSupportsPresentationMode?: (mode: SafariVideoPresentationMode) => boolean;
}

const videoHoldZoomDelayMs = 220;
const videoHoldClickSuppressMs = 350;
const mobileFastForwardDelayMs = 420;
const mobileFastForwardRate = 2;
const mobilePressMoveTolerancePx = 12;

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
  onDanmakuPrefChange,
  onDelete,
  onDeleteRecord,
  onMediaError,
  onMediaReady,
  onPosterReady,
  onPriorityPreloadComplete,
  onPlaybackInfoChange,
  onPlaybackControllerChange,
  onPlaybackEnded,
  onPrevious,
  onNext,
  previousEnabled,
  nextEnabled,
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
  const fullWarmKeyRef = useRef('');
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
  const aheadPreloadIndexRef = useRef(-1);
  const hlsLoadStopped = useRef(false);
  const skipNextAudioPreferenceSave = useRef(false);
  const wantsPlaying = useRef(false);
  const resumeAttempts = useRef(0);
  const directRecoveryAttempts = useRef(0);
  const directRecoveryLastErrorAt = useRef(0);
  const pendingDirectRecovery = useRef<{
    assetId: number;
    cacheKey: string;
    resumeAt: number;
    shouldResume: boolean;
  } | null>(null);
  const currentTimeRef = useRef(0);
  const networkResourceBytes = useRef(new Map<string, number>());
  const networkIdleTimer = useRef<number | null>(null);
  const networkProgressTimer = useRef<number | null>(null);
  const activeNetworkFragment = useRef<{ fragment: Fragment; kind: NetworkStreamKind } | null>(null);
  const separateAVTransfersRef = useRef(false);
  const storyboardLoadingRef = useRef(false);
  const storyboardUnavailableKeyRef = useRef('');
  const holdZoomTimer = useRef(0);
  const holdZoomActiveRef = useRef(false);
  const suppressVideoClickUntil = useRef(0);
  const holdZoomPointer = useRef({ clientX: 0, clientY: 0 });
  const mobileFastForwardTimer = useRef<number | null>(null);
  const mobilePressGesture = useRef<{
    fastForwarding: boolean;
    pointerId: number;
    restoreRate: number;
    startX: number;
    startY: number;
  } | null>(null);
  const [liveAsset, setLiveAsset] = useState(asset);
  const [audio, setAudio] = useState<VideoAudioPreference>(() => loadVideoAudioPreference());
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [playbackModeOptionsOpen, setPlaybackModeOptionsOpen] = useState(false);
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
  const [bufferedRanges, setBufferedRanges] = useState<MediaBufferedRange[]>([]);
  const [nativeHlsSource, setNativeHlsSource] = useState('');
  const [proxyStreamEnabled, setProxyStreamEnabled] = useState(false);
  const [pictureInPictureSupported, setPictureInPictureSupported] = useState(false);
  const [pictureInPictureActive, setPictureInPictureActive] = useState(false);
  const [pictureInPictureError, setPictureInPictureError] = useState('');
  const [proxySessionId] = useState(() => createVideoProxySessionId());
  const [proxyStartTime, setProxyStartTime] = useState(0);
  const [proxyRuntime, setProxyRuntime] = useState<VideoSegmentStatus | null>(null);
  const [plannedSegmentSeconds, setPlannedSegmentSeconds] = useState(hlsFallbackSegmentSeconds);
  const [sourcePriority, setSourcePriority] = useState<'current' | 'preload'>(() => layerMode === 'active' ? 'current' : 'preload');
  const [directReloadNonce, setDirectReloadNonce] = useState(0);
  const [duration, setDuration] = useState(asset.duration ?? 0);
  const [currentTime, setCurrentTime] = useState(0);
  const [scrubTime, setScrubTime] = useState<number | null>(null);
  const [storyboard, setStoryboard] = useState<VideoStoryboard | null>(null);
  const [storyboardHover, setStoryboardHover] = useState<{ percent: number; time: number } | null>(null);
  const [loadedStoryboardSheets, setLoadedStoryboardSheets] = useState<Set<string>>(() => new Set());
  const [frameSize, setFrameSize] = useState({ width: 0, height: 0 });
  const [holdZoom, setHoldZoom] = useState({ active: false, originX: 50, originY: 50 });
  const zoomActivityOwner = useRef<object>({});
  const [mobileControlsVisible, setMobileControlsVisible] = useState(false);
  const [mobileFastForwarding, setMobileFastForwarding] = useState(false);
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
    audioNetworkBytesPerSecond: 0,
    browserCachedBytes: 0,
    currentSegmentBytes: 0,
    currentSegmentTotalBytes: 0,
    networkBytesPerSecond: 0,
    separateAVTransfers: false,
    videoNetworkBytesPerSecond: 0,
  });

  useEffect(() => {
    const active = layerMode === 'active' && holdZoom.active;
    setViewerMediaZoomActive(zoomActivityOwner.current, active);
    return () => setViewerMediaZoomActive(zoomActivityOwner.current, false);
  }, [holdZoom.active, layerMode]);
  const playbackAsset = liveAsset.id === asset.id && liveAsset.cacheKey === asset.cacheKey ? liveAsset : asset;
  const browserFirst = viewerPrefs.videoProcessingMode === 'browser';
  const usesProxy = !playbackAsset.browserPlayable && (!browserFirst || browserDirectFailed) && !proxyFailed;
  // Neighbour videos keep only their poster in the DOM. The real media pipeline
  // is created only after the video becomes the active viewer item.
  const mediaLoadEnabled = layerMode === 'active';
  const source = useMemo(() => {
    if (!mediaLoadEnabled) return '';
    if (usesProxy) {
      return assetVideoHlsPlaylistUrl(playbackAsset, {
        clientId: proxyClientId.current,
        sessionId: proxySessionId,
        priority: 'playback',
      });
    }
    const directSource = assetVideoUrl(playbackAsset, layerMode === 'active' ? 'current' : sourcePriority);
    if (directReloadNonce <= 0) return directSource;
    const fragmentIndex = directSource.indexOf('#');
    const requestSource = fragmentIndex >= 0 ? directSource.slice(0, fragmentIndex) : directSource;
    const fragment = fragmentIndex >= 0 ? directSource.slice(fragmentIndex) : '';
    return `${requestSource}&reload=${directReloadNonce}${fragment}`;
  }, [directReloadNonce, layerMode, mediaLoadEnabled, playbackAsset, proxySessionId, sourcePriority, usesProxy]);

  const subtitleSource = subtitlesEnabled && selectedSubtitleId ? assetSubtitleUrl(asset, selectedSubtitleId) : '';
  const canPlay = !sourceFailed && Boolean(source);
  const posterSource = asset.thumbStatus === 'ready' ? assetPreviewUrl(asset) : '';
  const showPosterLayer = Boolean(posterSource) && (!canPlay || !firstFrameReady);
  const statusLabel = videoStatusLabel(playbackAsset, sourceFailed, proxyRuntime);
  const directPrewarmStart = Math.floor(Math.max(0, currentTime) / plannedSegmentSeconds) * plannedSegmentSeconds;

  useEffect(() => {
    if (layerMode !== 'active' || usesProxy || !playbackAsset.browserPlayable) return;
    void api.prewarmDirectVideo(playbackAsset.id, directPrewarmStart).catch(() => undefined);
  }, [directPrewarmStart, layerMode, playbackAsset.browserPlayable, playbackAsset.id, source, usesProxy]);

  useEffect(() => {
    if (layerMode === 'active' && !preloadSegmentReady && sourcePriority !== 'current') setSourcePriority('current');
  }, [layerMode, preloadSegmentReady, sourcePriority]);
  const displayedTime = scrubTime ?? currentTime;
  const storyboardCandidate = useMemo(() => {
    if (!storyboard || !storyboardHover || storyboard.interval <= 0 || storyboard.cacheKey !== asset.cacheKey) return null;
    const frameIndex = Math.min(storyboard.frameCount - 1, Math.max(0, Math.floor(storyboardHover.time / storyboard.interval)));
    const sheet = Math.floor(frameIndex / (storyboard.columns * storyboard.rows));
    return {
      cellIndex: frameIndex % (storyboard.columns * storyboard.rows),
      imageUrl: assetStoryboardSheetUrl(asset, sheet),
      sheet,
    };
  }, [asset, storyboard, storyboardHover]);
  const storyboardPreview = storyboard && storyboardHover && storyboardCandidate && loadedStoryboardSheets.has(storyboardCandidate.imageUrl)
    ? {
        cellHeight: storyboard.cellHeight,
        cellIndex: storyboardCandidate.cellIndex,
        cellWidth: storyboard.cellWidth,
        columns: storyboard.columns,
        imageUrl: storyboardCandidate.imageUrl,
        label: formatDuration(storyboardHover.time),
        percent: storyboardHover.percent,
        rows: storyboard.rows,
      }
    : null;
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

  function updateBackgroundPreloadProgress(video: HTMLVideoElement) {
    if (layerModeRef.current === 'active' || preloadSegmentReadyRef.current) return;
    const mediaDuration = video.duration || asset.duration || 0;
    const target = Math.min(plannedSegmentSeconds, mediaDuration > 0 ? mediaDuration : plannedSegmentSeconds);
    let bufferedThrough = 0;
    for (let index = 0; index < video.buffered.length; index += 1) {
      if (video.buffered.start(index) <= 0.25) bufferedThrough = Math.max(bufferedThrough, video.buffered.end(index));
    }
    if (usesProxy && bufferedThrough > 0.05) {
      video.pause();
      video.currentTime = 0;
      currentTimeRef.current = 0;
      setCurrentTime(0);
      preloadSegmentReadyRef.current = true;
      setPreloadSegmentReady(true);
      notifyPreparedMedia();
      return;
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
    fullWarmKeyRef.current = '';
    preloadSegmentReadyRef.current = false;
    priorityPreloadKeyRef.current = '';
    aheadPreloadIndexRef.current = -1;
    setFirstFrameReady(false);
    setPreloadSegmentReady(false);
	storyboardLoadingRef.current = false;
	storyboardUnavailableKeyRef.current = '';
	setStoryboard(null);
	setStoryboardHover(null);
	setLoadedStoryboardSheets(new Set());
  }, [asset.id, asset.cacheKey]);

  useEffect(() => {
	if (layerMode !== 'active') return undefined;
	const controller = new AbortController();
	let pollTimer: number | null = null;
	let cancelled = false;
	const waitForPoll = () => new Promise<void>((resolve) => {
		pollTimer = window.setTimeout(resolve, 3000);
	});
	void (async () => {
		try {
			await api.generateAssetStoryboard(asset.id, controller.signal);
		} catch {
			return;
		}
		while (!cancelled) {
			try {
				const result = await api.assetStoryboard(asset.id, controller.signal);
				if (!cancelled && result.assetId === asset.id && result.cacheKey === asset.cacheKey) {
					storyboardUnavailableKeyRef.current = '';
					setStoryboard(result);
				}
				return;
			} catch {
				if (cancelled) return;
				await waitForPoll();
			}
		}
	})();
	return () => {
		cancelled = true;
		controller.abort();
		if (pollTimer !== null) window.clearTimeout(pollTimer);
	};
  }, [asset.cacheKey, asset.id, layerMode]);

  useEffect(() => {
	if (!storyboardCandidate || loadedStoryboardSheets.has(storyboardCandidate.imageUrl)) return undefined;
	let active = true;
	const image = new Image();
	image.onload = () => {
		if (!active) return;
		setLoadedStoryboardSheets((current) => {
			if (current.has(storyboardCandidate.imageUrl)) return current;
			const next = new Set(current);
			next.add(storyboardCandidate.imageUrl);
			return next;
		});
	};
	image.onerror = () => undefined;
	image.src = storyboardCandidate.imageUrl;
	return () => {
		active = false;
	};
  }, [loadedStoryboardSheets, storyboardCandidate]);

  function handleStoryboardHover(hover: { percent: number; time: number } | null) {
	setStoryboardHover(hover);
	const currentKey = `${asset.id}:${asset.cacheKey}`;
	if (!hover || storyboard || storyboardLoadingRef.current || storyboardUnavailableKeyRef.current === currentKey) return;
	storyboardLoadingRef.current = true;
	const requestedID = asset.id;
	const requestedKey = asset.cacheKey;
	void api.assetStoryboard(requestedID).then((result) => {
		if (result.assetId === requestedID && result.cacheKey === requestedKey) setStoryboard(result);
	}).catch(() => {
		storyboardUnavailableKeyRef.current = currentKey;
	}).finally(() => {
		storyboardLoadingRef.current = false;
	});
  }

  useEffect(() => {
    if (layerMode !== 'active' || !usesProxy) return undefined;
    const key = `${asset.id}:${asset.cacheKey}`;
    if (priorityPreloadKeyRef.current === key) return undefined;
    const controller = new AbortController();
    const session = { clientId: proxyClientId.current, sessionId: proxySessionId };
    void (async () => {
      const first = await api.prewarmVideoSegments(asset.id, 0, 1, 'playback', session, controller.signal);
      if (first.segmentSeconds && first.segmentSeconds > 0) setPlannedSegmentSeconds(first.segmentSeconds);
      await api.prewarmVideoSegments(asset.id, 1, 1, 'critical', session, controller.signal);
      if (controller.signal.aborted || layerModeRef.current !== 'active') return;
      onPriorityPreloadComplete(asset.id, asset.cacheKey);
      priorityPreloadKeyRef.current = key;
    })().catch((err: unknown) => {
      if (controller.signal.aborted) return;
      onMediaError(asset.id, asset.cacheKey, err instanceof Error ? `当前视频预缓存失败：${err.message}` : '当前视频预缓存失败');
    });
    return () => controller.abort();
  }, [asset.cacheKey, asset.id, layerMode, onMediaError, onPriorityPreloadComplete, proxySessionId, usesProxy]);

  useEffect(() => {
    if (layerMode !== 'active' || !usesProxy || plannedSegmentSeconds <= 0) return;
    const currentSegment = hlsSegmentIndexForTime(currentTime, plannedSegmentSeconds);
    const nextSegment = currentSegment + 1;
    if (aheadPreloadIndexRef.current === nextSegment) return;
    aheadPreloadIndexRef.current = nextSegment;
    void api.prewarmVideoSegments(
      asset.id,
      nextSegment,
      1,
      'critical',
      { clientId: proxyClientId.current, sessionId: proxySessionId },
    ).catch(() => undefined);
  }, [asset.id, currentTime, layerMode, plannedSegmentSeconds, proxySessionId, usesProxy]);

  useEffect(() => {
    if (layerMode !== 'active' || !hasPlaybackStarted) return;
    const totalDuration = duration || asset.duration || 0;
    if (totalDuration <= 0) return;
    const fullWarmThreshold = Math.min(120, totalDuration * 0.1);
    if (currentTime + 0.05 < fullWarmThreshold) return;
    const key = `${asset.id}:${asset.cacheKey}`;
    if (fullWarmKeyRef.current === key) return;
    if (!usesProxy && !playbackAsset.browserPlayable) return;
    fullWarmKeyRef.current = key;
    if (usesProxy) {
      const from = hlsSegmentIndexForTime(currentTime, plannedSegmentSeconds);
      void api.prewarmAllVideoSegments(
        asset.id,
        from,
        { clientId: proxyClientId.current, sessionId: proxySessionId },
      ).catch(() => {
        if (fullWarmKeyRef.current === key) fullWarmKeyRef.current = '';
      });
      return;
    }
    if (playbackAsset.browserPlayable) {
      void api.prewarmDirectVideo(asset.id, currentTime, true).catch(() => {
        if (fullWarmKeyRef.current === key) fullWarmKeyRef.current = '';
      });
    }
  }, [asset.cacheKey, asset.duration, asset.id, currentTime, duration, hasPlaybackStarted, layerMode, plannedSegmentSeconds, playbackAsset.browserPlayable, proxySessionId, usesProxy]);

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

  useEffect(() => () => stopHLSSession(), [asset.id, proxySessionId, usesProxy]);

  const closeSettings = () => {
    setSettingsOpen(false);
    setPlaybackModeOptionsOpen(false);
    setPlaybackOptionsOpen(false);
    setDanmakuOptionsOpen(false);
  };

  const toggleSettings = () => {
    if (settingsOpen) {
      closeSettings();
      return;
    }
    setPlaybackModeOptionsOpen(false);
    setPlaybackOptionsOpen(false);
    setDanmakuOptionsOpen(false);
    setSettingsOpen(true);
  };

  const mediaStyle = useMemo(() => {
    const rotation = normalizeRotation(asset.rotation);
    if (rotation === 0) return undefined;
    return { ...rotatedContainStyle(asset, frameSize), bottom: 'auto', right: 'auto' };
  }, [asset, frameSize.height, frameSize.width]);

  const holdZoomScale = viewerPrefs.zoomMode === 'pixels'
    ? Math.min(
        zoomScaleRange.max,
        Math.max(zoomScaleRange.min, Math.min(frameSize.width, frameSize.height) / Math.max(1, viewerPrefs.zoomPixelArea)),
      )
    : Math.min(zoomScaleRange.max, Math.max(zoomScaleRange.min, viewerPrefs.zoomScale));
  const zoomedMediaStyle = useMemo(() => {
    if (!holdZoom.active) return mediaStyle;
    const baseTransform = typeof mediaStyle?.transform === 'string' ? mediaStyle.transform : '';
    return {
      ...mediaStyle,
      transform: `${baseTransform}${baseTransform ? ' ' : ''}scale(${holdZoomScale})`,
      transformOrigin: `${holdZoom.originX}% ${holdZoom.originY}%`,
    };
  }, [holdZoom.active, holdZoom.originX, holdZoom.originY, holdZoomScale, mediaStyle]);

  const clearHoldZoomTimer = () => {
    if (holdZoomTimer.current) window.clearTimeout(holdZoomTimer.current);
    holdZoomTimer.current = 0;
  };

  const clearMobileFastForwardTimer = () => {
    if (mobileFastForwardTimer.current === null) return;
    window.clearTimeout(mobileFastForwardTimer.current);
    mobileFastForwardTimer.current = null;
  };

  const endMobileFastForward = () => {
    clearMobileFastForwardTimer();
    const gesture = mobilePressGesture.current;
    mobilePressGesture.current = null;
    if (!gesture?.fastForwarding) return;
    const video = ref.current;
    if (video) video.playbackRate = gesture.restoreRate;
    suppressVideoClickUntil.current = Date.now() + videoHoldClickSuppressMs;
    setMobileFastForwarding(false);
  };

  const handleMobilePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!isMobileViewerInteraction() || !event.isPrimary || layerMode !== 'active') return;
    clearMobileFastForwardTimer();
    mobilePressGesture.current = {
      fastForwarding: false,
      pointerId: event.pointerId,
      restoreRate: ref.current?.playbackRate || playbackRate,
      startX: event.clientX,
      startY: event.clientY,
    };
    event.currentTarget.setPointerCapture?.(event.pointerId);
    mobileFastForwardTimer.current = window.setTimeout(() => {
      mobileFastForwardTimer.current = null;
      const gesture = mobilePressGesture.current;
      const video = ref.current;
      if (!gesture || gesture.pointerId !== event.pointerId || !video || video.paused || video.ended) return;
      gesture.fastForwarding = true;
      gesture.restoreRate = video.playbackRate;
      video.playbackRate = Math.max(mobileFastForwardRate, video.playbackRate);
      suppressVideoClickUntil.current = Date.now() + 1000;
      setMobileFastForwarding(true);
      setMobileControlsVisible(false);
    }, mobileFastForwardDelayMs);
  };

  const handleMobilePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const gesture = mobilePressGesture.current;
    if (!gesture || gesture.pointerId !== event.pointerId || gesture.fastForwarding) return;
    if (
      Math.abs(event.clientX - gesture.startX) > mobilePressMoveTolerancePx
      || Math.abs(event.clientY - gesture.startY) > mobilePressMoveTolerancePx
    ) clearMobileFastForwardTimer();
  };

  const handleMobilePointerEnd = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (mobilePressGesture.current?.pointerId !== event.pointerId) return;
    endMobileFastForward();
  };

  const updateHoldZoomOrigin = (clientX: number, clientY: number) => {
    const frame = frameRef.current;
    if (!frame) return;
    holdZoomPointer.current = { clientX, clientY };
    const rect = frame.getBoundingClientRect();
    const originX = Math.min(100, Math.max(0, ((clientX - rect.left) / Math.max(1, rect.width)) * 100));
    const originY = Math.min(100, Math.max(0, ((clientY - rect.top) / Math.max(1, rect.height)) * 100));
    setHoldZoom((current) => ({ ...current, originX, originY }));
  };

  const endHoldZoom = () => {
    clearHoldZoomTimer();
    if (!holdZoomActiveRef.current) return;
    holdZoomActiveRef.current = false;
    suppressVideoClickUntil.current = Date.now() + videoHoldClickSuppressMs;
    setHoldZoom((current) => ({ ...current, active: false }));
  };

  const handleVideoMouseDown = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (isMobileViewerInteraction() || event.button !== 0 || layerMode !== 'active' || (!firstFrameReady && !posterSource)) return;
    clearHoldZoomTimer();
    holdZoomPointer.current = { clientX: event.clientX, clientY: event.clientY };
    holdZoomTimer.current = window.setTimeout(() => {
      holdZoomTimer.current = 0;
      holdZoomActiveRef.current = true;
      suppressVideoClickUntil.current = Date.now() + 1000;
      updateHoldZoomOrigin(holdZoomPointer.current.clientX, holdZoomPointer.current.clientY);
      setHoldZoom((current) => ({ ...current, active: true }));
    }, videoHoldZoomDelayMs);
  };

  const handleVideoMouseMove = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (isMobileViewerInteraction()) return;
    holdZoomPointer.current = { clientX: event.clientX, clientY: event.clientY };
    if (!holdZoomActiveRef.current) return;
    if (event.buttons !== 1) {
      endHoldZoom();
      return;
    }
    updateHoldZoomOrigin(event.clientX, event.clientY);
  };

  const handleVideoSurfaceClick = (event: ReactMouseEvent<HTMLElement>) => {
    if (Date.now() <= suppressVideoClickUntil.current) {
      event.preventDefault();
      event.stopPropagation();
      suppressVideoClickUntil.current = 0;
      return;
    }
    if (isMobileViewerInteraction()) {
      event.preventDefault();
      event.stopPropagation();
      setMobileControlsVisible((visible) => {
        if (visible) closeSettings();
        return !visible;
      });
      return;
    }
    togglePlay();
  };

  useEffect(() => {
    const handleWindowMouseUp = () => endHoldZoom();
    window.addEventListener('mouseup', handleWindowMouseUp);
    return () => {
      window.removeEventListener('mouseup', handleWindowMouseUp);
      clearHoldZoomTimer();
      endMobileFastForward();
    };
  }, []);

  useEffect(() => {
    clearHoldZoomTimer();
    holdZoomActiveRef.current = false;
    suppressVideoClickUntil.current = 0;
    setHoldZoom({ active: false, originX: 50, originY: 50 });
    endMobileFastForward();
    setMobileControlsVisible(false);
  }, [asset.cacheKey, asset.id, layerMode]);

  const startProxyPlayback = () => {
    wantsPlaying.current = true;
    setPlayRequested(true);
    proxyPlayPending.current = true;
    resumeAttempts.current = 0;
    setPlayError('');
    setSourceFailed(false);
    setProxyStreamEnabled(true);
  };

  const promoteCurrentPlaybackSegment = (time = currentTimeRef.current) => {
    if (!usesProxy) return;
    const segmentIndex = hlsSegmentIndexForTime(time, plannedSegmentSeconds);
    void api.prewarmVideoSegments(
      asset.id,
      segmentIndex,
      1,
      'playback',
      { clientId: proxyClientId.current, sessionId: proxySessionId },
    ).catch(() => undefined);
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
      viewerAudioOutputBridge.prime();
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

  useEffect(() => {
    if (layerMode !== 'active') return;
    const pause = () => {
      wantsPlaying.current = false;
      proxyPlayPending.current = false;
      setPlayRequested(false);
      clearAutoplayTimer();
      clearResumeTimer();
      ref.current?.pause();
    };
    const controller: ViewerMediaPlaybackController = {
      play: () => {
        const video = ref.current;
        if (!video || !canPlay) return;
        viewerAudioOutputBridge.prime();
        void startPlayback(video);
      },
      pause,
      stop: () => {
        pause();
        const video = ref.current;
        if (!video) return;
        video.currentTime = 0;
        currentTimeRef.current = 0;
        setCurrentTime(0);
        setEnded(false);
      },
    };
    onPlaybackControllerChange?.(asset.id, controller);
    return () => onPlaybackControllerChange?.(asset.id, null);
  }, [asset.id, canPlay, layerMode, onPlaybackControllerChange]);

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

  const recordNetworkProgress = (key: string, loaded: number, total: number, startedAt: number, transferred = loaded, kind: NetworkStreamKind = 'combined') => {
    const safeLoaded = Math.max(0, Number.isFinite(loaded) ? loaded : 0);
    const safeTotal = Math.max(safeLoaded, Number.isFinite(total) ? total : 0);
    const previous = networkResourceBytes.current.get(key) ?? 0;
    const added = Math.max(0, safeLoaded - previous);
    networkResourceBytes.current.set(key, Math.max(previous, safeLoaded));
    const elapsedMs = Math.max(1, performance.now() - startedAt);
    const safeTransferred = Math.max(0, Number.isFinite(transferred) ? transferred : 0);
    const speed = safeTransferred > 0 ? safeTransferred * 1000 / elapsedMs : 0;
    setNetworkMetrics((current) => ({
      ...current,
      audioNetworkBytesPerSecond: kind === 'audio' ? speed : current.audioNetworkBytesPerSecond,
      browserCachedBytes: current.browserCachedBytes + added,
      currentSegmentBytes: safeLoaded,
      currentSegmentTotalBytes: safeTotal,
      networkBytesPerSecond: kind === 'combined' ? speed : current.networkBytesPerSecond,
      separateAVTransfers: current.separateAVTransfers || kind !== 'combined',
      videoNetworkBytesPerSecond: kind === 'video' ? speed : current.videoNetworkBytesPerSecond,
    }));
    if (networkIdleTimer.current !== null) window.clearTimeout(networkIdleTimer.current);
    networkIdleTimer.current = window.setTimeout(() => {
      networkIdleTimer.current = null;
      setNetworkMetrics((current) => ({
        ...current,
        audioNetworkBytesPerSecond: 0,
        networkBytesPerSecond: 0,
        videoNetworkBytesPerSecond: 0,
      }));
    }, 2500);
  };

  const stopFragmentNetworkProgress = () => {
    activeNetworkFragment.current = null;
    if (networkProgressTimer.current === null) return;
    window.clearInterval(networkProgressTimer.current);
    networkProgressTimer.current = null;
  };

  const fragmentNetworkKind = (fragment: Fragment): NetworkStreamKind => {
    if (String(fragment.type) === 'audio') {
      separateAVTransfersRef.current = true;
      return 'audio';
    }
    return separateAVTransfersRef.current ? 'video' : 'combined';
  };

  const startFragmentNetworkProgress = (fragment: Fragment, kind: NetworkStreamKind) => {
    stopFragmentNetworkProgress();
    activeNetworkFragment.current = { fragment, kind };
    const update = () => {
      const current = activeNetworkFragment.current;
      if (!current) return;
      const stats = current.fragment.stats;
      recordNetworkProgress(current.fragment.url, stats.loaded, stats.total, stats.loading.start, stats.loaded, current.kind);
    };
    update();
    networkProgressTimer.current = window.setInterval(update, 250);
  };

  async function startPlayback(video: HTMLVideoElement) {
    wantsPlaying.current = true;
    setPlayRequested(true);
    setEnded(false);
    setPlayError('');
    promoteCurrentPlaybackSegment(video.currentTime);
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

  function recoverDirectPlayback(video: HTMLVideoElement) {
    if (usesProxy || layerModeRef.current !== 'active') return false;
    const now = Date.now();
    if (now-directRecoveryLastErrorAt.current > 30_000) directRecoveryAttempts.current = 0;
    if (directRecoveryAttempts.current >= 2) return false;
    directRecoveryAttempts.current += 1;
    directRecoveryLastErrorAt.current = now;
    pendingDirectRecovery.current = {
      assetId: asset.id,
      cacheKey: asset.cacheKey,
      resumeAt: clampTime(video.currentTime || currentTimeRef.current, duration || asset.duration || 0),
      shouldResume: wantsPlaying.current || !video.paused || hasPlaybackStarted,
    };
    setHlsPhase('direct');
    setSourceFailed(false);
    setBuffering(true);
    setWaitingTrigger('network');
    setPlayError('');
    setDirectReloadNonce((current) => current + 1);
    return true;
  }

  function resumeRecoveredDirectPlayback(video: HTMLVideoElement) {
    const pending = pendingDirectRecovery.current;
    if (!pending || pending.assetId !== asset.id || pending.cacheKey !== asset.cacheKey) return;
    pendingDirectRecovery.current = null;
    if (pending.shouldResume) void startPlayback(video);
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
        hlsRef.current.config.maxBufferLength = plannedSegmentSeconds * hlsAheadSegments;
        hlsRef.current.config.maxMaxBufferLength = plannedSegmentSeconds * hlsAheadSegments;
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
      backBufferLength: plannedSegmentSeconds,
      fragLoadingMaxRetry: 2,
      lowLatencyMode: false,
      maxBufferLength: plannedSegmentSeconds * hlsAheadSegments,
      maxMaxBufferLength: plannedSegmentSeconds * hlsAheadSegments,
      startFragPrefetch: layerModeRef.current !== 'active',
      xhrSetup: (xhr, url) => {
        if (!url.includes('/hls/segments/')) return;
        const priority = layerModeRef.current === 'active' ? 'playback' : 'preload';
        xhr.setRequestHeader('X-LPicto-Segment-Priority', priority);
      },
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
      startFragmentNetworkProgress(data.frag, fragmentNetworkKind(data.frag));
    });
    hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
      setHlsPhase('ready');
      setBuffering(false);
      const stats = data.frag.stats;
      const payloadBytes = data.payload.byteLength;
      const loaded = Math.max(stats.loaded, payloadBytes);
      const active = activeNetworkFragment.current;
      const kind = active?.fragment.url === data.frag.url ? active.kind : fragmentNetworkKind(data.frag);
      recordNetworkProgress(data.frag.url, loaded, Math.max(stats.total, loaded), stats.loading.start, stats.loaded, kind);
      stopFragmentNetworkProgress();
      void pollVideoSegmentStatus(asset.id, proxySessionId);
    });
    hls.on(Hls.Events.FRAG_BUFFERED, () => {
      updateBufferedRange(video);
      updateBackgroundPreloadProgress(video);
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
  }, [asset.id, onMediaError, plannedSegmentSeconds, proxySessionId, source, usesProxy]);

  useEffect(() => {
    networkResourceBytes.current.clear();
    separateAVTransfersRef.current = false;
    setNetworkMetrics({
      audioNetworkBytesPerSecond: 0,
      browserCachedBytes: 0,
      currentSegmentBytes: 0,
      currentSegmentTotalBytes: 0,
      networkBytesPerSecond: 0,
      separateAVTransfers: false,
      videoNetworkBytesPerSecond: 0,
    });
    if (usesProxy) return undefined;
    if (typeof PerformanceObserver === 'undefined') return undefined;
    const startedAt = performance.now();
    const pathMarkers = [`/api/assets/${asset.id}/video`];
    const observer = new PerformanceObserver((list) => {
      list.getEntries().forEach((entry) => {
        if (!(entry instanceof PerformanceResourceTiming) || entry.startTime < startedAt || !pathMarkers.some((marker) => entry.name.includes(marker))) return;
        const bytes = entry.decodedBodySize || entry.encodedBodySize || entry.transferSize || 0;
        const key = `${entry.name}:${entry.startTime}`;
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
      autoplayDelaySeconds: viewerPrefs.videoPlaybackDelaySeconds,
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
      videoMetrics.networkState, videoMetrics.readyState, viewerPrefs.videoAutoplay,
      viewerPrefs.videoPlaybackDelaySeconds, waitingTrigger,
    ],
  );

  const playbackInfo = useMemo<VideoPlaybackInfo>(
    () => ({
      audioNetworkBytesPerSecond: networkMetrics.audioNetworkBytesPerSecond,
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
      separateAVTransfers: networkMetrics.separateAVTransfers,
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
      videoNetworkBytesPerSecond: networkMetrics.videoNetworkBytesPerSecond,
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
        ref.current.playbackRate = playbackRate;
        void startPlayback(ref.current);
      }
      autoplayTimer.current = null;
    }, viewerPrefs.videoPlaybackDelaySeconds * 1000);
    return clearAutoplayTimer;
  }, [
    asset.id,
    canPlay,
    layerMode,
    playbackRate,
    source,
    viewerPrefs.playbackMode,
    viewerPrefs.videoAutoplay,
    viewerPrefs.videoPlaybackDelaySeconds,
  ]);

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
    setBufferedRanges([]);
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
    setPlannedSegmentSeconds(hlsFallbackSegmentSeconds);
    setSourcePriority(layerModeRef.current === 'active' ? 'current' : 'preload');
    setDirectReloadNonce(0);
    proxyPlayPending.current = false;
    wantsPlaying.current = false;
    resumeAttempts.current = 0;
    directRecoveryAttempts.current = 0;
    directRecoveryLastErrorAt.current = 0;
    pendingDirectRecovery.current = null;
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
        if (runtime.segmentSeconds > 0) setPlannedSegmentSeconds(runtime.segmentSeconds);
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
    const mediaDuration = duration || asset.duration || video.duration || 0;
    const ranges = readMediaBufferedRanges(video, mediaDuration);
    setBufferedRanges((current) => mediaBufferedRangesEqual(current, ranges) ? current : ranges);
    let next = video.currentTime;
    for (let index = 0; index < video.buffered.length; index++) {
      const start = video.buffered.start(index);
      const end = video.buffered.end(index);
      if (start <= video.currentTime + 0.05 && end >= video.currentTime - 0.05) {
        next = end;
        break;
      }
    }
    setBufferedEnd(clampTime(next, duration || asset.duration || next));
    const hls = hlsRef.current;
    if (!hls || layerModeRef.current !== 'active') return;
    const forwardSeconds = Math.max(0, next - video.currentTime);
    if (forwardSeconds >= plannedSegmentSeconds * hlsAheadSegments && !hlsLoadStopped.current) {
      hls.stopLoad();
      hlsLoadStopped.current = true;
    } else if (forwardSeconds < plannedSegmentSeconds && hlsLoadStopped.current) {
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

  useEffect(() => {
    const video = ref.current as PresentationVideoElement | null;
    if (!video || layerMode !== 'active' || !isSafariBrowser()) {
      setPictureInPictureSupported(false);
      setPictureInPictureActive(false);
      return undefined;
    }
    const webkitSupported = Boolean(
      video.webkitSetPresentationMode
      && video.webkitSupportsPresentationMode?.('picture-in-picture'),
    );
    const standardSupported = document.pictureInPictureEnabled;
    setPictureInPictureSupported(webkitSupported || standardSupported);

    const syncPresentationState = () => {
      setPictureInPictureActive(
        video.webkitPresentationMode === 'picture-in-picture'
        || document.pictureInPictureElement === video,
      );
    };
    video.addEventListener('webkitpresentationmodechanged', syncPresentationState);
    video.addEventListener('enterpictureinpicture', syncPresentationState);
    video.addEventListener('leavepictureinpicture', syncPresentationState);
    syncPresentationState();
    return () => {
      video.removeEventListener('webkitpresentationmodechanged', syncPresentationState);
      video.removeEventListener('enterpictureinpicture', syncPresentationState);
      video.removeEventListener('leavepictureinpicture', syncPresentationState);
    };
  }, [asset.cacheKey, asset.id, canPlay, layerMode]);

  async function togglePictureInPicture() {
    const video = ref.current as PresentationVideoElement | null;
    if (!video || layerMode !== 'active') return;
    setPictureInPictureError('');
    try {
      if (
        video.webkitSetPresentationMode
        && video.webkitSupportsPresentationMode?.('picture-in-picture')
      ) {
        video.webkitSetPresentationMode(
          video.webkitPresentationMode === 'picture-in-picture' ? 'inline' : 'picture-in-picture',
        );
        return;
      }

      if (document.pictureInPictureElement === video) {
        await document.exitPictureInPicture();
      } else {
        await video.requestPictureInPicture();
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : '当前浏览器无法进入画中画';
      setPictureInPictureError(message || '当前浏览器无法进入画中画');
    }
  }

  return (
    <div className={`${canPlay ? 'video-stage' : 'video-stage video-stage-pending'}${holdZoom.active ? ' video-hold-zoom-active' : ''}${mobileControlsVisible ? ' mobile-controls-visible' : ''}${mobileFastForwarding ? ' mobile-fast-forward-active' : ''}`}>
      <div
        className={holdZoom.active ? 'video-frame video-hold-zooming' : 'video-frame'}
        ref={frameRef}
        onMouseDown={handleVideoMouseDown}
        onMouseMove={handleVideoMouseMove}
        onMouseUp={endHoldZoom}
        onPointerCancel={handleMobilePointerEnd}
        onPointerDown={handleMobilePointerDown}
        onPointerMove={handleMobilePointerMove}
        onPointerUp={handleMobilePointerEnd}
        onMouseLeave={() => {
          if (!holdZoomActiveRef.current) clearHoldZoomTimer();
        }}
      >
        {canPlay && (
          <video
            ref={ref}
            className="viewer-video"
            src={nativeHlsSource || undefined}
            poster={firstFrameReady ? undefined : posterSource || undefined}
            preload={layerMode === 'prepare' && !preloadSegmentReady ? 'auto' : 'metadata'}
            playsInline
            loop={viewerPrefs.playbackMode === 'single'}
            style={zoomedMediaStyle}
            onClick={handleVideoSurfaceClick}
            onDurationChange={(event) => {
              setDuration(event.currentTarget.duration || asset.duration || 0);
              updateBackgroundPreloadProgress(event.currentTarget);
            }}
            onLoadedData={(event) => {
              setFirstFrameReady(true);
              if (layerModeRef.current === 'active') notifyPreparedMedia();
              else updateBackgroundPreloadProgress(event.currentTarget);
            }}
            onError={(event) => {
              if (!usesProxy && browserFirst && !playbackAsset.browserPlayable && !browserDirectFailed) {
                setBrowserDirectFailed(true);
                setHlsPhase('idle');
                setSourceFailed(false);
                setBuffering(false);
                setPlayError('');
                return;
              }
              if (recoverDirectPlayback(event.currentTarget)) return;
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
              const pending = pendingDirectRecovery.current;
              if (pending && pending.assetId === asset.id && pending.cacheKey === asset.cacheKey) {
                const maxTime = Math.max(0, (event.currentTarget.duration || asset.duration || 0) - 0.05);
                event.currentTarget.currentTime = Math.min(pending.resumeAt, maxTime);
              }
              updateBufferedRange(event.currentTarget);
              updateVideoMetrics(event.currentTarget);
              updateBackgroundPreloadProgress(event.currentTarget);
            }}
            onPause={(event) => {
              setPaused(true);
              setEnded(event.currentTarget.ended);
              if (layerMode === 'active') viewerAudioOutputBridge.mediaStopped(`${asset.id}:${asset.cacheKey}`);
              scheduleResumeAfterUnexpectedPause(event.currentTarget);
            }}
            onPlaying={(event) => {
              setBuffering(false);
              setSeeking(false);
              setEnded(false);
              setWaitingTrigger('none');
              if (layerMode === 'active') {
                viewerAudioOutputBridge.mediaStarted(`${asset.id}:${asset.cacheKey}`);
                void api.markAssetPlayed(asset.id).catch(() => undefined);
              }
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
              updateBackgroundPreloadProgress(event.currentTarget);
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
              if (!event.currentTarget.seeking) resumeRecoveredDirectPlayback(event.currentTarget);
              if (layerModeRef.current === 'active') notifyMediaReady();
              else updateBackgroundPreloadProgress(event.currentTarget);
            }}
            onSeeked={(event) => {
              setBuffering(false);
              setSeeking(false);
              setWaitingTrigger('none');
              updateBufferedRange(event.currentTarget);
              updateBackgroundPreloadProgress(event.currentTarget);
              resumeRecoveredDirectPlayback(event.currentTarget);
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
              viewerAudioOutputBridge.mediaStopped(`${asset.id}:${asset.cacheKey}`);
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
        {posterSource && (
          <button
            className={`${canPlay ? 'video-poster-layer playable' : 'video-poster-layer'}${firstFrameReady ? ' video-poster-layer-ready' : ''}`}
            type="button"
            disabled={!canPlay || firstFrameReady}
            aria-hidden={firstFrameReady}
            tabIndex={firstFrameReady ? -1 : undefined}
            onClick={handleVideoSurfaceClick}
          >
            <img
              src={posterSource}
              alt={asset.filename}
              style={zoomedMediaStyle}
              onLoad={() => {
                if (layerModeRef.current !== 'active') onPosterReady(asset.id, asset.cacheKey);
              }}
            />
            {!firstFrameReady && canPlay && !holdZoom.active ? (
              <>
                <span className="video-big-play">
                  <Play size={34} fill="currentColor" />
                </span>
                {playError && <span className="video-play-error">{playError}</span>}
              </>
            ) : !firstFrameReady ? (
              <span className="video-status-badge">{statusLabel}</span>
            ) : null}
          </button>
        )}
        {!posterSource && !canPlay && <div className="video-pending">{statusLabel}</div>}
        {!posterSource && canPlay && usesProxy && !proxyStreamEnabled && (
          <button className="video-pending video-pending-button" type="button" onClick={handleVideoSurfaceClick}>
            <Play size={34} fill="currentColor" />
            <span>点击播放开始转码</span>
          </button>
        )}
        {mobileFastForwarding && <div className="video-mobile-fast-forward" role="status">{Math.max(mobileFastForwardRate, playbackRate)}× 快进</div>}
      </div>
      <div
        className={settingsOpen ? 'video-control-zone settings-open' : 'video-control-zone'}
        aria-hidden={holdZoom.active}
        onClick={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <div className="video-controls">
          <MediaProgressSlider
            className="video-progress-slider"
            ariaLabel="播放进度"
            buffered={bufferedRanges}
            disabled={!canPlay}
            duration={duration}
            value={Math.min(displayedTime, duration || displayedTime)}
            preview={storyboardPreview}
            onHoverChange={handleStoryboardHover}
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
          <div className="video-controls-row">
            <button type="button" aria-label="上一个" disabled={!previousEnabled} onClick={onPrevious} title={previousEnabled ? '上一个' : '上一个尚未预加载完成'}>
              <SkipBack size={20} />
            </button>
            <button className="video-primary-play" type="button" aria-label={paused ? '播放' : '暂停'} disabled={!canPlay} onClick={togglePlay} title={paused ? '播放' : '暂停'}>
              {paused ? <Play size={24} fill="currentColor" /> : <Pause size={24} fill="currentColor" />}
            </button>
            <button type="button" aria-label="下一个" disabled={!nextEnabled} onClick={onNext} title={nextEnabled ? '下一个' : '下一个尚未预加载完成'}>
              <SkipForward size={20} />
            </button>
            <span className="video-time">
              <span>{formatDuration(displayedTime)}</span>
              <i>/</i>
              <span>{formatDuration(duration)}</span>
            </span>
            <span className="video-controls-spacer" aria-hidden="true" />
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
                {audio.muted ? <VolumeX size={20} /> : <Volume2 size={20} />}
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
                onPointerDown={(event) => {
                  if (event.button === 0) toggleSettings();
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') toggleSettings();
                }}
              >
                <Settings size={20} />
              </button>
              {settingsOpen && (
              <div className="video-settings-popover" role="dialog" aria-label="视频设置">
                {playbackModeOptionsOpen && (
                  <div className="video-settings-choice-list" role="listbox" aria-label="播放模式">
                    {playbackModeOptions.map((option) => (
                      <button
                        className={option.value === viewerPrefs.playbackMode ? 'video-settings-choice selected' : 'video-settings-choice'}
                        key={option.value}
                        type="button"
                        role="option"
                        aria-selected={option.value === viewerPrefs.playbackMode}
                        onClick={() => {
                          onPlaybackModeChange(option.value as ViewerPlaybackMode);
                          setPlaybackModeOptionsOpen(false);
                        }}
                      >
                        <span>{option.label}</span>
                        {option.value === viewerPrefs.playbackMode && <Check size={16} />}
                      </button>
                    ))}
                  </div>
                )}
                <button
                  className="video-settings-expand"
                  type="button"
                  aria-expanded={playbackModeOptionsOpen}
                  onClick={() => {
                    setPlaybackModeOptionsOpen((value) => !value);
                    setPlaybackOptionsOpen(false);
                    setDanmakuOptionsOpen(false);
                  }}
                >
                  <span>播放模式</span>
                  <span className="video-settings-expand-value">
                    <output>{playbackModeOptions.find((option) => option.value === viewerPrefs.playbackMode)?.label ?? '顺序播放'}</output>
                    <ChevronDown className={playbackModeOptionsOpen ? 'expanded' : undefined} size={16} />
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
                <button
                  className="video-settings-expand"
                  type="button"
                  aria-expanded={playbackOptionsOpen}
                  onClick={() => {
                    setPlaybackOptionsOpen((value) => !value);
                    setPlaybackModeOptionsOpen(false);
                    setDanmakuOptionsOpen(false);
                  }}
                >
                  <span>播放速度</span>
                  <span className="video-settings-expand-value">
                    <output>{formatDiscreteValue(playbackRate)}</output>
                    <ChevronDown className={playbackOptionsOpen ? 'expanded' : undefined} size={16} />
                  </span>
                </button>
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
                  onClick={() => {
                    setDanmakuOptionsOpen((value) => !value);
                    setPlaybackModeOptionsOpen(false);
                    setPlaybackOptionsOpen(false);
                  }}
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
                <button
                  className="video-settings-action danger"
                  type="button"
                  disabled={deleting}
                  onClick={() => {
                    closeSettings();
                    onDeleteRecord();
                  }}
                >
                  <span>删除记录</span>
                  <Database size={16} />
                </button>
              </div>
              )}
            </div>
            <div className="video-control-group video-option-controls">
              {pictureInPictureSupported && (
              <button
                type="button"
                className={pictureInPictureActive ? 'active' : undefined}
                aria-label={pictureInPictureActive ? '退出画中画' : '画中画'}
                title={pictureInPictureError || (pictureInPictureActive ? '退出画中画' : '画中画')}
                onClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  void togglePictureInPicture();
                }}
              >
                <PictureInPicture2 size={20} />
              </button>
              )}
              <button
                type="button"
                title={fullscreen ? '退出全屏' : '全屏'}
                onClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  onToggleFullscreen();
                }}
              >
                {fullscreen ? <Minimize2 size={20} /> : <Maximize2 size={20} />}
              </button>
            </div>
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

function hlsSegmentIndexForTime(time: number, segmentSeconds: number) {
  const normalizedTime = Math.max(0, time);
  const normalizedSegmentSeconds = Math.max(hlsFirstSegmentSeconds, segmentSeconds);
  const firstDuration = Math.min(hlsFirstSegmentSeconds, normalizedSegmentSeconds);
  if (normalizedTime < firstDuration) return 0;
  return 1 + Math.floor((normalizedTime - firstDuration) / normalizedSegmentSeconds);
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
  autoplayDelaySeconds: number;
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
      return {
        label: '等待播放',
        reason: '等待自动播放计时',
        detail: `自动播放将在 ${input.autoplayDelaySeconds.toFixed(1)} 秒延迟后尝试`,
      };
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

function isMobileViewerInteraction() {
  return typeof window !== 'undefined' && window.matchMedia('(hover: none) and (pointer: coarse)').matches;
}

function clampTime(value: number, max: number) {
  if (!Number.isFinite(value)) return 0;
  if (!Number.isFinite(max) || max <= 0) return Math.max(0, value);
  return Math.min(max, Math.max(0, value));
}
