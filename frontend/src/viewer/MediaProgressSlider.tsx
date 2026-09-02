import { useState, type ChangeEventHandler, type FocusEventHandler, type KeyboardEventHandler, type PointerEventHandler } from 'react';

export interface MediaBufferedRange {
  end: number;
  start: number;
}

interface Props {
  ariaLabel: string;
  buffered: MediaBufferedRange[];
  className?: string;
  disabled?: boolean;
  duration: number;
  step?: number;
  value: number;
  onBlur?: FocusEventHandler<HTMLInputElement>;
  onChange: ChangeEventHandler<HTMLInputElement>;
  onKeyUp?: KeyboardEventHandler<HTMLInputElement>;
  onPointerCancel?: PointerEventHandler<HTMLInputElement>;
  onPointerDown?: PointerEventHandler<HTMLInputElement>;
  onPointerUp?: PointerEventHandler<HTMLInputElement>;
  onHoverChange?: (hover: { percent: number; time: number } | null) => void;
  preview?: {
    cellHeight: number;
    cellIndex: number;
    cellWidth: number;
    columns: number;
    imageUrl: string;
    label: string;
    percent: number;
    rows: number;
  } | null;
}

export default function MediaProgressSlider({
  ariaLabel,
  buffered,
  className = '',
  disabled = false,
  duration,
  step = 0.01,
  value,
  onBlur,
  onChange,
  onKeyUp,
  onPointerCancel,
  onPointerDown,
  onPointerUp,
  onHoverChange,
  preview,
}: Props) {
  const [hover, setHover] = useState<{ percent: number; time: number } | null>(null);
  const safeDuration = Number.isFinite(duration) && duration > 0 ? duration : 0.01;
  const safeValue = Math.min(safeDuration, Math.max(0, Number.isFinite(value) ? value : 0));
  const playedPercent = (safeValue / safeDuration) * 100;
  const hoverBuffered = hover !== null && buffered.some((range) => hover.time >= range.start && hover.time <= range.end);

  return (
    <div
      className={`media-progress-slider ${disabled ? 'is-disabled' : ''} ${className}`.trim()}
      onPointerLeave={() => {
        setHover(null);
        onHoverChange?.(null);
      }}
      onPointerMove={(event) => {
        if (disabled) return;
        const rect = event.currentTarget.getBoundingClientRect();
        const percent = Math.min(1, Math.max(0, (event.clientX - rect.left) / Math.max(1, rect.width)));
        const nextHover = { percent, time: percent * safeDuration };
        setHover(nextHover);
        onHoverChange?.(nextHover);
      }}
    >
      {preview && (
        <div
          className="media-progress-preview"
          style={{ left: `clamp(${preview.cellWidth / 2 + 8}px, ${preview.percent * 100}%, calc(100% - ${preview.cellWidth / 2 + 8}px))` }}
          aria-hidden="true"
        >
          <div
            className="media-progress-preview-frame"
            style={{
              width: preview.cellWidth,
              height: preview.cellHeight,
              backgroundImage: `url("${preview.imageUrl}")`,
              backgroundPosition: `${-(preview.cellIndex % preview.columns) * preview.cellWidth}px ${-Math.floor(preview.cellIndex / preview.columns) * preview.cellHeight}px`,
              backgroundSize: `${preview.columns * preview.cellWidth}px ${preview.rows * preview.cellHeight}px`,
            }}
          />
          <span className="media-progress-preview-label">{preview.label}</span>
          <small className={hoverBuffered ? 'media-progress-buffer-state ready' : 'media-progress-buffer-state'}>
            {hoverBuffered ? '已缓冲' : '尚未缓冲'}
          </small>
        </div>
      )}
      {hover && !preview && (
        <span
          className={hoverBuffered ? 'media-progress-buffer-tip ready' : 'media-progress-buffer-tip'}
          style={{ left: `clamp(34px, ${hover.percent * 100}%, calc(100% - 34px))` }}
        >
          {hoverBuffered ? '已缓冲' : '尚未缓冲'}
        </span>
      )}
      <div className="media-progress-track" aria-hidden="true">
        {buffered.map((range, index) => {
          const start = Math.min(safeDuration, Math.max(0, range.start));
          const end = Math.min(safeDuration, Math.max(start, range.end));
          return (
            <span
              className="media-progress-buffered"
              key={`${start}-${end}-${index}`}
              style={{ left: `${(start / safeDuration) * 100}%`, width: `${((end - start) / safeDuration) * 100}%` }}
            />
          );
        })}
        <span className="media-progress-played" style={{ width: `${playedPercent}%` }} />
      </div>
      <input
        aria-label={ariaLabel}
        disabled={disabled}
        max={safeDuration}
        min={0}
        step={step}
        type="range"
        value={safeValue}
        onBlur={onBlur}
        onChange={onChange}
        onKeyUp={onKeyUp}
        onPointerCancel={onPointerCancel}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
      />
    </div>
  );
}

export function readMediaBufferedRanges(media: HTMLMediaElement, duration: number): MediaBufferedRange[] {
  const limit = Number.isFinite(duration) && duration > 0 ? duration : Number.POSITIVE_INFINITY;
  const ranges: MediaBufferedRange[] = [];
  for (let index = 0; index < media.buffered.length; index += 1) {
    const start = Math.max(0, media.buffered.start(index));
    const end = Math.min(limit, Math.max(start, media.buffered.end(index)));
    if (end <= start) continue;
    const previous = ranges[ranges.length - 1];
    if (previous && start - previous.end <= 0.15) {
      previous.end = Math.max(previous.end, end);
    } else {
      ranges.push({ start, end });
    }
  }
  return ranges;
}

export function mediaBufferedRangesEqual(left: MediaBufferedRange[], right: MediaBufferedRange[]) {
  return left.length === right.length && left.every((range, index) => (
    Math.abs(range.start - right[index].start) < 0.01
    && Math.abs(range.end - right[index].end) < 0.01
  ));
}
