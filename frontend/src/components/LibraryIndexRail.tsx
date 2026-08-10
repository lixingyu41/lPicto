import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import type { LibraryAnchor } from '../types/api';

interface Props {
  anchors: LibraryAnchor[];
  hideLabels?: boolean;
  scrollRatio: number;
  totalCount: number;
  pageSize: number;
  onSeek: (anchor: LibraryAnchor, page: number, ratio: number) => void;
}

interface ActiveBubble {
  label: string;
  position: number;
}

interface PickResult {
  anchor: LibraryAnchor;
  page: number;
  position: number;
}

export default function LibraryIndexRail({ anchors, hideLabels = false, scrollRatio, totalCount, pageSize, onSeek }: Props) {
  const railRef = useRef<HTMLDivElement | null>(null);
  const draggingRef = useRef(false);
  const wheelAnimationRef = useRef(0);
  const wheelTargetRef = useRef<number | null>(null);
  const wheelFrameTimeRef = useRef(0);
  const [active, setActive] = useState<ActiveBubble | null>(null);
  const [dragging, setDragging] = useState(false);
  const visibleAnchors = useMemo(() => anchors.filter((anchor) => anchor.position >= 0 && anchor.position <= 1), [anchors]);
  const visibleMarks = useMemo(() => sampleAnchors(visibleAnchors, 8), [visibleAnchors]);

  function cancelWheelAnimation() {
    if (wheelAnimationRef.current) window.cancelAnimationFrame(wheelAnimationRef.current);
    wheelAnimationRef.current = 0;
    wheelTargetRef.current = null;
    wheelFrameTimeRef.current = 0;
  }

  useEffect(() => {
    const rail = railRef.current;
    if (!rail) return undefined;
    const scrollElement = rail.parentElement?.querySelector<HTMLElement>('.grid-scroll');
    if (!scrollElement) return undefined;
    const cancelForGridInput = () => cancelWheelAnimation();
    const handleWheel = (event: WheelEvent) => {
      const unit = event.deltaMode === 1
        ? 16
        : event.deltaMode === 2
          ? scrollElement.clientHeight
          : 1;
      const delta = event.deltaY !== 0 ? event.deltaY : event.deltaX;
      if (delta === 0) return;
      event.preventDefault();
      const maxScroll = Math.max(0, scrollElement.scrollHeight - scrollElement.clientHeight);
      const currentTarget = wheelTargetRef.current ?? scrollElement.scrollTop;
      wheelTargetRef.current = Math.min(maxScroll, Math.max(0, currentTarget + delta * unit * 2));
      if (wheelAnimationRef.current) return;
      wheelFrameTimeRef.current = performance.now();
      const animate = (now: number) => {
        const target = wheelTargetRef.current;
        if (target === null) {
          cancelWheelAnimation();
          return;
        }
        const elapsed = Math.min(48, Math.max(1, now - wheelFrameTimeRef.current));
        wheelFrameTimeRef.current = now;
        const distance = target - scrollElement.scrollTop;
        if (Math.abs(distance) < 0.5) {
          scrollElement.scrollTop = target;
          cancelWheelAnimation();
          return;
        }
        const progress = 1 - Math.exp(-elapsed / 70);
        scrollElement.scrollTop += distance * progress;
        wheelAnimationRef.current = window.requestAnimationFrame(animate);
      };
      wheelAnimationRef.current = window.requestAnimationFrame(animate);
    };
    rail.addEventListener('wheel', handleWheel, { passive: false });
    scrollElement.addEventListener('wheel', cancelForGridInput, { passive: true });
    scrollElement.addEventListener('pointerdown', cancelForGridInput, { passive: true });
    scrollElement.addEventListener('touchstart', cancelForGridInput, { passive: true });
    return () => {
      rail.removeEventListener('wheel', handleWheel);
      scrollElement.removeEventListener('wheel', cancelForGridInput);
      scrollElement.removeEventListener('pointerdown', cancelForGridInput);
      scrollElement.removeEventListener('touchstart', cancelForGridInput);
      cancelWheelAnimation();
    };
  }, [visibleAnchors.length]);

  if (visibleAnchors.length === 0) return null;
  const thumbPosition = clampRatio(dragging && active ? active.position : scrollRatio);

  function pick(clientY: number): PickResult | null {
    const rect = railRef.current?.getBoundingClientRect();
    if (!rect) return null;
    const ratio = Math.min(1, Math.max(0, (clientY - rect.top) / rect.height));
    let best = visibleAnchors[0];
    let bestDistance = Math.abs(best.position - ratio);
    for (const anchor of visibleAnchors) {
      const distance = Math.abs(anchor.position - ratio);
      if (distance < bestDistance) {
        best = anchor;
        bestDistance = distance;
      }
    }
    return { anchor: best, page: pageForRatio(visibleAnchors, ratio, totalCount, pageSize), position: ratio };
  }

  function activate(clientY: number, seek: boolean) {
    const result = pick(clientY);
    if (!result) return;
    setActive({ label: result.anchor.label, position: result.position });
    if (seek) {
      onSeek(result.anchor, result.page, result.position);
    }
  }

  function isInsideRail(clientX: number, clientY: number) {
    const rect = railRef.current?.getBoundingClientRect();
    return Boolean(rect && clientX >= rect.left && clientX <= rect.right && clientY >= rect.top && clientY <= rect.bottom);
  }

  return (
    <div
      className="library-index-rail"
      ref={railRef}
      onPointerDown={(event) => {
        cancelWheelAnimation();
        event.preventDefault();
        event.currentTarget.setPointerCapture(event.pointerId);
        draggingRef.current = true;
        setDragging(true);
        activate(event.clientY, true);
      }}
      onPointerEnter={(event) => {
        activate(event.clientY, false);
      }}
      onPointerMove={(event) => {
        activate(event.clientY, draggingRef.current);
      }}
      onPointerUp={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId)) {
          event.currentTarget.releasePointerCapture(event.pointerId);
        }
        draggingRef.current = false;
        setDragging(false);
        if (!isInsideRail(event.clientX, event.clientY)) {
          setActive(null);
        }
      }}
      onPointerCancel={() => {
        draggingRef.current = false;
        setDragging(false);
        setActive(null);
      }}
      onPointerLeave={() => {
        if (!draggingRef.current) {
          cancelWheelAnimation();
          setActive(null);
        }
      }}
    >
      <div className="library-index-track" />
      <div className="library-index-scroll-thumb" style={railPositionStyle(thumbPosition)} />
      {visibleMarks.map((anchor) => (
        <button
          className={`library-index-mark ${anchor.kind}`}
          key={anchor.key}
          style={railPositionStyle(anchor.position)}
          title={hideLabels ? undefined : anchor.label}
          type="button"
        />
      ))}
      {active && !hideLabels && (
        <div className="library-index-bubble" style={railPositionStyle(active.position)}>
          {active.label}
        </div>
      )}
    </div>
  );
}

function railPositionStyle(position: number): CSSProperties {
  const ratio = clampRatio(position);
  return {
    top: `${ratio * 100}%`,
    transform: `translateY(-${ratio * 100}%)`,
  };
}

function clampRatio(value: number) {
  return Math.min(1, Math.max(0, value));
}

function pageForRatio(anchors: LibraryAnchor[], ratio: number, totalCount: number, pageSize: number) {
  if (totalCount > 0 && pageSize > 0) {
    return Math.max(1, Math.floor((Math.min(1, Math.max(0, ratio)) * Math.max(0, totalCount - 1)) / pageSize) + 1);
  }
  if (anchors.length === 0) return 1;
  const sorted = [...anchors].sort((a, b) => a.position - b.position);
  if (ratio <= sorted[0].position) return sorted[0].page;
  for (let index = 1; index < sorted.length; index += 1) {
    const prev = sorted[index - 1];
    const next = sorted[index];
    if (ratio <= next.position) {
      const span = next.position - prev.position;
      const local = span > 0 ? (ratio - prev.position) / span : 0;
      return Math.max(1, Math.round(prev.page + (next.page - prev.page) * local));
    }
  }
  return sorted[sorted.length - 1].page;
}

function sampleAnchors(anchors: LibraryAnchor[], limit: number) {
  if (anchors.length <= limit) return anchors;
  const result: LibraryAnchor[] = [];
  const used = new Set<number>();
  for (let index = 0; index < limit; index += 1) {
    const sourceIndex = Math.round((index * (anchors.length - 1)) / (limit - 1));
    if (used.has(sourceIndex)) continue;
    used.add(sourceIndex);
    result.push(anchors[sourceIndex]);
  }
  return result;
}
