import { useMemo, useRef, useState, type CSSProperties } from 'react';
import type { LibraryAnchor, SortKey } from '../types/api';

interface Props {
  anchors: LibraryAnchor[];
  sort: SortKey;
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

export default function LibraryIndexRail({ anchors, sort, scrollRatio, totalCount, pageSize, onSeek }: Props) {
  const railRef = useRef<HTMLDivElement | null>(null);
  const draggingRef = useRef(false);
  const [active, setActive] = useState<ActiveBubble | null>(null);
  const [dragging, setDragging] = useState(false);
  const visibleAnchors = useMemo(() => anchors.filter((anchor) => anchor.position >= 0 && anchor.position <= 1), [anchors]);

  if (visibleAnchors.length === 0) return null;
  const thumbPosition = clampRatio(active?.position ?? scrollRatio);

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

  function activate(clientY: number) {
    const result = pick(clientY);
    if (!result) return;
    setActive({ label: result.anchor.label, position: result.position });
    onSeek(result.anchor, result.page, result.position);
  }

  return (
    <div
      className="library-index-rail"
      ref={railRef}
      onPointerDown={(event) => {
        event.preventDefault();
        event.currentTarget.setPointerCapture(event.pointerId);
        draggingRef.current = true;
        setDragging(true);
        activate(event.clientY);
      }}
      onPointerMove={(event) => {
        if (!draggingRef.current) return;
        activate(event.clientY);
      }}
      onPointerUp={(event) => {
        event.currentTarget.releasePointerCapture(event.pointerId);
        draggingRef.current = false;
        setDragging(false);
      }}
      onPointerCancel={() => {
        draggingRef.current = false;
        setDragging(false);
      }}
      onPointerLeave={() => {
        if (!dragging) {
          setActive(null);
        }
      }}
    >
      <div className="library-index-track" />
      <div className="library-index-scroll-thumb" style={railPositionStyle(thumbPosition)} />
      {visibleAnchors.map((anchor) => (
        <button
          className={`library-index-mark ${anchor.kind}`}
          key={anchor.key}
          style={railPositionStyle(anchor.position)}
          title={anchor.label}
          type="button"
          onMouseEnter={() => setActive({ label: anchor.label, position: anchor.position })}
        >
          {showInlineLabel(sort) && <span>{anchor.label}</span>}
        </button>
      ))}
      {active && (
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

function showInlineLabel(sort: SortKey) {
  return sort === 'filename' || sort === 'filename_asc' || sort === 'filename_desc' || sort === 'size' || sort === 'size_asc' || sort === 'size_desc';
}
