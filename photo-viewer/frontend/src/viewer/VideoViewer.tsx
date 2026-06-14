import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { Pause, Play, Volume2, VolumeX } from 'lucide-react';
import type { Asset } from '../types/api';
import { assetPreviewUrl, assetSubtitleUrl, assetVideoProxyUrl, assetVideoUrl } from '../api/client';
import { formatDuration } from '../utils/format';
import { isQuarterTurn, normalizeRotation, rotationStyle } from '../utils/rotation';
import { loadViewerPrefs, viewerPrefsChanged, type ViewerPrefs } from '../utils/viewerPrefs';

interface Props {
  asset: Asset;
  playbackRate: number;
  selectedSubtitleId: string;
  subtitlesEnabled: boolean;
}

const autoplayDelayMs = 800;
const videoAudioStorageKey = 'lpicto-video-audio';

interface VideoAudioPreference {
  lastVolume: number;
  muted: boolean;
  volume: number;
}

let sharedVideoAudio: VideoAudioPreference | null = null;

export default function VideoViewer({ asset, playbackRate, selectedSubtitleId, subtitlesEnabled }: Props) {
  const ref = useRef<HTMLVideoElement | null>(null);
  const frameRef = useRef<HTMLDivElement | null>(null);
  const autoplayTimer = useRef<number | null>(null);
  const [audio, setAudio] = useState<VideoAudioPreference>(() => loadVideoAudioPreference());
  const [prefs, setPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());
  const [forceProxy, setForceProxy] = useState(false);
  const [paused, setPaused] = useState(true);
  const [duration, setDuration] = useState(asset.duration ?? 0);
  const [currentTime, setCurrentTime] = useState(0);
  const [frameSize, setFrameSize] = useState({ width: 0, height: 0 });
  const source = useMemo(() => {
    if (forceProxy && asset.videoProxyStatus === 'ready') return assetVideoProxyUrl(asset);
    return assetVideoUrl(asset);
  }, [asset, forceProxy]);

  const subtitleSource = subtitlesEnabled && selectedSubtitleId ? assetSubtitleUrl(asset, selectedSubtitleId) : '';

  const mediaStyle = useMemo(() => {
    const rotation = normalizeRotation(asset.rotation);
    const box = videoFitBox(asset, frameSize);
    const style: CSSProperties = {
      height: `${box.height}px`,
      left: `${box.left}px`,
      top: `${box.top}px`,
      width: `${box.width}px`,
    };
    const transforms: string[] = [];
    if (rotation !== 0) {
      transforms.push(`rotate(${rotation}deg)`);
    }
    style.transformOrigin = 'center';
    if (transforms.length > 0) {
      style.transform = transforms.join(' ');
    }
    return Object.keys(style).length > 0 ? style : undefined;
  }, [asset, frameSize.height, frameSize.width]);

  const togglePlay = () => {
    const video = ref.current;
    if (!video) return;
    if (video.paused) {
      void video.play().catch(() => undefined);
    } else {
      video.pause();
    }
  };

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.code !== 'Space') return;
      event.preventDefault();
      togglePlay();
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  useEffect(() => {
    if (ref.current) {
      ref.current.playbackRate = playbackRate;
    }
  }, [playbackRate, source]);

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
    clearAutoplayTimer();
    const video = ref.current;
    if (!video) return clearAutoplayTimer;
    if (!prefs.videoAutoplay) {
      video.pause();
      return clearAutoplayTimer;
    }
    autoplayTimer.current = window.setTimeout(() => {
      if (ref.current) {
        ref.current.playbackRate = playbackRate;
        void ref.current.play().catch(() => undefined);
      }
      autoplayTimer.current = null;
    }, autoplayDelayMs);
    return clearAutoplayTimer;
  }, [asset.id, playbackRate, prefs.videoAutoplay, source]);

  useEffect(() => {
    const video = ref.current;
    if (!video) return;
    for (const track of Array.from(video.textTracks)) {
      track.mode = subtitlesEnabled && selectedSubtitleId ? 'showing' : 'disabled';
    }
  }, [selectedSubtitleId, subtitlesEnabled, subtitleSource]);

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
    setForceProxy(false);
  }, [asset.id]);

  function clearAutoplayTimer() {
    if (autoplayTimer.current === null) return;
    window.clearTimeout(autoplayTimer.current);
    autoplayTimer.current = null;
  }

  if (!source) {
    return (
      <div className="video-pending">
        {asset.videoPosterStatus === 'ready' && (
          <img src={assetPreviewUrl(asset)} alt={asset.filename} style={rotationStyle(asset)} />
        )}
        <div>{asset.videoProxyStatus === 'error' ? '视频代理生成失败' : '视频预览生成中'}</div>
      </div>
    );
  }

  return (
    <div className="video-stage">
      <div className="video-frame" ref={frameRef}>
        <video
          ref={ref}
          className="viewer-video"
          src={source}
          poster={asset.videoPosterStatus === 'ready' ? assetPreviewUrl(asset) : undefined}
          playsInline
          style={mediaStyle}
          onClick={togglePlay}
          onDurationChange={(event) => setDuration(event.currentTarget.duration || asset.duration || 0)}
          onError={() => {
            if (asset.videoProxyStatus === 'ready') setForceProxy(true);
          }}
          onLoadedMetadata={(event) => {
            const savedAudio = loadVideoAudioPreference();
            event.currentTarget.volume = savedAudio.volume;
            event.currentTarget.muted = savedAudio.muted;
            setAudio(savedAudio);
            setDuration(event.currentTarget.duration || asset.duration || 0);
            setPaused(event.currentTarget.paused);
            event.currentTarget.playbackRate = playbackRate;
          }}
          onPause={() => setPaused(true)}
          onPlay={() => setPaused(false)}
          onTimeUpdate={(event) => setCurrentTime(event.currentTarget.currentTime)}
          onVolumeChange={(event) => {
            const video = event.currentTarget;
            setAudio(saveVideoAudioPreference(video.volume, video.muted));
          }}
        >
          {subtitleSource && <track key={subtitleSource} kind="subtitles" src={subtitleSource} label="字幕" default />}
        </video>
      </div>
      <div className="video-control-zone">
        <div className="video-controls">
          <button type="button" onClick={togglePlay} title={paused ? '播放' : '暂停'}>
            {paused ? <Play size={18} /> : <Pause size={18} />}
          </button>
          <span>{formatDuration(currentTime)}</span>
          <input
            aria-label="播放进度"
            max={duration || 0}
            min={0}
            step={0.01}
            type="range"
            value={Math.min(currentTime, duration || currentTime)}
            onChange={(event) => {
              const next = Number(event.target.value);
              setCurrentTime(next);
              if (ref.current) ref.current.currentTime = next;
            }}
          />
          <span>{formatDuration(duration)}</span>
          <button
            type="button"
            onClick={() => {
              if (!ref.current) return;
              if (audio.muted || ref.current.volume === 0) {
                ref.current.volume = audio.lastVolume || 1;
                ref.current.muted = false;
              } else {
                ref.current.muted = true;
              }
            }}
            title={audio.muted ? '取消静音' : '静音'}
          >
            {audio.muted ? <VolumeX size={18} /> : <Volume2 size={18} />}
          </button>
          <input
            aria-label="音量"
            max={1}
            min={0}
            step={0.01}
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
    </div>
  );
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
  sharedVideoAudio = {
    lastVolume: lastVolume > 0 ? lastVolume : 1,
    muted: Boolean(parsed?.muted),
    volume,
  };
  return sharedVideoAudio;
}

function saveVideoAudioPreference(volumeValue: number, muted: boolean): VideoAudioPreference {
  const previous = loadVideoAudioPreference();
  const volume = clampVolume(volumeValue);
  const lastVolume = !muted && volume > 0 ? volume : previous.lastVolume;
  const next = {
    lastVolume: lastVolume > 0 ? lastVolume : 1,
    muted: muted || volume === 0,
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

function clampVolume(value: number) {
  if (!Number.isFinite(value)) return 1;
  return Math.min(1, Math.max(0, value));
}

function videoFitBox(asset: Asset, frameSize: { width: number; height: number }) {
  const frameWidth = frameSize.width || 1;
  const frameHeight = frameSize.height || 1;
  const naturalWidth = Math.max(1, asset.width || frameWidth);
  const naturalHeight = Math.max(1, asset.height || frameHeight);
  const rotatedWidth = isQuarterTurn(asset) ? naturalHeight : naturalWidth;
  const rotatedHeight = isQuarterTurn(asset) ? naturalWidth : naturalHeight;
  const scale = Math.min(frameWidth / rotatedWidth, frameHeight / rotatedHeight);
  const visibleWidth = rotatedWidth * scale;
  const visibleHeight = rotatedHeight * scale;
  const width = isQuarterTurn(asset) ? visibleHeight : visibleWidth;
  const height = isQuarterTurn(asset) ? visibleWidth : visibleHeight;
  return {
    height,
    left: (frameWidth - width) / 2,
    top: (frameHeight - height) / 2,
    width,
  };
}
