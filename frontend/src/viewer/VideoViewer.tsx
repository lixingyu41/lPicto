import { useEffect, useMemo, useRef, useState } from 'react';
import { Pause, Play, Volume2, VolumeX } from 'lucide-react';
import type { Asset } from '../types/api';
import { api, assetPreviewUrl, assetSubtitleUrl, assetVideoProxyUrl, assetVideoUrl } from '../api/client';
import { formatDuration } from '../utils/format';
import { normalizeRotation, rotatedContainStyle } from '../utils/rotation';
import { loadViewerPrefs, viewerPrefsChanged, type ViewerPrefs } from '../utils/viewerPrefs';

interface Props {
  asset: Asset;
  playbackRate: number;
  selectedSubtitleId: string;
  subtitlesEnabled: boolean;
}

const autoplayDelayMs = 800;
const proxyPollMs = 3000;
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
  const resumeTimer = useRef<number | null>(null);
  const proxyPollTimer = useRef<number | null>(null);
  const wantsPlaying = useRef(false);
  const resumeAttempts = useRef(0);
  const [liveAsset, setLiveAsset] = useState(asset);
  const [audio, setAudio] = useState<VideoAudioPreference>(() => loadVideoAudioPreference());
  const [prefs, setPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());
  const [proxyFailed, setProxyFailed] = useState(false);
  const [sourceFailed, setSourceFailed] = useState(false);
  const [hasPlaybackStarted, setHasPlaybackStarted] = useState(false);
  const [playError, setPlayError] = useState('');
  const [paused, setPaused] = useState(true);
  const [duration, setDuration] = useState(asset.duration ?? 0);
  const [currentTime, setCurrentTime] = useState(0);
  const [scrubTime, setScrubTime] = useState<number | null>(null);
  const [frameSize, setFrameSize] = useState({ width: 0, height: 0 });
  const playbackAsset = liveAsset.id === asset.id && liveAsset.cacheKey === asset.cacheKey ? liveAsset : asset;
  const source = useMemo(() => {
    if (playbackAsset.videoProxyStatus === 'ready' && !proxyFailed) {
      return assetVideoProxyUrl(playbackAsset);
    }
    return assetVideoUrl(asset);
  }, [asset, playbackAsset, proxyFailed]);

  const subtitleSource = subtitlesEnabled && selectedSubtitleId ? assetSubtitleUrl(asset, selectedSubtitleId) : '';
  const canPlay = Boolean(source) && !sourceFailed;
  const posterSource = asset.thumbStatus === 'ready' ? assetPreviewUrl(asset) : '';
  const showPosterLayer = Boolean(posterSource) && (!canPlay || (!hasPlaybackStarted && paused && currentTime <= 0.01));
  const statusLabel = videoStatusLabel(playbackAsset, sourceFailed);
  const displayedTime = scrubTime ?? currentTime;

  const mediaStyle = useMemo(() => {
    const rotation = normalizeRotation(asset.rotation);
    if (rotation === 0) return undefined;
    return { ...rotatedContainStyle(asset, frameSize), bottom: 'auto', right: 'auto' };
  }, [asset, frameSize.height, frameSize.width]);

  const togglePlay = () => {
    const video = ref.current;
    if (!video || !canPlay) return;
    if (video.paused) {
      wantsPlaying.current = true;
      resumeAttempts.current = 0;
      void startPlayback(video);
    } else {
      wantsPlaying.current = false;
      clearResumeTimer();
      video.pause();
    }
  };

  async function startPlayback(video: HTMLVideoElement) {
    wantsPlaying.current = true;
    setPlayError('');
    try {
      await video.play();
      resumeAttempts.current = 0;
      return;
    } catch (err) {
      let failure: unknown = err;
      if (!video.muted) {
        video.muted = true;
        setAudio(saveVideoAudioPreference(video.volume, true));
        try {
          await video.play();
          resumeAttempts.current = 0;
          return;
        } catch (retryErr) {
          failure = retryErr;
        }
      }
      wantsPlaying.current = false;
      setPlayError(playbackFailureMessage(failure));
    }
  }

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
    if (!canPlay) return clearAutoplayTimer;
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
  }, [asset.id, canPlay, playbackRate, prefs.videoAutoplay, source]);

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
    setSourceFailed(false);
    setHasPlaybackStarted(false);
    setPlayError('');
    setPaused(true);
    setCurrentTime(0);
    setScrubTime(null);
    setDuration(asset.duration ?? 0);
    setLiveAsset(asset);
    setProxyFailed(false);
    wantsPlaying.current = false;
    resumeAttempts.current = 0;
    clearResumeTimer();
  }, [asset.id]);

  useEffect(() => {
    setSourceFailed(false);
    setPlayError('');
  }, [source]);

  useEffect(() => {
    if (playbackAsset.videoProxyStatus === 'ready' || playbackAsset.videoProxyStatus === 'error') {
      clearProxyPollTimer();
      return clearProxyPollTimer;
    }
    clearProxyPollTimer();
    const poll = async () => {
      let nextStatus = playbackAsset.videoProxyStatus;
      try {
        const next = await api.asset(asset.id);
        nextStatus = next.videoProxyStatus;
        if (next.cacheKey === asset.cacheKey) {
          if (!(next.videoProxyStatus === 'ready' && hasPlaybackStarted)) {
            setLiveAsset(next);
          }
        }
      } catch {
        // Keep the current source usable if the lightweight status poll fails.
      } finally {
        if (nextStatus !== 'ready' && nextStatus !== 'error') {
          proxyPollTimer.current = window.setTimeout(poll, proxyPollMs);
        }
      }
    };
    proxyPollTimer.current = window.setTimeout(poll, proxyPollMs);
    return clearProxyPollTimer;
  }, [asset.id, asset.cacheKey, hasPlaybackStarted, playbackAsset.videoProxyStatus]);

  useEffect(() => () => {
    clearResumeTimer();
    clearProxyPollTimer();
  }, []);

  function clearAutoplayTimer() {
    if (autoplayTimer.current === null) return;
    window.clearTimeout(autoplayTimer.current);
    autoplayTimer.current = null;
  }

  function clearResumeTimer() {
    if (resumeTimer.current === null) return;
    window.clearTimeout(resumeTimer.current);
    resumeTimer.current = null;
  }

  function clearProxyPollTimer() {
    if (proxyPollTimer.current === null) return;
    window.clearTimeout(proxyPollTimer.current);
    proxyPollTimer.current = null;
  }

  function commitSeek(value = scrubTime) {
    if (value === null || !ref.current) return;
    const next = clampTime(value, duration || ref.current.duration || value);
    ref.current.currentTime = next;
    setCurrentTime(next);
    setScrubTime(null);
  }

  function scheduleResumeAfterUnexpectedPause(video: HTMLVideoElement) {
    if (!wantsPlaying.current || video.ended || resumeAttempts.current >= 2) return;
    clearResumeTimer();
    resumeTimer.current = window.setTimeout(() => {
      resumeTimer.current = null;
      const current = ref.current;
      if (!current || !wantsPlaying.current || !current.paused || current.ended) return;
      resumeAttempts.current += 1;
      void startPlayback(current);
    }, 80);
  }

  return (
    <div className={canPlay ? 'video-stage' : 'video-stage video-stage-pending'}>
      <div className="video-frame" ref={frameRef}>
        {canPlay && (
          <video
            ref={ref}
            className="viewer-video"
            src={source}
            poster={posterSource || undefined}
            preload="auto"
            playsInline
            style={mediaStyle}
            onClick={togglePlay}
            onDurationChange={(event) => setDuration(event.currentTarget.duration || asset.duration || 0)}
            onError={() => {
              if (!proxyFailed && playbackAsset.videoProxyStatus === 'ready' && asset.browserPlayable) {
                setProxyFailed(true);
                return;
              }
              setSourceFailed(true);
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
            onPause={(event) => {
              setPaused(true);
              scheduleResumeAfterUnexpectedPause(event.currentTarget);
            }}
            onPlay={() => {
              setPlayError('');
              clearResumeTimer();
              setPaused(false);
              setHasPlaybackStarted(true);
            }}
            onTimeUpdate={(event) => {
              const next = event.currentTarget.currentTime;
              if (scrubTime === null) {
                setCurrentTime(next);
              }
              if (next > 0.01) setHasPlaybackStarted(true);
            }}
            onVolumeChange={(event) => {
              const video = event.currentTarget;
              setAudio(saveVideoAudioPreference(video.volume, video.muted));
            }}
          >
            {subtitleSource && <track key={subtitleSource} kind="subtitles" src={subtitleSource} label="字幕" default />}
          </video>
        )}
        {showPosterLayer && (
          <button
            className={canPlay ? 'video-poster-layer playable' : 'video-poster-layer'}
            type="button"
            disabled={!canPlay}
            onClick={togglePlay}
          >
            <img src={posterSource} alt={asset.filename} style={mediaStyle} />
            {canPlay ? (
              <>
                <span className="video-big-play">
                  <Play size={34} fill="currentColor" />
                </span>
                {playError && <span className="video-play-error">{playError}</span>}
              </>
            ) : (
              <span className="video-status-badge">{statusLabel}</span>
            )}
          </button>
        )}
        {!posterSource && !canPlay && <div className="video-pending">{statusLabel}</div>}
      </div>
      <div className="video-control-zone">
        <div className="video-controls">
          <button type="button" disabled={!canPlay} onClick={togglePlay} title={paused ? '播放' : '暂停'}>
            {paused ? <Play size={18} /> : <Pause size={18} />}
          </button>
          <span>{formatDuration(displayedTime)}</span>
          <input
            aria-label="播放进度"
            max={duration || 0}
            min={0}
            step={0.01}
            disabled={!canPlay}
            type="range"
            value={Math.min(displayedTime, duration || displayedTime)}
            onBlur={(event) => commitSeek(Number(event.currentTarget.value))}
            onChange={(event) => {
              const next = Number(event.target.value);
              setScrubTime(next);
            }}
            onKeyUp={(event) => commitSeek(Number(event.currentTarget.value))}
            onPointerCancel={(event) => commitSeek(Number(event.currentTarget.value))}
            onPointerDown={(event) => setScrubTime(Number(event.currentTarget.value))}
            onPointerUp={(event) => commitSeek(Number(event.currentTarget.value))}
          />
          <span>{formatDuration(duration)}</span>
          <button
            type="button"
            disabled={!canPlay}
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
            disabled={!canPlay}
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

function videoStatusLabel(asset: Asset, sourceFailed: boolean) {
  if (sourceFailed) return '视频加载失败';
  if (asset.videoProxyStatus === 'error') return '视频转码失败';
  if (!asset.browserPlayable && asset.videoProxyStatus !== 'ready') return '等待转码';
  return '无法播放';
}

function playbackFailureMessage(err: unknown) {
  if (err instanceof DOMException && err.message) return `播放失败：${err.message}`;
  if (err instanceof Error && err.message) return `播放失败：${err.message}`;
  return '播放失败';
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

function clampTime(value: number, max: number) {
  if (!Number.isFinite(value)) return 0;
  if (!Number.isFinite(max) || max <= 0) return Math.max(0, value);
  return Math.min(max, Math.max(0, value));
}
