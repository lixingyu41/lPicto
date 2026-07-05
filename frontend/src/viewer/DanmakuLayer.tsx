import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
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
  const containerRef = useRef<HTMLDivElement | null>(null);
  const itemsRef = useRef<Map<string, HTMLSpanElement>>(new Map());
  const rafRef = useRef<number | null>(null);
  const containerWidthRef = useRef(0);
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
        .filter(
          (cue) =>
            currentTime >= cue.start &&
            currentTime <= cue.start + cue.displayDuration,
        )
        .slice(-140),
    [cues, currentTime],
  );

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) {
        containerWidthRef.current = entry.contentRect.width;
      }
    });
    ro.observe(el);
    containerWidthRef.current = el.clientWidth;
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    if (!enabled || !source || activeCues.length === 0) {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      activeCues.forEach((cue) => itemsRef.current.delete(cue.id));
      return;
    }

    const activeIds = new Set(activeCues.map((c) => c.id));
    for (const [id] of itemsRef.current) {
      if (!activeIds.has(id)) itemsRef.current.delete(id);
    }

    const rate = normalizePlaybackRate(playbackRate);

    function tick() {
      rafRef.current = requestAnimationFrame(tick);
      for (const cue of activeCues) {
        const el = itemsRef.current.get(cue.id);
        if (!el || cue.mode !== 'scroll') continue;
        const elapsed = Math.max(0, currentTime - cue.start) / rate;
        const progress = Math.min(1, elapsed / (cue.displayDuration / rate));
        const distance = containerWidthRef.current + el.offsetWidth;
        el.style.transform = `translateX(${-(progress * distance)}px)`;
      }
    }

    rafRef.current = requestAnimationFrame(tick);
    return () => {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [activeCues, currentTime, enabled, playbackRate, paused, source]);

  const setItemRef = useCallback(
    (id: string) =>
      (el: HTMLSpanElement | null) => {
        if (el) itemsRef.current.set(id, el);
        else itemsRef.current.delete(id);
      },
    [],
  );

  if (!enabled || !source || activeCues.length === 0) return null;

  return (
    <div
      ref={containerRef}
      className={paused ? 'video-danmaku-layer paused' : 'video-danmaku-layer'}
      aria-hidden="true"
    >
      {activeCues.map((cue) => (
        <span
          ref={setItemRef(cue.id)}
          className={danmakuClassName(cue)}
          key={cue.id}
          style={danmakuItemStyle(cue)}
        >
          {cue.text}
        </span>
      ))}
    </div>
  );
}

function danmakuClassName(cue: PositionedDanmakuCue) {
  return `video-danmaku-item video-danmaku-${cue.mode}`;
}

function danmakuItemStyle(cue: PositionedDanmakuCue): CSSProperties {
  return {
    '--danmaku-color': cue.color,
    '--danmaku-font-size': `${cue.fontSize}px`,
    '--danmaku-lane': cue.lane,
  };
}

function laneCount(width: number, height: number) {
  const controlSpace = width <= 720 ? 112 : 76;
  const available = Math.max(96, height - controlSpace - 18);
  return Math.max(4, Math.floor(available / 28));
}

function normalizePlaybackRate(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 1;
  return Math.min(3, Math.max(0.25, value));
}
