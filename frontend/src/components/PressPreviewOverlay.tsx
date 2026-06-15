import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { assetThumbUrl } from '../api/client';
import type { Asset } from '../types/api';
import { rotatedContainStyle } from '../utils/rotation';

interface Props {
  asset: Asset | null;
}

export default function PressPreviewOverlay({ asset }: Props) {
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const [overlaySize, setOverlaySize] = useState({ height: 0, width: 0 });

  const mediaStyle = useMemo<CSSProperties | undefined>(() => {
    if (!asset) return undefined;
    return rotatedContainStyle(asset, overlaySize);
  }, [asset, overlaySize.height, overlaySize.width]);

  useEffect(() => {
    const overlay = overlayRef.current;
    if (!overlay) return;
    const update = () => {
      const rect = overlay.getBoundingClientRect();
      setOverlaySize({ height: rect.height, width: rect.width });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(overlay);
    return () => observer.disconnect();
  }, [asset?.id]);

  if (!asset) return null;
  return (
    <div className="press-preview-overlay" ref={overlayRef}>
      <img
        key={asset.id}
        className="press-preview-media"
        src={assetThumbUrl(asset)}
        alt={asset.filename}
        draggable={false}
        style={mediaStyle}
      />
    </div>
  );
}
