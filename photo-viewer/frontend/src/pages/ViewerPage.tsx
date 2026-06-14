import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { RotateCw } from 'lucide-react';
import { api, assetOriginalUrl, assetPreviewUrl } from '../api/client';
import type { Asset, AssetSidecars, Neighbors } from '../types/api';
import { formatBytes, formatDateTime, formatDuration } from '../utils/format';
import ImageViewer from '../viewer/ImageViewer';
import VideoViewer from '../viewer/VideoViewer';
import { useKeyboard } from '../hooks/useKeyboard';
import { useSidebarPanel } from '../components/SidebarContext';
import { nextRotation } from '../utils/rotation';

interface WheelBase {
  next: Asset[];
  offset: number;
  previous: Asset[];
}

export default function ViewerPage() {
  const params = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [neighbors, setNeighbors] = useState<Neighbors | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sidecars, setSidecars] = useState<AssetSidecars | null>(null);
  const [sidecarError, setSidecarError] = useState<string | null>(null);
  const [subtitlesEnabled, setSubtitlesEnabled] = useState(false);
  const [selectedSubtitleId, setSelectedSubtitleId] = useState('');
  const [playbackRate, setPlaybackRate] = useState(1);
  const [fullscreen, setFullscreen] = useState(false);
  const preloadImages = useRef<HTMLImageElement[]>([]);
  const wheelBase = useRef<WheelBase | null>(null);
  const wheelResetTimer = useRef<number | null>(null);
  const viewerRef = useRef<HTMLElement | null>(null);
  const assetId = Number(params.assetId || 0);

  const query = useMemo(() => {
    const result: Record<string, string> = {};
    searchParams.forEach((value, key) => {
      result[key] = value;
    });
    return result;
  }, [searchParams]);

  useEffect(() => {
    let live = true;
    async function load() {
      try {
        const result = await api.neighbors(assetId, query);
        if (!live) return;
        setNeighbors(result);
        setError(null);
      } catch (err) {
        if (!live) return;
        setError(err instanceof Error ? err.message : '读取资源失败');
      }
    }
    if (assetId > 0) void load();
    return () => {
      live = false;
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
        setSubtitlesEnabled(Boolean(defaultID));
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
    return () => {
      if (wheelResetTimer.current !== null) {
        window.clearTimeout(wheelResetTimer.current);
      }
    };
  }, []);

  const goAsset = useCallback(
    (asset: Asset | undefined) => {
      if (!asset) return;
      navigate({ pathname: `/viewer/${asset.id}`, search: searchParams.toString() });
    },
    [navigate, searchParams],
  );

  const goWheelStep = useCallback(
    (direction: 1 | -1) => {
      const base =
        wheelBase.current ??
        (activeNeighbors
          ? { next: activeNeighbors.next, offset: 0, previous: activeNeighbors.previous }
          : null);
      if (!base) return;

      const nextOffset = base.offset + direction;
      const target = nextOffset > 0 ? base.next[nextOffset - 1] : base.previous[Math.abs(nextOffset) - 1];
      if (!target) return;

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

  const leave = useCallback(() => {
    const context = searchParams.get('context');
    if (context === 'folder') navigate('/folders');
    else if (context === 'album') navigate('/albums');
    else if (context === 'library') navigate('/library');
    else navigate('/library');
  }, [navigate, searchParams]);

  const rotateCurrentVideo = useCallback(async () => {
    if (!current || current.mediaType !== 'video') return;
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
      hasNext={Boolean(activeNeighbors?.next.length)}
      hasPrevious={Boolean(activeNeighbors?.previous.length)}
      fullscreen={fullscreen}
      playbackRate={playbackRate}
      selectedSubtitleId={selectedSubtitleId}
      sidecarError={sidecarError}
      sidecars={sidecars}
      subtitlesEnabled={subtitlesEnabled}
      onLeave={leave}
      onPlaybackRateChange={setPlaybackRate}
      onRotateVideo={() => void rotateCurrentVideo()}
      onSelectedSubtitleChange={setSelectedSubtitleId}
      onSubtitlesEnabledChange={setSubtitlesEnabled}
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
      activeNeighbors?.next.length,
      activeNeighbors?.previous.length,
      fullscreen,
      leave,
      rotateCurrentVideo,
      toggleFullscreen,
    ],
  );

  return (
    <section
      ref={viewerRef}
      className="viewer-page"
      onWheel={(event) => {
        event.preventDefault();
        if (event.deltaY > 0) goWheelStep(1);
        if (event.deltaY < 0) goWheelStep(-1);
      }}
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

function ViewerSidebarPanel({
  asset,
  error,
  hasNext,
  hasPrevious,
  fullscreen,
  playbackRate,
  selectedSubtitleId,
  sidecarError,
  sidecars,
  subtitlesEnabled,
  onLeave,
  onPlaybackRateChange,
  onRotateVideo,
  onSelectedSubtitleChange,
  onSubtitlesEnabledChange,
  onToggleFullscreen,
}: {
  asset: Asset | undefined;
  error: string | null;
  hasNext: boolean;
  hasPrevious: boolean;
  fullscreen: boolean;
  playbackRate: number;
  selectedSubtitleId: string;
  sidecarError: string | null;
  sidecars: AssetSidecars | null;
  subtitlesEnabled: boolean;
  onLeave: () => void;
  onPlaybackRateChange: (value: number) => void;
  onRotateVideo: () => void;
  onSelectedSubtitleChange: (value: string) => void;
  onSubtitlesEnabledChange: (value: boolean) => void;
  onToggleFullscreen: () => void;
}) {
  const nfoFields = sidecars?.nfo?.fields ?? {};
  const nfoFieldEntries = Object.entries(nfoFields).filter(([, value]) => value.trim() !== '');
  return (
    <div className="sidebar-control-stack">
      <div className="sidebar-control-title">查看器</div>
      <button className="sidebar-command" type="button" onClick={onLeave}>
        退出查看
      </button>
      <button className="sidebar-command" type="button" onClick={onToggleFullscreen}>
        {fullscreen ? '退出全屏' : '全屏'}
      </button>
      {asset?.mediaType === 'video' && (
        <>
          <div className="sidebar-icon-actions">
            <button type="button" title="旋转视频" onClick={onRotateVideo}>
              <RotateCw size={16} />
            </button>
            <span>{asset.rotation || 0}°</span>
          </div>
          <label className="sidebar-field">
            <span>倍速</span>
            <select value={playbackRate} onChange={(event) => onPlaybackRateChange(Number(event.target.value))}>
              {[0.5, 1, 1.5, 2, 3].map((value) => (
                <option key={value} value={value}>
                  {value}x
                </option>
              ))}
            </select>
          </label>
          <label className="sidebar-check-row">
            <input
              type="checkbox"
              checked={subtitlesEnabled}
              disabled={!sidecars?.subtitles.length}
              onChange={(event) => onSubtitlesEnabledChange(event.target.checked)}
            />
            <span>字幕</span>
          </label>
          <label className="sidebar-field">
            <span>字幕文件</span>
            <select
              value={selectedSubtitleId}
              disabled={!sidecars?.subtitles.length}
              onChange={(event) => onSelectedSubtitleChange(event.target.value)}
            >
              {!sidecars?.subtitles.length && <option value="">无字幕</option>}
              {sidecars?.subtitles.map((subtitle) => (
                <option key={subtitle.id} value={subtitle.id}>
                  {subtitle.filename}
                </option>
              ))}
            </select>
          </label>
        </>
      )}
      {error && <div className="sidebar-error">{error}</div>}
      {sidecarError && <div className="sidebar-error">{sidecarError}</div>}
      {asset && (
        <div className="sidebar-asset-info">
          <strong>{asset.filename}</strong>
          <span>{asset.relPath}</span>
          <dl>
            <div>
              <dt>类型</dt>
              <dd>{asset.mediaType === 'image' ? '照片' : '视频'}</dd>
            </div>
            <div>
              <dt>大小</dt>
              <dd>{formatBytes(asset.size)}</dd>
            </div>
            <div>
              <dt>MIME</dt>
              <dd>{asset.mimeType || '-'}</dd>
            </div>
            <div>
              <dt>时间</dt>
              <dd>{formatDateTime(asset.timelineAt)}</dd>
            </div>
            <div>
              <dt>导入</dt>
              <dd>{formatDateTime(asset.importedAt)}</dd>
            </div>
            <div>
              <dt>修改</dt>
              <dd>{formatDateTime(asset.mtime)}</dd>
            </div>
            {asset.width && asset.height && (
              <div>
                <dt>尺寸</dt>
                <dd>
                  {asset.width} x {asset.height}
                </dd>
              </div>
            )}
            {asset.duration !== null && (
              <div>
                <dt>时长</dt>
                <dd>{formatDuration(asset.duration)}</dd>
              </div>
            )}
            {asset.mediaType === 'video' && (
              <div>
                <dt>旋转</dt>
                <dd>{asset.rotation || 0}°</dd>
              </div>
            )}
            <div>
              <dt>上一张</dt>
              <dd>{hasPrevious ? '可用' : '无'}</dd>
            </div>
            <div>
              <dt>下一张</dt>
              <dd>{hasNext ? '可用' : '无'}</dd>
            </div>
          </dl>
        </div>
      )}
      {sidecars?.nfo && (
        <div className="sidebar-nfo">
          <div className="sidebar-control-title">NFO</div>
          <small>{sidecars.nfo.filename}</small>
          {nfoFieldEntries.length > 0 && (
            <dl>
              {nfoFieldEntries.map(([key, value]) => (
                <div key={key}>
                  <dt>{key}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
          )}
          {sidecars.nfo.text && <pre>{sidecars.nfo.text}</pre>}
        </div>
      )}
    </div>
  );
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
