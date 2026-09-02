import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Database, Maximize2, Minimize2, RotateCw, Settings, Trash2 } from 'lucide-react';
import type { Asset } from '../types/api';
import {
  loadViewerPrefs,
  playbackModeOptions,
  saveViewerPrefs,
  stepViewerZoomPrefs,
  viewerPrefsChanged,
  type ViewerPlaybackMode,
  type ViewerPrefs,
} from '../utils/viewerPrefs';
import { rotatedContainStyle } from '../utils/rotation';
import { viewerImageUrl } from '../utils/imagePreload';
import { setViewerMediaZoomActive } from '../utils/viewerInteractionState';
import { assetThumbUrl } from '../api/client';
import type { ViewerMediaLayerMode } from './mediaLayer';

interface Props {
  asset: Asset;
  deleting: boolean;
  fullscreen: boolean;
  layerMode: ViewerMediaLayerMode;
  preloadEnabled: boolean;
  playbackMode: ViewerPlaybackMode;
  slideshowSeconds: number;
  onDelete: () => void;
  onDeleteRecord: () => void;
  onMediaError: (assetId: number, cacheKey: string, message: string) => void;
  onMediaReady: (assetId: number, cacheKey: string) => void;
  onPosterReady: (assetId: number, cacheKey: string) => void;
  onPlaybackEnded: () => void;
  onPlaybackModeChange: (value: ViewerPlaybackMode) => void;
  onRotate: () => void;
  onToggleFullscreen: () => void;
}

interface ZoomState {
  active: boolean;
  height: number;
  scale: number;
  width: number;
  x: number;
  y: number;
}

interface DecodedImageSize {
  height: number;
  key: string;
  width: number;
}

export default function ImageViewer({ asset, deleting, fullscreen, layerMode, preloadEnabled, playbackMode, slideshowSeconds, onDelete, onDeleteRecord, onMediaError, onMediaReady, onPosterReady, onPlaybackEnded, onPlaybackModeChange, onRotate, onToggleFullscreen }: Props) {
  const imageRef = useRef<HTMLImageElement | null>(null);
  const thumbnailRef = useRef<HTMLImageElement | null>(null);
  const zoomImageRef = useRef<HTMLImageElement | null>(null);
  const stageRef = useRef<HTMLDivElement | null>(null);
  const settingsRef = useRef<HTMLDivElement | null>(null);
  const readyPaintFrameRef = useRef<number | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [prefs, setPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());
  const [zoom, setZoom] = useState<ZoomState>({
    active: false,
    height: 0,
    scale: 1,
    width: 0,
    x: 0,
    y: 0,
  });
  const [stageSize, setStageSize] = useState({ height: 0, width: 0 });
  const [readyImageKey, setReadyImageKey] = useState('');
  const [preRenderedImageKey, setPreRenderedImageKey] = useState('');
  const [decodedImageSize, setDecodedImageSize] = useState<DecodedImageSize | null>(null);
  const [requestPriority, setRequestPriority] = useState<'current' | 'preload'>(() => layerMode === 'active' ? 'current' : 'preload');
  const src = viewerImageUrl(asset, requestPriority);
  const thumbnailSrc = assetThumbUrl(asset);
  const imageKey = `${asset.id}:${asset.cacheKey}:${src}`;
  const mainImageReady = readyImageKey === imageKey;
  const displayTier = mainImageReady ? 'original' : 'thumbnail';
  const displayedAsset = useMemo(
    () => mainImageReady && decodedImageSize?.key === imageKey
      ? { ...asset, width: decodedImageSize.width, height: decodedImageSize.height }
      : asset,
    [asset, decodedImageSize, imageKey, mainImageReady],
  );
  const imageStyle = useMemo(
    () => rotatedContainStyle(displayedAsset, stageSize),
    [displayedAsset, stageSize.height, stageSize.width],
  );
  const zoomPointer = useRef({ clientX: 0, clientY: 0 });
  const zoomActiveRef = useRef(false);
  const zoomFrameRef = useRef<number | null>(null);
  const pendingZoomRef = useRef<Omit<ZoomState, 'active'> | null>(null);
  const zoomActivityOwner = useRef<object>({});

  useEffect(() => {
    const active = layerMode === 'active' && zoom.active;
    setViewerMediaZoomActive(zoomActivityOwner.current, active);
    return () => setViewerMediaZoomActive(zoomActivityOwner.current, false);
  }, [layerMode, zoom.active]);

  useEffect(() => {
    function onPrefsChanged() {
      setPrefs(loadViewerPrefs());
    }
    window.addEventListener(viewerPrefsChanged, onPrefsChanged);
    window.addEventListener('storage', onPrefsChanged);
    return () => {
      window.removeEventListener(viewerPrefsChanged, onPrefsChanged);
      window.removeEventListener('storage', onPrefsChanged);
    };
  }, []);

  useEffect(() => {
    if (!settingsOpen) return undefined;
    function close(event: PointerEvent) {
      if (event.target instanceof Node && settingsRef.current?.contains(event.target)) return;
      setSettingsOpen(false);
    }
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') setSettingsOpen(false);
    }
    document.addEventListener('pointerdown', close, true);
    document.addEventListener('keydown', closeOnEscape, true);
    return () => {
      document.removeEventListener('pointerdown', close, true);
      document.removeEventListener('keydown', closeOnEscape, true);
    };
  }, [settingsOpen]);

  useEffect(() => {
    if (layerMode !== 'active' || playbackMode !== 'continuous' || !mainImageReady || zoom.active) return undefined;
    const timer = window.setTimeout(onPlaybackEnded, Math.max(1, slideshowSeconds) * 1000);
    return () => window.clearTimeout(timer);
  }, [asset.id, layerMode, mainImageReady, onPlaybackEnded, playbackMode, slideshowSeconds, zoom.active]);

  useEffect(() => {
    setSettingsOpen(false);
  }, [asset.id]);

  useEffect(() => {
    if (layerMode === 'active') return;
    setSettingsOpen(false);
    setZoom((current) => current.active ? { ...current, active: false } : current);
  }, [layerMode]);

  useEffect(() => {
    if (!zoom.active) return;
    function endZoom() {
      zoomActiveRef.current = false;
      setZoom((current) => ({ ...current, active: false }));
    }
    window.addEventListener('mouseup', endZoom);
    return () => window.removeEventListener('mouseup', endZoom);
  }, [zoom.active]);

  useEffect(() => () => {
    if (zoomFrameRef.current !== null) window.cancelAnimationFrame(zoomFrameRef.current);
    if (readyPaintFrameRef.current !== null) window.cancelAnimationFrame(readyPaintFrameRef.current);
  }, []);

  useEffect(() => {
    if (zoom.active || preRenderedImageKey !== imageKey) return;
    setReadyImageKey(imageKey);
  }, [imageKey, preRenderedImageKey, zoom.active]);

  useLayoutEffect(() => {
    if (layerMode !== 'active' || mainImageReady || requestPriority === 'current') return;
    setRequestPriority('current');
  }, [layerMode, mainImageReady, requestPriority]);

  useEffect(() => {
    zoomActiveRef.current = false;
    if (readyPaintFrameRef.current !== null) {
      window.cancelAnimationFrame(readyPaintFrameRef.current);
      readyPaintFrameRef.current = null;
    }
    setZoom({
      active: false,
      height: 0,
      scale: 1,
      width: 0,
      x: 0,
      y: 0,
    });
    setDecodedImageSize(null);
  }, [src]);

  useEffect(() => {
    const stage = stageRef.current;
    if (!stage) return;
    const update = () => {
      const rect = stage.getBoundingClientRect();
      setStageSize({ height: rect.height, width: rect.width });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(stage);
    return () => observer.disconnect();
  }, []);

  function calculateZoom(clientX: number, clientY: number, nextPrefs = prefs): Omit<ZoomState, 'active'> | null {
    const image = mainImageReady ? imageRef.current : thumbnailRef.current;
    const stage = stageRef.current;
    if (!image || !stage) return null;
    zoomPointer.current = { clientX, clientY };
    const stageRect = stage.getBoundingClientRect();
    const naturalWidth = mainImageReady
      ? image.naturalWidth || asset.width || stageRect.width
      : asset.width || image.naturalWidth || stageRect.width;
    const naturalHeight = mainImageReady
      ? image.naturalHeight || asset.height || stageRect.height
      : asset.height || image.naturalHeight || stageRect.height;
    const imageRect = containRect(stageRect, naturalWidth, naturalHeight);
    if (naturalWidth <= 0 || naturalHeight <= 0 || imageRect.width <= 0 || imageRect.height <= 0) return null;

    const imageX = clampNumber(clientX - imageRect.left, 0, imageRect.width);
    const imageY = clampNumber(clientY - imageRect.top, 0, imageRect.height);
    const sourceX = (imageX / imageRect.width) * naturalWidth;
    const sourceY = (imageY / imageRect.height) * naturalHeight;
    const stageX = clientX - stageRect.left;
    const stageY = clientY - stageRect.top;
    const pixelsPerSourcePixel =
      nextPrefs.zoomMode === 'pixels'
        ? Math.min(stageRect.width, stageRect.height) / nextPrefs.zoomPixelArea
        : (imageRect.width * nextPrefs.zoomScale) / naturalWidth;
    return {
      height: naturalHeight,
      scale: pixelsPerSourcePixel,
      width: naturalWidth,
      x: stageX - sourceX * pixelsPerSourcePixel,
      y: stageY - sourceY * pixelsPerSourcePixel,
    };
  }

  function applyZoom(geometry: Omit<ZoomState, 'active'>) {
    const image = zoomImageRef.current;
    if (!image) return;
    image.style.width = `${geometry.width}px`;
    image.style.height = `${geometry.height}px`;
    image.style.transform = `translate3d(${geometry.x}px, ${geometry.y}px, 0) scale(${geometry.scale})`;
  }

  function scheduleZoom(clientX: number, clientY: number, nextPrefs = prefs) {
    const geometry = calculateZoom(clientX, clientY, nextPrefs);
    if (!geometry) return;
    pendingZoomRef.current = geometry;
    if (zoomFrameRef.current !== null) return;
    zoomFrameRef.current = window.requestAnimationFrame(() => {
      zoomFrameRef.current = null;
      const pending = pendingZoomRef.current;
      pendingZoomRef.current = null;
      if (pending && zoomActiveRef.current) applyZoom(pending);
    });
  }

  const adjustZoomByWheel = useCallback((event: WheelEvent) => {
    if (!zoom.active) return;
    if (event.deltaY === 0) return;
    event.preventDefault();
    event.stopPropagation();
    const direction = event.deltaY < 0 ? 1 : -1;
    const currentPrefs = loadViewerPrefs();
    const nextPrefs = stepViewerZoomPrefs(currentPrefs, direction);
    saveViewerPrefs(nextPrefs);
    setPrefs(nextPrefs);
    const geometry = calculateZoom(zoomPointer.current.clientX, zoomPointer.current.clientY, nextPrefs);
    if (geometry) {
      pendingZoomRef.current = null;
      applyZoom(geometry);
      setZoom({ active: true, ...geometry });
    }
  }, [asset.height, asset.width, prefs, zoom.active]);

  useEffect(() => {
    const stage = stageRef.current;
    if (!stage) return;
    stage.addEventListener('wheel', adjustZoomByWheel, { passive: false });
    return () => stage.removeEventListener('wheel', adjustZoomByWheel);
  }, [adjustZoomByWheel]);

  return (
    <div
      ref={stageRef}
      className={zoom.active ? 'image-stage zooming' : 'image-stage'}
      data-image-tier={displayTier}
      onMouseDown={(event) => {
        if (isMobileViewerInteraction()) return;
        if (event.button !== 0) return;
        if (!mainImageReady && !(thumbnailRef.current?.complete && thumbnailRef.current.naturalWidth > 0)) return;
        event.preventDefault();
        zoomActiveRef.current = true;
        const geometry = calculateZoom(event.clientX, event.clientY);
        if (!geometry) return;
        setZoom({ active: true, ...geometry });
      }}
      onMouseMove={(event) => {
        if (isMobileViewerInteraction()) return;
        if (!zoom.active) return;
        if (event.buttons !== 1) {
          zoomActiveRef.current = false;
          setZoom((current) => ({ ...current, active: false }));
          return;
        }
        scheduleZoom(event.clientX, event.clientY);
      }}
      onMouseUp={() => {
        zoomActiveRef.current = false;
        setZoom((current) => ({ ...current, active: false }));
      }}
    >
      {preloadEnabled && <img
        key={src}
        ref={imageRef}
        className={mainImageReady ? 'viewer-image viewer-image-original viewer-image-ready' : 'viewer-image viewer-image-original viewer-image-loading'}
        src={src}
        alt={asset.filename}
        decoding="async"
        fetchPriority={layerMode === 'active' ? 'high' : 'low'}
        loading="eager"
        draggable={false}
        style={imageStyle}
        onLoad={(event) => {
          const image = event.currentTarget;
          void Promise.resolve(typeof image.decode === 'function' ? image.decode() : undefined)
            .catch(() => undefined)
            .then(() => {
              if (imageRef.current !== image || image.getAttribute('src') !== src || !image.complete || image.naturalWidth <= 0) return;
              setDecodedImageSize({ key: imageKey, width: image.naturalWidth, height: image.naturalHeight });
              // A decoded image is not necessarily rasterized while its media
              // layer is hidden. Keep the neighbour layer paintable and only
              // expose the green ready state after the browser has had two
              // frame opportunities to build its display/compositor surface.
              readyPaintFrameRef.current = window.requestAnimationFrame(() => {
                readyPaintFrameRef.current = window.requestAnimationFrame(() => {
                  readyPaintFrameRef.current = null;
                  if (imageRef.current !== image || image.getAttribute('src') !== src || !image.complete) return;
                  setPreRenderedImageKey(imageKey);
                  if (!zoomActiveRef.current) setReadyImageKey(imageKey);
                  onMediaReady(asset.id, asset.cacheKey);
                });
              });
            });
        }}
        onError={() => onMediaError(asset.id, asset.cacheKey, '图片加载失败')}
        onDragStart={(event) => event.preventDefault()}
      />}
      {preloadEnabled && !mainImageReady && (
        <img
          ref={thumbnailRef}
          className="viewer-image viewer-image-placeholder"
          src={thumbnailSrc}
          alt=""
          decoding="async"
          fetchPriority={layerMode === 'active' ? 'high' : 'low'}
          draggable={false}
          style={imageStyle}
          onLoad={() => onPosterReady(asset.id, asset.cacheKey)}
          onDragStart={(event) => event.preventDefault()}
        />
      )}
      {zoom.active && (
        <div className="image-zoom-layer image-zoom-layer-preview">
          <img
            ref={zoomImageRef}
            className="image-zoom-layer-content"
            src={mainImageReady ? src : thumbnailSrc}
            alt=""
            decoding="async"
            draggable={false}
            style={{
              height: zoom.height,
              transform: `translate3d(${zoom.x}px, ${zoom.y}px, 0) scale(${zoom.scale})`,
              width: zoom.width,
            }}
            onDragStart={(event) => event.preventDefault()}
          />
        </div>
      )}
      <div className="image-control-zone" onClick={(event) => event.stopPropagation()} onMouseDown={(event) => event.stopPropagation()}>
        <div className="image-controls">
          <div className="video-settings-wrap image-settings-wrap" data-viewer-wheel-control ref={settingsRef}>
            <button
              className={settingsOpen ? 'active' : undefined}
              type="button"
              title="图片设置"
              aria-label="图片设置"
              aria-expanded={settingsOpen}
              onPointerDown={(event) => {
                if (event.button === 0) setSettingsOpen((open) => !open);
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') setSettingsOpen((open) => !open);
              }}
            >
              <Settings size={18} />
            </button>
            {settingsOpen && (
              <div className="video-settings-popover image-settings-popover" role="dialog" aria-label="图片设置">
                <label className="video-settings-row">
                  <span>播放模式</span>
                  <select
                    aria-label="播放模式"
                    value={playbackMode}
                    onChange={(event) => onPlaybackModeChange(event.currentTarget.value as ViewerPlaybackMode)}
                  >
                    {playbackModeOptions.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
                <button className="video-settings-action" type="button" onClick={onRotate}>
                  <span>图片旋转</span>
                  <span><output>{asset.rotation || 0}°</output><RotateCw size={16} /></span>
                </button>
                <button
                  className="video-settings-action danger"
                  type="button"
                  disabled={deleting}
                  onClick={() => {
                    setSettingsOpen(false);
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
                    setSettingsOpen(false);
                    onDeleteRecord();
                  }}
                >
                  <span>删除记录</span>
                  <Database size={16} />
                </button>
              </div>
            )}
          </div>
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
  );
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function isMobileViewerInteraction() {
  return typeof window !== 'undefined' && window.matchMedia('(hover: none) and (pointer: coarse)').matches;
}

function containRect(container: DOMRect, naturalWidth: number, naturalHeight: number) {
  const naturalRatio = naturalWidth / naturalHeight;
  const containerRatio = container.width / container.height;
  if (containerRatio > naturalRatio) {
    const height = container.height;
    const width = height * naturalRatio;
    return {
      height,
      left: container.left + (container.width - width) / 2,
      top: container.top,
      width,
    };
  }
  const width = container.width;
  const height = width / naturalRatio;
  return {
    height,
    left: container.left,
    top: container.top + (container.height - height) / 2,
    width,
  };
}
