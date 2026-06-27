import { useEffect, useMemo, useRef, useState } from 'react';
import { Captions, CaptionsOff, Gauge, Maximize2, Minimize2, Pause, Play, RotateCw, Volume2, VolumeX } from 'lucide-react';
import type { Asset, SubtitleInfo, VideoProxyHeartbeat, VideoProxyRuntime } from '../types/api';
import { api, assetPreviewUrl, assetSubtitleUrl, assetVideoProxyUrl, assetVideoUrl } from '../api/client';
import { formatDuration } from '../utils/format';
import { normalizeRotation, rotatedContainStyle } from '../utils/rotation';
import { loadViewerPrefs, nextPlaybackRate, viewerPrefsChanged, type ViewerPrefs } from '../utils/viewerPrefs';

interface Props {
  asset: Asset;
  fullscreen: boolean;
  playbackRate: number;
  selectedSubtitleId: string;
  subtitles: SubtitleInfo[];
  subtitlesEnabled: boolean;
  onPlaybackRateChange: (value: number) => void;
  onRotate: () => void;
  onSelectedSubtitleChange: (value: string) => void;
  onSubtitlesEnabledChange: (value: boolean) => void;
  onToggleFullscreen: () => void;
  onProxyRuntimeChange?: (runtime: VideoProxyRuntime | null) => void;
}

const autoplayDelayMs = 800;
const proxyPollMs = 3000;
const proxyKeepaliveMs = 15000;
const videoAudioStorageKey = 'lpicto-video-audio';
const videoProxyClientStorageKey = 'lpicto-video-proxy-client';

interface VideoAudioPreference {
  lastVolume: number;
  muted: boolean;
  version: number;
  volume: number;
}

let sharedVideoAudio: VideoAudioPreference | null = null;

export default function VideoViewer({
  asset,
  fullscreen,
  playbackRate,
  selectedSubtitleId,
  subtitles,
  subtitlesEnabled,
  onPlaybackRateChange,
  onRotate,
  onSelectedSubtitleChange,
  onSubtitlesEnabledChange,
  onToggleFullscreen,
  onProxyRuntimeChange,
}: Props) {
  const ref = useRef<HTMLVideoElement | null>(null);
  const frameRef = useRef<HTMLDivElement | null>(null);
  const autoplayTimer = useRef<number | null>(null);
  const resumeTimer = useRef<number | null>(null);
  const proxyPollTimer = useRef<number | null>(null);
  const proxyKeepaliveTimer = useRef<number | null>(null);
  const proxyPlayPending = useRef(false);
  const proxyClientId = useRef(loadVideoProxyClientId());
  const skipNextAudioPreferenceSave = useRef(false);
  const wantsPlaying = useRef(false);
  const resumeAttempts = useRef(0);
  const currentTimeRef = useRef(0);
  const [liveAsset, setLiveAsset] = useState(asset);
  const [audio, setAudio] = useState<VideoAudioPreference>(() => loadVideoAudioPreference());
  const [prefs, setPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());
  const [proxyFailed, setProxyFailed] = useState(false);
  const [sourceFailed, setSourceFailed] = useState(false);
  const [hasPlaybackStarted, setHasPlaybackStarted] = useState(false);
  const [playError, setPlayError] = useState('');
  const [paused, setPaused] = useState(true);
  const [proxyStreamEnabled, setProxyStreamEnabled] = useState(false);
  const [proxySessionId, setProxySessionId] = useState(() => createVideoProxySessionId());
  const [proxyStartTime, setProxyStartTime] = useState(0);
  const [proxyRuntime, setProxyRuntime] = useState<VideoProxyRuntime | null>(null);
  const [duration, setDuration] = useState(asset.duration ?? 0);
  const [currentTime, setCurrentTime] = useState(0);
  const [scrubTime, setScrubTime] = useState<number | null>(null);
  const [frameSize, setFrameSize] = useState({ width: 0, height: 0 });
  const playbackAsset = liveAsset.id === asset.id && liveAsset.cacheKey === asset.cacheKey ? liveAsset : asset;
  const usesProxy = !playbackAsset.browserPlayable && !proxyFailed;
  const source = useMemo(() => {
    if (usesProxy) {
      return proxyStreamEnabled
        ? assetVideoProxyUrl(playbackAsset, proxyStartTime, { clientId: proxyClientId.current, sessionId: proxySessionId })
        : '';
    }
    return assetVideoUrl(asset);
  }, [asset, playbackAsset, proxySessionId, proxyStartTime, proxyStreamEnabled, usesProxy]);

  const subtitleSource = subtitlesEnabled && selectedSubtitleId ? assetSubtitleUrl(asset, selectedSubtitleId) : '';
  const canPlay = !sourceFailed && (usesProxy || Boolean(source));
  const posterSource = asset.thumbStatus === 'ready' ? assetPreviewUrl(asset) : '';
  const showPosterLayer = Boolean(posterSource) && (!canPlay || (!hasPlaybackStarted && paused && currentTime <= 0.01));
  const statusLabel = videoStatusLabel(playbackAsset, sourceFailed, proxyRuntime);
  const displayedTime = scrubTime ?? currentTime;
  const hasSubtitles = subtitles.length > 0;

  const mediaStyle = useMemo(() => {
    const rotation = normalizeRotation(asset.rotation);
    if (rotation === 0) return undefined;
    return { ...rotatedContainStyle(asset, frameSize), bottom: 'auto', right: 'auto' };
  }, [asset, frameSize.height, frameSize.width]);

  const startProxyPlayback = () => {
    wantsPlaying.current = true;
    proxyPlayPending.current = true;
    resumeAttempts.current = 0;
    setPlayError('');
    setSourceFailed(false);
    setProxyStreamEnabled(true);
  };

  const proxyHeartbeat = (state: VideoProxyHeartbeat['state'], wantsStream: boolean): VideoProxyHeartbeat => ({
    clientId: proxyClientId.current,
    sessionId: proxySessionId,
    state,
    currentTime: currentTimeRef.current,
    playbackRate,
    wantsStream,
    hidden: document.hidden,
  });

  const sendProxyHeartbeat = (state: VideoProxyHeartbeat['state'], wantsStream: boolean) => {
    if (!usesProxy) return;
    void api.keepVideoProxyAlive(asset.id, proxyStartTime, proxyHeartbeat(state, wantsStream)).catch(() => undefined);
  };

  const stopProxySession = (sessionId: string, startSeconds: number, stoppedAt: number) => {
    void api
      .keepVideoProxyAlive(asset.id, startSeconds, {
        clientId: proxyClientId.current,
        sessionId,
        state: 'stopped',
        currentTime: stoppedAt,
        playbackRate,
        wantsStream: false,
        hidden: document.hidden,
      })
      .catch(() => undefined);
  };

  const togglePlay = () => {
    if (!canPlay) return;
    if (usesProxy && !proxyStreamEnabled) {
      startProxyPlayback();
      return;
    }
    const video = ref.current;
    if (!video) return;
    if (video.paused) {
      wantsPlaying.current = true;
      resumeAttempts.current = 0;
      void startPlayback(video);
    } else {
      wantsPlaying.current = false;
      clearResumeTimer();
      video.pause();
      if (usesProxy) {
        sendProxyHeartbeat('paused', false);
      }
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
        skipNextAudioPreferenceSave.current = true;
        video.muted = true;
        setAudio({ ...loadVideoAudioPreference(), muted: true, volume: video.volume });
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
    currentTimeRef.current = currentTime;
  }, [currentTime]);

  useEffect(() => {
    if (!usesProxy || !proxyStreamEnabled || !proxyPlayPending.current || !source) return;
    const video = ref.current;
    if (!video) return;
    proxyPlayPending.current = false;
    video.playbackRate = playbackRate;
    void startPlayback(video);
  }, [playbackRate, proxyStreamEnabled, source, usesProxy]);

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
    if (!canPlay) return clearAutoplayTimer;
    if (usesProxy) {
      if (!prefs.videoAutoplay || proxyStreamEnabled) return clearAutoplayTimer;
      autoplayTimer.current = window.setTimeout(() => {
        startProxyPlayback();
        autoplayTimer.current = null;
      }, autoplayDelayMs);
      return clearAutoplayTimer;
    }
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
  }, [asset.id, canPlay, playbackRate, prefs.videoAutoplay, proxyStreamEnabled, source, usesProxy]);

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
    currentTimeRef.current = 0;
    setScrubTime(null);
    setDuration(asset.duration ?? 0);
    setLiveAsset(asset);
    setProxyFailed(false);
    setProxyStreamEnabled(false);
    setProxySessionId(createVideoProxySessionId());
    setProxyStartTime(0);
    setProxyRuntime(null);
    proxyPlayPending.current = false;
    wantsPlaying.current = false;
    resumeAttempts.current = 0;
    clearResumeTimer();
  }, [asset.id]);

  useEffect(() => {
    setSourceFailed(false);
    setPlayError('');
  }, [source]);

  useEffect(() => {
    clearProxyPollTimer();
    if (!usesProxy) {
      setProxyRuntime(null);
      return clearProxyPollTimer;
    }
    const poll = async () => {
      try {
        const runtime = await api.videoProxyStatus(asset.id, proxyStartTime, {
          clientId: proxyClientId.current,
          sessionId: proxySessionId,
        });
        setProxyRuntime(runtime);
        if (runtime.command === 'start_stream' && wantsPlaying.current && !proxyStreamEnabled) {
          startProxyPlayback();
        }
      } catch {
        // Keep playback usable if the lightweight status poll fails.
      } finally {
        proxyPollTimer.current = window.setTimeout(poll, proxyPollMs);
      }
    };
    void poll();
    return clearProxyPollTimer;
  }, [asset.id, proxySessionId, proxyStartTime, proxyStreamEnabled, usesProxy]);

  useEffect(() => {
    onProxyRuntimeChange?.(usesProxy ? proxyRuntime : null);
  }, [onProxyRuntimeChange, proxyRuntime, usesProxy]);

  useEffect(() => {
    if (!usesProxy || !proxyStreamEnabled) return undefined;
    const releaseAssetID = asset.id;
    const releaseStartTime = proxyStartTime;
    const releaseSessionID = proxySessionId;
    return () => {
      void api
        .keepVideoProxyAlive(releaseAssetID, releaseStartTime, {
          clientId: proxyClientId.current,
          sessionId: releaseSessionID,
          state: 'stopped',
          currentTime: currentTimeRef.current,
          playbackRate,
          wantsStream: false,
          hidden: document.hidden,
        })
        .catch(() => undefined);
    };
  }, [asset.id, proxySessionId, proxyStartTime, proxyStreamEnabled, usesProxy]);

  useEffect(() => {
    clearProxyKeepaliveTimer();
    if (!usesProxy || sourceFailed || !proxyStreamEnabled) {
      return clearProxyKeepaliveTimer;
    }
    const shouldSync = wantsPlaying.current || proxyPlayPending.current || !paused || hasPlaybackStarted;
    if (!shouldSync) {
      return clearProxyKeepaliveTimer;
    }
    const keepalive = async () => {
      const state: VideoProxyHeartbeat['state'] = !paused && hasPlaybackStarted ? 'playing' : 'preparing';
      try {
        const runtime = await api.keepVideoProxyAlive(asset.id, proxyStartTime, proxyHeartbeat(state, true));
        setProxyRuntime(runtime);
        if (runtime.command === 'start_stream' && wantsPlaying.current && !source) {
          startProxyPlayback();
        }
      } catch {
        // Playback owns the main request; keepalive failure should not interrupt it.
      } finally {
        proxyKeepaliveTimer.current = window.setTimeout(keepalive, proxyKeepaliveMs);
      }
    };
    void keepalive();
    return clearProxyKeepaliveTimer;
  }, [asset.id, hasPlaybackStarted, paused, playbackRate, proxySessionId, proxyStartTime, proxyStreamEnabled, source, sourceFailed, usesProxy]);

  useEffect(() => () => {
    clearResumeTimer();
    clearProxyPollTimer();
    clearProxyKeepaliveTimer();
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

  function clearProxyKeepaliveTimer() {
    if (proxyKeepaliveTimer.current === null) return;
    window.clearTimeout(proxyKeepaliveTimer.current);
    proxyKeepaliveTimer.current = null;
  }

  function commitSeek(value = scrubTime) {
    if (value === null || !ref.current) return;
    const next = clampTime(value, duration || ref.current.duration || value);
    if (usesProxy) {
      const shouldResume = wantsPlaying.current || !ref.current.paused;
      const previousSessionId = proxySessionId;
      const previousStartTime = proxyStartTime;
      wantsPlaying.current = shouldResume;
      proxyPlayPending.current = shouldResume;
      resumeAttempts.current = 0;
      stopProxySession(previousSessionId, previousStartTime, currentTimeRef.current);
      setProxySessionId(createVideoProxySessionId());
      setProxyStreamEnabled(true);
      setProxyStartTime(next);
      setCurrentTime(next);
      currentTimeRef.current = next;
      setScrubTime(null);
      return;
    }
    ref.current.currentTime = next;
    setCurrentTime(next);
    currentTimeRef.current = next;
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
            src={source || undefined}
            poster={posterSource || undefined}
            preload={usesProxy ? 'none' : 'metadata'}
            playsInline
            style={mediaStyle}
            onClick={togglePlay}
            onDurationChange={(event) => setDuration(usesProxy ? asset.duration || 0 : event.currentTarget.duration || asset.duration || 0)}
            onError={() => {
              setSourceFailed(true);
            }}
            onLoadedMetadata={(event) => {
              const savedAudio = loadVideoAudioPreference();
              event.currentTarget.volume = savedAudio.volume;
              event.currentTarget.muted = savedAudio.muted;
              setAudio(savedAudio);
              setDuration(usesProxy ? asset.duration || 0 : event.currentTarget.duration || asset.duration || 0);
              setPaused(event.currentTarget.paused);
              event.currentTarget.playbackRate = playbackRate;
            }}
            onPause={(event) => {
              setPaused(true);
              scheduleResumeAfterUnexpectedPause(event.currentTarget);
              if (usesProxy && !wantsPlaying.current) {
                sendProxyHeartbeat('paused', false);
              }
            }}
            onPlay={() => {
              setPlayError('');
              clearResumeTimer();
              setPaused(false);
              setHasPlaybackStarted(true);
              if (usesProxy) {
                sendProxyHeartbeat('playing', true);
              }
            }}
            onTimeUpdate={(event) => {
              const next = clampTime((usesProxy ? proxyStartTime : 0) + event.currentTarget.currentTime, duration || asset.duration || 0);
              currentTimeRef.current = next;
              if (scrubTime === null) {
                setCurrentTime(next);
              }
              if (next > 0.01) setHasPlaybackStarted(true);
            }}
            onVolumeChange={(event) => {
              const video = event.currentTarget;
              if (skipNextAudioPreferenceSave.current) {
                skipNextAudioPreferenceSave.current = false;
                setAudio({ ...loadVideoAudioPreference(), muted: video.muted, volume: video.volume });
                return;
              }
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
        {!posterSource && canPlay && usesProxy && !proxyStreamEnabled && (
          <button className="video-pending video-pending-button" type="button" onClick={togglePlay}>
            <Play size={34} fill="currentColor" />
            <span>点击播放开始转码</span>
          </button>
        )}
      </div>
      <div className="video-control-zone" onClick={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()}>
        <div className="video-controls">
          <button type="button" disabled={!canPlay} onClick={togglePlay} title={paused ? '播放' : '暂停'}>
            {paused ? <Play size={18} /> : <Pause size={18} />}
          </button>
          <span className="video-time">{formatDuration(displayedTime)}</span>
          <input
            className="video-progress-slider"
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
          <span className="video-time">{formatDuration(duration)}</span>
          <div className="video-control-group video-audio-controls">
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
              className="video-volume-slider"
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
          <div className="video-control-group video-option-controls">
            <button
              className="video-rate-button"
              type="button"
              title={`倍速 ${playbackRate}x`}
              onClick={() => onPlaybackRateChange(nextPlaybackRate(playbackRate))}
            >
              <Gauge size={16} />
              <span className="video-rate-label">{playbackRate}x</span>
            </button>
            <button
              className={subtitlesEnabled && hasSubtitles ? 'active' : ''}
              type="button"
              title={hasSubtitles ? (subtitlesEnabled ? '关闭字幕' : '开启字幕') : '无字幕'}
              disabled={!hasSubtitles}
              onClick={() => onSubtitlesEnabledChange(!subtitlesEnabled)}
            >
              {subtitlesEnabled && hasSubtitles ? <Captions size={18} /> : <CaptionsOff size={18} />}
            </button>
            {hasSubtitles && (
              <select
                className="video-subtitle-select"
                aria-label="字幕选择"
                value={selectedSubtitleId}
                onChange={(event) => onSelectedSubtitleChange(event.target.value)}
              >
                {subtitles.map((subtitle) => (
                  <option key={subtitle.id} value={subtitle.id}>
                    {subtitle.filename}
                  </option>
                ))}
              </select>
            )}
            <button type="button" title={`旋转 ${asset.rotation || 0}°`} onClick={onRotate}>
              <RotateCw size={18} />
            </button>
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
    </div>
  );
}

function videoProxyRuntimeLabel(runtime: VideoProxyRuntime) {
  if (runtime.message) return runtime.message;
  if (runtime.status === 'cached' || runtime.cached) return '已缓存';
  if (runtime.status === 'error') return '转码失败';
  if (runtime.status === 'queued' || runtime.queued) return '等待转码槽位';
  if (runtime.transcoding) return `实时转码 ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`;
  return '准备转码';
}

function videoStatusLabel(asset: Asset, sourceFailed: boolean, runtime: VideoProxyRuntime | null) {
  if (sourceFailed) return '视频加载失败';
  if (!asset.browserPlayable) return runtime ? videoProxyRuntimeLabel(runtime) : '准备转码';
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
  const version = parsed?.version === 2 ? 2 : 1;
  sharedVideoAudio = {
    lastVolume: lastVolume > 0 ? lastVolume : 1,
    muted: version === 2 ? Boolean(parsed?.muted) : false,
    version: 2,
    volume,
  };
  if (version !== 2) {
    try {
      window.localStorage.setItem(videoAudioStorageKey, JSON.stringify(sharedVideoAudio));
    } catch {
      // localStorage can be unavailable in private or restricted browser contexts.
    }
  }
  return sharedVideoAudio;
}

function saveVideoAudioPreference(volumeValue: number, muted: boolean): VideoAudioPreference {
  const previous = loadVideoAudioPreference();
  const volume = clampVolume(volumeValue);
  const lastVolume = !muted && volume > 0 ? volume : previous.lastVolume;
  const next = {
    lastVolume: lastVolume > 0 ? lastVolume : 1,
    muted: muted || volume === 0,
    version: 2,
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

function loadVideoProxyClientId() {
  try {
    const existing = window.sessionStorage.getItem(videoProxyClientStorageKey);
    if (existing) return existing;
    const next = createVideoProxySessionId();
    window.sessionStorage.setItem(videoProxyClientStorageKey, next);
    return next;
  } catch {
    return createVideoProxySessionId();
  }
}

function createVideoProxySessionId() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
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
