import { useEffect, useRef, useState } from 'react';
import { Database, Maximize2, Minimize2, Pause, Play, Settings, Trash2, Volume2, VolumeX } from 'lucide-react';
import { api, assetAudioCoverUrl, assetAudioUrl } from '../api/client';
import type { Asset } from '../types/api';
import { formatDuration } from '../utils/format';
import { playbackModeOptions, playbackRates, type ViewerPlaybackMode, type ViewerPrefs } from '../utils/viewerPrefs';
import { viewerAudioOutputBridge } from './audioOutputBridge';
import type { ViewerMediaLayerMode } from './mediaLayer';

interface Props {
  asset: Asset;
  deleting: boolean;
  fullscreen: boolean;
  layerMode: ViewerMediaLayerMode;
  mediaDetailsOpen: boolean;
  playbackRate: number;
  preloadEnabled: boolean;
  viewerPrefs: ViewerPrefs;
  onDelete: () => void;
  onDeleteRecord: () => void;
  onMediaError: (assetId: number, cacheKey: string, message: string) => void;
  onMediaReady: (assetId: number, cacheKey: string) => void;
  onPlaybackEnded: () => void;
  onPlaybackModeChange: (value: ViewerPlaybackMode) => void;
  onPlaybackRateChange: (value: number) => void;
  onToggleMediaDetails: () => void;
  onToggleFullscreen: () => void;
}

interface AudioPreference {
  lastVolume: number;
  muted: boolean;
  version: number;
  volume: number;
}

const audioPreferenceKey = 'lpicto-video-audio';
const defaultAudioPreference: AudioPreference = { lastVolume: 1, muted: false, version: 1, volume: 1 };

export default function AudioViewer({
  asset,
  deleting,
  fullscreen,
  layerMode,
  mediaDetailsOpen,
  playbackRate,
  preloadEnabled,
  viewerPrefs,
  onDelete,
  onDeleteRecord,
  onMediaError,
  onMediaReady,
  onPlaybackEnded,
  onPlaybackModeChange,
  onPlaybackRateChange,
  onToggleMediaDetails,
  onToggleFullscreen,
}: Props) {
  const ref = useRef<HTMLAudioElement | null>(null);
  const readyKey = useRef('');
  const [sourceReady, setSourceReady] = useState(asset.browserPlayable);
  const [proxyProgress, setProxyProgress] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(asset.duration ?? 0);
  const [audioPreference, setAudioPreference] = useState(loadAudioPreference);
  const [settingsOpen, setSettingsOpen] = useState(false);

  useEffect(() => {
    readyKey.current = '';
    setSourceReady(asset.browserPlayable);
    setProxyProgress(0);
    setPlaying(false);
    setCurrentTime(0);
    setDuration(asset.duration ?? 0);
  }, [asset.id, asset.cacheKey, asset.browserPlayable, asset.duration]);

  useEffect(() => {
    if (!preloadEnabled || asset.browserPlayable) return;
    const controller = new AbortController();
    let timer = 0;
    let live = true;
    const priority = layerMode === 'active' ? 'current' : 'preload';
    async function poll() {
      try {
        const status = await api.audioProxyStatus(asset.id, controller.signal);
        if (!live) return;
        setProxyProgress(status.progress);
        if (status.cached || status.status === 'ready') {
          setSourceReady(true);
          return;
        }
        if (status.status === 'error') {
          onMediaError(asset.id, asset.cacheKey, status.error || '音频兼容转换失败');
          return;
        }
        timer = window.setTimeout(() => void poll(), 800);
      } catch (err) {
        if (!live || controller.signal.aborted) return;
        onMediaError(asset.id, asset.cacheKey, err instanceof Error ? err.message : '音频兼容转换失败');
      }
    }
    void api.startAudioProxy(asset.id, priority, controller.signal)
      .then((status) => {
        if (!live) return;
        setProxyProgress(status.progress);
        if (status.cached || status.status === 'ready') setSourceReady(true);
        else void poll();
      })
      .catch((err) => {
        if (!live || controller.signal.aborted) return;
        onMediaError(asset.id, asset.cacheKey, err instanceof Error ? err.message : '音频兼容转换失败');
      });
    return () => {
      live = false;
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [asset.browserPlayable, asset.cacheKey, asset.id, layerMode, onMediaError, preloadEnabled]);

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    element.playbackRate = playbackRate;
  }, [playbackRate, sourceReady]);

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    element.volume = audioPreference.volume;
    element.muted = audioPreference.muted;
  }, [audioPreference, sourceReady]);

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    if (layerMode !== 'active') {
      element.pause();
      return;
    }
    if (element.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA && (viewerPrefs.videoAutoplay || viewerPrefs.playbackMode === 'continuous')) {
      void element.play().catch(() => undefined);
    }
  }, [layerMode, sourceReady, viewerPrefs.playbackMode, viewerPrefs.videoAutoplay]);

  const markReady = () => {
    const key = `${asset.id}:${asset.cacheKey}`;
    if (readyKey.current === key) return;
    readyKey.current = key;
    onMediaReady(asset.id, asset.cacheKey);
  };

  const updatePreference = (next: AudioPreference) => {
    setAudioPreference(next);
    localStorage.setItem(audioPreferenceKey, JSON.stringify(next));
  };

  const togglePlayback = () => {
    if (layerMode !== 'active' || !sourceReady) return;
    const element = ref.current;
    if (!element) return;
    if (element.paused) {
      viewerAudioOutputBridge.prime();
      void element.play().catch(() => undefined);
    }
    else element.pause();
  };

  const handleStageClick = (event: React.MouseEvent<HTMLDivElement>) => {
    if (event.target instanceof Element && event.target.closest('[data-viewer-wheel-control]')) return;
    togglePlayback();
  };

  return (
    <div
      className={`audio-stage audio-stage-${layerMode}${layerMode === 'active' && sourceReady ? ' is-playable' : ''}`}
      title={sourceReady ? (playing ? '单击暂停' : '单击播放') : '正在准备音频'}
      onClick={handleStageClick}
    >
      <div
        aria-disabled={!sourceReady}
        aria-label={playing ? '暂停音频' : '播放音频'}
        className={`audio-artwork-shell${layerMode === 'active' && sourceReady ? ' is-playable' : ''}`}
        role={layerMode === 'active' ? 'button' : undefined}
        tabIndex={layerMode === 'active' && sourceReady ? 0 : -1}
        title={sourceReady ? (playing ? '单击暂停' : '单击播放') : '正在准备音频'}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' && event.key !== ' ') return;
          event.preventDefault();
          togglePlayback();
        }}
      >
        <img className="audio-artwork" src={assetAudioCoverUrl()} alt="" draggable={false} />
        {!sourceReady && (
          <div className="audio-proxy-progress" role="status">
            <span>正在准备无损兼容音频</span>
            <progress max={1} value={proxyProgress} />
          </div>
        )}
      </div>
      {preloadEnabled && sourceReady && (
        <audio
          ref={ref}
          preload="auto"
          src={assetAudioUrl(asset)}
          loop={viewerPrefs.playbackMode === 'single'}
          onCanPlay={markReady}
          onLoadedMetadata={(event) => setDuration(Number.isFinite(event.currentTarget.duration) ? event.currentTarget.duration : asset.duration ?? 0)}
          onTimeUpdate={(event) => setCurrentTime(event.currentTarget.currentTime)}
          onPlay={() => setPlaying(true)}
          onPlaying={() => {
            if (layerMode === 'active') viewerAudioOutputBridge.mediaStarted(`${asset.id}:${asset.cacheKey}`);
          }}
          onPause={() => {
            setPlaying(false);
            if (layerMode === 'active') viewerAudioOutputBridge.mediaStopped(`${asset.id}:${asset.cacheKey}`);
          }}
          onEnded={() => {
            setPlaying(false);
            if (viewerPrefs.playbackMode === 'continuous') onPlaybackEnded();
            viewerAudioOutputBridge.mediaStopped(`${asset.id}:${asset.cacheKey}`);
          }}
          onError={() => onMediaError(asset.id, asset.cacheKey, '音频加载失败')}
        />
      )}
      {layerMode === 'active' && (
        <div className="audio-controls" data-viewer-wheel-control>
          <button type="button" aria-label={playing ? '暂停' : '播放'} onClick={togglePlayback}>
            {playing ? <Pause size={20} fill="currentColor" /> : <Play size={20} fill="currentColor" />}
          </button>
          <span>{formatDuration(currentTime)}</span>
          <input
            aria-label="音频进度"
            type="range"
            min={0}
            max={Math.max(duration, 0.01)}
            step={0.01}
            value={Math.min(currentTime, Math.max(duration, 0.01))}
            onChange={(event) => {
              const value = Number(event.target.value);
              setCurrentTime(value);
              if (ref.current) ref.current.currentTime = value;
            }}
          />
          <span>{formatDuration(duration)}</span>
          <button type="button" aria-label={audioPreference.muted ? '取消静音' : '静音'} onClick={() => updatePreference({ ...audioPreference, muted: !audioPreference.muted })}>
            {audioPreference.muted ? <VolumeX size={18} /> : <Volume2 size={18} />}
          </button>
          <input
            aria-label="音量"
            className="audio-volume"
            type="range"
            min={0}
            max={1}
            step={0.01}
            value={audioPreference.volume}
            onChange={(event) => {
              const volume = Number(event.target.value);
              updatePreference({ ...audioPreference, lastVolume: volume > 0 ? volume : audioPreference.lastVolume, muted: volume === 0, volume });
            }}
          />
          <button className={settingsOpen ? 'active' : ''} type="button" aria-label="音频设置" onClick={() => setSettingsOpen((open) => !open)}>
            <Settings size={18} />
          </button>
          <button type="button" aria-label={fullscreen ? '退出全屏' : '全屏'} onClick={onToggleFullscreen}>
            {fullscreen ? <Minimize2 size={18} /> : <Maximize2 size={18} />}
          </button>
          {settingsOpen && (
            <div className="audio-settings-popover">
              <label>倍速<select value={playbackRate} onChange={(event) => onPlaybackRateChange(Number(event.target.value))}>
                {playbackRates.map((rate) => <option key={rate} value={rate}>{rate}×</option>)}
              </select></label>
              <label>播放方式<select value={viewerPrefs.playbackMode} onChange={(event) => onPlaybackModeChange(event.target.value as ViewerPlaybackMode)}>
                {playbackModeOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select></label>
              <button className={mediaDetailsOpen ? 'active' : ''} type="button" onClick={onToggleMediaDetails}><Database size={16} />媒体详情</button>
              <button disabled={deleting} type="button" onClick={onDeleteRecord}><Database size={16} />删除记录</button>
              <button className="danger" disabled={deleting} type="button" onClick={onDelete}><Trash2 size={16} />删除媒体</button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function loadAudioPreference(): AudioPreference {
  try {
    const value = JSON.parse(localStorage.getItem(audioPreferenceKey) ?? '') as Partial<AudioPreference>;
    const volume = Number.isFinite(value.volume) ? Math.min(1, Math.max(0, value.volume!)) : defaultAudioPreference.volume;
    return {
      lastVolume: Number.isFinite(value.lastVolume) ? Math.min(1, Math.max(0.01, value.lastVolume!)) : volume || 1,
      muted: Boolean(value.muted),
      version: 1,
      volume,
    };
  } catch {
    return defaultAudioPreference;
  }
}
