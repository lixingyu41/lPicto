import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
} from 'react';
import { useLocation, useNavigate, useParams, useSearchParams, type Location } from 'react-router-dom';
import { Check, FolderOpen, GripHorizontal, LogOut, Plus, X } from 'lucide-react';
import { api } from '../api/client';
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
  VideoSegmentStatus,
} from '../types/api';
import AssetDeleteDialog from '../components/AssetDeleteDialog';
import AssetRecordDeleteDialog from '../components/AssetRecordDeleteDialog';
import RatingStars, { normalizeAssetRating } from '../components/RatingStars';
import { formatBytes, formatDateTime, formatDuration } from '../utils/format';
import ImageViewer from '../viewer/ImageViewer';
import VideoViewer, { type VideoPlaybackInfo } from '../viewer/VideoViewer';
import AudioViewer from '../viewer/AudioViewer';
import { viewerAudioOutputBridge } from '../viewer/audioOutputBridge';
import type { ViewerMediaLayerMode } from '../viewer/mediaLayer';
import { useKeyboard } from '../hooks/useKeyboard';
import { useRestoreSidebarState, type SidebarReturnState } from '../components/SidebarContext';
import { nextRotation } from '../utils/rotation';
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

const wheelStepThreshold = 60;
const viewerReturnPageSize = waterfallPageSize;
const mediaPrepareTimeoutMs = 15000;
const viewerRetainRadius = 4;
const viewerPreloadRadius = 3;
const viewerIndicatorCount = viewerRetainRadius * 2 + 1;
const viewerIndicatorCenter = viewerRetainRadius;
type DanmakuPrefKey = 'danmakuDensity' | 'danmakuFontScale' | 'danmakuOpacity' | 'danmakuSpeed';
type ViewerLoadIndicatorStatus = 'idle' | 'loading' | 'ready';
type PreparedMediaStatus = 'ready' | 'failed';

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
  const [sidecarError, setSidecarError] = useState<string | null>(null);
  const [assetTags, setAssetTags] = useState<AssetTag[]>([]);
	const [assetAI, setAssetAI] = useState<AssetAIResult | null>(null);
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
  const [videoProxyRuntime, setVideoProxyRuntime] = useState<VideoSegmentStatus | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletePlan, setDeletePlan] = useState<AssetDeletePlan | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteSubmitting, setDeleteSubmitting] = useState(false);
  const [recordDeleteDialogOpen, setRecordDeleteDialogOpen] = useState(false);
  const [recordDeleteError, setRecordDeleteError] = useState<string | null>(null);
  const [recordDeleteSubmitting, setRecordDeleteSubmitting] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [assetInfoCollapsed, setAssetInfoCollapsed] = useState(false);
  const [viewerPanelWidth, setViewerPanelWidth] = useState(() => loadViewerPanelWidth());
  const [viewerAvailableWidth, setViewerAvailableWidth] = useState(() => window.innerWidth);
  const wheelBase = useRef<WheelBase | null>(null);
  const wheelDelta = useRef(0);
  const wheelGestureLocked = useRef(false);
  const wheelGestureTimer = useRef<number | null>(null);
  const viewerRef = useRef<HTMLElement | null>(null);
  const viewerBodyRef = useRef<HTMLDivElement | null>(null);
  const viewerPanelResizeCleanupRef = useRef<(() => void) | null>(null);
  const [mediaDetailsOpen, setMediaDetailsOpen] = useState(false);
  const mediaDetailsPosition = useMemo(() => ({ x: 16, y: 16 }), []);
  const viewerReturnStateRef = useRef(decodeReturnState<Partial<SidebarReturnState>>(searchParams.get('returnState'), {}));
  const restoreSidebarState = useRestoreSidebarState();
  const viewerLocationState = location.state as ViewerLocationState | null;
  const backgroundLocation = viewerLocationState?.backgroundLocation;
  const assetId = Number(params.assetId || assetIdFromPath(location.pathname) || 0);
  const initialAsset = viewerLocationState?.initialAsset?.id === assetId ? viewerLocationState.initialAsset : undefined;
  const [displayedAsset, setDisplayedAsset] = useState<Asset | undefined>();
  const [mediaWindow, setMediaWindow] = useState<Asset[]>([]);
  const [preparedMediaStatus, setPreparedMediaStatus] = useState<Record<string, PreparedMediaStatus>>({});
  const [priorityReadyKey, setPriorityReadyKey] = useState('');
  const [mediaLoadFailure, setMediaLoadFailure] = useState<{ key: string; message: string } | null>(null);
  const currentAssetIdRef = useRef<number | null>(null);
  const currentCacheKeyRef = useRef('');
  const currentAssetRef = useRef<Asset | undefined>(undefined);
  const displayedMediaKeyRef = useRef('');
  const failedMediaKeyRef = useRef('');
  const lastNavigationDirection = useRef<-1 | 0 | 1>(0);

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
        const result = await api.neighbors(assetId, query, controller.signal);
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
  const current = activeNeighbors?.current ?? initialAsset;
  const currentAssetId = current?.id ?? null;
  currentAssetIdRef.current = currentAssetId;
  currentCacheKeyRef.current = current?.cacheKey ?? '';
  currentAssetRef.current = current;
  const currentMediaKey = current ? mediaReadyKey(current.id, current.cacheKey) : '';
  const displayedMediaKey = displayedAsset ? mediaReadyKey(displayedAsset.id, displayedAsset.cacheKey) : '';
  displayedMediaKeyRef.current = displayedMediaKey;
  const currentIsDisplayed = Boolean(currentMediaKey) && currentMediaKey === displayedMediaKey;
  const currentMediaFailed = Boolean(currentMediaKey) && mediaLoadFailure?.key === currentMediaKey;
  const currentPreparedStatus = currentMediaKey ? preparedMediaStatus[currentMediaKey] : undefined;
  const currentPriorityReady = Boolean(currentMediaKey) && (
    priorityReadyKey === currentMediaKey
    || (currentPreparedStatus === 'ready' && (current?.mediaType !== 'video' || current.browserPlayable))
  );
  const indicatorAssets = useMemo(() => viewerIndicatorAssets(neighbors, current), [current, neighbors]);
  const preloadRangeKeys = useMemo(() => {
    const start = viewerIndicatorCenter - viewerPreloadRadius;
    const end = viewerIndicatorCenter + viewerPreloadRadius;
    return new Set(
      indicatorAssets
        .slice(start, end + 1)
        .filter((asset): asset is Asset => Boolean(asset))
        .map((asset) => mediaReadyKey(asset.id, asset.cacheKey)),
    );
  }, [indicatorAssets]);
  const mediaWindowKeys = useMemo(
    () => new Set(mediaWindow.map((asset) => mediaReadyKey(asset.id, asset.cacheKey))),
    [mediaWindow],
  );
  const backgroundVideoPreloadKey = useMemo(() => {
    if (!currentPriorityReady) return '';
    const order = lastNavigationDirection.current > 0
      ? [5, 6, 7, 3, 2, 1]
      : lastNavigationDirection.current < 0
        ? [3, 2, 1, 5, 6, 7]
        : [5, 3, 6, 2, 7, 1];
    for (const index of order) {
      const asset = indicatorAssets[index];
      if (!asset || (asset.mediaType !== 'video' && asset.mediaType !== 'audio')) continue;
      const key = mediaReadyKey(asset.id, asset.cacheKey);
      if (!mediaWindowKeys.has(key) || key === currentMediaKey) continue;
      if (preparedMediaStatus[key] === undefined) return key;
    }
    return '';
  }, [currentMediaKey, currentPriorityReady, indicatorAssets, mediaWindowKeys, preparedMediaStatus]);
  const viewerPreloadIndicators = useMemo(() => {
    return indicatorAssets.map((asset): ViewerLoadIndicatorStatus => {
      if (!asset) return 'idle';
      const key = mediaReadyKey(asset.id, asset.cacheKey);
      const prepared = preparedMediaStatus[key];
      if (prepared === 'ready') return 'ready';
      if (prepared === 'failed' || !mediaWindowKeys.has(key)) return 'idle';
      if (currentPriorityReady && asset.mediaType === 'image' && preloadRangeKeys.has(key)) return 'loading';
      if (key === currentMediaKey || key === backgroundVideoPreloadKey) return 'loading';
      return 'idle';
    });
  }, [backgroundVideoPreloadKey, currentMediaKey, currentPriorityReady, indicatorAssets, mediaWindowKeys, preloadRangeKeys, preparedMediaStatus]);

  useLayoutEffect(() => {
    if (!current) return;
    const key = mediaReadyKey(current.id, current.cacheKey);
    setDisplayedAsset((displayed) => {
      if (!displayed) return current;
      if (mediaReadyKey(displayed.id, displayed.cacheKey) === key) return displayed === current ? displayed : current;
      return preparedMediaStatus[key] === 'ready' ? current : displayed;
    });
  }, [current, preparedMediaStatus]);

  useLayoutEffect(() => {
    if (!current) return;
    setMediaWindow((existing) => mergeMediaWindow(existing, [current, ...existing]));
  }, [current]);

  useLayoutEffect(() => {
    if (!activeNeighbors) return;
    const desired = viewerMediaWindow(activeNeighbors);
    const desiredKeys = new Set(desired.map((asset) => mediaReadyKey(asset.id, asset.cacheKey)));
    if (
      displayedAsset &&
      displayedMediaKey !== currentMediaKey &&
      currentPreparedStatus !== 'ready' &&
      !desiredKeys.has(displayedMediaKey)
    ) {
      desired.push(displayedAsset);
    }
    setMediaWindow((existing) => mergeMediaWindow(existing, desired));
  }, [activeNeighbors, currentMediaKey, currentPreparedStatus, displayedAsset, displayedMediaKey]);

  useEffect(() => {
    const retainedKeys = new Set(mediaWindow.map((asset) => mediaReadyKey(asset.id, asset.cacheKey)));
    setPreparedMediaStatus((existing) => prunePreparedMediaStatus(existing, retainedKeys));
  }, [mediaWindow]);

  useLayoutEffect(() => {
    failedMediaKeyRef.current = '';
    setMediaLoadFailure(null);
  }, [currentMediaKey]);

  useEffect(() => {
    if (!currentMediaKey || currentIsDisplayed || currentMediaFailed) return undefined;
    const timer = window.setTimeout(() => {
      if (currentMediaKey !== mediaReadyKey(currentAssetIdRef.current ?? 0, currentCacheKeyRef.current)) return;
      failedMediaKeyRef.current = currentMediaKey;
      setMediaLoadFailure({ key: currentMediaKey, message: '媒体加载超过 15 秒，已保留上一画面' });
    }, mediaPrepareTimeoutMs);
    return () => window.clearTimeout(timer);
  }, [currentIsDisplayed, currentMediaFailed, currentMediaKey]);

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
				setAssetAI(result); setAssetAIError(null);
				if (result.status === 'pending' || result.status === 'processing') timer = window.setTimeout(() => void loadAI(asset), 5000);
			} catch (err) {
				if (!live) return;
				setAssetAI(null); setAssetAIError(err instanceof Error ? err.message : '读取 AI 结果失败');
			}
		}
		setAssetAI(null); setAssetAIError(null); setAITagError(null);
		if (current && current.mediaType !== 'audio') void loadAI(current);
		return () => { live = false; window.clearTimeout(timer); };
	}, [current?.id]);

  useEffect(() => {
    if (activeNeighborAssetId === null) return;
    wheelBase.current = null;
    wheelDelta.current = 0;
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
        setSidecars(result);
        setSidecarError(null);
        const defaultID = result.defaultSubtitleId ?? result.subtitles[0]?.id ?? '';
        setSelectedSubtitleId(defaultID);
        setSubtitlesEnabled(Boolean(defaultID) && loadViewerPrefs().subtitlesEnabled);
      } catch (err) {
        if (!live) return;
        setSidecars(null);
        setSidecarError(err instanceof Error ? err.message : '读取附加信息失败');
        setSelectedSubtitleId('');
        setSubtitlesEnabled(false);
      }
    }
    if (current) {
      void loadSidecars(current);
    } else {
      setSidecars(null);
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
        setAssetTags(result.items);
        setTagError(null);
      } catch (err) {
        if (!live) return;
        setAssetTags([]);
        setTagError(err instanceof Error ? err.message : '读取标签失败');
      }
    }
    if (current) {
      void loadTags(current);
    } else {
      setAssetTags([]);
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

  const handleMediaReady = useCallback((sourceAssetId: number, sourceCacheKey: string) => {
    const key = mediaReadyKey(sourceAssetId, sourceCacheKey);
    setPreparedMediaStatus((existing) => existing[key] === 'ready' ? existing : { ...existing, [key]: 'ready' });
    if (sourceAssetId !== currentAssetIdRef.current || sourceCacheKey !== currentCacheKeyRef.current) return;
    if (failedMediaKeyRef.current === key) return;
    const readyAsset = currentAssetRef.current;
    if (!readyAsset || readyAsset.id !== sourceAssetId || readyAsset.cacheKey !== sourceCacheKey) return;
    if (readyAsset.mediaType !== 'video' || readyAsset.browserPlayable) setPriorityReadyKey(key);
    setDisplayedAsset(readyAsset);
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
      message: displayedMediaKeyRef.current && displayedMediaKeyRef.current !== key
        ? `${message}，已保留上一画面`
        : message,
    });
  }, []);

  const goAsset = useCallback(
    (asset: Asset | undefined, direction: -1 | 0 | 1 = 0) => {
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
      if (direction !== 0) lastNavigationDirection.current = direction;
      navigate(
        { pathname: `/viewer/${asset.id}`, search: searchParams.toString() },
        overlay && backgroundLocation
          ? { replace: true, state: { backgroundLocation, initialAsset: asset } }
          : { state: { initialAsset: asset } },
      );
    },
    [backgroundLocation, navigate, overlay, searchParams],
  );

  const playNextAsset = useCallback(() => {
    const next = activeNeighbors?.next[0];
    if (next) goAsset(next, 1);
  }, [activeNeighbors, goAsset]);

  const goWheelStep = useCallback(
    (direction: 1 | -1) => {
      const base =
        wheelBase.current ??
        (activeNeighbors
          ? { current: activeNeighbors.current, next: activeNeighbors.next, offset: 0, previous: activeNeighbors.previous }
          : null);
      if (!base) return;

      const nextOffset = base.offset + direction;
      const target = wheelTargetAtOffset(base, nextOffset);
      if (!target) return;

      base.offset = nextOffset;
      wheelBase.current = base;
      goAsset(target, direction);
    },
    [activeNeighbors, goAsset],
  );

  useEffect(() => {
    const handleWheel = (event: WheelEvent) => {
      if (!wheelBelongsToViewer(event, viewerRef.current)) return;
      if (wheelGestureTimer.current !== null) window.clearTimeout(wheelGestureTimer.current);
      wheelGestureTimer.current = window.setTimeout(() => {
        wheelGestureLocked.current = false;
        wheelGestureTimer.current = null;
        wheelDelta.current = 0;
      }, 180);
      if (isViewerWheelControl(event)) {
        if (event.cancelable) event.preventDefault();
        wheelDelta.current = 0;
        return;
      }
      if (isImageZoomWheel(event)) {
        wheelDelta.current = 0;
        return;
      }
      if (event.cancelable) event.preventDefault();
      if (wheelGestureLocked.current) return;
      wheelDelta.current += event.deltaY;
      if (Math.abs(wheelDelta.current) < wheelStepThreshold) return;
      const direction = wheelDelta.current > 0 ? 1 : -1;
      wheelDelta.current = 0;
      wheelGestureLocked.current = true;
      goWheelStep(direction);
    };
    window.addEventListener('wheel', handleWheel, { capture: true, passive: false });
    return () => {
      window.removeEventListener('wheel', handleWheel, true);
      if (wheelGestureTimer.current !== null) {
        window.clearTimeout(wheelGestureTimer.current);
        wheelGestureTimer.current = null;
      }
      wheelGestureLocked.current = false;
    };
  }, [goWheelStep]);

  const leave = useCallback(() => {
    if (overlay) {
      navigate(-1);
      return;
    }
    void (async () => {
      const context = searchParams.get('context');
      const fallback = context === 'folder' ? '/folders' : context === 'album' ? '/albums' : '/library';
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

  const addCurrentAssetTag = useCallback(async () => {
    if (!current || tagSaving) return;
    const tag = tagDraft.trim();
    if (!tag) return;
    setTagSaving(true);
    setTagError(null);
    try {
      const result = await api.addAssetTag(current.id, tag);
      setAssetTags(result.items);
      setTagDraft('');
    } catch (err) {
      setTagError(err instanceof Error ? err.message : '添加标签失败');
    } finally {
      setTagSaving(false);
    }
  }, [current, tagDraft, tagSaving]);

  const removeCurrentAssetTag = useCallback(async (tag: string) => {
    if (!current || tagSaving) return;
    setTagSaving(true);
    setTagError(null);
    try {
      const result = await api.removeAssetTag(current.id, tag);
      setAssetTags(result.items);
    } catch (err) {
      setTagError(err instanceof Error ? err.message : '删除标签失败');
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
    if (target) {
      void target.requestFullscreen();
    }
  }, []);

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
    }
    document.addEventListener('fullscreenchange', onFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', onFullscreenChange);
  }, []);

  useKeyboard(
    useCallback(
      (event: KeyboardEvent) => {
        if (event.key === 'Escape' && mediaDetailsOpen) {
          setMediaDetailsOpen(false);
          return;
        }
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
      [activeNeighbors, closeDeleteDialog, closeRecordDeleteDialog, deleteDialogOpen, goAsset, leave, mediaDetailsOpen, recordDeleteDialogOpen, toggleFullscreen],
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
      sidecarError={sidecarError}
      sidecars={sidecars}
      tags={assetTags}
      preloadIndicators={viewerPreloadIndicators}
      aiResult={assetAI}
      aiError={assetAIError}
      aiTagError={aiTagError}
      aiTagSaving={aiTagSaving}
      tagDraft={tagDraft}
      tagError={tagError}
      tagSaving={tagSaving}
      assetInfoCollapsed={assetInfoCollapsed}
      onLeave={leave}
      onToggleAssetInfo={() => setAssetInfoCollapsed((collapsed) => !collapsed)}
      onOpenFolder={openAssetFolder}
      onNFOSearch={searchByNFOValue}
      onTagDraftChange={setTagDraft}
      onAddTag={() => void addCurrentAssetTag()}
      onRemoveTag={(tag) => void removeCurrentAssetTag(tag)}
      onSaveAITag={saveCurrentAITag}
      onRemoveAITag={removeCurrentAITag}
      onReanalyzeAI={() => current && void api.reanalyzeAssetAI(current.id).then(() => setAssetAI((value) => value ? { ...value, status: 'pending', error: undefined } : value))}
      onRatingChange={(rating) => void rateCurrentAsset(rating)}
    />
  );

  const renderMediaLayer = (asset: Asset) => {
    const key = mediaReadyKey(asset.id, asset.cacheKey);
    const requestedCurrent = key === currentMediaKey;
    const displayed = key === displayedMediaKey;
    const layerMode: ViewerMediaLayerMode = displayed ? (requestedCurrent ? 'active' : 'hold') : 'prepare';
    const preloadEnabled = requestedCurrent
      || preparedMediaStatus[key] === 'ready'
      || (currentPriorityReady && asset.mediaType === 'image' && preloadRangeKeys.has(key))
      || key === backgroundVideoPreloadKey;
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
          mediaDetailsOpen={mediaDetailsOpen}
          onMediaError={handleMediaError}
          onMediaReady={handleMediaReady}
          playbackMode={viewerPrefs.playbackMode}
          slideshowSeconds={viewerPrefs.imageSlideshowSeconds}
          onPlaybackEnded={playNextAsset}
          onPlaybackModeChange={updatePlaybackMode}
          onRotate={() => void rotateCurrentAsset()}
          onToggleMediaDetails={() => setMediaDetailsOpen((open) => !open)}
          onToggleFullscreen={toggleFullscreen}
        />
        ) : asset.mediaType === 'video' ? (
        <VideoViewer
          asset={asset}
          fullscreen={fullscreen}
          layerMode={layerMode}
          preloadEnabled={preloadEnabled}
          playbackRate={playbackRate}
          viewerPrefs={viewerPrefs}
          selectedSubtitleId={selectedSubtitleId}
          subtitles={layerMode === 'active' ? sidecars?.subtitles ?? [] : []}
          subtitlesEnabled={layerMode === 'active' && subtitlesEnabled}
          deleting={deleteLoading || deleteSubmitting || recordDeleteSubmitting}
          onDanmakuPrefChange={updateDanmakuPref}
          onDelete={openDeleteDialog}
          onDeleteRecord={openRecordDeleteDialog}
          mediaDetailsOpen={mediaDetailsOpen}
          onMediaError={handleMediaError}
          onMediaReady={handleMediaReady}
          onPriorityPreloadComplete={handlePriorityPreloadComplete}
          onPlaybackInfoChange={handleCurrentPlaybackInfoChange}
          onPlaybackEnded={playNextAsset}
          onPlaybackModeChange={updatePlaybackMode}
          onPlaybackRateChange={updatePlaybackRate}
          onRotate={() => void rotateCurrentAsset()}
          onSelectedSubtitleChange={updateSelectedSubtitle}
          onSubtitlesEnabledChange={updateSubtitlesEnabled}
          onToggleMediaDetails={() => setMediaDetailsOpen((open) => !open)}
          onToggleFullscreen={toggleFullscreen}
          onProxyRuntimeChange={handleCurrentProxyRuntimeChange}
        />
        ) : (
        <AudioViewer
          asset={asset}
          deleting={deleteLoading || deleteSubmitting || recordDeleteSubmitting}
          fullscreen={fullscreen}
          layerMode={layerMode}
          mediaDetailsOpen={mediaDetailsOpen}
          playbackRate={playbackRate}
          preloadEnabled={preloadEnabled}
          viewerPrefs={viewerPrefs}
          onDelete={openDeleteDialog}
          onDeleteRecord={openRecordDeleteDialog}
          onMediaError={handleMediaError}
          onMediaReady={handleMediaReady}
          onPlaybackEnded={playNextAsset}
          onPlaybackModeChange={updatePlaybackMode}
          onPlaybackRateChange={updatePlaybackRate}
          onToggleMediaDetails={() => setMediaDetailsOpen((open) => !open)}
          onToggleFullscreen={toggleFullscreen}
        />
        )}
      </div>
    );
  };

  const viewerStyle = {
    '--viewer-info-width': `${fitViewerPanelWidth(viewerPanelWidth, viewerAvailableWidth)}px`,
  } as CSSProperties;

  return (
    <>
      <section
        ref={viewerRef}
        className={overlay ? 'viewer-page viewer-overlay' : 'viewer-page'}
        style={viewerStyle}
      >
        <div
          ref={viewerBodyRef}
          className="viewer-body"
          onContextMenu={(event) => {
            if (!(event.target instanceof Element) || !event.target.closest('.image-stage, .video-stage, .audio-stage')) return;
            if (event.target.closest('[data-viewer-wheel-control]')) return;
            event.preventDefault();
            leave();
          }}
        >
          {mediaWindow.map(renderMediaLayer)}
          {currentMediaFailed && mediaLoadFailure && (
            <div className="viewer-media-load-error" role="status">{mediaLoadFailure.message}</div>
          )}
          {mediaDetailsOpen && current && (
            <MediaDetailsCard
              asset={current}
              containerRef={viewerBodyRef}
              initialPosition={mediaDetailsPosition}
              playbackInfo={videoPlaybackInfo}
              runtime={currentVideoProxyRuntime}
              onClose={() => setMediaDetailsOpen(false)}
            />
          )}
        </div>
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

function isImageZoomWheel(event: WheelEvent) {
  return event.target instanceof Element && Boolean(event.target.closest('.image-stage.zooming'));
}

function isViewerWheelControl(event: WheelEvent) {
  return event.composedPath().some((target) => target instanceof Element && target.hasAttribute('data-viewer-wheel-control'));
}

function wheelTargetAtOffset(base: WheelBase, offset: number) {
  if (offset === 0) return base.current;
  return offset > 0 ? base.next[offset - 1] : base.previous[Math.abs(offset) - 1];
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
	aiResult,
	aiError,
  aiTagError,
  aiTagSaving,
  tagDraft,
  tagError,
  tagSaving,
  assetInfoCollapsed,
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
	aiResult: AssetAIResult | null;
	aiError: string | null;
  aiTagError: string | null;
  aiTagSaving: boolean;
  tagDraft: string;
  tagError: string | null;
  tagSaving: boolean;
  assetInfoCollapsed: boolean;
  onLeave: () => void;
  onToggleAssetInfo: () => void;
  onOpenFolder: (asset: Asset) => void;
  onNFOSearch: (field: NFOFilterField | 'nfo', value: string) => void;
  onTagDraftChange: (value: string) => void;
  onAddTag: () => void;
  onRemoveTag: (tag: string) => void;
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
                  {assetInfoRows(asset, tags).map((item) => (
                    <div key={item.label}>
                      <dt>{item.label}</dt>
                      <dd>{item.value}</dd>
                    </div>
                  ))}
                </dl>
              </>
            )}
          </div>
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
    </div>
  );
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
                    onClick={() => void onRemove(item.tag).then((removed) => {
                      if (removed && draft?.previousTag === item.tag) setDraft(null);
                    })}
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
  onAdd: () => void;
  onDraftChange: (value: string) => void;
  onRemove: (tag: string) => void;
}) {
  const [adding, setAdding] = useState(false);
  return (
    <div className="sidebar-asset-tags">
      <div className="sidebar-control-title">自标</div>
      {error && <div className="sidebar-error">{error}</div>}
      <div className="sidebar-asset-tag-list">
        {tags.map((item) => (
          <span className="sidebar-asset-tag" key={item.tag}>
            {item.tag}
            <button type="button" title="删除标签" disabled={saving} onClick={() => onRemove(item.tag)}>
              <X size={12} />
            </button>
          </span>
        ))}
        <button className="sidebar-asset-tag-add" type="button" title="添加自标" onClick={() => setAdding((value) => !value)}>
          <Plus size={13} />
        </button>
      </div>
      {adding && (
        <form
          className="sidebar-asset-tag-form"
          onSubmit={(event) => {
            event.preventDefault();
            onAdd();
            if (draft.trim()) setAdding(false);
          }}
        >
          <input
            autoFocus
            value={draft}
            placeholder="输入标签"
            disabled={saving}
            onChange={(event) => onDraftChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                setAdding(false);
                onDraftChange('');
              }
            }}
          />
          <button type="submit" title="添加标签" disabled={saving || draft.trim() === ''}>
            <Check size={14} />
          </button>
        </form>
      )}
    </div>
  );
}

function videoPlaybackInfoLabel(info: VideoPlaybackInfo | null) {
  if (!info) return '加载中';
  return info.playbackStateLabel;
}

function videoProxyRuntimeLabel(runtime: VideoSegmentStatus) {
  if (runtime.message) return runtime.message;
  if (runtime.status === 'cached' || runtime.cached) return '已缓存';
  if (runtime.status === 'error') return '转码失败';
  if (runtime.status === 'queued' || runtime.queued) return '等待转码槽位';
  if (runtime.transcoding) return `实时转码 ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`;
  return '准备转码';
}

function MediaDetailsCard({
  asset,
  containerRef,
  initialPosition,
  playbackInfo,
  runtime,
  onClose,
}: {
  asset: Asset;
  containerRef: RefObject<HTMLDivElement | null>;
  initialPosition: { x: number; y: number };
  playbackInfo: VideoPlaybackInfo | null;
  runtime: VideoSegmentStatus | null;
  onClose: () => void;
}) {
  const cardRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<{ pointerId: number; offsetX: number; offsetY: number } | null>(null);
  const [position, setPosition] = useState(initialPosition);
  const [layout, setLayout] = useState({
    mediaHeight: 0,
    mediaWidth: 0,
    playerHeight: 0,
    playerWidth: 0,
    windowHeight: 0,
    windowWidth: 0,
  });

  const clampPosition = useCallback((next: { x: number; y: number }) => {
    const container = containerRef.current;
    const card = cardRef.current;
    if (!container || !card) return next;
    return {
      x: Math.max(8, Math.min(next.x, container.clientWidth - card.offsetWidth - 8)),
      y: Math.max(8, Math.min(next.y, container.clientHeight - card.offsetHeight - 8)),
    };
  }, [containerRef]);

  useEffect(() => {
    const container = containerRef.current;
    const card = cardRef.current;
    if (!container || !card) return;
    const measure = () => {
      const player = container.querySelector<HTMLElement>('.video-frame, .image-stage');
      const media = container.querySelector<HTMLElement>('.viewer-video, .viewer-image');
      const windowRect = container.getBoundingClientRect();
      const playerRect = player?.getBoundingClientRect();
      const mediaRect = media?.getBoundingClientRect();
      setLayout({
        mediaHeight: Math.round(mediaRect?.height ?? 0),
        mediaWidth: Math.round(mediaRect?.width ?? 0),
        playerHeight: Math.round(playerRect?.height ?? 0),
        playerWidth: Math.round(playerRect?.width ?? 0),
        windowHeight: Math.round(windowRect.height),
        windowWidth: Math.round(windowRect.width),
      });
      setPosition((current) => clampPosition(current));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(container);
    observer.observe(card);
    const player = container.querySelector<HTMLElement>('.video-frame, .image-stage');
    const media = container.querySelector<HTMLElement>('.viewer-video, .viewer-image');
    if (player) observer.observe(player);
    if (media) observer.observe(media);
    const timer = window.setInterval(measure, 500);
    return () => {
      observer.disconnect();
      window.clearInterval(timer);
    };
  }, [asset.id, clampPosition, containerRef]);

  useEffect(() => {
    setPosition(clampPosition(initialPosition));
  }, [clampPosition, initialPosition.x, initialPosition.y]);

  const rows = mediaDetailRows(asset, playbackInfo, layout, runtime);
  const runtimeProgress = runtime ? Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100) : 0;
  return (
    <div
      ref={cardRef}
      className="viewer-media-details"
      data-viewer-wheel-control
      role="dialog"
      aria-label="媒体详情"
      style={{ left: position.x, top: position.y }}
      onContextMenu={(event) => event.preventDefault()}
      onPointerDown={(event) => event.stopPropagation()}
    >
      <div
        className="viewer-media-details-header"
        onPointerDown={(event) => {
          if ((event.target as Element).closest('button')) return;
          const container = containerRef.current;
          const card = cardRef.current;
          if (!container || !card) return;
          const cardRect = card.getBoundingClientRect();
          dragRef.current = {
            pointerId: event.pointerId,
            offsetX: event.clientX - cardRect.left,
            offsetY: event.clientY - cardRect.top,
          };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          const drag = dragRef.current;
          const container = containerRef.current;
          if (!drag || drag.pointerId !== event.pointerId || !container) return;
          const rect = container.getBoundingClientRect();
          setPosition(clampPosition({
            x: event.clientX - rect.left - drag.offsetX,
            y: event.clientY - rect.top - drag.offsetY,
          }));
        }}
        onPointerUp={(event) => {
          if (dragRef.current?.pointerId !== event.pointerId) return;
          dragRef.current = null;
          event.currentTarget.releasePointerCapture(event.pointerId);
        }}
        onPointerCancel={() => {
          dragRef.current = null;
        }}
      >
        <GripHorizontal size={16} />
        <strong>媒体详情</strong>
        <button type="button" title="关闭媒体详情" onClick={onClose}>
          <X size={16} />
        </button>
      </div>
      <div className="viewer-media-details-name" title={asset.filename}>{asset.filename}</div>
      <dl className="viewer-media-details-list">
        {rows.map((row) => (
          <div key={row.label}>
            <dt>{row.label}</dt>
            <dd>{row.value}</dd>
          </div>
        ))}
      </dl>
      {asset.mediaType === 'video' && runtime && (
        <div className="viewer-media-details-progress" aria-label={`转码进度 ${runtimeProgress}%`}>
          <span style={{ width: `${runtimeProgress}%` }} />
        </div>
      )}
    </div>
  );
}

function mediaDetailRows(
  asset: Asset,
  info: VideoPlaybackInfo | null,
  layout: {
    mediaHeight: number;
    mediaWidth: number;
    playerHeight: number;
    playerWidth: number;
    windowHeight: number;
    windowWidth: number;
  },
  runtime: VideoSegmentStatus | null,
) {
  const rows = [
    { label: '媒体类型', value: asset.mediaType === 'video' ? '视频' : asset.mediaType === 'audio' ? '音频' : '图片' },
    { label: asset.mediaType === 'video' ? '视频分辨率' : asset.mediaType === 'audio' ? '全局封面尺寸' : '图片分辨率', value: asset.mediaType === 'audio' ? '1024 x 1024 px' : formatDimensions(asset.width, asset.height) },
    { label: '当前显示尺寸', value: formatDimensions(layout.mediaWidth, layout.mediaHeight) },
    { label: '播放器区域', value: formatDimensions(layout.playerWidth, layout.playerHeight) },
    { label: '播放器窗口', value: `${formatDimensions(layout.windowWidth, layout.windowHeight)} · 宽 ${layout.windowWidth}px` },
  ];
  if (asset.mediaType === 'audio') {
    rows.splice(1, 0,
      { label: '播放来源', value: asset.browserPlayable ? '原文件按需读取' : 'FLAC 无损兼容缓存' },
      { label: '播放位置', value: `0:00 / ${formatDuration(asset.duration || 0)}` },
    );
    rows.push(
      { label: '音频码率', value: formatBitrate(asset.audioBitrate) },
      { label: '总码率', value: formatBitrate(asset.overallBitrate) },
      { label: '音频编码', value: asset.audioCodec || '未知' },
      { label: '封装格式', value: asset.container || '未知' },
      { label: '源文件总大小', value: formatBytes(asset.size) },
    );
    return rows;
  }
  if (asset.mediaType !== 'video') return rows;
  const bufferedPercent = info?.duration ? Math.round(Math.min(1, Math.max(0, info.bufferedEnd / info.duration)) * 100) : 0;
  const droppedPercent = info?.totalFrames ? (info.droppedFrames / info.totalFrames) * 100 : 0;
  const playbackRows = [{ label: '播放状态', value: videoPlaybackInfoLabel(info) }];
  if (info?.notPlayingReason) {
    playbackRows.push({ label: '未播放原因', value: info.notPlayingReason });
    playbackRows.push({ label: '原因详情', value: videoNotPlayingDetail(info, runtime) });
  }
  playbackRows.push(
    { label: '播放位置', value: `${formatDuration(info?.currentTime ?? 0)} / ${formatDuration(info?.duration || asset.duration || 0)}` },
    { label: '播放来源', value: asset.browserPlayable ? '原文件按需读取' : 'HLS 实时分片转码' },
  );
  rows.splice(1, 0, ...playbackRows);
  rows.push(
    { label: '当前分辨率', value: formatDimensions(info?.decodedWidth, info?.decodedHeight) },
    { label: '视频帧率', value: asset.fps ? `${formatDecimal(asset.fps)} FPS` : '未知' },
    { label: '视频码率', value: formatBitrate(asset.videoBitrate) },
    { label: '音频码率', value: formatBitrate(asset.audioBitrate) },
    { label: '总码率', value: formatBitrate(asset.overallBitrate) },
    { label: '视频编码', value: asset.videoCodec || '未知' },
    { label: '音频编码', value: asset.audioCodec || '未知' },
    { label: '封装格式', value: asset.container || '未知' },
    { label: '播放速度', value: `${formatDecimal(info?.playbackRate || 1)}x` },
    { label: '网络加载速度', value: info ? `${formatBytes(Math.round(info.networkBytesPerSecond))}/s` : '等待统计' },
    { label: '当前分片大小', value: currentSegmentSizeLabel(asset, info, runtime) },
    { label: '浏览器缓存', value: browserMediaCacheLabel(asset, info, runtime) },
    { label: '媒体缓存', value: serverMediaCacheLabel(asset, runtime) },
    { label: '源文件总大小', value: formatBytes(asset.size) },
    { label: '缓存进度', value: info?.duration ? `${bufferedPercent}% · 至 ${formatDuration(info.bufferedEnd)}` : '等待媒体信息' },
    { label: '丢帧', value: info?.totalFrames ? `${info.droppedFrames} / ${info.totalFrames} (${formatDecimal(droppedPercent)}%)` : '暂无统计' },
    { label: '转码状态', value: asset.browserPlayable ? '无需转码' : runtime ? videoProxyRuntimeLabel(runtime) : '等待分片' },
  );
  if (runtime?.segmentIndex !== undefined && runtime.segmentIndex >= 0) {
    rows.push({
      label: '当前切片',
      value: `${runtime.segmentIndex + 1} · ${formatDuration(runtime.secondsDone || 0)} / ${formatDuration(runtime.duration || 0)}`,
    });
  }
  return rows;
}

function currentSegmentSizeLabel(asset: Asset, info: VideoPlaybackInfo | null, runtime: VideoSegmentStatus | null) {
  if (!info) return '等待统计';
  if (asset.browserPlayable) {
    return info.currentSegmentBytes > 0 ? `本次请求 ${formatBytes(info.currentSegmentBytes)}` : '原文件按需读取，无转码分片';
  }
  const loaded = info.currentSegmentBytes || runtime?.bytes || 0;
  const total = info.currentSegmentTotalBytes || runtime?.bytes || 0;
  return `${formatBytes(loaded)} / ${total > 0 ? formatBytes(total) : '总大小未知'}`;
}

function browserMediaCacheLabel(asset: Asset, info: VideoPlaybackInfo | null, runtime: VideoSegmentStatus | null) {
  if (!info) return '等待统计';
  const total = asset.browserPlayable ? asset.size : runtime?.estimatedTotalBytes || 0;
  const totalLabel = total > 0 ? `${asset.browserPlayable ? '' : '约 '}${formatBytes(total)}` : '总大小未知';
  return `${formatBytes(info.browserCachedBytes)} / ${totalLabel}`;
}

function serverMediaCacheLabel(asset: Asset, runtime: VideoSegmentStatus | null) {
  if (asset.browserPlayable) return '无需转码缓存';
  if (!runtime) return '等待缓存统计';
  const total = runtime.estimatedTotalBytes > 0 ? `约 ${formatBytes(runtime.estimatedTotalBytes)}` : '预计总大小未知';
  return `${formatBytes(runtime.cachedBytes)} / ${total} · ${runtime.cachedSegments}/${runtime.segmentCount} 个分片`;
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

function assetInfoRows(asset: Asset, tags: AssetTag[]) {
  const rows = [
    { label: '类型', value: asset.mediaType === 'image' ? '照片' : asset.mediaType === 'audio' ? '音频' : '视频' },
    { label: '大小', value: formatBytes(asset.size) },
    { label: '时间', value: formatDateTime(asset.timelineAt) },
    { label: '星级', value: asset.rating === 0 ? '未评级' : `${asset.rating} 星` },
  ];
  if (asset.width && asset.height) rows.push({ label: '尺寸', value: `${asset.width} x ${asset.height}` });
  if ((asset.mediaType === 'video' || asset.mediaType === 'audio') && asset.duration !== null) rows.push({ label: '时长', value: formatDuration(asset.duration) });
  if (asset.mediaType !== 'audio') rows.push({ label: '旋转', value: `${asset.rotation || 0}°` });
  rows.push({ label: '标签', value: tags.length > 0 ? tags.map((item) => item.tag).join('、') : '无标签' });
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

function viewerMediaWindow(neighbors: Neighbors) {
  return uniqueMediaAssets([
    ...neighbors.previous.slice(0, viewerRetainRadius).reverse(),
    neighbors.current,
    ...neighbors.next.slice(0, viewerRetainRadius),
  ]);
}

function viewerIndicatorAssets(neighbors: Neighbors | null, current: Asset | undefined): Array<Asset | undefined> {
  if (!current) return Array.from({ length: viewerIndicatorCount }, () => undefined);
  if (!neighbors) {
    return Array.from({ length: viewerIndicatorCount }, (_, index) => index === viewerIndicatorCenter ? current : undefined);
  }
  const sequence = [
    ...neighbors.previous.slice(0, viewerRetainRadius + 1).reverse(),
    neighbors.current,
    ...neighbors.next.slice(0, viewerRetainRadius + 1),
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
