import { useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { Play } from 'lucide-react';
import type { Asset, SortKey } from '../types/api';
import { assetThumbUrl } from '../api/client';
import { effectiveAspect, rotatedCoverStyle } from '../utils/rotation';
import { assetGroupLabel, type AssetGroupMode } from '../utils/assetGrouping';

interface Props {
  assets: Asset[];
  loading: boolean;
  hasMore: boolean;
  onLoadMore: () => void;
  buildViewerUrl: (asset: Asset) => string;
  onPressPreviewChange?: (asset: Asset | null) => void;
  onScrollRatioChange?: (ratio: number) => void;
  groupMode?: AssetGroupMode;
  sort?: SortKey;
  scrollSignal?: number;
  scrollTarget?: { ratio: number; signal: number };
}

interface RowItem {
  asset: Asset;
  width: number;
}

interface AssetGridRow {
  key: string;
  type: 'assets';
  items: RowItem[];
  height: number;
}

interface GroupGridRow {
  key: string;
  type: 'group';
  label: string;
  height: number;
}

type GridRow = AssetGridRow | GroupGridRow;

const rowHeight = 176;
const groupHeaderHeight = 34;
const minTileWidth = 84;
const maxAspect = 2.8;
const minAspect = 0.42;
const gap = 10;
const pressPreviewDelayMs = 120;

export default function AssetGrid({
  assets,
  loading,
  hasMore,
  onLoadMore,
  buildViewerUrl,
  onPressPreviewChange,
  onScrollRatioChange,
  groupMode = 'none',
  sort = 'timeline_desc',
  scrollSignal = 0,
  scrollTarget,
}: Props) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const assetsByID = useRef<Map<number, Asset>>(new Map());
  const pressState = useRef({
    active: false,
    pending: false,
    pointerX: 0,
    pointerY: 0,
    timer: 0,
  });
  const previewFrame = useRef(0);
  const lastPreviewID = useRef<number | null>(null);
  const onPressPreviewChangeRef = useRef(onPressPreviewChange);
  const onScrollRatioChangeRef = useRef(onScrollRatioChange);
  const suppressClickUntil = useRef(0);
  const [width, setWidth] = useState(0);

  useEffect(() => {
    assetsByID.current = new Map(assets.map((asset) => [asset.id, asset]));
  }, [assets]);

  useEffect(() => {
    onPressPreviewChangeRef.current = onPressPreviewChange;
  }, [onPressPreviewChange]);

  useEffect(() => {
    onScrollRatioChangeRef.current = onScrollRatioChange;
  }, [onScrollRatioChange]);

  useEffect(() => {
    if (!parentRef.current) return;
    setWidth(parentRef.current.getBoundingClientRect().width);
    const observer = new ResizeObserver(([entry]) => {
      setWidth(entry.contentRect.width);
    });
    observer.observe(parentRef.current);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (parentRef.current) {
      parentRef.current.scrollTop = 0;
      emitScrollRatio();
    }
  }, [scrollSignal]);

  useEffect(() => {
    if (!parentRef.current || !scrollTarget) return;
    const element = parentRef.current;
    const maxScroll = element.scrollHeight - element.clientHeight;
    element.scrollTop = maxScroll > 0 ? maxScroll * clampRatio(scrollTarget.ratio) : 0;
    emitScrollRatio();
  }, [scrollTarget?.signal]);

  useEffect(() => {
    function handleMouseMove(event: MouseEvent) {
      if (!pressState.current.pending && !pressState.current.active) return;
      pressState.current.pointerX = event.clientX;
      pressState.current.pointerY = event.clientY;
      if (!pressState.current.active) return;
      updatePreviewFromPoint();
    }

    function handleMouseUp() {
      endPressPreview();
    }

    window.addEventListener('mousemove', handleMouseMove);
    window.addEventListener('mouseup', handleMouseUp);
    window.addEventListener('blur', handleMouseUp);
    return () => {
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
      window.removeEventListener('blur', handleMouseUp);
      clearPressTimer();
      if (previewFrame.current) {
        window.cancelAnimationFrame(previewFrame.current);
      }
    };
  }, []);

  useEffect(() => {
    const element = parentRef.current;
    if (!element) return;
    function handleScroll() {
      schedulePreviewUpdate();
      emitScrollRatio();
    }
    element.addEventListener('scroll', handleScroll, { passive: true });
    return () => element.removeEventListener('scroll', handleScroll);
  }, []);

  const gridRows = useMemo(() => buildRows(assets, width, groupMode, sort), [assets, groupMode, sort, width]);
  const virtualizer = useVirtualizer({
    count: gridRows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => (gridRows[index]?.height ?? rowHeight) + gap,
    overscan: 5,
  });

  const rows = virtualizer.getVirtualItems();
  const lastRow = rows[rows.length - 1];
  useEffect(() => {
    if (!lastRow) return;
    if (hasMore && !loading && lastRow.index >= gridRows.length - 3) {
      onLoadMore();
    }
  }, [gridRows.length, hasMore, lastRow, loading, onLoadMore]);

  const totalHeight = virtualizer.getTotalSize();

  return (
    <div className="grid-scroll" ref={parentRef}>
      <div className="grid-virtual" style={{ height: totalHeight }}>
        {rows.map((row) => {
          const gridRow = gridRows[row.index];
          if (!gridRow) return null;
          if (gridRow.type === 'group') {
            return (
              <div
                className="grid-group-row"
                key={row.key}
                style={{ transform: `translateY(${row.start}px)`, height: gridRow.height }}
              >
                <span>{gridRow.label}</span>
              </div>
            );
          }
          return (
            <div
              className="grid-row"
              key={row.key}
              style={{ transform: `translateY(${row.start}px)`, height: gridRow.height }}
            >
              {gridRow.items.map(({ asset, width: tileWidth }) => {
                return (
                  <a
                    className="asset-tile"
                    href={buildViewerUrl(asset)}
                    key={asset.id}
                    data-asset-id={asset.id}
                    draggable={false}
                    style={{ width: tileWidth, height: rowHeight }}
                    title={asset.relPath}
                    onMouseDown={(event) => startPressPreview(event, asset)}
                    onDragStart={(event) => event.preventDefault()}
                    onClick={(event) => {
                      if (Date.now() > suppressClickUntil.current) return;
                      event.preventDefault();
                      event.stopPropagation();
                      suppressClickUntil.current = 0;
                    }}
                  >
                    {assetReadyForThumb(asset) ? (
                      <img
                        className="asset-media"
                        src={assetThumbUrl(asset)}
                        alt={asset.filename}
                        loading="lazy"
                        draggable={false}
                        style={rotatedCoverStyle(asset, { width: tileWidth, height: rowHeight })}
                      />
                    ) : (
                      <div className="thumb-placeholder">{assetProcessingText(asset)}</div>
                    )}
                    {asset.mediaType === 'video' && (
                      <span className="asset-video-chip" title="视频">
                        <Play size={12} fill="currentColor" />
                      </span>
                    )}
                  </a>
                );
              })}
            </div>
          );
        })}
      </div>
      {loading && <div className="loading-line">加载中</div>}
    </div>
  );

  function startPressPreview(event: ReactMouseEvent<HTMLAnchorElement>, asset: Asset) {
    if (!onPressPreviewChangeRef.current || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey) {
      return;
    }
    event.preventDefault();
    clearPressTimer();
    pressState.current.pending = true;
    pressState.current.active = false;
    pressState.current.pointerX = event.clientX;
    pressState.current.pointerY = event.clientY;
    pressState.current.timer = window.setTimeout(() => {
      pressState.current.pending = false;
      pressState.current.active = true;
      suppressClickUntil.current = Date.now() + 1000;
      emitPreviewAsset(assetFromPoint() ?? asset);
    }, pressPreviewDelayMs);
  }

  function clearPressTimer() {
    if (!pressState.current.timer) return;
    window.clearTimeout(pressState.current.timer);
    pressState.current.timer = 0;
  }

  function endPressPreview() {
    clearPressTimer();
    pressState.current.pending = false;
    if (!pressState.current.active) return;
    pressState.current.active = false;
    suppressClickUntil.current = Date.now() + 350;
    emitPreviewAsset(null);
  }

  function schedulePreviewUpdate() {
    if (!pressState.current.active || previewFrame.current) return;
    previewFrame.current = window.requestAnimationFrame(() => {
      previewFrame.current = 0;
      updatePreviewFromPoint();
      window.setTimeout(updatePreviewFromPoint, 0);
    });
  }

  function updatePreviewFromPoint() {
    const asset = assetFromPoint();
    if (asset) {
      emitPreviewAsset(asset);
    }
  }

  function assetFromPoint() {
    const target = document.elementFromPoint(pressState.current.pointerX, pressState.current.pointerY);
    if (!(target instanceof Element)) return null;
    const tile = target.closest<HTMLElement>('[data-asset-id]');
    if (!tile || !parentRef.current?.contains(tile)) return null;
    const id = Number(tile.dataset.assetId);
    if (!Number.isFinite(id)) return null;
    return assetsByID.current.get(id) ?? null;
  }

  function emitPreviewAsset(asset: Asset | null) {
    const nextID = asset?.id ?? null;
    if (lastPreviewID.current === nextID) return;
    lastPreviewID.current = nextID;
    onPressPreviewChangeRef.current?.(asset);
  }

  function emitScrollRatio() {
    const element = parentRef.current;
    if (!element || !onScrollRatioChangeRef.current) return;
    const maxScroll = element.scrollHeight - element.clientHeight;
    const ratio = maxScroll > 0 ? element.scrollTop / maxScroll : 0;
    onScrollRatioChangeRef.current(Math.min(1, Math.max(0, ratio)));
  }
}

function clampRatio(value: number) {
  if (!Number.isFinite(value)) return 0;
  return Math.min(1, Math.max(0, value));
}

function buildRows(assets: Asset[], containerWidth: number, groupMode: AssetGroupMode, sort: SortKey): GridRow[] {
  if (containerWidth <= 0) return [];
  const rows: GridRow[] = [];
  let items: RowItem[] = [];
  let usedWidth = 0;
  let currentGroup = '';
  let rowIndex = 0;
  function flushRow(stretch: boolean) {
    if (items.length === 0) return;
    rows.push({ key: `row-${rowIndex}`, type: 'assets', items: stretch ? stretchRow(items, containerWidth) : items, height: rowHeight });
    rowIndex += 1;
    items = [];
    usedWidth = 0;
  }
  for (const asset of assets) {
    const groupLabel = assetGroupLabel(asset, groupMode, sort);
    if (groupLabel && groupLabel !== currentGroup) {
      flushRow(false);
      currentGroup = groupLabel;
      rows.push({ key: `group-${groupLabel}-${asset.id}`, type: 'group', label: groupLabel, height: groupHeaderHeight });
    }
    const tileWidth = Math.min(containerWidth, Math.max(minTileWidth, Math.round(rowHeight * assetAspect(asset))));
    const nextWidth = usedWidth + (items.length > 0 ? gap : 0) + tileWidth;
    if (items.length > 0 && nextWidth > containerWidth) {
      flushRow(true);
    }
    items.push({ asset, width: tileWidth });
    usedWidth += (items.length > 1 ? gap : 0) + tileWidth;
  }
  flushRow(false);
  return rows;
}

function stretchRow(items: RowItem[], containerWidth: number): RowItem[] {
  if (items.length === 0) return items;
  const available = containerWidth - gap * Math.max(0, items.length - 1);
  const current = items.reduce((sum, item) => sum + item.width, 0);
  if (available <= 0 || current <= 0) return items;
  let remaining = available;
  return items.map((item, index) => {
    const width =
      index === items.length - 1 ? remaining : Math.max(minTileWidth, Math.round((item.width / current) * available));
    remaining -= width;
    return { ...item, width };
  });
}

function assetAspect(asset: Asset): number {
  if (asset.width && asset.height && asset.width > 0 && asset.height > 0) {
    return Math.min(maxAspect, Math.max(minAspect, effectiveAspect(asset)));
  }
  if (asset.mediaType === 'video') return 16 / 9;
  return 1;
}

function assetReadyForThumb(asset: Asset): boolean {
  if (asset.mediaType === 'video') return asset.videoPosterStatus === 'ready';
  return asset.thumbStatus === 'ready';
}

function assetProcessingText(asset: Asset): string {
  if (asset.mediaType === 'video') return asset.videoPosterStatus === 'error' ? '封面失败' : '预览生成中';
  return asset.thumbStatus === 'error' ? '缩略图失败' : '处理中';
}
