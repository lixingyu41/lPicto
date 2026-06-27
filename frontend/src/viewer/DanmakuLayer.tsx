import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { layoutDanmaku, parseDanmakuText, type PositionedDanmakuCue } from './danmaku';

interface Props {
  currentTime: number;
  enabled: boolean;
  format: string;
  frameHeight: number;
  frameWidth: number;
  paused: boolean;
  playbackRate: number;
  source: string;
}

type DanmakuStyle = CSSProperties & {
  '--danmaku-color': string;
  '--danmaku-delay': string;
  '--danmaku-duration': string;
  '--danmaku-font-size': string;
  '--danmaku-lane': number;
};

export default function DanmakuLayer({
  currentTime,
  enabled,
  format,
  frameHeight,
  frameWidth,
  paused,
  playbackRate,
  source,
}: Props) {
  const [cues, setCues] = useState<PositionedDanmakuCue[]>([]);

  useEffect(() => {
    if (!enabled || !source) {
      setCues([]);
      return undefined;
    }
    const controller = new AbortController();
    async function load() {
      try {
        const response = await fetch(source, { signal: controller.signal });
        if (!response.ok) throw new Error(response.statusText);
        const text = await response.text();
        const parsed = parseDanmakuText(text, format);
        setCues(layoutDanmaku(parsed, laneCount(frameWidth, frameHeight)));
      } catch (err) {
        if (!(err instanceof DOMException && err.name === 'AbortError')) {
          setCues([]);
        }
      }
    }
    void load();
    return () => controller.abort();
  }, [enabled, format, frameHeight, frameWidth, source]);

  const activeCues = useMemo(
    () =>
      cues
        .filter((cue) => currentTime >= cue.start && currentTime <= cue.start + cue.displayDuration)
        .slice(-140),
    [cues, currentTime],
  );

  if (!enabled || !source || activeCues.length === 0) return null;

  const rate = normalizePlaybackRate(playbackRate);
  return (
    <div className={paused ? 'video-danmaku-layer paused' : 'video-danmaku-layer'} aria-hidden="true">
      {activeCues.map((cue) => (
        <span className={danmakuClassName(cue)} key={cue.id} style={danmakuStyle(cue, currentTime, rate)}>
          {cue.text}
        </span>
      ))}
    </div>
  );
}

function danmakuClassName(cue: PositionedDanmakuCue) {
  return `video-danmaku-item video-danmaku-${cue.mode}`;
}

function danmakuStyle(cue: PositionedDanmakuCue, currentTime: number, playbackRate: number): DanmakuStyle {
  const elapsed = Math.max(0, currentTime - cue.start) / playbackRate;
  return {
    '--danmaku-color': cue.color,
    '--danmaku-delay': `${-elapsed}s`,
    '--danmaku-duration': `${cue.displayDuration / playbackRate}s`,
    '--danmaku-font-size': `${cue.fontSize}px`,
    '--danmaku-lane': cue.lane,
  };
}

function laneCount(width: number, height: number) {
  const controlSpace = width <= 720 ? 112 : 76;
  const available = Math.max(96, height - controlSpace - 18);
  return Math.max(4, Math.floor(available / 32));
}

function normalizePlaybackRate(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 1;
  return Math.min(3, Math.max(0.25, value));
}
