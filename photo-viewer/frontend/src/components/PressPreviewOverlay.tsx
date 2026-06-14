import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { assetPreviewUrl, assetVideoProxyUrl, assetVideoUrl } from '../api/client';
import type { Asset } from '../types/api';
import { rotatedContainStyle } from '../utils/rotation';

interface Props {
  asset: Asset | null;
}

export default function PressPreviewOverlay({ asset }: Props) {
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const ignorePauseUntil = useRef(0);
  const [autoPlayVideos, setAutoPlayVideos] = useState(true);
  const [overlaySize, setOverlaySize] = useState({ height: 0, width: 0 });
  const videoSource = useMemo(() => {
    if (!asset || asset.mediaType !== 'video') return null;
    if (asset.browserPlayable) return assetVideoUrl(asset);
    if (asset.videoProxyStatus === 'ready') return assetVideoProxyUrl(asset);
    return null;
  }, [asset]);

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

  useEffect(() => {
    if (!asset || asset.mediaType !== 'video' || !videoSource || !autoPlayVideos) return;
    const timer = window.setTimeout(() => {
      void videoRef.current?.play().catch(() => undefined);
    }, 800);
    return () => {
      ignorePauseUntil.current = Date.now() + 160;
      window.clearTimeout(timer);
    };
  }, [asset?.id, asset?.mediaType, autoPlayVideos, videoSource]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!asset || asset.mediaType !== 'video' || event.code !== 'Space') return;
      const video = videoRef.current;
      if (!video) return;
      event.preventDefault();
      if (video.paused) {
        void video.play();
      } else {
        video.pause();
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [asset]);

  if (!asset) return null;
  return (
    <div className="press-preview-overlay" ref={overlayRef}>
      {asset.mediaType === 'video' && videoSource ? (
        <video
          key={asset.id}
          ref={videoRef}
          className="press-preview-media"
          src={videoSource}
          poster={assetPreviewUrl(asset)}
          controls
          playsInline
          preload="metadata"
          style={mediaStyle}
          onPlay={() => setAutoPlayVideos(true)}
          onPause={() => {
            if (Date.now() < ignorePauseUntil.current) return;
            setAutoPlayVideos(false);
          }}
        />
      ) : (
        <img
          key={asset.id}
          className="press-preview-media"
          src={assetPreviewUrl(asset)}
          alt={asset.filename}
          draggable={false}
          style={mediaStyle}
        />
      )}
    </div>
  );
}
