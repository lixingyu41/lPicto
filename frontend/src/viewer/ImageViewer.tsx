import { useEffect, useMemo, useRef, useState } from 'react';
import type { Asset } from '../types/api';
import { assetOriginalUrl, assetPreviewUrl } from '../api/client';
import { loadViewerPrefs, viewerPrefsChanged, type ViewerPrefs } from '../utils/viewerPrefs';
import { rotatedContainStyle } from '../utils/rotation';

interface Props {
  asset: Asset;
}

interface ZoomState {
  active: boolean;
  backgroundHeight: number;
  backgroundWidth: number;
  backgroundX: number;
  backgroundY: number;
}

export default function ImageViewer({ asset }: Props) {
  const imageRef = useRef<HTMLImageElement | null>(null);
  const stageRef = useRef<HTMLDivElement | null>(null);
  const [prefs, setPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());
  const [zoom, setZoom] = useState<ZoomState>({
    active: false,
    backgroundHeight: 0,
    backgroundWidth: 0,
    backgroundX: 0,
    backgroundY: 0,
  });
  const [stageSize, setStageSize] = useState({ height: 0, width: 0 });
  const src = asset.browserPlayable ? assetOriginalUrl(asset) : assetPreviewUrl(asset);
  const imageStyle = useMemo(
    () => rotatedContainStyle(asset, stageSize),
    [asset, stageSize.height, stageSize.width],
  );

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
    if (!zoom.active) return;
    function endZoom() {
      setZoom((current) => ({ ...current, active: false }));
    }
    window.addEventListener('mouseup', endZoom);
    return () => window.removeEventListener('mouseup', endZoom);
  }, [zoom.active]);

  useEffect(() => {
    setZoom({
      active: false,
      backgroundHeight: 0,
      backgroundWidth: 0,
      backgroundX: 0,
      backgroundY: 0,
    });
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

  function updateZoom(clientX: number, clientY: number) {
    const image = imageRef.current;
    const stage = stageRef.current;
    if (!image || !stage) return;
    const stageRect = stage.getBoundingClientRect();
    const naturalWidth = image.naturalWidth || asset.width || stageRect.width;
    const naturalHeight = image.naturalHeight || asset.height || stageRect.height;
    const imageRect = containRect(stageRect, naturalWidth, naturalHeight);
    if (naturalWidth <= 0 || naturalHeight <= 0 || imageRect.width <= 0 || imageRect.height <= 0) return;

    const imageX = clampNumber(clientX - imageRect.left, 0, imageRect.width);
    const imageY = clampNumber(clientY - imageRect.top, 0, imageRect.height);
    const sourceX = (imageX / imageRect.width) * naturalWidth;
    const sourceY = (imageY / imageRect.height) * naturalHeight;
    const stageX = clientX - stageRect.left;
    const stageY = clientY - stageRect.top;
    const pixelsPerSourcePixel =
      prefs.zoomMode === 'pixels'
        ? Math.min(stageRect.width, stageRect.height) / prefs.zoomPixelArea
        : (imageRect.width * prefs.zoomScale) / naturalWidth;
    const backgroundWidth = naturalWidth * pixelsPerSourcePixel;
    const backgroundHeight = naturalHeight * pixelsPerSourcePixel;

    setZoom((current) => ({
      ...current,
      backgroundHeight,
      backgroundWidth,
      backgroundX: stageX - sourceX * pixelsPerSourcePixel,
      backgroundY: stageY - sourceY * pixelsPerSourcePixel,
    }));
  }

  return (
    <div
      ref={stageRef}
      className={zoom.active ? 'image-stage zooming' : 'image-stage'}
      onMouseDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        updateZoom(event.clientX, event.clientY);
        setZoom((current) => ({ ...current, active: true }));
      }}
      onMouseMove={(event) => {
        if (!zoom.active) return;
        if (event.buttons !== 1) {
          setZoom((current) => ({ ...current, active: false }));
          return;
        }
        updateZoom(event.clientX, event.clientY);
      }}
      onMouseUp={() => setZoom((current) => ({ ...current, active: false }))}
    >
      <img
        ref={imageRef}
        className="viewer-image"
        src={src}
        alt={asset.filename}
        draggable={false}
        style={imageStyle}
        onDragStart={(event) => event.preventDefault()}
      />
      {zoom.active && (
        <div
          className="image-zoom-layer"
          style={{
            backgroundImage: `url("${src}")`,
            backgroundPosition: `${zoom.backgroundX}px ${zoom.backgroundY}px`,
            backgroundSize: `${zoom.backgroundWidth}px ${zoom.backgroundHeight}px`,
          }}
        />
      )}
    </div>
  );
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
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
