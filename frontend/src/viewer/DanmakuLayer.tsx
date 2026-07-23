import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { layoutDanmaku, parseDanmakuText, type DanmakuCue, type PositionedDanmakuCue } from "./danmaku";
import type { ViewerPrefs } from "../utils/viewerPrefs";

interface Props {
  currentTime: number;
  density: ViewerPrefs["danmakuDensity"];
  enabled: boolean;
  format: string;
  frameHeight: number;
  frameWidth: number;
  fontScale: ViewerPrefs["danmakuFontScale"];
  opacity: ViewerPrefs["danmakuOpacity"];
  paused: boolean;
  playbackRate: number;
  source: string;
  speed: ViewerPrefs["danmakuSpeed"];
}

export default function DanmakuLayer({
  currentTime,
  density,
  enabled,
  format,
  frameHeight,
  frameWidth,
  fontScale,
  opacity,
  paused,
  playbackRate,
  source,
  speed,
}: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const itemsRef = useRef<Map<string, HTMLSpanElement>>(new Map());
  const rafRef = useRef<number | null>(null);
  const containerWidthRef = useRef(0);
  const playbackClockRef = useRef({ mediaTime: currentTime, wallTime: performance.now() });
  const currentTimeRef = useRef(currentTime);
  currentTimeRef.current = currentTime;
  const [rawCues, setRawCues] = useState<DanmakuCue[]>([]);

  useEffect(() => {
    if (!enabled || !source) {
      setRawCues([]);
      return undefined;
    }
    const controller = new AbortController();
    async function load() {
      try {
        const response = await fetch(source, { signal: controller.signal });
        if (!response.ok) throw new Error(response.statusText);
        const text = await response.text();
        setRawCues(parseDanmakuText(text, format));
      } catch (err) {
        if (!(err instanceof DOMException && err.name === "AbortError")) {
          setRawCues([]);
        }
      }
    }
    void load();
    return () => controller.abort();
  }, [enabled, format, source]);

  const cues = useMemo<PositionedDanmakuCue[]>(
    () => layoutDanmaku(scaleCueDuration(rawCues, speed), laneCount(frameWidth, frameHeight, density)),
    [density, frameHeight, frameWidth, rawCues, speed],
  );

  const activeCues = useMemo(
    () =>
      cues.filter(
        (cue) =>
          currentTime >= cue.start &&
          currentTime <= cue.start + cue.displayDuration,
      ).slice(0, maxActiveCueCount(density)),
    [cues, currentTime, density],
  );

  useEffect(() => {
    playbackClockRef.current = { mediaTime: currentTime, wallTime: performance.now() };
    currentTimeRef.current = currentTime;
  }, [currentTime, paused, playbackRate]);

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
    if (rafRef.current !== null) {
      cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    }
    if (!enabled || !source) return;

    const activeIds = new Set(activeCues.map((c) => c.id));
    for (const [id] of itemsRef.current) {
      if (!activeIds.has(id)) itemsRef.current.delete(id);
    }

    if (activeCues.length === 0) return;

    const rate = normalizePlaybackRate(playbackRate);

    function tick() {
      rafRef.current = requestAnimationFrame(tick);
      const now = mediaTimeAtFrame(playbackClockRef.current, paused, rate);
      for (const cue of activeCues) {
        const el = itemsRef.current.get(cue.id);
        if (!el || cue.mode !== "scroll") continue;
        const containerWidth = containerWidthRef.current || containerRef.current?.clientWidth || 0;
        const distance = containerWidth + el.offsetWidth;
        const progress = Math.min(1, Math.max(0, (now - cue.start) / cue.displayDuration));
        const x = containerWidth - progress * distance;
        el.style.transform = `translate3d(${x}px, 0, 0)`;
      }
    }

    rafRef.current = requestAnimationFrame(tick);
    return () => {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [activeCues, enabled, paused, playbackRate, source]);

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
      className={paused ? "video-danmaku-layer paused" : "video-danmaku-layer"}
      aria-hidden="true"
    >
      {activeCues.map((cue) => (
        <span
          ref={setItemRef(cue.id)}
          className={`video-danmaku-item video-danmaku-${cue.mode}`}
          key={cue.id}
          style={danmakuItemStyle(cue, fontScale, opacity)}
        >
          {cue.text}
        </span>
      ))}
    </div>
  );
}

function danmakuItemStyle(cue: PositionedDanmakuCue, fontScale: number, opacity: number): CSSProperties {
  return {
    "--danmaku-color": cue.color,
    "--danmaku-font-size": `${Math.round(cue.fontSize * clampNumber(fontScale, 0.75, 1.5, 1))}px`,
    "--danmaku-lane": cue.lane,
    opacity: clampNumber(opacity, 0.15, 1, 0.95),
  } as CSSProperties;
}

function laneCount(width: number, height: number, density: number) {
  const controlSpace = width <= 720 ? 112 : 76;
  const available = Math.max(96, height - controlSpace - 18);
  return Math.max(2, Math.floor((available / 28) * clampNumber(density, 0.25, 1.5, 1)));
}

function normalizePlaybackRate(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 1;
  return Math.min(3, Math.max(0.25, value));
}

function mediaTimeAtFrame(clock: { mediaTime: number; wallTime: number }, paused: boolean, playbackRate: number) {
  if (paused) return clock.mediaTime;
  return clock.mediaTime + ((performance.now() - clock.wallTime) / 1000) * playbackRate;
}

function scaleCueDuration(cues: DanmakuCue[], speed: number): DanmakuCue[] {
  const normalized = clampNumber(speed, 0.5, 2, 1);
  return cues.map((cue) => {
    const displayDuration = cue.displayDuration / normalized;
    return {
      ...cue,
      displayDuration,
      end: cue.start + displayDuration,
    };
  });
}

function maxActiveCueCount(density: number) {
  return Math.max(24, Math.round(140 * clampNumber(density, 0.25, 1.5, 1)));
}

function clampNumber(value: number, min: number, max: number, fallback: number) {
  if (!Number.isFinite(value)) return fallback;
  return Math.min(max, Math.max(min, value));
}
