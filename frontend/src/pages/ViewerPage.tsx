import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type TouchEvent as ReactTouchEvent,
} from 'react';
import { createPortal, flushSync } from 'react-dom';
import { useLocation, useNavigate, useParams, useSearchParams, type Location } from 'react-router-dom';
import { Check, Download, FolderOpen, LogOut, Plus, Search, X } from 'lucide-react';
import { api, assetDownloadUrl, assetThumbUrl } from '../api/client';
import type {
  Asset,
	AssetAIResult,
  AssetAITag,
  AssetDeletePlan,
  AssetDeleteResult,
  AssetRating,
  AssetSidecars,
  AssetTag,
  Neighbors,
  NFOField,
  NFOFilterField,
  TagSummary,
  VideoSegmentStatus,
} from '../types/api';
import AssetDeleteDialog from '../components/AssetDeleteDialog';
import AssetRecordDeleteDialog from '../components/AssetRecordDeleteDialog';
import RatingStars, { normalizeAssetRating } from '../components/RatingStars';
import { formatBytes, formatDateTime, formatDuration } from '../utils/format';
import ImageViewer from '../viewer/ImageViewer';
import type { VideoPlaybackInfo } from '../viewer/VideoViewer';
import type { AudioPlaybackInfo } from '../viewer/AudioViewer';
import { viewerAudioOutputBridge } from '../viewer/audioOutputBridge';
import type { ViewerMediaLayerMode } from '../viewer/mediaLayer';
import type { ViewerMediaPlaybackController } from '../viewer/mediaPlaybackController';
import { useKeyboard } from '../hooks/useKeyboard';
import { useRestoreSidebarState, useViewerInfoPanel, type SidebarReturnState } from '../components/SidebarContext';
import { nextRotation, rotatedCoverStyle } from '../utils/rotation';
import {
  decodeReturnState,
  emitAssetRatingChanged,
  emitViewerOverlayAssetFocus,
  encodeReturnState,
  loadViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import {
  loadViewerPrefs,
  saveViewerPrefs,
  viewerPrefsChanged,
  type ViewerPlaybackMode,
  type ViewerPrefs,
} from '../utils/viewerPrefs';
import { waterfallPageSize } from '../utils/waterfallPaging';
import {
  fitViewerPanelWidth,
  loadViewerPanelWidth,
  normalizeViewerPanelWidth,
  saveViewerPanelWidth,
} from '../utils/viewerPanelPrefs';

const AudioViewer = lazy(() => import('../viewer/AudioViewer'));
const VideoViewer = lazy(() => import('../viewer/VideoViewer'));

interface WheelBase {
  current: Asset;
  next: Asset[];
  offset: number;
  previous: Asset[];
}

interface ViewerPageProps {
  overlay?: boolean;
}

interface ViewerLocationState {
  backgroundLocation?: Location;
  initialAsset?: Asset;
}

interface SafariFullscreenVideo extends HTMLVideoElement {
  webkitDisplayingFullscreen?: boolean;
  webkitEnterFullscreen?: () => void;
  webkitExitFullscreen?: () => void;
}

const wheelPixelStep = 40;
const wheelCommitDelayMs = 120;
const mobileSwipeThresholdPx = 56;
const mobileSwipeAxisRatio = 1.25;
const mobileSwipeClickSuppressMs = 500;
const viewerReturnPageSize = waterfallPageSize;
const mediaPrepareTimeoutMs = 15000;
const playingNeighborPreloadBufferSeconds = 20;
const viewerRetainRadius = 2;
const viewerIndicatorCount = viewerRetainRadius * 2 + 1;
const viewerIndicatorCenter = viewerRetainRadius;
type DanmakuPrefKey = 'danmakuDensity' | 'danmakuFontScale' | 'danmakuOpacity' | 'danmakuSpeed';
type ViewerLoadIndicatorStatus = 'idle' | 'loading' | 'ready';
type PreparedMediaStatus = 'poster' | 'ready' | 'failed';

const neighborParamKeys = new Set([
  'albumId',
  'albumIds',
  'albumFilter',
  'albumUnassigned',
  'collectionId',
  'context',
  'durationMax',
  'durationMin',
  'dimensionMode',
  'folderId',
  'from',
  'group',
  'heightMax',
  'heightMin',
  'manualTag',
	'combinedQuery',
	'combinedTag',
	'combinedTags',
	'tagNodes',
	'aiDescription',
	'aiTag',
  'nfo',
  'nfoActor',
  'nfoId',
  'nfoTag',
  'nfoTitle',
  'nfoYear',
  'orientation',
  'playedOnly',
  'q',
  'rating',
  'recursive',
  'sizeMax',
  'sizeMin',
  'sort',
  'to',
  'type',
  'widthMax',
  'widthMin',
]);

export default function ViewerPage({ overlay = false }: ViewerPageProps) {
  const params = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [neighbors, setNeighbors] = useState<Neighbors | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sidecars, setSidecars] = useState<AssetSidecars | null>(null);
  const [sidecarsAssetId, setSidecarsAssetId] = useState<number | null>(null);
  const [sidecarError, setSidecarError] = useState<string | null>(null);
  const [assetTags, setAssetTags] = useState<AssetTag[]>([]);
  const [assetTagsAssetId, setAssetTagsAssetId] = useState<number | null>(null);
	const [assetAI, setAssetAI] = useState<AssetAIResult | null>(null);
	const [assetAIAssetId, setAssetAIAssetId] = useState<number | null>(null);
	const [assetAIError, setAssetAIError] = useState<string | null>(null);
  const [aiTagError, setAITagError] = useState<string | null>(null);
  const [aiTagSaving, setAITagSaving] = useState(false);
  const [tagDraft, setTagDraft] = useState('');
  const [tagError, setTagError] = useState<string | null>(null);
  const [tagSaving, setTagSaving] = useState(false);
  const [subtitlesEnabled, setSubtitlesEnabled] = useState(false);
  const [selectedSubtitleId, setSelectedSubtitleId] = useState('');
  const [viewerPrefs, setViewerPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());
  const [playbackRate, setPlaybackRate] = useState(() => loadViewerPrefs().playbackRate);
  const [videoPlaybackInfo, setVideoPlaybackInfo] = useState<VideoPlaybackInfo | null>(null);
  const [audioPlaybackInfo, setAudioPlaybackInfo] = useState<AudioPlaybackInfo | null>(null);
  const [videoProxyRuntime, setVideoProxyRuntime] = useState<VideoSegmentStatus | null>(null);
	const [viewerPageVisible, setViewerPageVisible] = useState(() => document.visibilityState === 'visible');
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletePlan, setDeletePlan] = useState<AssetDeletePlan | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteSubmitting, setDeleteSubmitting] = useState(false);
  const [recordDeleteDialogOpen, setRecordDeleteDialogOpen] = useState(false);
  const [recordDeleteError, setRecordDeleteError] = useState<string | null>(null);
  const [recordDeleteSubmitting, setRecordDeleteSubmitting] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [fullscreenFallback, setFullscreenFallback] = useState(false);
  const [assetInfoCollapsed, setAssetInfoCollapsed] = useState(false);
  const [viewerPanelWidth, setViewerPanelWidth] = useState(() => loadViewerPanelWidth());
  const [viewerAvailableWidth, setViewerAvailableWidth] = useState(() => window.innerWidth);
  const wheelBase = useRef<WheelBase | null>(null);
  const wheelDelta = useRef(0);
  const wheelPendingSteps = useRef(0);
  const wheelFrame = useRef<number | null>(null);
  const wheelCommitTimer = useRef<number | null>(null);
  const wheelSelectedAsset = useRef<Asset | null>(null);
  const mobileSwipeRef = useRef<{ pointerId: number; startX: number; startY: number } | null>(null);
  const suppressMobileClickUntilRef = useRef(0);
  const viewerRef = useRef<HTMLElement | null>(null);
  const viewerBodyRef = useRef<HTMLDivElement | null>(null);
  const viewerPanelResizeCleanupRef = useRef<(() => void) | null>(null);
  const activePlaybackControllerRef = useRef<{ assetId: number; controller: ViewerMediaPlaybackController } | null>(null);
  const lastMediaControlRef = useRef<{ action: string; at: number }>({ action: '', at: 0 });
  const viewerReturnStateRef = useRef(decodeReturnState<Partial<SidebarReturnState>>(searchParams.get('returnState'), {}));
  const restoreSidebarState = useRestoreSidebarState();
  const viewerInfoPanelState = useViewerInfoPanel();
  const viewerLocationState = location.state as ViewerLocationState | null;
  const backgroundLocation = viewerLocationState?.backgroundLocation;
  const assetId = Number(params.assetId || assetIdFromPath(location.pathname) || 0);
  const initialAsset = viewerLocationState?.initialAsset?.id === assetId ? viewerLocationState.initialAsset : undefined;
  const [selectedAsset, setSelectedAsset] = useState<Asset | undefined>();
  const [mediaWindow, setMediaWindow] = useState<Asset[]>([]);
  const [preparedMediaStatus, setPreparedMediaStatus] = useState<Record<string, PreparedMediaStatus>>({});
  const [priorityReadyKey, setPriorityReadyKey] = useState('');
  const [mediaLoadFailure, setMediaLoadFailure] = useState<{ key: string; message: string } | null>(null);
  const currentAssetIdRef = useRef<number | null>(null);
  const currentCacheKeyRef = useRef('');
  const currentAssetRef = useRef<Asset | undefined>(undefined);
  const failedMediaKeyRef = useRef('');
  const sidecarsCacheRef = useRef(new Map<number, AssetSidecars>());
  const assetTagsCacheRef = useRef(new Map<number, AssetTag[]>());
  const assetAICacheRef = useRef(new Map<number, AssetAIResult>());

  useLayoutEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer) return;
    const updateWidth = () => setViewerAvailableWidth(viewer.getBoundingClientRect().width);
    updateWidth();
    const observer = new ResizeObserver(updateWidth);
    observer.observe(viewer);
    return () => observer.disconnect();
  }, []);

  useEffect(
    () => () => {
      viewerPanelResizeCleanupRef.current?.();
      viewerAudioOutputBridge.dispose();
    },
    [],
  );

  useEffect(() => {
    document.documentElement.classList.toggle('viewer-fullscreen-active', fullscreen);
    return () => document.documentElement.classList.remove('viewer-fullscreen-active');
  }, [fullscreen]);

  const query = useMemo(() => {
    const result: Record<string, string> = {};
    searchParams.forEach((value, key) => {
      if (neighborParamKeys.has(key)) result[key] = value;
    });
    return result;
  }, [searchParams]);

  useEffect(() => {
    let live = true;
    const controller = new AbortController();
    async function load() {
      try {
        const result = await api.neighbors(assetId, { ...query, limit: 50 }, controller.signal);
        if (!live) return;
        setNeighbors(result);
        setError(null);
      } catch (err) {
        if (isAbortError(err)) return;
        if (!live) return;
        setError(err instanceof Error ? err.message : '读取资源失败');
      }
    }
    if (assetId > 0) void load();
    return () => {
      live = false;
      controller.abort();
    };
  }, [assetId, query]);

  const activeNeighbors = neighbors?.current.id === assetId ? neighbors : null;
  const activeNeighborAssetId = activeNeighbors?.current.id ?? null;
  const committedAsset = activeNeighbors?.current ?? initialAsset;
  const current = selectedAsset ?? committedAsset;
  const currentAssetId = current?.id ?? null;
  currentAssetIdRef.current = currentAssetId;
  currentCacheKeyRef.current = current?.cacheKey ?? '';
  currentAssetRef.current = current;
  const currentMediaKey = current ? mediaReadyKey(current.id, current.cacheKey) : '';
  const committedMediaKey = committedAsset ? mediaReadyKey(committedAsset.id, committedAsset.cacheKey) : '';
  const displayedMediaKey = currentMediaKey;
  const currentMediaFailed = Boolean(currentMediaKey) && mediaLoadFailure?.key === currentMediaKey;
  const currentPreparedStatus = currentMediaKey ? preparedMediaStatus[currentMediaKey] : undefined;
  const currentPriorityReady = Boolean(currentMediaKey) && (
    priorityReadyKey === currentMediaKey
    || (currentPreparedStatus === 'ready' && (current?.mediaType !== 'video' || current.browserPlayable))
  );
	const videoActivelyPlaying = Boolean(videoPlaybackInfo && videoPlaybackInfo.hasPlaybackStarted && !videoPlaybackInfo.paused && !videoPlaybackInfo.ended);
	const audioActivelyPlaying = Boolean(audioPlaybackInfo?.playing);
	const activeBufferedAhead = videoActivelyPlaying && videoPlaybackInfo
		? Math.max(0, videoPlaybackInfo.bufferedEnd - videoPlaybackInfo.currentTime)
		: audioActivelyPlaying && audioPlaybackInfo
			? Math.max(0, audioPlaybackInfo.bufferedEnd - audioPlaybackInfo.currentTime)
			: 0;
	const neighborPreloadAllowed = videoActivelyPlaying || audioActivelyPlaying
		? activeBufferedAhead >= playingNeighborPreloadBufferSeconds
		: viewerPageVisible;
  const indicatorAssets = useMemo(() => viewerIndicatorAssets(neighbors, current), [current, neighbors]);
  const mediaWindowKeys = useMemo(
    () => new Set(mediaWindow.map((asset) => mediaReadyKey(asset.id, asset.cacheKey))),
    [mediaWindow],
  );
  const backgroundMediaPreloadKey = useMemo(() => {
    if (!currentPriorityReady || !neighborPreloadAllowed) return '';
    const order = [viewerIndicatorCenter + 1, viewerIndicatorCenter - 1, viewerIndicatorCenter + 2, viewerIndicatorCenter - 2];
    for (const index of order) {
      const asset = indicatorAssets[index];
      if (!asset) continue;
      const key = mediaReadyKey(asset.id, asset.cacheKey);
      if (!mediaWindowKeys.has(key) || key === currentMediaKey) continue;
      if (preparedMediaStatus[key] === undefined) return key;
    }
    return '';
  }, [currentMediaKey, currentPriorityReady, indicatorAssets, mediaWindowKeys, neighborPreloadAllowed, preparedMediaStatus]);

	useEffect(() => {
		const update = () => setViewerPageVisible(document.visibilityState === 'visible');
		document.addEventListener('visibilitychange', update);
		return () => {
			document.removeEventListener('visibilitychange', update);
		};
	}, []);
  const viewerPreloadIndicators = useMemo(() => {
    return indicatorAssets.map((asset): ViewerLoadIndicatorStatus => {
      if (!asset) return 'idle';
      const key = mediaReadyKey(asset.id, asset.cacheKey);
      const prepared = preparedMediaStatus[key];
      if (prepared === 'ready' || (asset.mediaType === 'video' && prepared === 'poster')) return 'ready';
      if (prepared === 'failed' || !mediaWindowKeys.has(key)) return 'idle';
      if (key === currentMediaKey || key === backgroundMediaPreloadKey) return 'loading';
      return 'idle';
    });
  }, [backgroundMediaPreloadKey, currentMediaKey, indicatorAssets, mediaWindowKeys, preparedMediaStatus]);
  const viewerPreloadStatusByKey = useMemo(() => {
    const statuses: Record<string, ViewerLoadIndicatorStatus> = {};
    indicatorAssets.forEach((asset, index) => {
      if (!asset) return;
      statuses[mediaReadyKey(asset.id, asset.cacheKey)] = viewerPreloadIndicators[index] ?? 'idle';
    });
    return statuses;
  }, [indicatorAssets, viewerPreloadIndicators]);
  const viewerNeighborSlots = useMemo(
    () => indicatorAssets.flatMap((asset, index) => (
      index === viewerIndicatorCenter ? [] : [{ asset, status: viewerPreloadIndicators[index] }]
    )),
    [indicatorAssets, viewerPreloadIndicators],
  );
  const previousAsset = indicatorAssets[viewerIndicatorCenter - 1];
  const nextAsset = indicatorAssets[viewerIndicatorCenter + 1];
  const previousReady = Boolean(previousAsset && mediaNavigationReady(previousAsset, preparedMediaStatus[mediaReadyKey(previousAsset.id, previousAsset.cacheKey)]));
  const nextReady = Boolean(nextAsset && mediaNavigationReady(nextAsset, preparedMediaStatus[mediaReadyKey(nextAsset.id, nextAsset.cacheKey)]));

  useLayoutEffect(() => {
    if (!committedAsset) return;
    setSelectedAsset((selected) => selected === committedAsset ? selected : committedAsset);
  }, [committedAsset]);

  useLayoutEffect(() => {
    if (!current) return;
    setMediaWindow((existing) => mergeMediaWindow(existing, [current, ...existing]));
  }, [current]);

  useLayoutEffect(() => {
    if (!activeNeighbors) return;
    const desired = viewerMediaWindow(activeNeighbors, current);
    setMediaWindow((existing) => mergeMediaWindow(existing, desired));
  }, [activeNeighbors, current]);

  useEffect(() => {
    const retainedKeys = new Set(mediaWindow.map((asset) => mediaReadyKey(asset.id, asset.cacheKey)));
    setPreparedMediaStatus((existing) => prunePreparedMediaStatus(existing, retainedKeys));
  }, [mediaWindow]);

  useLayoutEffect(() => {
    failedMediaKeyRef.current = '';
    setMediaLoadFailure(null);
  }, [currentMediaKey]);

  useEffect(() => {
    if (!currentMediaKey || currentPreparedStatus === 'ready' || currentMediaFailed) return undefined;
    const timer = window.setTimeout(() => {
      if (currentMediaKey !== mediaReadyKey(currentAssetIdRef.current ?? 0, currentCacheKeyRef.current)) return;
      failedMediaKeyRef.current = currentMediaKey;
      setMediaLoadFailure({ key: currentMediaKey, message: '媒体加载超过 15 秒，已保留缩略图' });
    }, mediaPrepareTimeoutMs);
    return () => window.clearTimeout(timer);
  }, [currentMediaFailed, currentMediaKey, currentPreparedStatus]);

  useEffect(() => {
    if (!overlay || !current) return;
    emitViewerOverlayAssetFocus(current.id);
  }, [current?.id, overlay]);

  useEffect(() => {
    setVideoProxyRuntime(null);
    setVideoPlaybackInfo(null);
    setDeleteDialogOpen(false);
    setDeletePlan(null);
    setDeleteError(null);
  }, [current?.id]);

	useEffect(() => {
		let live = true;
		let timer = 0;
		async function loadAI(asset: Asset) {
			try {
				const result = await api.assetAI(asset.id);
				if (!live) return;
				assetAICacheRef.current.set(asset.id, result);
				setAssetAI(result); setAssetAIAssetId(asset.id); setAssetAIError(null);
				if (result.status === 'pending' || result.status === 'processing') timer = window.setTimeout(() => void loadAI(asset), 5000);
			} catch (err) {
				if (!live) return;
				setAssetAI(null); setAssetAIAssetId(asset.id); setAssetAIError(err instanceof Error ? err.message : '读取 AI 结果失败');
			}
		}
		const cached = current ? assetAICacheRef.current.get(current.id) : undefined;
		setAssetAI(cached ?? null); setAssetAIAssetId(cached && current ? current.id : null); setAssetAIError(null); setAITagError(null);
		if (current && current.mediaType !== 'audio') void loadAI(current);
		return () => { live = false; window.clearTimeout(timer); };
	}, [current?.id]);

  useEffect(() => {
    if (activeNeighborAssetId === null) return;
    wheelBase.current = null;
    wheelDelta.current = 0;
    wheelPendingSteps.current = 0;
  }, [activeNeighborAssetId]);

  const currentVideoProxyRuntime =
    videoProxyRuntime && currentAssetId !== null && videoProxyRuntime.assetId === currentAssetId ? videoProxyRuntime : null;

  const handleProxyRuntimeChange = useCallback(
    (sourceAssetId: number, runtime: VideoSegmentStatus | null) => {
      if (sourceAssetId !== currentAssetIdRef.current) return;
      setVideoProxyRuntime(runtime && runtime.assetId === sourceAssetId ? runtime : null);
    },
    [],
  );
  const handleCurrentProxyRuntimeChange = useCallback(
    (runtime: VideoSegmentStatus | null) => {
      if (currentAssetId === null) return;
      handleProxyRuntimeChange(currentAssetId, runtime);
    },
    [currentAssetId, handleProxyRuntimeChange],
  );

  useEffect(() => {
    let live = true;
    async function loadSidecars(asset: Asset) {
      try {
        const result = await api.assetSidecars(asset.id);
        if (!live) return;
        sidecarsCacheRef.current.set(asset.id, result);
        setSidecars(result);
        setSidecarsAssetId(asset.id);
        setSidecarError(null);
        const defaultID = result.defaultSubtitleId ?? result.subtitles[0]?.id ?? '';
        setSelectedSubtitleId(defaultID);
        setSubtitlesEnabled(Boolean(defaultID) && loadViewerPrefs().subtitlesEnabled);
      } catch (err) {
        if (!live) return;
        setSidecars(null);
        setSidecarsAssetId(asset.id);
        setSidecarError(err instanceof Error ? err.message : '读取附加信息失败');
        setSelectedSubtitleId('');
        setSubtitlesEnabled(false);
      }
    }
    if (current) {
      const cached = sidecarsCacheRef.current.get(current.id);
      if (cached) {
        setSidecars(cached);
        setSidecarsAssetId(current.id);
      }
      void loadSidecars(current);
    } else {
      setSidecars(null);
      setSidecarsAssetId(null);
      setSidecarError(null);
      setSelectedSubtitleId('');
      setSubtitlesEnabled(false);
    }
    return () => {
      live = false;
    };
  }, [current?.id]);

  useEffect(() => {
    let live = true;
    async function loadTags(asset: Asset) {
      try {
        const result = await api.assetTags(asset.id);
        if (!live) return;
        assetTagsCacheRef.current.set(asset.id, result.items);
        setAssetTags(result.items);
        setAssetTagsAssetId(asset.id);
        setTagError(null);
      } catch (err) {
        if (!live) return;
        setAssetTags([]);
        setAssetTagsAssetId(asset.id);
        setTagError(err instanceof Error ? err.message : '读取标签失败');
      }
    }
    if (current) {
      const cached = assetTagsCacheRef.current.get(current.id);
      if (cached) {
        setAssetTags(cached);
        setAssetTagsAssetId(current.id);
      }
      void loadTags(current);
    } else {
      setAssetTags([]);
      setAssetTagsAssetId(null);
      setTagError(null);
    }
    setTagDraft('');
    return () => {
      live = false;
    };
  }, [current?.id]);

  useEffect(() => {
    function onPrefsChanged() {
      const prefs = loadViewerPrefs();
      setViewerPrefs(prefs);
      setPlaybackRate(prefs.playbackRate);
      setSubtitlesEnabled(Boolean(selectedSubtitleId) && prefs.subtitlesEnabled);
    }
    window.addEventListener(viewerPrefsChanged, onPrefsChanged);
    window.addEventListener('storage', onPrefsChanged);
    return () => {
      window.removeEventListener(viewerPrefsChanged, onPrefsChanged);
      window.removeEventListener('storage', onPrefsChanged);
    };
  }, [selectedSubtitleId]);

  const updatePlaybackRate = useCallback((value: number) => {
    const prefs = { ...loadViewerPrefs(), playbackRate: value };
    saveViewerPrefs(prefs);
    const saved = loadViewerPrefs();
    setViewerPrefs(saved);
    setPlaybackRate(saved.playbackRate);
  }, []);

  const updatePlaybackMode = useCallback((value: ViewerPlaybackMode) => {
    const prefs = { ...loadViewerPrefs(), playbackMode: value };
    saveViewerPrefs(prefs);
    setViewerPrefs(loadViewerPrefs());
  }, []);

  const updateSubtitlesEnabled = useCallback((value: boolean) => {
    const prefs = { ...loadViewerPrefs(), subtitlesEnabled: value };
    saveViewerPrefs(prefs);
    const saved = loadViewerPrefs();
    setViewerPrefs(saved);
    setSubtitlesEnabled(Boolean(selectedSubtitleId) && saved.subtitlesEnabled);
  }, [selectedSubtitleId]);

  const updateSelectedSubtitle = useCallback((value: string) => {
    setSelectedSubtitleId(value);
    if (value) {
      const prefs = { ...loadViewerPrefs(), subtitlesEnabled: true };
      saveViewerPrefs(prefs);
      setViewerPrefs(loadViewerPrefs());
      setSubtitlesEnabled(true);
    } else {
      setSubtitlesEnabled(false);
    }
  }, []);

  const updateDanmakuPref = useCallback((key: DanmakuPrefKey, value: number) => {
    const prefs = { ...loadViewerPrefs(), [key]: value };
    saveViewerPrefs(prefs);
    setViewerPrefs(loadViewerPrefs());
  }, []);

  const handleCurrentPlaybackInfoChange = useCallback((info: VideoPlaybackInfo | null) => {
    setVideoPlaybackInfo(info);
  }, []);

  const handleCurrentAudioPlaybackInfoChange = useCallback((info: AudioPlaybackInfo | null) => {
    setAudioPlaybackInfo(info);
  }, []);

  const handlePlaybackControllerChange = useCallback((sourceAssetId: number, controller: ViewerMediaPlaybackController | null) => {
    if (controller) {
      activePlaybackControllerRef.current = { assetId: sourceAssetId, controller };
      return;
    }
    if (activePlaybackControllerRef.current?.assetId === sourceAssetId) activePlaybackControllerRef.current = null;
  }, []);

  const handleMediaReady = useCallback((sourceAssetId: number, sourceCacheKey: string) => {
    const key = mediaReadyKey(sourceAssetId, sourceCacheKey);
    setPreparedMediaStatus((existing) => existing[key] === 'ready' ? existing : { ...existing, [key]: 'ready' });
    if (sourceAssetId !== currentAssetIdRef.current || sourceCacheKey !== currentCacheKeyRef.current) return;
    if (failedMediaKeyRef.current === key) return;
    const readyAsset = currentAssetRef.current;
    if (!readyAsset || readyAsset.id !== sourceAssetId || readyAsset.cacheKey !== sourceCacheKey) return;
    if (readyAsset.mediaType !== 'video' || readyAsset.browserPlayable) setPriorityReadyKey(key);
  }, []);

  const handlePosterReady = useCallback((sourceAssetId: number, sourceCacheKey: string) => {
    const key = mediaReadyKey(sourceAssetId, sourceCacheKey);
    setPreparedMediaStatus((existing) => existing[key] ? existing : { ...existing, [key]: 'poster' });
  }, []);

  const handlePriorityPreloadComplete = useCallback((sourceAssetId: number, sourceCacheKey: string) => {
    if (sourceAssetId !== currentAssetIdRef.current || sourceCacheKey !== currentCacheKeyRef.current) return;
    setPriorityReadyKey(mediaReadyKey(sourceAssetId, sourceCacheKey));
  }, []);

  const handleMediaError = useCallback((sourceAssetId: number, sourceCacheKey: string, message: string) => {
    const key = mediaReadyKey(sourceAssetId, sourceCacheKey);
    setPreparedMediaStatus((existing) => existing[key] === 'failed' ? existing : { ...existing, [key]: 'failed' });
    if (sourceAssetId !== currentAssetIdRef.current || sourceCacheKey !== currentCacheKeyRef.current) return;
    failedMediaKeyRef.current = key;
    setMediaLoadFailure({
      key,
      message,
    });
  }, []);

  const revealAsset = useCallback(
    (asset: Asset | undefined) => {
      if (!asset) return;
      const source = currentAssetRef.current;
      if (
        source
        && (source.mediaType === 'video' || source.mediaType === 'audio')
        && (asset.mediaType === 'video' || asset.mediaType === 'audio')
      ) {
        viewerAudioOutputBridge.beginTransition(
          mediaReadyKey(source.id, source.cacheKey),
          mediaReadyKey(asset.id, asset.cacheKey),
        );
      }
      flushSync(() => {
        setSelectedAsset(asset);
        setMediaWindow((existing) => mergeMediaWindow(existing, [asset, ...existing]));
      });
    },
    [],
  );

  const commitAsset = useCallback(
    (asset: Asset | undefined) => {
      if (!asset) return;
      const destination = { pathname: `/viewer/${asset.id}`, search: searchParams.toString() };
      navigate(
        destination,
        overlay && backgroundLocation
          ? { flushSync: true, replace: true, state: { backgroundLocation, initialAsset: asset } }
          : { flushSync: true, state: { initialAsset: asset } },
      );
    },
    [backgroundLocation, navigate, overlay, searchParams],
  );

  const goAsset = useCallback(
    (asset: Asset | undefined, _direction: -1 | 0 | 1 = 0) => {
      if (!asset) return;
      if (wheelCommitTimer.current !== null) {
        window.clearTimeout(wheelCommitTimer.current);
        wheelCommitTimer.current = null;
      }
      wheelSelectedAsset.current = null;
      revealAsset(asset);
      commitAsset(asset);
    },
    [commitAsset, revealAsset],
  );

  const handleViewerTouchStart = useCallback((event: ReactTouchEvent<HTMLDivElement>) => {
    if (event.touches.length !== 1 || isViewerTouchControl(event.target)) return;
    const touch = event.touches[0];
    mobileSwipeRef.current = { pointerId: touch.identifier, startX: touch.clientX, startY: touch.clientY };
  }, []);

  const handleViewerTouchEnd = useCallback((event: ReactTouchEvent<HTMLDivElement>) => {
    const gesture = mobileSwipeRef.current;
    mobileSwipeRef.current = null;
    const touch = event.changedTouches[0];
    if (!gesture || !touch || gesture.pointerId !== touch.identifier || isViewerTouchControl(event.target)) return;
    const deltaX = touch.clientX - gesture.startX;
    const deltaY = touch.clientY - gesture.startY;
    if (Math.abs(deltaX) < mobileSwipeThresholdPx || Math.abs(deltaX) < Math.abs(deltaY) * mobileSwipeAxisRatio) return;
    suppressMobileClickUntilRef.current = Date.now() + mobileSwipeClickSuppressMs;
    event.preventDefault();
    if (deltaX < 0) goAsset(activeNeighbors?.next[0], 1);
    else goAsset(activeNeighbors?.previous[0], -1);
  }, [activeNeighbors, goAsset]);

  const handleViewerClickCapture = useCallback((event: ReactMouseEvent<HTMLDivElement>) => {
    if (Date.now() > suppressMobileClickUntilRef.current) return;
    event.preventDefault();
    event.stopPropagation();
  }, []);

  const playNextAsset = useCallback(() => {
    const next = activeNeighbors?.next[0];
    if (next) goAsset(next, 1);
  }, [activeNeighbors, goAsset]);

  const runMediaControl = useCallback((action: 'play' | 'pause' | 'stop' | 'previous' | 'next') => {
    const now = performance.now();
    if (lastMediaControlRef.current.action === action && now - lastMediaControlRef.current.at < 180) return;
    lastMediaControlRef.current = { action, at: now };
    if (action === 'previous') {
      if (previousReady && previousAsset) goAsset(previousAsset, -1);
      return;
    }
    if (action === 'next') {
      if (nextReady && nextAsset) goAsset(nextAsset, 1);
      return;
    }
    const active = activePlaybackControllerRef.current;
    if (!current || active?.assetId !== current.id) return;
    active.controller[action]();
  }, [current, goAsset, nextAsset, nextReady, previousAsset, previousReady]);

  useEffect(() => {
    if (!current || (current.mediaType !== 'video' && current.mediaType !== 'audio')) return;
    const mediaSession = navigator.mediaSession;
    if (!mediaSession) return;
    const setHandler = (action: MediaSessionAction, handler: MediaSessionActionHandler | null) => {
      try {
        mediaSession.setActionHandler(action, handler);
      } catch {
        // Browsers expose different subsets of Media Session actions.
      }
    };
    setHandler('play', () => runMediaControl('play'));
    setHandler('pause', () => runMediaControl('pause'));
    setHandler('stop', () => runMediaControl('stop'));
    setHandler('previoustrack', previousReady && previousAsset ? () => runMediaControl('previous') : null);
    setHandler('nexttrack', nextReady && nextAsset ? () => runMediaControl('next') : null);
    if (typeof MediaMetadata !== 'undefined') {
      mediaSession.metadata = new MediaMetadata({
        title: current.displayTitle || current.filename,
        album: current.parentRelPath || current.relPath,
      });
    }
    return () => {
      setHandler('play', null);
      setHandler('pause', null);
      setHandler('stop', null);
      setHandler('previoustrack', null);
      setHandler('nexttrack', null);
      mediaSession.metadata = null;
    };
  }, [current, nextAsset, nextReady, previousAsset, previousReady, runMediaControl]);

  useEffect(() => {
    if (!current || (current.mediaType !== 'video' && current.mediaType !== 'audio') || !navigator.mediaSession) return;
    navigator.mediaSession.playbackState = current.mediaType === 'video'
      ? videoPlaybackInfo?.paused === false ? 'playing' : 'paused'
      : audioPlaybackInfo?.playing ? 'playing' : 'paused';
  }, [audioPlaybackInfo?.playing, current, videoPlaybackInfo?.paused]);

  useEffect(() => {
    if (!current || (current.mediaType !== 'video' && current.mediaType !== 'audio') || !navigator.mediaSession) return;
    return () => {
      navigator.mediaSession.playbackState = 'none';
    };
  }, [current?.id, current?.mediaType]);

  useEffect(() => {
    if (!current || (current.mediaType !== 'video' && current.mediaType !== 'audio')) return;
    const playing = current.mediaType === 'video' ? videoPlaybackInfo?.paused === false : Boolean(audioPlaybackInfo?.playing);
    const handleMediaKey = (event: KeyboardEvent) => {
      const key = event.code || event.key;
      if (key === 'MediaPlayPause') runMediaControl(playing ? 'pause' : 'play');
      else if (key === 'MediaPlay') runMediaControl('play');
      else if (key === 'MediaPause') runMediaControl('pause');
      else if (key === 'MediaStop') runMediaControl('stop');
      else if (key === 'MediaTrackPrevious') runMediaControl('previous');
      else if (key === 'MediaTrackNext') runMediaControl('next');
      else return;
      event.preventDefault();
    };
    window.addEventListener('keydown', handleMediaKey);
    return () => window.removeEventListener('keydown', handleMediaKey);
  }, [audioPlaybackInfo?.playing, current, runMediaControl, videoPlaybackInfo?.paused]);

  const goWheelSteps = useCallback(
    (steps: number) => {
      if (!steps) return;
      const base =
        wheelBase.current ??
        (activeNeighbors
          ? { current: activeNeighbors.current, next: activeNeighbors.next, offset: 0, previous: activeNeighbors.previous }
          : null);
      if (!base) return;
      const direction = steps > 0 ? 1 : -1;
      let nextOffset = base.offset;
      let target: Asset | undefined;
      for (let index = 0; index < Math.abs(steps); index += 1) {
        const candidateOffset = nextOffset + direction;
        const candidate = wheelTargetAtOffset(base, candidateOffset);
        if (!candidate) break;
        nextOffset = candidateOffset;
        target = candidate;
      }
      if (!target) return;
      base.offset = nextOffset;
      wheelBase.current = base;
      wheelSelectedAsset.current = target;
      revealAsset(target);
      if (wheelCommitTimer.current !== null) window.clearTimeout(wheelCommitTimer.current);
      wheelCommitTimer.current = window.setTimeout(() => {
        wheelCommitTimer.current = null;
        const selected = wheelSelectedAsset.current;
        if (!selected) return;
        wheelSelectedAsset.current = null;
        commitAsset(selected);
      }, wheelCommitDelayMs);
    },
    [activeNeighbors, commitAsset, revealAsset],
  );

  useEffect(() => {
    const handleWheel = (event: WheelEvent) => {
      if (!wheelBelongsToViewer(event, viewerRef.current)) return;
      if (isViewerWheelControl(event)) {
        wheelDelta.current = 0;
        return;
      }
      if (isMediaZoomWheel(event)) {
        wheelDelta.current = 0;
        return;
      }
      if (event.cancelable) event.preventDefault();
      const delta = normalizedViewerWheelDelta(event);
      if (event.deltaMode !== 0 || Math.abs(delta) >= 50) {
        wheelPendingSteps.current += delta > 0 ? 1 : -1;
      } else {
        wheelDelta.current += delta;
        const pixelSteps = Math.trunc(wheelDelta.current / wheelPixelStep);
        if (pixelSteps !== 0) {
          wheelPendingSteps.current += pixelSteps;
          wheelDelta.current -= pixelSteps * wheelPixelStep;
        }
      }
      if (wheelPendingSteps.current === 0 || wheelFrame.current !== null) return;
      wheelFrame.current = window.requestAnimationFrame(() => {
        wheelFrame.current = null;
        const steps = wheelPendingSteps.current;
        wheelPendingSteps.current = 0;
        goWheelSteps(steps);
      });
    };
    window.addEventListener('wheel', handleWheel, { capture: true, passive: false });
    return () => {
      window.removeEventListener('wheel', handleWheel, true);
      if (wheelFrame.current !== null) window.cancelAnimationFrame(wheelFrame.current);
      wheelFrame.current = null;
      if (wheelCommitTimer.current !== null) window.clearTimeout(wheelCommitTimer.current);
      wheelCommitTimer.current = null;
    };
  }, [goWheelSteps]);

  const leave = useCallback(() => {
    if (overlay) {
      navigate(-1);
      return;
    }
    void (async () => {
      const context = searchParams.get('context');
      const fallback = context === 'folder' ? '/folders' : context === 'album' ? '/albums' : context === 'recent' ? '/recent' : '/library';
      const returnPath = searchParams.get('returnPath');
      const targetPath = returnPathMatchesFallback(returnPath, fallback) ? returnPath! : fallback;
      const restoreState = await returnStateForCurrentAsset(searchParams, current?.id);
      if (restoreState) {
        navigate(appendRestoreParam(targetPath, restoreState), overlay ? { replace: true } : undefined);
        return;
      }
      const storageReturnPath = loadViewerReturnPath();
      navigate(returnPathMatchesFallback(storageReturnPath, fallback) ? storageReturnPath : fallback);
    })();
  }, [current?.id, navigate, overlay, searchParams]);

  const closeDeleteDialog = useCallback(() => {
    if (deleteSubmitting) return;
    setDeleteDialogOpen(false);
    setDeletePlan(null);
    setDeleteError(null);
  }, [deleteSubmitting]);

  const openDeleteDialog = useCallback(async () => {
    if (!current) return;
    setDeleteDialogOpen(true);
    setDeleteLoading(true);
    setDeleteSubmitting(false);
    setDeletePlan(null);
    setDeleteError(null);
    try {
      const plan = await api.assetDeletePlan(current.id);
      setDeletePlan(plan);
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : '读取删除范围失败');
    } finally {
      setDeleteLoading(false);
    }
  }, [current?.id]);

  const goAfterDelete = useCallback(
    (result: AssetDeleteResult) => {
      const deleted = new Set(result.deletedAssetIds);
      const target = activeNeighbors ? [...activeNeighbors.next, ...activeNeighbors.previous].find((asset) => !deleted.has(asset.id)) : undefined;
      if (target) {
        goAsset(target);
        return;
      }
      leave();
    },
    [activeNeighbors, goAsset, leave],
  );

  const confirmDeleteAsset = useCallback(async () => {
    if (!current || !deletePlan || deleteSubmitting) return;
    setDeleteSubmitting(true);
    setDeleteError(null);
    try {
      const result = await api.deleteAsset(current.id, deletePlan.token);
      if (result.stale && result.plan) {
        setDeletePlan(result.plan);
        setDeleteError('删除范围已变化，请重新确认');
        return;
      }
      if (result.failures.length > 0) {
        setDeleteError(`删除失败：${result.failures.map((failure) => failure.relPath).join('、')}`);
      }
      if (result.deletedAssetIds.includes(current.id)) {
        setDeleteDialogOpen(false);
        goAfterDelete(result);
      }
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : '删除失败');
    } finally {
      setDeleteSubmitting(false);
    }
  }, [current, deletePlan, deleteSubmitting, goAfterDelete]);

  const openRecordDeleteDialog = useCallback(() => {
    if (!current) return;
    setRecordDeleteError(null);
    setRecordDeleteSubmitting(false);
    setRecordDeleteDialogOpen(true);
  }, [current?.id]);

  const closeRecordDeleteDialog = useCallback(() => {
    if (recordDeleteSubmitting) return;
    setRecordDeleteDialogOpen(false);
    setRecordDeleteError(null);
  }, [recordDeleteSubmitting]);

  const confirmDeleteAssetRecord = useCallback(async () => {
    if (!current || recordDeleteSubmitting) return;
    setRecordDeleteSubmitting(true);
    setRecordDeleteError(null);
    try {
      const result = await api.deleteAssetRecord(current.id);
      if (result.deletedAssetIds.includes(current.id)) {
        setRecordDeleteDialogOpen(false);
        goAfterDelete(result);
        return;
      }
      setRecordDeleteError('媒体记录已经不存在');
    } catch (err) {
      setRecordDeleteError(err instanceof Error ? err.message : '删除媒体记录失败');
    } finally {
      setRecordDeleteSubmitting(false);
    }
  }, [current, goAfterDelete, recordDeleteSubmitting]);

  const rotateCurrentAsset = useCallback(async () => {
    if (!current) return;
    const pref = await api.updateAssetPreferences(current.id, nextRotation(current.rotation));
    setNeighbors((value) => (value ? updateNeighborRotation(value, pref.assetId, pref.rotation) : value));
  }, [current]);

  const rateCurrentAsset = useCallback(async (rating: AssetRating) => {
    if (!current) return;
    const pref = await api.updateAssetRating(current.id, rating);
    const nextRating = normalizeAssetRating(pref.rating);
    emitAssetRatingChanged(pref.assetId, nextRating);
    setNeighbors((value) => {
      if (value) return updateNeighborRating(value, pref.assetId, nextRating);
      if (current.id === pref.assetId) {
        return { current: { ...current, rating: nextRating }, previous: [], next: [] };
      }
      return value;
    });
  }, [current]);

  const addCurrentAssetTag = useCallback(async (value?: string) => {
    if (!current || tagSaving) return false;
    const tag = (value ?? tagDraft).trim();
    if (!tag) return false;
    setTagSaving(true);
    setTagError(null);
    try {
      const result = await api.addAssetTag(current.id, tag);
      assetTagsCacheRef.current.set(current.id, result.items);
      setAssetTags(result.items);
      setTagDraft('');
      return true;
    } catch (err) {
      setTagError(err instanceof Error ? err.message : '添加标签失败');
      return false;
    } finally {
      setTagSaving(false);
    }
  }, [current, tagDraft, tagSaving]);

  const removeCurrentAssetTag = useCallback(async (tag: string) => {
    if (!current || tagSaving) return false;
    setTagSaving(true);
    setTagError(null);
    try {
      const result = await api.removeAssetTag(current.id, tag);
      assetTagsCacheRef.current.set(current.id, result.items);
      setAssetTags(result.items);
      return true;
    } catch (err) {
      setTagError(err instanceof Error ? err.message : '删除标签失败');
      return false;
    } finally {
      setTagSaving(false);
    }
  }, [current, tagSaving]);

  const saveCurrentAITag = useCallback(async (payload: {
    previousTag?: string;
    tag: string;
    categoryKey: string;
    subjectKey: string;
  }) => {
    if (!current || aiTagSaving) return false;
    setAITagSaving(true);
    setAITagError(null);
    try {
      const result = await api.replaceAssetAITag(current.id, payload);
      assetAICacheRef.current.set(current.id, result);
      setAssetAI(result);
      return true;
    } catch (err) {
      setAITagError(err instanceof Error ? err.message : '保存 AI 标签失败');
      return false;
    } finally {
      setAITagSaving(false);
    }
  }, [aiTagSaving, current]);

  const removeCurrentAITag = useCallback(async (tag: string) => {
    if (!current || aiTagSaving) return false;
    setAITagSaving(true);
    setAITagError(null);
    try {
      const result = await api.deleteAssetAITag(current.id, tag);
      assetAICacheRef.current.set(current.id, result);
      setAssetAI(result);
      return true;
    } catch (err) {
      setAITagError(err instanceof Error ? err.message : '删除 AI 标签失败');
      return false;
    } finally {
      setAITagSaving(false);
    }
  }, [aiTagSaving, current]);

  const searchByNFOValue = useCallback(
    (field: NFOFilterField | 'nfo', value: string) => {
      const query = new URLSearchParams();
      query.set(searchParamForNFOField(field), value);
      navigate({ pathname: '/library', search: query.toString() });
    },
    [navigate],
  );

  const openAssetFolder = useCallback(
    (asset: Asset) => {
      const query = new URLSearchParams();
      query.set('folder', asset.parentRelPath);
      query.set('recursive', '0');
      navigate({ pathname: '/folders', search: query.toString() });
    },
    [navigate],
  );

  const toggleFullscreen = useCallback(() => {
    const target = viewerBodyRef.current;
    if (document.fullscreenElement) {
      void document.exitFullscreen();
      return;
    }

    const video = target?.querySelector<HTMLVideoElement>('[data-layer-mode="active"] video');
    const safariVideo = video as SafariFullscreenVideo | undefined;
    if (safariVideo?.webkitDisplayingFullscreen) {
      safariVideo.webkitExitFullscreen?.();
      return;
    }
    if (fullscreenFallback) {
      setFullscreenFallback(false);
      setFullscreen(false);
      return;
    }

    // iPhone Safari does not expose the element Fullscreen API. Its native
    // video presentation API must be invoked synchronously from this click.
    if (safariVideo?.webkitEnterFullscreen && document.fullscreenEnabled !== true) {
      try {
        safariVideo.webkitEnterFullscreen();
        setFullscreen(true);
        return;
      } catch {
        // Fall through to the viewport-covering mode below.
      }
    }

    if (target?.requestFullscreen) {
      void target.requestFullscreen().catch(() => {
        setFullscreenFallback(true);
        setFullscreen(true);
      });
      return;
    }

    setFullscreenFallback(true);
    setFullscreen(true);
  }, [fullscreenFallback]);

  const startViewerPanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (event.button !== 0) return;
      event.preventDefault();
      viewerPanelResizeCleanupRef.current?.();
      const startX = event.clientX;
      const startWidth = fitViewerPanelWidth(viewerPanelWidth, viewerAvailableWidth);
      document.body.classList.add('viewer-panel-resizing');
      const onPointerMove = (moveEvent: globalThis.PointerEvent) => {
        const width = normalizeViewerPanelWidth(startWidth - (moveEvent.clientX - startX));
        setViewerPanelWidth(width);
        saveViewerPanelWidth(width);
      };
      const endResize = () => {
        document.body.classList.remove('viewer-panel-resizing');
        window.removeEventListener('pointermove', onPointerMove);
        window.removeEventListener('pointerup', endResize);
        window.removeEventListener('pointercancel', endResize);
        viewerPanelResizeCleanupRef.current = null;
      };
      viewerPanelResizeCleanupRef.current = endResize;
      window.addEventListener('pointermove', onPointerMove);
      window.addEventListener('pointerup', endResize);
      window.addEventListener('pointercancel', endResize);
    },
    [viewerAvailableWidth, viewerPanelWidth],
  );

  useEffect(() => {
    function onFullscreenChange() {
      const target = viewerBodyRef.current;
      const fullscreenElement = document.fullscreenElement;
      setFullscreen(Boolean(fullscreenElement && target && (fullscreenElement === target || fullscreenElement.contains(target))));
      if (fullscreenElement) setFullscreenFallback(false);
    }
    document.addEventListener('fullscreenchange', onFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', onFullscreenChange);
  }, []);

  useEffect(() => {
    const videos = viewerBodyRef.current?.querySelectorAll<HTMLVideoElement>('video');
    if (!videos?.length) return undefined;
    const onBegin = () => {
      setFullscreenFallback(false);
      setFullscreen(true);
    };
    const onEnd = () => setFullscreen(false);
    videos.forEach((video) => {
      video.addEventListener('webkitbeginfullscreen', onBegin);
      video.addEventListener('webkitendfullscreen', onEnd);
    });
    return () => videos.forEach((video) => {
      video.removeEventListener('webkitbeginfullscreen', onBegin);
      video.removeEventListener('webkitendfullscreen', onEnd);
    });
  }, [mediaWindow]);

  useKeyboard(
    useCallback(
      (event: KeyboardEvent) => {
        if (event.target instanceof Element && event.target.closest('button, input, select')) return;
        if (deleteDialogOpen) {
          if (event.key === 'Escape') closeDeleteDialog();
          return;
        }
        if (recordDeleteDialogOpen) {
          if (event.key === 'Escape') closeRecordDeleteDialog();
          return;
        }
        if (event.key === 'Escape') leave();
        if (event.key.toLowerCase() === 'f') toggleFullscreen();
        if (event.key === 'ArrowLeft' || event.key.toLowerCase() === 'a') goAsset(activeNeighbors?.previous[0], -1);
        if (event.key === 'ArrowRight' || event.key.toLowerCase() === 'd') goAsset(activeNeighbors?.next[0], 1);
      },
      [activeNeighbors, closeDeleteDialog, closeRecordDeleteDialog, deleteDialogOpen, goAsset, leave, recordDeleteDialogOpen, toggleFullscreen],
    ),
  );

  useEffect(() => {
    if (overlay) return;
    restoreSidebarState(viewerReturnStateRef.current);
  }, [overlay, restoreSidebarState]);

  const viewerInfoPanel = (
    <ViewerSidebarPanel
      asset={current}
      error={error}
      sidecarError={sidecarsAssetId === currentAssetId ? sidecarError : null}
      sidecars={sidecarsAssetId === currentAssetId ? sidecars : null}
      tags={assetTagsAssetId === currentAssetId ? assetTags : []}
      preloadIndicators={viewerPreloadIndicators}
      neighborSlots={viewerNeighborSlots}
      aiResult={assetAIAssetId === currentAssetId ? assetAI : null}
      aiError={assetAIAssetId === currentAssetId ? assetAIError : null}
      aiTagError={aiTagError}
      aiTagSaving={aiTagSaving}
      tagDraft={tagDraft}
      tagError={tagError}
      tagSaving={tagSaving}
      assetInfoCollapsed={assetInfoCollapsed}
      audioPlaybackInfo={audioPlaybackInfo}
      videoPlaybackInfo={videoPlaybackInfo}
      videoProxyRuntime={currentVideoProxyRuntime}
      onLeave={leave}
      onToggleAssetInfo={() => setAssetInfoCollapsed((collapsed) => !collapsed)}
      onOpenFolder={openAssetFolder}
      onNFOSearch={searchByNFOValue}
      onTagDraftChange={setTagDraft}
      onAddTag={addCurrentAssetTag}
      onRemoveTag={removeCurrentAssetTag}
      onSaveAITag={saveCurrentAITag}
      onRemoveAITag={removeCurrentAITag}
      onReanalyzeAI={() => current && void api.reanalyzeAssetAI(current.id).then(() => setAssetAI((value) => {
        const next = value ? { ...value, status: 'pending' as const, error: undefined } : value;
        if (next) assetAICacheRef.current.set(current.id, next);
        return next;
      }))}
      onRatingChange={(rating) => void rateCurrentAsset(rating)}
    />
  );

  const renderMediaLayer = (asset: Asset) => {
    const key = mediaReadyKey(asset.id, asset.cacheKey);
    const requestedCurrent = key === currentMediaKey;
    const displayed = key === displayedMediaKey;
    const layerMode: ViewerMediaLayerMode = displayed ? (requestedCurrent ? 'active' : 'hold') : 'prepare';
    const playbackLayerMode: ViewerMediaLayerMode = layerMode === 'active' && key !== committedMediaKey ? 'prepare' : layerMode;
    const preloadEnabled = requestedCurrent
      || displayed
      || preparedMediaStatus[key] === 'ready'
      || key === backgroundMediaPreloadKey;
    return (
      <div
        aria-hidden={layerMode !== 'active'}
        className={`viewer-media-layer viewer-media-layer-${layerMode}`}
        data-asset-id={asset.id}
        data-layer-mode={layerMode}
        data-preload-enabled={preloadEnabled ? 'true' : 'false'}
        key={key}
        ref={(element) => {
          if (element) (element as HTMLDivElement & { inert: boolean }).inert = layerMode !== 'active';
        }}
      >
        {asset.mediaType === 'image' ? (
        <ImageViewer
          asset={asset}
          deleting={deleteLoading || deleteSubmitting || recordDeleteSubmitting}
          fullscreen={fullscreen}
          layerMode={layerMode}
          preloadEnabled={preloadEnabled}
          onDelete={openDeleteDialog}
          onDeleteRecord={openRecordDeleteDialog}
          onMediaError={handleMediaError}
          onMediaReady={handleMediaReady}
          onPosterReady={handlePosterReady}
          playbackMode={viewerPrefs.playbackMode}
          slideshowSeconds={viewerPrefs.imageSlideshowSeconds}
          onPlaybackEnded={playNextAsset}
          onPlaybackModeChange={updatePlaybackMode}
          onRotate={() => void rotateCurrentAsset()}
          onToggleFullscreen={toggleFullscreen}
        />
        ) : asset.mediaType === 'video' ? (
        <Suspense fallback={null}><VideoViewer
          asset={asset}
          fullscreen={fullscreen}
          layerMode={playbackLayerMode}
          playbackRate={playbackRate}
          viewerPrefs={viewerPrefs}
          selectedSubtitleId={selectedSubtitleId}
          subtitles={playbackLayerMode === 'active' && sidecarsAssetId === asset.id ? sidecars?.subtitles ?? [] : []}
          subtitlesEnabled={playbackLayerMode === 'active' && sidecarsAssetId === asset.id && subtitlesEnabled}
          deleting={deleteLoading || deleteSubmitting || recordDeleteSubmitting}
          onDanmakuPrefChange={updateDanmakuPref}
          onDelete={openDeleteDialog}
          onDeleteRecord={openRecordDeleteDialog}
          onMediaError={handleMediaError}
          onMediaReady={handleMediaReady}
          onPosterReady={handlePosterReady}
          onPriorityPreloadComplete={handlePriorityPreloadComplete}
          onPlaybackInfoChange={handleCurrentPlaybackInfoChange}
          onPlaybackControllerChange={handlePlaybackControllerChange}
          onPlaybackEnded={playNextAsset}
          onPrevious={() => previousAsset && goAsset(previousAsset, -1)}
          onNext={() => nextAsset && goAsset(nextAsset, 1)}
          previousEnabled={previousReady}
          nextEnabled={nextReady}
          onPlaybackModeChange={updatePlaybackMode}
          onPlaybackRateChange={updatePlaybackRate}
          onRotate={() => void rotateCurrentAsset()}
          onSelectedSubtitleChange={updateSelectedSubtitle}
          onSubtitlesEnabledChange={updateSubtitlesEnabled}
          onToggleFullscreen={toggleFullscreen}
          onProxyRuntimeChange={handleCurrentProxyRuntimeChange}
        /></Suspense>
        ) : (
        <Suspense fallback={null}><AudioViewer
          asset={asset}
          deleting={deleteLoading || deleteSubmitting || recordDeleteSubmitting}
          fullscreen={fullscreen}
          layerMode={playbackLayerMode}
          playbackRate={playbackRate}
          preloadEnabled={playbackLayerMode === 'active'}
          viewerPrefs={viewerPrefs}
          onDelete={openDeleteDialog}
          onDeleteRecord={openRecordDeleteDialog}
          onMediaError={handleMediaError}
          onMediaReady={handleMediaReady}
          onPlaybackInfoChange={handleCurrentAudioPlaybackInfoChange}
          onPlaybackControllerChange={handlePlaybackControllerChange}
          onPlaybackEnded={playNextAsset}
          onPrevious={() => previousAsset && goAsset(previousAsset, -1)}
          onNext={() => nextAsset && goAsset(nextAsset, 1)}
          previousEnabled={previousReady}
          nextEnabled={nextReady}
          onPlaybackModeChange={updatePlaybackMode}
          onPlaybackRateChange={updatePlaybackRate}
          onToggleFullscreen={toggleFullscreen}
        /></Suspense>
        )}
      </div>
    );
  };

  const fittedViewerPanelWidth = fitViewerPanelWidth(viewerPanelWidth, viewerAvailableWidth);

  useLayoutEffect(() => {
    const shell = viewerRef.current?.closest<HTMLElement>('.app-shell');
    if (!shell) return;
    // Let the pinned top bar cover the resize gutter at the top edge so the
    // media bar and information panel meet without a visible vertical seam.
    const offset = viewerInfoPanelState.visible ? fittedViewerPanelWidth : 0;
    shell.style.setProperty('--viewer-info-offset', `${offset}px`);
    return () => {
      shell.style.removeProperty('--viewer-info-offset');
    };
  }, [fittedViewerPanelWidth, viewerInfoPanelState.visible]);

  const viewerStyle = {
    '--viewer-info-width': `${fittedViewerPanelWidth}px`,
  } as CSSProperties;

  return (
    <>
      <section
        ref={viewerRef}
        className={`${overlay ? 'viewer-page viewer-overlay' : 'viewer-page'}${viewerInfoPanelState.visible ? '' : ' viewer-info-hidden'}${fullscreenFallback ? ' viewer-fullscreen-fallback' : ''}`}
        style={viewerStyle}
      >
        <div
          ref={viewerBodyRef}
          className="viewer-body"
          onClickCapture={handleViewerClickCapture}
          onTouchCancel={() => { mobileSwipeRef.current = null; }}
          onTouchEnd={handleViewerTouchEnd}
          onTouchStart={handleViewerTouchStart}
          onContextMenu={(event) => {
            if (!(event.target instanceof Element) || !event.target.closest('.image-stage, .video-stage, .audio-stage')) return;
            if (event.target.closest('[data-viewer-wheel-control]')) return;
            event.preventDefault();
            leave();
          }}
        >
          {mediaWindow.map(renderMediaLayer)}
          <FullscreenMediaFilmstrip
            current={current}
            fullscreen={fullscreen}
            loadStatusByKey={viewerPreloadStatusByKey}
            neighborQuery={query}
            next={activeNeighbors?.next ?? []}
            previous={activeNeighbors?.previous ?? []}
            onSelect={goAsset}
          />
          {currentMediaFailed && mediaLoadFailure && (
            <div className="viewer-media-load-error" role="status">{mediaLoadFailure.message}</div>
          )}
        </div>
        {viewerInfoPanelState.visible && (
          <>
            <button
              aria-label="调整查看器信息栏宽度"
              className="viewer-info-resize-handle"
              title="拖动调整查看器信息栏宽度"
              type="button"
              onPointerDown={startViewerPanelResize}
            />
            <aside className="viewer-info-panel" data-viewer-wheel-control>
              {viewerInfoPanel}
            </aside>
          </>
        )}
      </section>
      {deleteDialogOpen && (
        <AssetDeleteDialog
          error={deleteError}
          loading={deleteLoading}
          plan={deletePlan}
          submitting={deleteSubmitting}
          onClose={closeDeleteDialog}
          onConfirm={() => void confirmDeleteAsset()}
        />
      )}
      {recordDeleteDialogOpen && current && (
        <AssetRecordDeleteDialog
          asset={current}
          error={recordDeleteError}
          submitting={recordDeleteSubmitting}
          onClose={closeRecordDeleteDialog}
          onConfirm={() => void confirmDeleteAssetRecord()}
        />
      )}
    </>
  );
}

function isAbortError(err: unknown) {
  return err instanceof DOMException && err.name === 'AbortError';
}

function assetIdFromPath(pathname: string) {
  const match = pathname.match(/^\/viewer\/(\d+)/);
  return match?.[1] ?? '';
}

function isMediaZoomWheel(event: WheelEvent) {
  return event.target instanceof Element && Boolean(event.target.closest('.image-stage.zooming, .video-stage.video-hold-zoom-active'));
}

function isViewerWheelControl(event: WheelEvent) {
  return event.composedPath().some((target) => target instanceof Element && target.hasAttribute('data-viewer-wheel-control'));
}

function normalizedViewerWheelDelta(event: WheelEvent) {
  if (event.deltaMode === 1) return event.deltaY * 40;
  if (event.deltaMode === 2) return event.deltaY * Math.max(window.innerHeight, 1);
  return event.deltaY;
}

function wheelTargetAtOffset(base: WheelBase, offset: number) {
  if (offset === 0) return base.current;
  return offset > 0 ? base.next[offset - 1] : base.previous[Math.abs(offset) - 1];
}

function isViewerTouchControl(target: EventTarget | null) {
  return target instanceof Element && Boolean(target.closest(
    '.video-control-zone, .audio-controls, .image-control-zone, .viewer-media-load-error, [data-viewer-touch-control]',
  ));
}

function wheelBelongsToViewer(event: WheelEvent, viewer: HTMLElement | null) {
  if (!viewer) return false;
  if (event.composedPath().includes(viewer)) return true;
  return event.target instanceof Node && viewer.contains(event.target);
}

async function returnStateForCurrentAsset(searchParams: URLSearchParams, assetId: number | undefined) {
  const rawReturnState = searchParams.get('returnState');
  const baseState = decodeReturnState<Record<string, unknown> & Partial<GridReturnState>>(rawReturnState, {});
  if (!assetId) {
    return rawReturnState ? baseState : null;
  }
  try {
    const position = await api.assetPosition(assetId, assetPositionParams(searchParams));
    return {
      ...baseState,
      focusAssetId: assetId,
      loadedItemCount: viewerReturnPageSize,
      loadedStartIndex: Math.max(0, (position.page - 1) * viewerReturnPageSize),
      scrollRatio: position.position,
      scrollTop: 0,
    };
  } catch {
    return rawReturnState ? baseState : null;
  }
}

function assetPositionParams(searchParams: URLSearchParams) {
  const params: Record<string, string | number> = { pageSize: viewerReturnPageSize };
  searchParams.forEach((value, key) => {
    if (key === 'returnPath' || key === 'returnState') return;
    params[key] = value;
  });
  return params;
}

function returnPathMatchesFallback(returnPath: string | null, fallback: string) {
  if (!returnPath) return false;
  try {
    return new URL(returnPath, window.location.origin).pathname === fallback;
  } catch {
    return returnPath === fallback || returnPath.startsWith(`${fallback}?`);
  }
}

function appendRestoreParam(path: string, state: object) {
  const separator = path.includes('?') ? '&' : '?';
  return `${path}${separator}restore=${encodeReturnState(state)}`;
}

function ViewerSidebarPanel({
  asset,
  error,
  sidecarError,
  sidecars,
	tags,
	preloadIndicators,
	neighborSlots,
	aiResult,
	aiError,
  aiTagError,
  aiTagSaving,
  tagDraft,
  tagError,
  tagSaving,
  assetInfoCollapsed,
  audioPlaybackInfo,
  videoPlaybackInfo,
  videoProxyRuntime,
  onLeave,
  onToggleAssetInfo,
  onOpenFolder,
  onNFOSearch,
  onTagDraftChange,
  onAddTag,
  onRemoveTag,
  onSaveAITag,
  onRemoveAITag,
	onReanalyzeAI,
  onRatingChange,
}: {
  asset: Asset | undefined;
  error: string | null;
  sidecarError: string | null;
  sidecars: AssetSidecars | null;
	tags: AssetTag[];
	preloadIndicators: ViewerLoadIndicatorStatus[];
	neighborSlots: ViewerNeighborSlot[];
	aiResult: AssetAIResult | null;
	aiError: string | null;
  aiTagError: string | null;
  aiTagSaving: boolean;
  tagDraft: string;
  tagError: string | null;
  tagSaving: boolean;
  assetInfoCollapsed: boolean;
  audioPlaybackInfo: AudioPlaybackInfo | null;
  videoPlaybackInfo: VideoPlaybackInfo | null;
  videoProxyRuntime: VideoSegmentStatus | null;
  onLeave: () => void;
  onToggleAssetInfo: () => void;
  onOpenFolder: (asset: Asset) => void;
  onNFOSearch: (field: NFOFilterField | 'nfo', value: string) => void;
  onTagDraftChange: (value: string) => void;
  onAddTag: (tag?: string) => Promise<boolean>;
  onRemoveTag: (tag: string) => Promise<boolean>;
  onSaveAITag: (payload: { previousTag?: string; tag: string; categoryKey: string; subjectKey: string }) => Promise<boolean>;
  onRemoveAITag: (tag: string) => Promise<boolean>;
	onReanalyzeAI: () => void;
  onRatingChange: (rating: AssetRating) => void;
}) {
  const nfoFields = sidecars?.nfo?.fields ?? {};
  const nfoGroups = sidecars?.nfo?.groups?.filter((group) => group.items.length > 0) ?? [];
  const nfoFieldEntries = Object.entries(nfoFields).filter(([, value]) => value.trim() !== '');
  const preloadReadyCount = preloadIndicators.filter((status) => status === 'ready').length;
  const preloadLoadingCount = preloadIndicators.filter((status) => status === 'loading').length;
  return (
    <div className="sidebar-control-stack">
      <div className="sidebar-control-title">查看器</div>
      <div className="sidebar-viewer-actions">
        <button className="sidebar-square-button sidebar-viewer-leave-button" type="button" title="退出查看" onClick={onLeave}>
          <LogOut size={16} />
          <span>退出查看</span>
        </button>
        {asset && (
          <a
            aria-label="下载原文件"
            className="sidebar-square-button sidebar-viewer-download-button"
            href={assetDownloadUrl(asset)}
            title="下载原文件"
          >
            <Download size={17} />
          </a>
        )}
        <div
          className="sidebar-viewer-preload-status"
          role="status"
          aria-label={`预加载状态：${preloadReadyCount} 个已完成，${preloadLoadingCount} 个加载中`}
        >
          {preloadIndicators.map((status, index) => (
            <span
              className={`sidebar-viewer-preload-dot ${status}`}
              key={index}
              title={status === 'ready' ? '加载好了' : status === 'loading' ? '加载过程中' : '没有加载'}
            />
          ))}
        </div>
      </div>
      {error && <div className="sidebar-error">{error}</div>}
      {sidecarError && <div className="sidebar-error">{sidecarError}</div>}
      {asset && (
        <>
          <div className={`sidebar-asset-info${assetInfoCollapsed ? ' is-collapsed' : ''}`}>
            <button
              className="sidebar-asset-info-title"
              type="button"
              aria-expanded={!assetInfoCollapsed}
              title={assetInfoCollapsed ? '展开媒体信息' : '折叠媒体信息'}
              onClick={onToggleAssetInfo}
            >
              <strong>{asset.filename}</strong>
              {aiResult?.palette && aiResult.palette.length > 0 && (
                <div className="viewer-title-palette" aria-label="主题配色">
                  {aiResult.palette.slice(0, 5).map((color, index) => (
                    <span key={`${color.hex}-${index}`} style={{ backgroundColor: color.hex }} title={`${color.hex} · ${Math.round(color.weight * 100)}%`} />
                  ))}
                </div>
              )}
            </button>
            {!assetInfoCollapsed && (
              <>
                <button className="sidebar-asset-folder-link" type="button" onClick={() => onOpenFolder(asset)}>
                  <FolderOpen size={14} />
                  <span>{assetFolderLabel(asset)}</span>
                </button>
                <dl className="sidebar-asset-info-details">
                  {assetInfoRows(asset).map((item) => (
                    <div key={item.label}>
                      <dt>{item.label}</dt>
                      <dd>{item.value}</dd>
                    </div>
                  ))}
                </dl>
              </>
            )}
          </div>
          {asset.mediaType !== 'image' && (
            <ViewerPlaybackDetails
              asset={asset}
              audioInfo={audioPlaybackInfo}
              videoInfo={videoPlaybackInfo}
              runtime={videoProxyRuntime}
            />
          )}
          <SidebarAssetTags
            draft={tagDraft}
            error={tagError}
            saving={tagSaving}
            tags={tags}
            onAdd={onAddTag}
            onDraftChange={onTagDraftChange}
            onRemove={onRemoveTag}
          />
		  {asset.mediaType !== 'audio' && <div className="sidebar-asset-tags sidebar-ai-result">
			<div className="sidebar-control-title">AI 描述</div>
			{aiError && <div className="sidebar-error">{aiError}</div>}
			{!aiResult && !aiError && <div className="sidebar-empty-line">读取中</div>}
			{aiResult?.status === 'pending' && <div className="sidebar-empty-line">待分析</div>}
			{aiResult?.status === 'processing' && <div className="sidebar-empty-line">处理中</div>}
			{aiResult?.status === 'failed' && <div className="sidebar-error">{aiResult.error || '分析失败'} <button type="button" onClick={onReanalyzeAI}>重试</button></div>}
			{aiResult?.status === 'ready' && <p className="sidebar-ai-description">{aiResult.description}</p>}
            {aiResult && (
              <ViewerEditableAITags
                error={aiTagError}
                saving={aiTagSaving}
                tags={aiResult.tags ?? []}
                onRemove={onRemoveAITag}
                onSave={onSaveAITag}
              />
            )}
		  </div>}
          <div className="sidebar-control-title">星级</div>
          <RatingStars value={normalizeAssetRating(asset.rating)} onChange={onRatingChange} />
        </>
      )}
      {sidecars?.nfo && (
        <div className="sidebar-nfo">
          <div className="sidebar-nfo-header">
            <div className="sidebar-control-title">NFO</div>
            <small>{sidecars.nfo.filename}</small>
          </div>
          {nfoGroups.length > 0
            ? nfoGroups.map((group) => (
                <section className="sidebar-nfo-group" key={group.title}>
                  <div className="sidebar-nfo-group-title">{group.title}</div>
                  <div className="sidebar-nfo-items">
                    {group.items.map((item, index) => (
                      <NFOValue groupTitle={group.title} key={`${item.key}-${item.value}-${index}`} item={item} onSearch={onNFOSearch} />
                    ))}
                  </div>
                </section>
              ))
            : nfoFieldEntries.length > 0 && (
                <section className="sidebar-nfo-group">
                  <div className="sidebar-nfo-group-title">字段</div>
                  <div className="sidebar-nfo-items">
                    {nfoFieldEntries.map(([key, value]) => (
                      <NFOValue
                        key={key}
                        item={{ key, label: key, value, copyable: false }}
                        onSearch={onNFOSearch}
                      />
                    ))}
                  </div>
                </section>
              )}
          {nfoGroups.length === 0 && nfoFieldEntries.length === 0 && sidecars.nfo.text && <pre>{sidecars.nfo.text}</pre>}
        </div>
      )}
      <ViewerNeighborThumbnails slots={neighborSlots} />
    </div>
  );
}

interface ViewerNeighborSlot {
  asset?: Asset;
  status: ViewerLoadIndicatorStatus;
}

function ViewerNeighborThumbnails({ slots }: { slots: ViewerNeighborSlot[] }) {
  const previous = slots.slice(0, viewerRetainRadius);
  const next = slots.slice(viewerRetainRadius, viewerRetainRadius * 2);
  return (
    <section className="viewer-neighbor-thumbnails" aria-label="前后媒体">
      <div className="viewer-neighbor-thumbnail-grid">
        {previous.map((slot, index) => (
          <ViewerNeighborThumbnail key={slot.asset ? mediaReadyKey(slot.asset.id, slot.asset.cacheKey) : `previous-${index}`} slot={slot} />
        ))}
      </div>
      <div className="viewer-neighbor-thumbnail-divider" aria-hidden="true" />
      <div className="viewer-neighbor-thumbnail-grid">
        {next.map((slot, index) => (
          <ViewerNeighborThumbnail key={slot.asset ? mediaReadyKey(slot.asset.id, slot.asset.cacheKey) : `next-${index}`} slot={slot} />
        ))}
      </div>
    </section>
  );
}

function ViewerNeighborThumbnail({ slot }: { slot: ViewerNeighborSlot }) {
  const frameRef = useRef<HTMLDivElement | null>(null);
  const [frameSize, setFrameSize] = useState({ width: 0, height: 0 });
  useLayoutEffect(() => {
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

  const asset = slot.asset;
  const showThumbnail = Boolean(asset && (asset.mediaType === 'audio' || asset.thumbStatus === 'ready'));
  return (
    <div
      className={`viewer-neighbor-thumbnail${asset ? '' : ' is-empty'}`}
      ref={frameRef}
      title={asset?.displayTitle || asset?.filename || '没有媒体'}
    >
      {showThumbnail && asset && (
        <img
          alt={asset.displayTitle || asset.filename}
          draggable={false}
          src={assetThumbUrl(asset)}
          style={rotatedCoverStyle(asset, frameSize)}
          onError={(event) => {
            event.currentTarget.hidden = true;
          }}
        />
      )}
      <span
        className={`sidebar-viewer-preload-dot viewer-neighbor-thumbnail-status ${slot.status}`}
        aria-label={slot.status === 'ready' ? '加载好了' : slot.status === 'loading' ? '加载过程中' : '没有加载'}
        title={slot.status === 'ready' ? '加载好了' : slot.status === 'loading' ? '加载过程中' : '没有加载'}
      />
    </div>
  );
}

const filmstripNeighborPageSize = 12;

function FullscreenMediaFilmstrip({
  current,
  fullscreen,
  loadStatusByKey,
  neighborQuery,
  next,
  onSelect,
  previous,
}: {
  current?: Asset;
  fullscreen: boolean;
  loadStatusByKey: Record<string, ViewerLoadIndicatorStatus>;
  neighborQuery: Record<string, string>;
  next: Asset[];
  onSelect: (asset: Asset | undefined, direction?: -1 | 0 | 1) => void;
  previous: Asset[];
}) {
  const [open, setOpen] = useState(false);
  const hideTimerRef = useRef<number | null>(null);
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const currentItemRef = useRef<HTMLButtonElement | null>(null);
  const loadEdgesNearViewportRef = useRef<() => void>(() => undefined);
  const edgeLoadingRef = useRef({ next: false, previous: false });
  const edgeAbortRef = useRef<{ next: AbortController | null; previous: AbortController | null }>({ next: null, previous: null });
  const previousScrollRestoreRef = useRef<{ left: number; width: number } | null>(null);
  const [previousItems, setPreviousItems] = useState(previous);
  const [nextItems, setNextItems] = useState(next);
  const [hasMorePrevious, setHasMorePrevious] = useState(previous.length >= filmstripNeighborPageSize);
  const [hasMoreNext, setHasMoreNext] = useState(next.length >= filmstripNeighborPageSize);
  const hoverCapable = typeof window !== 'undefined' && window.matchMedia('(hover: hover) and (pointer: fine)').matches;
  const previousSignature = previous.map((asset) => mediaReadyKey(asset.id, asset.cacheKey)).join(',');
  const nextSignature = next.map((asset) => mediaReadyKey(asset.id, asset.cacheKey)).join(',');
  const neighborQuerySignature = JSON.stringify(neighborQuery);
  const items = useMemo(
    () => current ? [...previousItems].reverse().concat(current, nextItems) : [],
    [current, nextItems, previousItems],
  );

  useEffect(() => {
    edgeAbortRef.current.previous?.abort();
    edgeAbortRef.current.next?.abort();
    edgeAbortRef.current = { next: null, previous: null };
    edgeLoadingRef.current = { next: false, previous: false };
    previousScrollRestoreRef.current = null;
    setPreviousItems(previous);
    setNextItems(next);
    setHasMorePrevious(previous.length >= filmstripNeighborPageSize);
    setHasMoreNext(next.length >= filmstripNeighborPageSize);
  }, [current?.cacheKey, current?.id, neighborQuerySignature, nextSignature, previousSignature]);

  useEffect(() => () => {
    edgeAbortRef.current.previous?.abort();
    edgeAbortRef.current.next?.abort();
  }, []);

  useLayoutEffect(() => {
    const restore = previousScrollRestoreRef.current;
    const viewport = viewportRef.current;
    if (!restore || !viewport) return;
    previousScrollRestoreRef.current = null;
    viewport.scrollLeft = restore.left + Math.max(0, viewport.scrollWidth - restore.width);
  }, [previousItems.length]);

  const loadEdge = useCallback(async (direction: 'previous' | 'next') => {
    const hasMore = direction === 'previous' ? hasMorePrevious : hasMoreNext;
    const sourceItems = direction === 'previous' ? previousItems : nextItems;
    if (!current || !hasMore || edgeLoadingRef.current[direction]) return;
    const edge = sourceItems[sourceItems.length - 1];
    if (!edge) return;

    edgeLoadingRef.current[direction] = true;
    const controller = new AbortController();
    edgeAbortRef.current[direction] = controller;
    try {
      const result = await api.neighbors(edge.id, neighborQuery, controller.signal);
      const candidates = direction === 'previous' ? result.previous : result.next;
      const existingIds = new Set([current.id, ...previousItems.map((asset) => asset.id), ...nextItems.map((asset) => asset.id)]);
      const added = candidates.filter((asset) => {
        if (existingIds.has(asset.id)) return false;
        existingIds.add(asset.id);
        return true;
      });
      const canContinue = candidates.length >= filmstripNeighborPageSize && added.length > 0;
      if (direction === 'previous') {
        const viewport = viewportRef.current;
        if (viewport && added.length > 0) {
          previousScrollRestoreRef.current = { left: viewport.scrollLeft, width: viewport.scrollWidth };
        }
        if (added.length > 0) setPreviousItems((existing) => existing.concat(added));
        setHasMorePrevious(canContinue);
      } else {
        if (added.length > 0) setNextItems((existing) => existing.concat(added));
        setHasMoreNext(canContinue);
      }
    } catch (error) {
      if (!isAbortError(error)) {
        if (direction === 'previous') setHasMorePrevious(false);
        else setHasMoreNext(false);
      }
    } finally {
      if (edgeAbortRef.current[direction] === controller) edgeAbortRef.current[direction] = null;
      edgeLoadingRef.current[direction] = false;
    }
  }, [current, hasMoreNext, hasMorePrevious, neighborQuery, nextItems, previousItems]);

  const loadEdgesNearViewport = useCallback(() => {
    const viewport = viewportRef.current;
    if (!open || !viewport) return;
    const threshold = Math.max(480, viewport.clientWidth * 1.25);
    if (viewport.scrollLeft <= threshold) void loadEdge('previous');
    if (viewport.scrollWidth - viewport.scrollLeft - viewport.clientWidth <= threshold) void loadEdge('next');
  }, [loadEdge, open]);
  loadEdgesNearViewportRef.current = loadEdgesNearViewport;

  const cancelHide = useCallback(() => {
    if (hideTimerRef.current === null) return;
    window.clearTimeout(hideTimerRef.current);
    hideTimerRef.current = null;
  }, []);

  const reveal = useCallback(() => {
    cancelHide();
    setOpen(true);
  }, [cancelHide]);

  const scheduleHide = useCallback(() => {
    cancelHide();
    hideTimerRef.current = window.setTimeout(() => {
      hideTimerRef.current = null;
      setOpen(false);
    }, 500);
  }, [cancelHide]);

  useEffect(() => {
    if (fullscreen) return;
    cancelHide();
    setOpen(false);
  }, [cancelHide, fullscreen]);

  useEffect(() => () => cancelHide(), [cancelHide]);

  useLayoutEffect(() => {
    if (!open || !currentItemRef.current) return;
    currentItemRef.current.scrollIntoView({ behavior: 'auto', block: 'nearest', inline: 'center' });
    const frame = window.requestAnimationFrame(() => loadEdgesNearViewportRef.current());
    return () => window.cancelAnimationFrame(frame);
  }, [current?.id, open]);

  if (!fullscreen || !hoverCapable || !current || items.length === 0) return null;

  const currentIndex = previousItems.length;
  return (
    <div className={`viewer-filmstrip-shell${open ? ' open' : ''}`} data-viewer-wheel-control>
      <div className="viewer-filmstrip-trigger" aria-hidden="true" onMouseEnter={reveal} />
      <section
        aria-label="全屏媒体胶片栏"
        className="viewer-filmstrip"
        onMouseEnter={reveal}
        onMouseLeave={scheduleHide}
      >
        <div
          className="viewer-filmstrip-viewport"
          ref={viewportRef}
          onScroll={loadEdgesNearViewport}
          onWheel={(event) => {
            const viewport = viewportRef.current;
            if (!viewport) return;
            event.preventDefault();
            event.stopPropagation();
            const delta = Math.abs(event.deltaY) >= Math.abs(event.deltaX) ? event.deltaY : event.deltaX;
            viewport.scrollLeft += normalizedViewerWheelDeltaValue(delta, event.deltaMode);
          }}
        >
          <div className="viewer-filmstrip-track">
            {items.map((asset, index) => {
              const selected = asset.id === current.id;
              return (
                <FullscreenMediaFilmstripItem
                  asset={asset}
                  currentRef={selected ? (element) => { currentItemRef.current = element; } : undefined}
                  direction={index < currentIndex ? -1 : 1}
                  key={mediaReadyKey(asset.id, asset.cacheKey)}
                  loadStatus={loadStatusByKey[mediaReadyKey(asset.id, asset.cacheKey)] ?? 'idle'}
                  selected={selected}
                  onSelect={onSelect}
                />
              );
            })}
          </div>
        </div>
      </section>
    </div>
  );
}

function FullscreenMediaFilmstripItem({
  asset,
  currentRef,
  direction,
  loadStatus,
  onSelect,
  selected,
}: {
  asset: Asset;
  currentRef?: (element: HTMLButtonElement | null) => void;
  direction: -1 | 1;
  loadStatus: ViewerLoadIndicatorStatus;
  onSelect: (asset: Asset | undefined, direction?: -1 | 0 | 1) => void;
  selected: boolean;
}) {
  const frameRef = useRef<HTMLButtonElement | null>(null);
  const [frameSize, setFrameSize] = useState({ width: 0, height: 0 });
  useLayoutEffect(() => {
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

  const showThumbnail = asset.mediaType === 'audio' || asset.thumbStatus === 'ready';
  return (
    <button
      aria-current={selected ? 'true' : undefined}
      aria-label={`${selected ? '当前媒体：' : '切换到：'}${asset.displayTitle || asset.filename}`}
      className={`viewer-filmstrip-item${selected ? ' current' : ''}`}
      ref={(element) => {
        frameRef.current = element;
        currentRef?.(element);
      }}
      style={{ aspectRatio: filmstripThumbnailRatio(asset) }}
      title={asset.displayTitle || asset.filename}
      type="button"
      onClick={() => {
        if (!selected) onSelect(asset, direction);
      }}
    >
      {showThumbnail && (
        <img
          alt=""
          decoding="async"
          draggable={false}
          loading="lazy"
          src={assetThumbUrl(asset)}
          style={rotatedCoverStyle(asset, frameSize)}
          onError={(event) => { event.currentTarget.hidden = true; }}
        />
      )}
      <span
        aria-label={loadStatus === 'ready' ? '加载好了' : loadStatus === 'loading' ? '加载过程中' : '没有加载'}
        className={`sidebar-viewer-preload-dot viewer-neighbor-thumbnail-status ${loadStatus}`}
        title={loadStatus === 'ready' ? '加载好了' : loadStatus === 'loading' ? '加载过程中' : '没有加载'}
      />
      {selected && <span className="viewer-filmstrip-current-indicator" aria-hidden="true" />}
    </button>
  );
}

function filmstripThumbnailRatio(asset: Asset) {
  if (asset.mediaType === 'audio') return 1;
  let width = asset.width || 16;
  let height = asset.height || 9;
  const rotation = ((asset.rotation % 360) + 360) % 360;
  if (rotation === 90 || rotation === 270) [width, height] = [height, width];
  return Math.min(1.9, Math.max(0.72, width / Math.max(1, height)));
}

function normalizedViewerWheelDeltaValue(delta: number, deltaMode: number) {
  if (deltaMode === 1) return delta * 40;
  if (deltaMode === 2) return delta * window.innerWidth;
  return delta;
}

interface EditableAITagDraft {
  previousTag?: string;
  tag: string;
  categoryKey: string;
  subjectKey: string;
}

const closeupTagOptions = [
  '脸部特写', '头部特写', '眼部特写', '鼻部特写', '嘴部特写', '嘴唇特写', '舌部特写', '牙齿特写', '耳部特写',
  '颈部特写', '肩部特写', '锁骨特写', '胸部特写', '腹部特写', '肚脐特写', '腰部特写', '背部特写',
  '手部特写', '手掌特写', '手指特写', '手臂特写', '肘部特写', '手腕特写',
  '臀部特写', '腿部特写', '大腿特写', '膝部特写', '小腿特写', '脚踝特写', '脚部特写', '脚底特写', '脚趾特写', '全身特写',
];
interface EditableAITagKind {
  key: string;
  label: string;
  categoryKey: string;
  subjectKey: string;
  options?: readonly string[];
}
const aiTagKinds: readonly EditableAITagKind[] = [
  { key: 'closeup.part', label: '特写', categoryKey: 'closeup', subjectKey: 'part', options: closeupTagOptions },
  { key: 'people.count', label: '人物数量', categoryKey: 'people', subjectKey: 'count', options: Array.from({ length: 20 }, (_, index) => `${index + 1}人`) },
  { key: 'action.posture', label: '姿态', categoryKey: 'action', subjectKey: 'posture', options: ['坐姿', '躺姿', '站立', '蹲姿', '跪姿', '俯卧', '仰卧'] },
  { key: 'action.activity', label: '动作', categoryKey: 'action', subjectKey: 'activity', options: ['行走', '跳舞', '做操', '跑步', '游泳', '骑行', '瑜伽', '健身', '挥手', '比心', '摆姿势'] },
  { key: 'shoes.shoes', label: '鞋子', categoryKey: 'shoes', subjectKey: 'shoes' },
  { key: 'socks.socks', label: '袜子', categoryKey: 'socks', subjectKey: 'socks' },
  { key: 'clothes.top', label: '上衣', categoryKey: 'clothes', subjectKey: 'top' },
  { key: 'clothes.outerwear', label: '外套', categoryKey: 'clothes', subjectKey: 'outerwear' },
  { key: 'clothes.dress', label: '裙装', categoryKey: 'clothes', subjectKey: 'dress' },
  { key: 'clothes.pants', label: '裤装', categoryKey: 'clothes', subjectKey: 'pants' },
  { key: 'clothes.sportswear', label: '运动服', categoryKey: 'clothes', subjectKey: 'sportswear' },
  { key: 'clothes.swimwear', label: '泳装', categoryKey: 'clothes', subjectKey: 'swimwear' },
  { key: 'clothes.hat', label: '帽子', categoryKey: 'clothes', subjectKey: 'hat' },
  { key: 'clothes.accessories', label: '配饰', categoryKey: 'clothes', subjectKey: 'accessories' },
];

function ViewerEditableAITags({
  error,
  saving,
  tags,
  onRemove,
  onSave,
}: {
  error: string | null;
  saving: boolean;
  tags: AssetAITag[];
  onRemove: (tag: string) => Promise<boolean>;
  onSave: (draft: EditableAITagDraft) => Promise<boolean>;
}) {
  const [draft, setDraft] = useState<EditableAITagDraft | null>(null);
  const [pendingDeleteTag, setPendingDeleteTag] = useState('');
  const selectedKindKey = draft ? `${draft.categoryKey}.${draft.subjectKey}` : '';
  const selectedKind = aiTagKinds.find((kind) => kind.key === selectedKindKey) ?? aiTagKinds[0];
  const fixedOptions = selectedKind.options
    ? selectedKind.options.includes(draft?.tag ?? '')
      ? selectedKind.options
      : [draft?.tag ?? '', ...selectedKind.options].filter(Boolean)
    : null;
  const groups = [...tags.reduce((result, tag) => {
    const category = tag.categoryLabel || '其他';
    result.set(category, [...(result.get(category) ?? []), tag]);
    return result;
  }, new Map<string, AssetAITag[]>())];
  const edit = (item: AssetAITag) => {
    const supported = aiTagKinds.some((kind) => kind.categoryKey === item.categoryKey && kind.subjectKey === item.subjectKey);
    const fallback = aiTagKinds[0];
    setDraft({
      previousTag: item.tag,
      tag: supported ? item.tag : fallback.options?.[0] ?? '',
      categoryKey: supported ? item.categoryKey! : fallback.categoryKey,
      subjectKey: supported ? item.subjectKey! : fallback.subjectKey,
    });
  };
  const add = () => {
    const initial = aiTagKinds[0];
    setDraft({
      tag: initial.options?.[0] ?? '',
      categoryKey: initial.categoryKey,
      subjectKey: initial.subjectKey,
    });
  };
  const changeKind = (key: string) => {
    const kind = aiTagKinds.find((item) => item.key === key) ?? aiTagKinds[0];
    setDraft((current) => current ? {
      ...current,
      categoryKey: kind.categoryKey,
      subjectKey: kind.subjectKey,
      tag: kind.options?.[0] ?? '',
    } : current);
  };
  return (
    <div className="viewer-editable-ai-tags">
      <div className="viewer-ai-tags-title">
        <div className="sidebar-control-title">AI 标签</div>
        <button className="sidebar-asset-tag-add" disabled={saving} type="button" title="添加标签" onClick={add}>
          <Plus size={13} />
        </button>
      </div>
      {error && <div className="sidebar-error">{error}</div>}
      <div className="sidebar-asset-tag-list">
        {groups.map(([category, items]) => (
          <div className="viewer-ai-tag-group" key={category}>
            <span>{category}</span>
            <div>
              {items.map((item) => (
                <span className="sidebar-asset-tag viewer-ai-tag-chip" key={item.tag}>
                  <button
                    disabled={saving}
                    type="button"
                    title={`${(item.facets ?? []).map((facet) => facet.labels.join(' / ')).join('\n')}\n点击修改`}
                    onClick={() => edit(item)}
                  >
                    <small>{item.subjectLabel || category}</small>{item.tag}
                  </button>
                  <button
                    disabled={saving}
                    type="button"
                    title={`删除 ${item.tag}`}
                    onClick={() => setPendingDeleteTag(item.tag)}
                  >
                    <X size={11} />
                  </button>
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>
      {draft && (
        <form
          className="viewer-ai-tag-editor"
          onSubmit={(event) => {
            event.preventDefault();
            void onSave(draft).then((saved) => {
              if (saved) setDraft(null);
            });
          }}
        >
          <select aria-label="标签分类" disabled={saving} value={selectedKind.key} onChange={(event) => changeKind(event.target.value)}>
            {aiTagKinds.map((kind) => <option key={kind.key} value={kind.key}>{kind.label}</option>)}
          </select>
          {fixedOptions ? (
            <select aria-label="标签值" disabled={saving} value={draft.tag} onChange={(event) => setDraft({ ...draft, tag: event.target.value })}>
              {fixedOptions.map((option) => <option key={option} value={option}>{option}</option>)}
            </select>
          ) : (
            <input
              autoFocus
              disabled={saving}
              maxLength={80}
              placeholder="输入标签"
              value={draft.tag}
              onChange={(event) => setDraft({ ...draft, tag: event.target.value })}
            />
          )}
          <button disabled={saving} type="button" title="取消" onClick={() => setDraft(null)}><X size={13} /></button>
          <button disabled={saving || !draft.tag.trim()} type="submit" title="保存"><Check size={13} /></button>
        </form>
      )}
      {pendingDeleteTag && (
        <ViewerTagDeleteConfirmDialog
          saving={saving}
          tag={pendingDeleteTag}
          onCancel={() => setPendingDeleteTag('')}
          onConfirm={() => void onRemove(pendingDeleteTag).then((removed) => {
            if (!removed) return;
            if (draft?.previousTag === pendingDeleteTag) setDraft(null);
            setPendingDeleteTag('');
          })}
        />
      )}
    </div>
  );
}

function SidebarAssetTags({
  draft,
  error,
  saving,
  tags,
  onAdd,
  onDraftChange,
  onRemove,
}: {
  draft: string;
  error: string | null;
  saving: boolean;
  tags: AssetTag[];
  onAdd: (tag?: string) => Promise<boolean>;
  onDraftChange: (value: string) => void;
  onRemove: (tag: string) => Promise<boolean>;
}) {
  const [adding, setAdding] = useState(false);
  const [pendingDeleteTag, setPendingDeleteTag] = useState('');
  const [options, setOptions] = useState<TagSummary[]>([]);
  const [optionsLoading, setOptionsLoading] = useState(false);
  const [optionsError, setOptionsError] = useState('');
  const [pickerPosition, setPickerPosition] = useState({ left: 8, top: 8 });
  const rootRef = useRef<HTMLDivElement | null>(null);
  const pickerRef = useRef<HTMLDivElement | null>(null);
  const assigned = useMemo(() => new Set(tags.map((item) => item.tag)), [tags]);
  const matchingOptions = useMemo(() => {
    const query = draft.trim().toLocaleLowerCase();
    return options.filter((item) => !query || item.name.toLocaleLowerCase().includes(query));
  }, [draft, options]);

  useEffect(() => {
    if (!adding) return;
    let live = true;
    setOptionsLoading(true);
    setOptionsError('');
    void api.tags().then((result) => {
      if (live) setOptions(result.items ?? []);
    }).catch((err) => {
      if (live) setOptionsError(err instanceof Error ? err.message : '读取自标失败');
    }).finally(() => {
      if (live) setOptionsLoading(false);
    });
    const close = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!rootRef.current?.contains(target) && !pickerRef.current?.contains(target)) {
        setAdding(false);
        onDraftChange('');
      }
    };
    document.addEventListener('pointerdown', close);
    return () => {
      live = false;
      document.removeEventListener('pointerdown', close);
    };
  }, [adding, onDraftChange]);

  const selectTag = async (tag: string) => {
    if (assigned.has(tag)) return;
    const saved = await onAdd(tag);
    if (saved) setAdding(false);
  };
  const togglePicker = (button: HTMLButtonElement) => {
    if (adding) {
      setAdding(false);
      onDraftChange('');
      return;
    }
    const rect = button.getBoundingClientRect();
    const width = Math.min(320, window.innerWidth - 16);
    const height = Math.min(420, window.innerHeight - 16);
    setPickerPosition({
      left: Math.max(8, rect.left - width - 10),
      top: Math.max(8, Math.min(rect.top - 8, window.innerHeight - height - 8)),
    });
    setAdding(true);
  };
  return (
    <div className="sidebar-asset-tags" ref={rootRef}>
      <div className="sidebar-control-title">自标</div>
      {error && <div className="sidebar-error">{error}</div>}
      <div className="sidebar-asset-tag-list">
        {tags.map((item) => (
          <span className="sidebar-asset-tag" key={item.tag}>
            {item.tag}
            <button type="button" title="删除标签" disabled={saving} onClick={() => setPendingDeleteTag(item.tag)}>
              <X size={12} />
            </button>
          </span>
        ))}
        <button
          className="sidebar-asset-tag-add"
          type="button"
          title="添加自标"
          onClick={(event) => togglePicker(event.currentTarget)}
        >
          <Plus size={13} />
        </button>
      </div>
      {adding && createPortal(
        <div
          className="sidebar-asset-tag-picker"
          ref={pickerRef}
          role="dialog"
          aria-label="选择或创建自标"
          style={pickerPosition}
        >
          <form
            className="sidebar-asset-tag-form"
            onSubmit={(event) => {
              event.preventDefault();
              void onAdd(draft).then((saved) => {
                if (saved) setAdding(false);
              });
            }}
          >
            <label>
              <Search size={14} aria-hidden="true" />
              <input
                autoFocus
                value={draft}
                placeholder="搜索或输入新自标"
                disabled={saving}
                onChange={(event) => onDraftChange(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Escape') {
                    setAdding(false);
                    onDraftChange('');
                  }
                }}
              />
            </label>
            <button type="submit" title="创建并添加自标" disabled={saving || draft.trim() === '' || assigned.has(draft.trim())}>
              <Check size={14} />
            </button>
          </form>
          <div className="sidebar-asset-tag-options">
            {optionsLoading && <span>读取自标中</span>}
            {optionsError && <span className="sidebar-error">{optionsError}</span>}
            {!optionsLoading && !optionsError && matchingOptions.length === 0 && <span>没有匹配的已有自标，可直接创建</span>}
            {matchingOptions.map((item) => (
              <button
                className={assigned.has(item.name) ? 'selected' : ''}
                disabled={saving || assigned.has(item.name)}
                key={item.id}
                type="button"
                onClick={() => void selectTag(item.name)}
              >
                <span>{item.name}</span>
                <small>{assigned.has(item.name) ? '已添加' : `${item.assetCount} 项`}</small>
              </button>
            ))}
          </div>
        </div>,
        document.body,
      )}
      {pendingDeleteTag && (
        <ViewerTagDeleteConfirmDialog
          saving={saving}
          tag={pendingDeleteTag}
          onCancel={() => setPendingDeleteTag('')}
          onConfirm={() => void onRemove(pendingDeleteTag).then((removed) => {
            if (removed) setPendingDeleteTag('');
          })}
        />
      )}
    </div>
  );
}

function ViewerTagDeleteConfirmDialog({
  onCancel,
  onConfirm,
  saving,
  tag,
}: {
  onCancel: () => void;
  onConfirm: () => void;
  saving: boolean;
  tag: string;
}) {
  return createPortal(
    <div
      className="modal-backdrop"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !saving) onCancel();
      }}
    >
      <div aria-label="确认删除标签" aria-modal="true" className="asset-delete-dialog viewer-tag-delete-dialog" role="dialog">
        <div className="modal-title">
          <span>确认删除标签</span>
          <button disabled={saving} title="关闭" type="button" onClick={onCancel}><X size={17} /></button>
        </div>
        <div className="asset-delete-content">
          <div className="asset-delete-summary">
            <strong>删除“{tag}”标签？</strong>
            <span>只会从当前媒体移除该标签，其他标签不会受到影响。</span>
          </div>
        </div>
        <div className="modal-actions">
          <span />
          <button className="text-button" disabled={saving} type="button" onClick={onCancel}>取消</button>
          <button className="command-button danger" disabled={saving} type="button" onClick={onConfirm}>
            <Check size={16} />
            {saving ? '删除中' : '确认删除'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

function videoPlaybackInfoLabel(info: VideoPlaybackInfo | null) {
  if (!info) return '加载中';
  return info.playbackStateLabel;
}

function audioPlaybackInfoLabel(info: AudioPlaybackInfo | null) {
  if (!info || !info.sourceReady) return info?.proxyProgress ? `兼容转换 ${Math.round(info.proxyProgress * 100)}%` : '准备中';
  if (info.playing) return '播放中';
  if (info.readyState < HTMLMediaElement.HAVE_CURRENT_DATA) return '加载中';
  return '已暂停';
}

function transferBitrateLabel(bytesPerSecond: number | null | undefined) {
  if (!bytesPerSecond || bytesPerSecond <= 0) return '当前无传输';
  return formatBitrate(bytesPerSecond * 8);
}

function videoProxyRuntimeLabel(runtime: VideoSegmentStatus) {
  if (runtime.message) return runtime.message;
  if (runtime.status === 'cached' || runtime.cached) return '已缓存';
  if (runtime.status === 'error') return '转码失败';
  if (runtime.status === 'queued' || runtime.queued) return '等待转码槽位';
  if (runtime.transcoding) return `实时转码 ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`;
  return '准备转码';
}

function ViewerPlaybackDetails({
  asset,
  audioInfo,
  videoInfo,
  runtime,
}: {
  asset: Asset;
  audioInfo: AudioPlaybackInfo | null;
  videoInfo: VideoPlaybackInfo | null;
  runtime: VideoSegmentStatus | null;
}) {
  const rows = playbackDetailRows(asset, videoInfo, audioInfo, runtime);
  const runtimeProgress = runtime ? Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100) : 0;
  return (
    <section className="viewer-playback-details" aria-label="播放信息">
      <div className="sidebar-control-title">播放信息</div>
      {videoInfo?.notPlayingReason && (
        <div className="viewer-playback-details-alert">
          <strong>{videoInfo.notPlayingReason}</strong>
          <span>{videoNotPlayingDetail(videoInfo, runtime)}</span>
        </div>
      )}
      <dl className="sidebar-asset-info-details viewer-playback-details-list">
        {rows.map((row) => (
          <div key={row.label}>
            <dt>{row.label}</dt>
            <dd>{row.value}</dd>
          </div>
        ))}
      </dl>
      {asset.mediaType === 'video' && !asset.browserPlayable && runtime && runtimeProgress > 0 && runtimeProgress < 100 && (
        <div className="viewer-playback-details-progress" aria-label={`转码进度 ${runtimeProgress}%`}>
          <span style={{ width: `${runtimeProgress}%` }} />
        </div>
      )}
    </section>
  );
}

function playbackDetailRows(
  asset: Asset,
  videoInfo: VideoPlaybackInfo | null,
  audioInfo: AudioPlaybackInfo | null,
  runtime: VideoSegmentStatus | null,
) {
  if (asset.mediaType === 'audio') {
    return [
      { label: '状态', value: audioPlaybackInfoLabel(audioInfo) },
      { label: '位置', value: `${formatDuration(audioInfo?.currentTime ?? 0)} / ${formatDuration(audioInfo?.duration || asset.duration || 0)}` },
      { label: '前向缓冲', value: forwardBufferLabel(audioInfo?.currentTime, audioInfo?.bufferedEnd) },
      { label: '播放速度', value: `${formatDecimal(audioInfo?.playbackRate || 1)}x` },
      { label: '播放来源', value: asset.browserPlayable ? '原文件按需读取' : 'FLAC 无损兼容缓存' },
    ];
  }

  const rows = [
    { label: '状态', value: videoPlaybackInfoLabel(videoInfo) },
    { label: '位置', value: `${formatDuration(videoInfo?.currentTime ?? 0)} / ${formatDuration(videoInfo?.duration || asset.duration || 0)}` },
    { label: '前向缓冲', value: forwardBufferLabel(videoInfo?.currentTime, videoInfo?.bufferedEnd) },
    { label: '播放规格', value: `${formatDimensions(videoInfo?.decodedWidth, videoInfo?.decodedHeight)} · ${formatDecimal(videoInfo?.playbackRate || 1)}x` },
    ...playbackTransferRows(videoInfo),
    { label: '本次加载数据', value: videoInfo ? formatBytes(videoInfo.browserCachedBytes) : '等待统计' },
    { label: '播放来源', value: asset.browserPlayable ? '原文件按需读取' : 'HLS 实时分片转码' },
  ];
  if (videoInfo?.totalFrames) {
    const droppedPercent = (videoInfo.droppedFrames / videoInfo.totalFrames) * 100;
    rows.push({ label: '丢帧', value: `${videoInfo.droppedFrames} / ${videoInfo.totalFrames} (${formatDecimal(droppedPercent)}%)` });
  }
  if (!asset.browserPlayable) {
    rows.push({ label: '服务端任务', value: videoSegmentServerTaskLabel(runtime) });
    rows.push({ label: '当前切片', value: currentVideoSegmentLabel(runtime) });
    rows.push({ label: '切片总数', value: runtime?.segmentCount ? `${runtime.segmentCount} 片` : '等待统计' });
    rows.push({ label: '服务缓存', value: serverMediaCacheLabel(asset, runtime) });
  }
  return rows;
}

function playbackTransferRows(info: VideoPlaybackInfo | null) {
  if (info?.separateAVTransfers) {
    return [
      { label: '视频传输码率', value: transferBitrateLabel(info.videoNetworkBytesPerSecond) },
      { label: '音频传输码率', value: transferBitrateLabel(info.audioNetworkBytesPerSecond) },
    ];
  }
  return [{ label: '传输码率', value: info ? transferBitrateLabel(info.networkBytesPerSecond) : '等待统计' }];
}

function videoSegmentServerTaskLabel(runtime: VideoSegmentStatus | null) {
  if (!runtime) return '等待服务端状态';
  if (runtime.segmentIndex < 0) return runtime.message || '当前无切片任务';
  const segment = `第 ${runtime.segmentIndex + 1} 片`;
  if (runtime.transcoding) return `${segment}正在转码 · ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`;
  if (runtime.queued) return `${segment}等待转码槽位`;
  if (runtime.status === 'error') return `${segment}转码失败`;
  if (runtime.cached || runtime.status === 'cached') return `${segment}已完成`;
  return `${segment}等待处理`;
}

function currentVideoSegmentLabel(runtime: VideoSegmentStatus | null) {
  if (!runtime || runtime.segmentIndex < 0) return '当前无活动切片';
  const start = runtime.segmentIndex * Math.max(0, runtime.segmentSeconds || runtime.duration || 0);
  const duration = Math.max(0, runtime.duration || runtime.segmentSeconds || 0);
  return duration > 0
    ? `第 ${runtime.segmentIndex + 1} 片 · ${formatDuration(start)}–${formatDuration(start + duration)}`
    : `第 ${runtime.segmentIndex + 1} 片`;
}

function forwardBufferLabel(currentTime = 0, bufferedEnd = 0) {
  const seconds = Math.max(0, bufferedEnd - currentTime);
  return `${formatDecimal(seconds)} 秒 · 至 ${formatDuration(bufferedEnd)}`;
}

function serverMediaCacheLabel(asset: Asset, runtime: VideoSegmentStatus | null) {
  if (asset.browserPlayable) return '无需转码缓存';
  if (!runtime) return '等待缓存统计';
  const total = runtime.estimatedTotalBytes > 0 ? `约 ${formatBytes(runtime.estimatedTotalBytes)}` : '预计总大小未知';
  return `${formatBytes(runtime.cachedBytes)} / ${total} · 已缓存 ${runtime.cachedSegments} 片`;
}

function videoNotPlayingDetail(info: VideoPlaybackInfo, runtime: VideoSegmentStatus | null) {
  const details = [info.notPlayingDetail];
  details.push(`媒体数据：${videoReadyStateLabel(info.readyState)}`);
  details.push(`网络状态：${videoNetworkStateLabel(info.networkState)}`);
  details.push(`前向缓存：${Math.max(0, info.bufferedEnd - info.currentTime).toFixed(1)} 秒`);
  if (runtime && runtime.segmentIndex >= 0) {
    const progress = Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100);
    details.push(`切片 ${runtime.segmentIndex + 1}：${videoProxyRuntimeLabel(runtime)}，${progress}%`);
  }
  if (info.playError) details.push(`错误：${info.playError}`);
  return details.filter(Boolean).join('；');
}

function videoReadyStateLabel(value: number) {
  switch (value) {
    case 0: return '未取得媒体信息';
    case 1: return '只有元数据';
    case 2: return '已有当前帧';
    case 3: return '可短暂连续播放';
    case 4: return '数据充足';
    default: return `未知状态 ${value}`;
  }
}

function videoNetworkStateLabel(value: number) {
  switch (value) {
    case 0: return '尚未初始化';
    case 1: return '当前无网络活动';
    case 2: return '正在加载数据';
    case 3: return '未找到可用媒体源';
    default: return `未知状态 ${value}`;
  }
}

function formatDimensions(width: number | null | undefined, height: number | null | undefined) {
  return width && height ? `${Math.round(width)} x ${Math.round(height)} px` : '未知';
}

function formatBitrate(value: number | null | undefined) {
  if (!value || value <= 0) return '未知';
  if (value >= 1_000_000) return `${formatDecimal(value / 1_000_000)} Mbps`;
  return `${formatDecimal(value / 1_000)} Kbps`;
}

function formatDecimal(value: number) {
  return value.toFixed(2).replace(/\.00$/, '').replace(/(\.\d)0$/, '$1');
}

function assetInfoRows(asset: Asset) {
  const rows = [
    { label: '类型', value: asset.mediaType === 'image' ? '照片' : asset.mediaType === 'audio' ? '音频' : '视频' },
    { label: '文件大小', value: formatBytes(asset.size) },
    { label: '媒体时间', value: formatDateTime(asset.timelineAt) },
  ];
  if (asset.width && asset.height) rows.push({ label: '文件分辨率', value: `${asset.width} x ${asset.height}` });
  if ((asset.mediaType === 'video' || asset.mediaType === 'audio') && asset.duration !== null) rows.push({ label: '时长', value: formatDuration(asset.duration) });
  if (asset.mediaType !== 'audio') rows.push({ label: '旋转', value: `${asset.rotation || 0}°` });
  if (asset.mediaType === 'video' || asset.mediaType === 'audio') {
    if (asset.container) rows.push({ label: '封装格式', value: asset.container });
    if (asset.mediaType === 'video' && asset.videoCodec) rows.push({ label: '视频编码', value: asset.videoCodec });
    if (asset.audioCodec) rows.push({ label: '音频编码', value: asset.audioCodec });
    if (asset.mediaType === 'video' && asset.fps && asset.fps > 0) rows.push({ label: '帧率', value: `${formatDecimal(asset.fps)} FPS` });
    if (asset.overallBitrate && asset.overallBitrate > 0) rows.push({ label: '文件总码率', value: formatBitrate(asset.overallBitrate) });
    if (asset.mediaType === 'video' && asset.videoBitrate && asset.videoBitrate > 0) rows.push({ label: '视频码率', value: formatBitrate(asset.videoBitrate) });
    if (asset.audioBitrate && asset.audioBitrate > 0) rows.push({ label: '音频码率', value: formatBitrate(asset.audioBitrate) });
  }
  return rows;
}

function assetFolderLabel(asset: Asset) {
  return asset.parentRelPath || '全部存储';
}

function NFOValue({
  groupTitle,
  item,
  onSearch,
}: {
  groupTitle?: string;
  item: NFOField;
  onSearch: (field: NFOFilterField | 'nfo', value: string) => void;
}) {
  const content = (
    <>
      <span>{item.label}</span>
      {item.value}
    </>
  );
  const field = nfoSearchFieldForItem(item, groupTitle);
  const value = nfoSearchValue(field, item.value);
  return (
    <button className="sidebar-nfo-item searchable" type="button" title="搜索此项" onClick={() => onSearch(field, value)}>
      {content}
    </button>
  );
}

function searchParamForNFOField(field: NFOFilterField | 'nfo') {
  switch (field) {
    case 'actor':
      return 'nfoActor';
    case 'id':
      return 'nfoId';
    case 'tag':
      return 'nfoTag';
    case 'title':
      return 'nfoTitle';
    case 'year':
      return 'nfoYear';
    default:
      return 'nfo';
  }
}

function nfoSearchFieldForItem(item: NFOField, groupTitle = ''): NFOFilterField | 'nfo' {
  const key = item.key.trim().toLowerCase();
  const label = item.label.trim().toLowerCase();
  const group = groupTitle.trim().toLowerCase();
  if (group === '演员' || key === 'actor' || label === '演员') return 'actor';
  if (group === 'id' || key === 'uniqueid' || key.startsWith('uniqueid:') || label === 'id' || label === 'imdb' || label === 'tmdb') return 'id';
  if (group === '标记' || group === '类型' || key === 'tag' || key === 'genre' || label === '标签' || label === '类型') return 'tag';
  if (key === 'title' || key === 'originaltitle' || key === 'sorttitle' || label === '标题' || label === '原名' || label === '排序') return 'title';
  if (key === 'year' || label === '年份') return 'year';
  return 'nfo';
}

function nfoSearchValue(field: NFOFilterField | 'nfo', value: string) {
  const clean = value.trim();
  if (field === 'actor') {
    return clean.split(/\s+\/\s+/, 1)[0] || clean;
  }
  return clean;
}

function updateNeighborRotation(neighbors: Neighbors, assetId: number, rotation: number): Neighbors {
  const update = (asset: Asset) => (asset.id === assetId ? { ...asset, rotation } : asset);
  return {
    current: update(neighbors.current),
    previous: neighbors.previous.map(update),
    next: neighbors.next.map(update),
  };
}

function updateNeighborRating(neighbors: Neighbors, assetId: number, rating: AssetRating): Neighbors {
  const update = (asset: Asset) => (asset.id === assetId ? { ...asset, rating } : asset);
  return {
    current: update(neighbors.current),
    previous: neighbors.previous.map(update),
    next: neighbors.next.map(update),
  };
}

function viewerMediaWindow(neighbors: Neighbors, selected: Asset | undefined) {
  const sequence = [
    ...neighbors.previous.slice().reverse(),
    neighbors.current,
    ...neighbors.next,
  ];
  const selectedIndex = selected ? sequence.findIndex((asset) => asset.id === selected.id) : -1;
  if (selectedIndex < 0) return uniqueMediaAssets([selected]);
  return uniqueMediaAssets(sequence.slice(
    Math.max(0, selectedIndex - viewerRetainRadius),
    selectedIndex + viewerRetainRadius + 1,
  ));
}

function viewerIndicatorAssets(neighbors: Neighbors | null, current: Asset | undefined): Array<Asset | undefined> {
  if (!current) return Array.from({ length: viewerIndicatorCount }, () => undefined);
  if (!neighbors) {
    return Array.from({ length: viewerIndicatorCount }, (_, index) => index === viewerIndicatorCenter ? current : undefined);
  }
  const sequence = [
    ...neighbors.previous.slice().reverse(),
    neighbors.current,
    ...neighbors.next,
  ];
  const currentIndex = sequence.findIndex((asset) => asset.id === current.id);
  if (currentIndex < 0) {
    return Array.from({ length: viewerIndicatorCount }, (_, index) => index === viewerIndicatorCenter ? current : undefined);
  }
  return Array.from(
    { length: viewerIndicatorCount },
    (_, index) => sequence[currentIndex + index - viewerIndicatorCenter],
  );
}

function mergeMediaWindow(existing: Asset[], desired: Array<Asset | undefined>) {
  const next = uniqueMediaAssets(desired);
  if (
    existing.length === next.length &&
    existing.every((asset, index) => asset === next[index])
  ) {
    return existing;
  }
  return next;
}

function uniqueMediaAssets(assets: Array<Asset | undefined>) {
  const seen = new Set<string>();
  return assets.filter((asset): asset is Asset => {
    if (!asset) return false;
    const key = mediaReadyKey(asset.id, asset.cacheKey);
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function prunePreparedMediaStatus(existing: Record<string, PreparedMediaStatus>, retainedKeys: Set<string>) {
  const next = Object.fromEntries(Object.entries(existing).filter(([key]) => retainedKeys.has(key))) as Record<string, PreparedMediaStatus>;
  const existingKeys = Object.keys(existing);
  if (existingKeys.length === Object.keys(next).length && existingKeys.every((key) => next[key] === existing[key])) return existing;
  return next;
}

function mediaReadyKey(assetId: number, cacheKey: string) {
  return `${assetId}:${cacheKey}`;
}

function mediaNavigationReady(asset: Asset, status: PreparedMediaStatus | undefined) {
  return status === 'ready' || (asset.mediaType === 'video' && status === 'poster');
}
