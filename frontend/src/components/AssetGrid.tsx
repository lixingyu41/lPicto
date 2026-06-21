import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { Play } from 'lucide-react';
import type { Asset, SortKey } from '../types/api';
import { assetThumbUrl } from '../api/client';
import { effectiveAspect, rotatedCoverStyle } from '../utils/rotation';
import { assetGroupLabel, type AssetGroupMode } from '../utils/assetGrouping';
import { gridRowHeightChanged, gridRowHeightForLevel, loadGridRowHeightLevel } from '../utils/gridPrefs';
import { preloadViewerAsset } from '../utils/imagePreload';

interface Props {
  assets: Asset[];
  loading: boolean;
  hasMore: boolean;
  onLoadMore: () => void;
  buildViewerUrl: (asset: Asset) => string;
  onOpenAsset?: (asset: Asset) => void;
  onOpenViewer?: (asset: Asset, viewerUrl: string) => void;
  onAssetMissing?: (asset: Asset) => void;
  onPressPreviewChange?: (asset: Asset | null) => void;
  onScrollRatioChange?: (ratio: number) => void;
  onScrollStateChange?: (state: { ratio: number; scrollTop: number }) => void;
  totalCount?: number;
  loadedStartIndex?: number;
  focusAssetId?: number | null;
  groupMode?: AssetGroupMode;
  sort?: SortKey;
  scrollSignal?: number;
  scrollTarget?: { ratio: number; signal: number };
  scrollTopTarget?: { scrollTop: number; signal: number };
}

interface RowItem {
  asset: Asset;
  index: number;
  width: number;
}

interface AssetGridRow {
  key: string;
  type: 'assets';
  items: RowItem[];
  height: number;
  startAssetIndex: number;
  endAssetIndex: number;
}

interface GroupGridRow {
  key: string;
  type: 'group';
  label: string;
  height: number;
  assetIndex: number;
}

type GridRow = AssetGridRow | GroupGridRow;

const groupHeaderHeight = 34;
const minTileWidth = 84;
const maxAspect = 2.8;
const minAspect = 0.42;
const gap = 10;
const pressPreviewDelayMs = 220;
const pressPreviewDragSlopPx = 6;
const pressPreviewClickSuppressMs = 180;

export default function AssetGrid({
  assets,
  loading,
  hasMore,
  onLoadMore,
  buildViewerUrl,
  onOpenAsset,
  onOpenViewer,
  onAssetMissing,
  onPressPreviewChange,
  onScrollRatioChange,
  onScrollStateChange,
  totalCount = assets.length,
  loadedStartIndex = 0,
  focusAssetId = null,
  groupMode = 'none',
  sort = 'timeline_desc',
  scrollSignal = 0,
  scrollTarget,
  scrollTopTarget,
}: Props) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const assetsByID = useRef<Map<number, Asset>>(new Map());
  const pressState = useRef({
    active: false,
    moved: false,
    pending: false,
    pointerX: 0,
    pointerY: 0,
    previewStartedAt: 0,
    startX: 0,
    startY: 0,
    timer: 0,
  });
  const previewFrame = useRef(0);
  const hoverPreloadTimer = useRef(0);
  const lastPreviewID = useRef<number | null>(null);
  const gridRowsRef = useRef<GridRow[]>([]);
  const scrollMetaRef = useRef({ loadedStartIndex: 0, totalCount: assets.length });
  const appliedFocusAssetId = useRef<number | null>(null);
  const appliedScrollTopTargetSignal = useRef<number | null>(null);
  const onPressPreviewChangeRef = useRef(onPressPreviewChange);
  const onOpenAssetRef = useRef(onOpenAsset);
  const onOpenViewerRef = useRef(onOpenViewer);
  const onAssetMissingRef = useRef(onAssetMissing);
  const onScrollRatioChangeRef = useRef(onScrollRatioChange);
  const onScrollStateChangeRef = useRef(onScrollStateChange);
  const suppressClickUntil = useRef(0);
  const [width, setWidth] = useState(0);
  const [rowHeight, setRowHeight] = useState(() => gridRowHeightForLevel(loadGridRowHeightLevel()));
  const visibleAssets = useMemo(() => assets.filter(assetReadyForThumb), [assets]);
  useEffect(() => {
    assetsByID.current = new Map(visibleAssets.map((asset) => [asset.id, asset]));
  }, [visibleAssets]);

  useEffect(() => {
    onOpenAssetRef.current = onOpenAsset;
  }, [onOpenAsset]);

  useEffect(() => {
    onOpenViewerRef.current = onOpenViewer;
  }, [onOpenViewer]);

  useEffect(() => {
    onAssetMissingRef.current = onAssetMissing;
  }, [onAssetMissing]);

  useEffect(() => {
    onPressPreviewChangeRef.current = onPressPreviewChange;
  }, [onPressPreviewChange]);

  useEffect(() => {
    onScrollRatioChangeRef.current = onScrollRatioChange;
  }, [onScrollRatioChange]);

  useEffect(() => {
    onScrollStateChangeRef.current = onScrollStateChange;
  }, [onScrollStateChange]);

  useEffect(() => {
    scrollMetaRef.current = { loadedStartIndex, totalCount: totalCount === assets.length ? visibleAssets.length : totalCount };
  }, [assets.length, loadedStartIndex, totalCount, visibleAssets.length]);

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
    const updateRowHeight = () => setRowHeight(gridRowHeightForLevel(loadGridRowHeightLevel()));
    window.addEventListener(gridRowHeightChanged, updateRowHeight);
    window.addEventListener('storage', updateRowHeight);
    return () => {
      window.removeEventListener(gridRowHeightChanged, updateRowHeight);
      window.removeEventListener('storage', updateRowHeight);
    };
  }, []);

  useEffect(() => {
    if (parentRef.current) {
      parentRef.current.scrollTop = 0;
      emitScrollState();
    }
  }, [scrollSignal]);

  useEffect(() => {
    if (!parentRef.current || !scrollTarget) return;
    const element = parentRef.current;
    element.scrollTop = scrollTopForGlobalRatio(element, gridRowsRef.current, scrollTarget.ratio, scrollMetaRef.current);
    emitScrollState();
  }, [scrollTarget?.signal]);

  useEffect(() => {
    function handleMouseMove(event: MouseEvent) {
      if (!pressState.current.pending && !pressState.current.active) return;
      trackPressPointer(event.clientX, event.clientY);
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
      clearHoverPreloadTimer();
    };
  }, []);

  useEffect(() => {
    const element = parentRef.current;
    if (!element) return;
    function handleScroll() {
      schedulePreviewUpdate();
      emitScrollState();
    }
    element.addEventListener('scroll', handleScroll, { passive: true });
    return () => element.removeEventListener('scroll', handleScroll);
  }, []);

  const gridRows = useMemo(() => buildRows(visibleAssets, width, groupMode, sort, rowHeight), [groupMode, rowHeight, sort, visibleAssets, width]);
  gridRowsRef.current = gridRows;
  const virtualizer = useVirtualizer({
    count: gridRows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => (gridRows[index]?.height ?? rowHeight) + gap,
    overscan: 5,
  });

  useEffect(() => {
    virtualizer.measure();
  }, [rowHeight, virtualizer]);

  useEffect(() => {
    emitScrollState();
  }, [gridRows, loadedStartIndex, totalCount]);

  const rows = virtualizer.getVirtualItems();
  const lastRow = rows[rows.length - 1];
  useEffect(() => {
    if (!lastRow) return;
    if (hasMore && !loading && lastRow.index >= gridRows.length - 3) {
      onLoadMore();
    }
  }, [gridRows.length, hasMore, lastRow, loading, onLoadMore]);

  const totalHeight = virtualizer.getTotalSize();

  useEffect(() => {
    if (!parentRef.current || !focusAssetId || appliedFocusAssetId.current === focusAssetId) return;
    const element = parentRef.current;
    const targetTop = scrollTopForAsset(element, gridRowsRef.current, focusAssetId);
    if (targetTop === null) return;
    const frame = window.requestAnimationFrame(() => {
      element.scrollTop = targetTop;
      appliedFocusAssetId.current = focusAssetId;
      emitScrollState();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [focusAssetId, gridRows, totalHeight]);

  useEffect(() => {
    if (!parentRef.current || !scrollTopTarget) return;
    if (appliedScrollTopTargetSignal.current === scrollTopTarget.signal) return;
    if (scrollTopTarget.scrollTop > 0 && gridRowsRef.current.length === 0) return;
    const element = parentRef.current;
    const frame = window.requestAnimationFrame(() => {
      const maxScroll = Math.max(0, element.scrollHeight - element.clientHeight);
      element.scrollTop = Math.min(maxScroll, Math.max(0, scrollTopTarget.scrollTop));
      appliedScrollTopTargetSignal.current = scrollTopTarget.signal;
      emitScrollState();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [scrollTopTarget?.scrollTop, scrollTopTarget?.signal, totalHeight]);

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
                    onFocus={() => preloadViewerAsset(asset)}
                    onMouseEnter={() => scheduleHoverPreload(asset)}
                    onMouseLeave={clearHoverPreloadTimer}
                    onMouseDown={(event) => startPressPreview(event, asset)}
                    onDragStart={(event) => event.preventDefault()}
                    onClick={(event) => {
                      if (Date.now() <= suppressClickUntil.current) {
                        event.preventDefault();
                        event.stopPropagation();
                        suppressClickUntil.current = 0;
                        return;
                      }
                      const viewerUrl = buildViewerUrl(asset);
                      event.currentTarget.href = viewerUrl;
                      preloadViewerAsset(asset, 'high');
                      onOpenAssetRef.current?.(asset);
                      if (onOpenViewerRef.current && !usesNativeNavigation(event)) {
                        event.preventDefault();
                        onOpenViewerRef.current(asset, viewerUrl);
                      }
                    }}
                  >
                    <AssetTileMedia
                      asset={asset}
                      rowHeight={rowHeight}
                      tileWidth={tileWidth}
                      onMissing={() => onAssetMissingRef.current?.(asset)}
                    />
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
      {loading && <div className="grid-loading-dot" aria-label="加载中" />}
    </div>
  );

  function startPressPreview(event: ReactMouseEvent<HTMLAnchorElement>, asset: Asset) {
    clearHoverPreloadTimer();
    preloadViewerAsset(asset, 'high');
    if (!onPressPreviewChangeRef.current || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey) {
      return;
    }
    event.currentTarget.href = buildViewerUrl(asset);
    onOpenAssetRef.current?.(asset);
    event.preventDefault();
    clearPressTimer();
    pressState.current.pending = true;
    pressState.current.active = false;
    pressState.current.moved = false;
    pressState.current.pointerX = event.clientX;
    pressState.current.pointerY = event.clientY;
    pressState.current.previewStartedAt = 0;
    pressState.current.startX = event.clientX;
    pressState.current.startY = event.clientY;
    pressState.current.timer = window.setTimeout(() => {
      pressState.current.pending = false;
      pressState.current.active = true;
      pressState.current.previewStartedAt = Date.now();
      emitPreviewAsset(assetFromPoint() ?? asset);
    }, pressPreviewDelayMs);
  }

  function clearPressTimer() {
    if (!pressState.current.timer) return;
    window.clearTimeout(pressState.current.timer);
    pressState.current.timer = 0;
  }

  function scheduleHoverPreload(asset: Asset) {
    clearHoverPreloadTimer();
    hoverPreloadTimer.current = window.setTimeout(() => preloadViewerAsset(asset), 90);
  }

  function clearHoverPreloadTimer() {
    if (!hoverPreloadTimer.current) return;
    window.clearTimeout(hoverPreloadTimer.current);
    hoverPreloadTimer.current = 0;
  }

  function endPressPreview() {
    clearPressTimer();
    pressState.current.pending = false;
    if (!pressState.current.active) return;
    const previewDuration = Date.now() - pressState.current.previewStartedAt;
    const shouldSuppressClick = pressState.current.moved || previewDuration >= pressPreviewClickSuppressMs;
    pressState.current.active = false;
    pressState.current.previewStartedAt = 0;
    suppressClickUntil.current = shouldSuppressClick ? Date.now() + 350 : 0;
    emitPreviewAsset(null);
  }

  function trackPressPointer(clientX: number, clientY: number) {
    pressState.current.pointerX = clientX;
    pressState.current.pointerY = clientY;
    const dx = clientX - pressState.current.startX;
    const dy = clientY - pressState.current.startY;
    if (dx * dx + dy * dy >= pressPreviewDragSlopPx * pressPreviewDragSlopPx) {
      pressState.current.moved = true;
    }
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

  function emitScrollState() {
    const element = parentRef.current;
    if (!element) return;
    const ratio = fullScrollRatio(element);
    const clamped = Math.min(1, Math.max(0, ratio));
    onScrollRatioChangeRef.current?.(clamped);
    onScrollStateChangeRef.current?.({ ratio: clamped, scrollTop: element.scrollTop });
  }

  function fullScrollRatio(element: HTMLDivElement) {
    const { loadedStartIndex: startIndex, totalCount: fullCount } = scrollMetaRef.current;
    const rowsForRatio = gridRowsRef.current;
    if (fullCount <= 1 || rowsForRatio.length === 0) {
      const maxScroll = element.scrollHeight - element.clientHeight;
      return maxScroll > 0 ? element.scrollTop / maxScroll : 0;
    }
    const localIndex = localAssetIndexAtScrollTop(rowsForRatio, element.scrollTop);
    return clampRatio((startIndex + localIndex) / Math.max(1, fullCount - 1));
  }
}

function usesNativeNavigation(event: ReactMouseEvent<HTMLAnchorElement>) {
  return event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0;
}

function AssetTileMedia({
  asset,
  rowHeight,
  tileWidth,
  onMissing,
}: {
  asset: Asset;
  rowHeight: number;
  tileWidth: number;
  onMissing: () => void;
}) {
  const [sourceFailed, setSourceFailed] = useState(false);

  useEffect(() => {
    setSourceFailed(false);
  }, [asset.id, asset.cacheKey]);

  const thumbReady = assetReadyForThumb(asset);
  if (!thumbReady || sourceFailed) {
    return null;
  }

  return (
    <img
      className="asset-media"
      src={assetThumbUrl(asset)}
      alt={asset.filename}
      loading="lazy"
      decoding="async"
      draggable={false}
      style={rotatedCoverStyle(asset, { width: tileWidth, height: rowHeight })}
      onError={() => {
        setSourceFailed(true);
        onMissing();
      }}
    />
  );
}

function clampRatio(value: number) {
  if (!Number.isFinite(value)) return 0;
  return Math.min(1, Math.max(0, value));
}

function localAssetIndexAtScrollTop(rows: GridRow[], scrollTop: number) {
  let offset = 0;
  let lastIndex = 0;
  for (const row of rows) {
    const rowExtent = row.height + gap;
    if (scrollTop <= offset + rowExtent) {
      if (row.type === 'group') return row.assetIndex;
      const rowProgress = row.height > 0 ? clampRatio((scrollTop - offset) / row.height) : 0;
      const span = Math.max(1, row.items.length);
      return Math.min(row.endAssetIndex, row.startAssetIndex + rowProgress * span);
    }
    if (row.type === 'group') {
      lastIndex = row.assetIndex;
    } else {
      lastIndex = row.endAssetIndex;
    }
    offset += rowExtent;
  }
  return lastIndex;
}

function scrollTopForGlobalRatio(
  element: HTMLDivElement,
  rows: GridRow[],
  ratio: number,
  meta: { loadedStartIndex: number; totalCount: number },
) {
  if (meta.totalCount <= 1 || rows.length === 0) {
    const maxScroll = element.scrollHeight - element.clientHeight;
    return maxScroll > 0 ? maxScroll * clampRatio(ratio) : 0;
  }
  const targetLocalIndex = clampRatio(ratio) * (meta.totalCount - 1) - meta.loadedStartIndex;
  let offset = 0;
  for (const row of rows) {
    if (row.type === 'assets' && targetLocalIndex <= row.endAssetIndex) {
      return offset;
    }
    offset += row.height + gap;
  }
  return Math.max(0, element.scrollHeight - element.clientHeight);
}

function scrollTopForAsset(element: HTMLDivElement, rows: GridRow[], assetId: number) {
  let offset = 0;
  for (const row of rows) {
    if (row.type === 'assets' && row.items.some((item) => item.asset.id === assetId)) {
      const centered = offset - Math.max(0, (element.clientHeight - row.height) / 2);
      const maxScroll = Math.max(0, element.scrollHeight - element.clientHeight);
      return Math.min(maxScroll, Math.max(0, centered));
    }
    offset += row.height + gap;
  }
  return null;
}

function buildRows(
  assets: Asset[],
  containerWidth: number,
  groupMode: AssetGroupMode,
  sort: SortKey,
  rowHeight: number,
): GridRow[] {
  if (containerWidth <= 0) return [];
  const rows: GridRow[] = [];
  let items: RowItem[] = [];
  let usedWidth = 0;
  let currentGroup = '';
  let rowIndex = 0;
  function flushRow(stretch: boolean) {
    if (items.length === 0) return;
    rows.push({
      key: `row-${rowIndex}`,
      type: 'assets',
      items: stretch ? stretchRow(items, containerWidth) : items,
      height: rowHeight,
      startAssetIndex: items[0].index,
      endAssetIndex: items[items.length - 1].index,
    });
    rowIndex += 1;
    items = [];
    usedWidth = 0;
  }
  for (const [assetIndex, asset] of assets.entries()) {
    const groupLabel = assetGroupLabel(asset, groupMode, sort);
    if (groupLabel && groupLabel !== currentGroup) {
      flushRow(false);
      currentGroup = groupLabel;
      rows.push({ key: `group-${groupLabel}-${asset.id}`, type: 'group', label: groupLabel, height: groupHeaderHeight, assetIndex });
    }
    const tileWidth = Math.min(containerWidth, Math.max(minTileWidth, Math.round(rowHeight * assetAspect(asset))));
    const nextWidth = usedWidth + (items.length > 0 ? gap : 0) + tileWidth;
    if (items.length > 0 && nextWidth > containerWidth) {
      flushRow(true);
    }
    items.push({ asset, index: assetIndex, width: tileWidth });
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
  return asset.thumbStatus === 'ready';
}
