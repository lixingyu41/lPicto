import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useParams, useSearchParams, type Location } from 'react-router-dom';
import { Captions, CaptionsOff, Gauge, LogOut, Maximize2, Minimize2, RotateCw } from 'lucide-react';
import { api, assetOriginalUrl, assetPreviewUrl } from '../api/client';
import type { Asset, AssetSidecars, Neighbors, NFOField } from '../types/api';
import { formatBytes, formatDateTime, formatDuration } from '../utils/format';
import ImageViewer from '../viewer/ImageViewer';
import VideoViewer from '../viewer/VideoViewer';
import { useKeyboard } from '../hooks/useKeyboard';
import { useRestoreSidebarState, useSidebarPanel, type SidebarReturnState } from '../components/SidebarContext';
import { nextRotation } from '../utils/rotation';
import { decodeReturnState, loadViewerReturnPath } from '../utils/pageState';
import { loadViewerPrefs, nextPlaybackRate, saveViewerPrefs, viewerPrefsChanged } from '../utils/viewerPrefs';

interface WheelBase {
  next: Asset[];
  offset: number;
  previous: Asset[];
}

interface ViewerPageProps {
  overlay?: boolean;
}

interface ViewerLocationState {
  backgroundLocation?: Location;
}

const wheelStepCooldownMs = 220;
const wheelStepThreshold = 60;

export default function ViewerPage({ overlay = false }: ViewerPageProps) {
  const params = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [neighbors, setNeighbors] = useState<Neighbors | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sidecars, setSidecars] = useState<AssetSidecars | null>(null);
  const [sidecarError, setSidecarError] = useState<string | null>(null);
  const [subtitlesEnabled, setSubtitlesEnabled] = useState(false);
  const [selectedSubtitleId, setSelectedSubtitleId] = useState('');
  const [playbackRate, setPlaybackRate] = useState(() => loadViewerPrefs().playbackRate);
  const [fullscreen, setFullscreen] = useState(false);
  const preloadImages = useRef<HTMLImageElement[]>([]);
  const wheelBase = useRef<WheelBase | null>(null);
  const wheelDelta = useRef(0);
  const wheelLastStepAt = useRef(0);
  const wheelResetTimer = useRef<number | null>(null);
  const viewerRef = useRef<HTMLElement | null>(null);
  const viewerReturnStateRef = useRef(decodeReturnState<Partial<SidebarReturnState>>(searchParams.get('returnState'), {}));
  const restoreSidebarState = useRestoreSidebarState();
  const viewerLocationState = location.state as ViewerLocationState | null;
  const backgroundLocation = viewerLocationState?.backgroundLocation;
  const assetId = Number(params.assetId || assetIdFromPath(location.pathname) || 0);

  const query = useMemo(() => {
    const result: Record<string, string> = {};
    searchParams.forEach((value, key) => {
      result[key] = value;
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

  useEffect(() => {
    if (!activeNeighbors) return;
    preloadImages.current = preloadOrder(activeNeighbors).map((asset) => {
      const image = new Image();
      image.decoding = 'async';
      image.src = preloadUrl(asset);
      return image;
    });
  }, [activeNeighbors]);

  const current = activeNeighbors?.current;

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
    function onPrefsChanged() {
      const prefs = loadViewerPrefs();
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
    setPlaybackRate(prefs.playbackRate);
  }, []);

  const updateSubtitlesEnabled = useCallback((value: boolean) => {
    saveViewerPrefs({ ...loadViewerPrefs(), subtitlesEnabled: value });
    setSubtitlesEnabled(Boolean(selectedSubtitleId) && value);
  }, [selectedSubtitleId]);

  const updateSelectedSubtitle = useCallback((value: string) => {
    setSelectedSubtitleId(value);
    if (value) {
      saveViewerPrefs({ ...loadViewerPrefs(), subtitlesEnabled: true });
      setSubtitlesEnabled(true);
    } else {
      setSubtitlesEnabled(false);
    }
  }, []);

  useEffect(() => {
    return () => {
      if (wheelResetTimer.current !== null) {
        window.clearTimeout(wheelResetTimer.current);
      }
    };
  }, []);

  const goAsset = useCallback(
    (asset: Asset | undefined) => {
      if (!asset) return;
      navigate(
        { pathname: `/viewer/${asset.id}`, search: searchParams.toString() },
        overlay && backgroundLocation ? { replace: true, state: { backgroundLocation } } : undefined,
      );
    },
    [backgroundLocation, navigate, overlay, searchParams],
  );

  const goWheelStep = useCallback(
    (direction: 1 | -1, now = Date.now()) => {
      if (now - wheelLastStepAt.current < wheelStepCooldownMs) return;
      const base =
        wheelBase.current ??
        (activeNeighbors
          ? { next: activeNeighbors.next, offset: 0, previous: activeNeighbors.previous }
          : null);
      if (!base) return;

      const nextOffset = base.offset + direction;
      const target = nextOffset > 0 ? base.next[nextOffset - 1] : base.previous[Math.abs(nextOffset) - 1];
      if (!target) return;

      wheelLastStepAt.current = now;
      base.offset = nextOffset;
      wheelBase.current = base;
      goAsset(target);
      if (wheelResetTimer.current !== null) {
        window.clearTimeout(wheelResetTimer.current);
      }
      wheelResetTimer.current = window.setTimeout(() => {
        wheelBase.current = null;
        wheelResetTimer.current = null;
      }, 320);
    },
    [activeNeighbors, goAsset],
  );

  useEffect(() => {
    const element = viewerRef.current;
    if (!element) return;
    const handleWheel = (event: WheelEvent) => {
      if (event.cancelable) event.preventDefault();
      wheelDelta.current += event.deltaY;
      if (Math.abs(wheelDelta.current) < wheelStepThreshold) return;
      const direction = wheelDelta.current > 0 ? 1 : -1;
      wheelDelta.current = 0;
      goWheelStep(direction, Date.now());
    };
    element.addEventListener('wheel', handleWheel, { passive: false });
    return () => {
      element.removeEventListener('wheel', handleWheel);
    };
  }, [goWheelStep]);

  const leave = useCallback(() => {
    if (overlay) {
      navigate(-1);
      return;
    }
    const context = searchParams.get('context');
    const fallback = context === 'folder' ? '/folders' : context === 'album' ? '/albums' : '/library';
    const returnPath = searchParams.get('returnPath');
    const returnState = searchParams.get('returnState');
    if (returnPath === fallback && returnState) {
      navigate(`${fallback}?restore=${encodeURIComponent(returnState)}`);
      return;
    }
    const storageReturnPath = loadViewerReturnPath();
    navigate(storageReturnPath === fallback ? storageReturnPath : fallback);
  }, [navigate, overlay, searchParams]);

  const rotateCurrentAsset = useCallback(async () => {
    if (!current) return;
    const pref = await api.updateAssetPreferences(current.id, nextRotation(current.rotation));
    setNeighbors((value) => (value ? updateNeighborRotation(value, pref.assetId, pref.rotation) : value));
  }, [current]);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) {
      void document.exitFullscreen();
      return;
    }
    if (viewerRef.current) {
      void viewerRef.current.requestFullscreen();
    }
  }, []);

  useEffect(() => {
    function onFullscreenChange() {
      setFullscreen(document.fullscreenElement === viewerRef.current);
    }
    document.addEventListener('fullscreenchange', onFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', onFullscreenChange);
  }, []);

  useKeyboard(
    useCallback(
      (event: KeyboardEvent) => {
        if (event.key === 'Escape') leave();
        if (event.key.toLowerCase() === 'f') toggleFullscreen();
        if (event.key === 'ArrowLeft' || event.key.toLowerCase() === 'a') goAsset(activeNeighbors?.previous[0]);
        if (event.key === 'ArrowRight' || event.key.toLowerCase() === 'd') goAsset(activeNeighbors?.next[0]);
      },
      [activeNeighbors, goAsset, leave, toggleFullscreen],
    ),
  );

  useSidebarPanel(
    'viewer',
    <ViewerSidebarPanel
      asset={current}
      error={error}
      fullscreen={fullscreen}
      playbackRate={playbackRate}
      selectedSubtitleId={selectedSubtitleId}
      sidecarError={sidecarError}
      sidecars={sidecars}
      subtitlesEnabled={subtitlesEnabled}
      onLeave={leave}
      onPlaybackRateChange={updatePlaybackRate}
      onRotate={() => void rotateCurrentAsset()}
      onSelectedSubtitleChange={updateSelectedSubtitle}
      onSubtitlesEnabledChange={updateSubtitlesEnabled}
      onToggleFullscreen={toggleFullscreen}
    />,
    [
      current?.id,
      current?.rotation,
      error,
      playbackRate,
      selectedSubtitleId,
      sidecarError,
      sidecars,
      subtitlesEnabled,
      fullscreen,
      leave,
      updatePlaybackRate,
      rotateCurrentAsset,
      updateSelectedSubtitle,
      updateSubtitlesEnabled,
      toggleFullscreen,
    ],
  );

  useEffect(() => {
    if (overlay) return;
    restoreSidebarState(viewerReturnStateRef.current);
  }, [overlay, restoreSidebarState]);

  return (
    <section
      ref={viewerRef}
      className={overlay ? 'viewer-page viewer-overlay' : 'viewer-page'}
    >
      <div className="viewer-body">
        {current &&
          (current.mediaType === 'image' ? (
            <ImageViewer asset={current} />
          ) : (
            <VideoViewer
              asset={current}
              playbackRate={playbackRate}
              selectedSubtitleId={selectedSubtitleId}
              subtitlesEnabled={subtitlesEnabled}
            />
          ))}
      </div>
    </section>
  );
}

function isAbortError(err: unknown) {
  return err instanceof DOMException && err.name === 'AbortError';
}

function assetIdFromPath(pathname: string) {
  const match = pathname.match(/^\/viewer\/(\d+)/);
  return match?.[1] ?? '';
}

function ViewerSidebarPanel({
  asset,
  error,
  fullscreen,
  playbackRate,
  selectedSubtitleId,
  sidecarError,
  sidecars,
  subtitlesEnabled,
  onLeave,
  onPlaybackRateChange,
  onRotate,
  onSelectedSubtitleChange,
  onSubtitlesEnabledChange,
  onToggleFullscreen,
}: {
  asset: Asset | undefined;
  error: string | null;
  fullscreen: boolean;
  playbackRate: number;
  selectedSubtitleId: string;
  sidecarError: string | null;
  sidecars: AssetSidecars | null;
  subtitlesEnabled: boolean;
  onLeave: () => void;
  onPlaybackRateChange: (value: number) => void;
  onRotate: () => void;
  onSelectedSubtitleChange: (value: string) => void;
  onSubtitlesEnabledChange: (value: boolean) => void;
  onToggleFullscreen: () => void;
}) {
  const nfoFields = sidecars?.nfo?.fields ?? {};
  const nfoGroups = sidecars?.nfo?.groups?.filter((group) => group.items.length > 0) ?? [];
  const nfoFieldEntries = Object.entries(nfoFields).filter(([, value]) => value.trim() !== '');
  const hasSubtitles = Boolean(sidecars?.subtitles.length);
  return (
    <div className="sidebar-control-stack">
      <div className="sidebar-control-title">查看器</div>
      <div className="sidebar-viewer-actions">
        <button className="sidebar-square-button" type="button" title="退出查看" onClick={onLeave}>
          <LogOut size={16} />
        </button>
        <button className="sidebar-square-button" type="button" title={fullscreen ? '退出全屏' : '全屏'} onClick={onToggleFullscreen}>
          {fullscreen ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
        </button>
        {asset && (
          <button className="sidebar-square-button" type="button" title={`旋转 ${asset.rotation || 0}°`} onClick={onRotate}>
            <RotateCw size={16} />
          </button>
        )}
        {asset?.mediaType === 'video' && (
          <>
            <button
              className="sidebar-square-button sidebar-rate-button"
              type="button"
              title={`倍速 ${playbackRate}x`}
              onClick={() => onPlaybackRateChange(nextPlaybackRate(playbackRate))}
            >
              <Gauge size={15} />
              <span>{playbackRate}x</span>
            </button>
            <button
              className={subtitlesEnabled && hasSubtitles ? 'sidebar-square-button active' : 'sidebar-square-button'}
              type="button"
              title={hasSubtitles ? (subtitlesEnabled ? '关闭弹幕' : '开启弹幕') : '无弹幕'}
              disabled={!hasSubtitles}
              onClick={() => onSubtitlesEnabledChange(!subtitlesEnabled)}
            >
              {subtitlesEnabled && hasSubtitles ? <Captions size={16} /> : <CaptionsOff size={16} />}
            </button>
          </>
        )}
      </div>
      {asset?.mediaType === 'video' && (
        <>
          {hasSubtitles && (
            <select
              className="sidebar-subtitle-select"
              value={selectedSubtitleId}
              onChange={(event) => {
                onSelectedSubtitleChange(event.target.value);
                onSubtitlesEnabledChange(Boolean(event.target.value));
              }}
            >
              {sidecars?.subtitles.map((subtitle) => (
                <option key={subtitle.id} value={subtitle.id}>
                  {subtitle.filename}
                </option>
              ))}
            </select>
          )}
        </>
      )}
      {error && <div className="sidebar-error">{error}</div>}
      {sidecarError && <div className="sidebar-error">{sidecarError}</div>}
      {asset && (
        <div className="sidebar-asset-info">
          <strong>{asset.filename}</strong>
          <span>{asset.relPath}</span>
          <div className="sidebar-info-chips">
            {assetInfoChips(asset).map((value) => (
              <span className="sidebar-info-chip" key={value}>
                {value}
              </span>
            ))}
          </div>
        </div>
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
                      <NFOValue key={`${item.key}-${item.value}-${index}`} item={item} />
                    ))}
                  </div>
                </section>
              ))
            : nfoFieldEntries.length > 0 && (
                <section className="sidebar-nfo-group">
                  <div className="sidebar-nfo-group-title">字段</div>
                  <div className="sidebar-nfo-items">
                    {nfoFieldEntries.map(([key, value]) => (
                      <span className="sidebar-nfo-item" key={key}>
                        <span>{key}</span>
                        {value}
                      </span>
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

function assetInfoChips(asset: Asset) {
  const chips = [asset.mediaType === 'image' ? '照片' : '视频', formatBytes(asset.size), formatDateTime(asset.timelineAt)];
  if (asset.width && asset.height) chips.push(`${asset.width} x ${asset.height}`);
  if (asset.duration !== null) chips.push(formatDuration(asset.duration));
  if (asset.mediaType === 'video') chips.push(`${asset.rotation || 0}°`);
  return chips;
}

function NFOValue({ item }: { item: NFOField }) {
  const content = (
    <>
      <span>{item.label}</span>
      {item.value}
    </>
  );
  if (!item.copyable) {
    return <span className="sidebar-nfo-item">{content}</span>;
  }
  return (
    <button className="sidebar-nfo-item copyable" type="button" title="点击复制" onClick={() => void copyText(item.value)}>
      {content}
    </button>
  );
}

async function copyText(value: string) {
  if (!navigator.clipboard) return;
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    return;
  }
}

function updateNeighborRotation(neighbors: Neighbors, assetId: number, rotation: number): Neighbors {
  const update = (asset: Asset) => (asset.id === assetId ? { ...asset, rotation } : asset);
  return {
    current: update(neighbors.current),
    previous: neighbors.previous.map(update),
    next: neighbors.next.map(update),
  };
}

function viewerImageUrl(asset: Asset) {
  return asset.browserPlayable ? assetOriginalUrl(asset) : assetPreviewUrl(asset);
}

function preloadUrl(asset: Asset) {
  return asset.mediaType === 'image' ? viewerImageUrl(asset) : assetPreviewUrl(asset);
}

function preloadOrder(neighbors: Neighbors) {
  const order = [
    neighbors.current,
    neighbors.next[0],
    neighbors.next[1],
    neighbors.previous[0],
    neighbors.next[2],
    neighbors.previous[1],
    neighbors.previous[2],
  ];
  const seen = new Set<number>();
  return order.filter((asset): asset is Asset => {
    if (!asset || seen.has(asset.id)) return false;
    seen.add(asset.id);
    return true;
  });
}
