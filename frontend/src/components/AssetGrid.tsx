import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { Check, Play, Plus, Square, X } from 'lucide-react';
import type { Asset, SortField, SortKey } from '../types/api';
import { api, assetThumbUrl } from '../api/client';
import { effectiveAspect, rotatedCoverStyle } from '../utils/rotation';
import { assetGroupLabel, type AssetGroupMode } from '../utils/assetGrouping';
import { gridRowHeightChanged, gridRowHeightForLevel, loadGridRowHeightLevel } from '../utils/gridPrefs';
import { preloadViewerAsset } from '../utils/imagePreload';
import { viewerOverlayCloseCompleted } from '../utils/pageState';
import {
  waterfallOverscanRows,
  waterfallPrefetchScreens,
  waterfallPreviousPrefetchScreens,
  waterfallResizeSettleMs,
} from '../utils/waterfallPaging';
import {
  assetGridBatchCommandEvent,
  assetGridBatchStateEvent,
  assetGridBatchStateRequestEvent,
  type AssetGridBatchCommand,
  type AssetGridBatchState,
} from '../utils/batchSelection';
import {
  loadMediaViewPreferences,
  mediaColumnDefinition,
  mediaColumnWidth,
  mediaViewPreferencesChanged,
  orderedVisibleColumns,
  saveMediaViewPreferences,
  type MediaColumnId,
  type MediaViewPreferences,
} from '../utils/mediaViewPrefs';
import HierarchicalTagPicker from './HierarchicalTagPicker';

interface Props {
  assets: Asset[];
  loading: boolean;
  hasMore: boolean;
  hasPrevious?: boolean;
  onLoadMore: () => void;
  onLoadPrevious?: () => void;
  buildViewerUrl: (asset: Asset) => string;
  onOpenAsset?: (asset: Asset) => void;
  onOpenViewer?: (asset: Asset, viewerUrl: string) => void;
  onPressPreviewChange?: (asset: Asset | null) => void;
  onScrollRatioChange?: (ratio: number) => void;
  onScrollStateChange?: (state: { ratio: number; scrollTop: number }) => void;
  totalCount?: number;
  loadedStartIndex?: number;
  focusAssetId?: number | null;
  groupMode?: AssetGroupMode;
  sort?: SortKey;
  onSortChange?: (sort: SortKey) => void;
  selectedTags?: string[];
  onTagFilterChange?: (tags: string[]) => void;
  scrollSignal?: number;
  scrollTarget?: { ratio: number; signal: number };
  scrollTopTarget?: { scrollTop: number; signal: number };
  enableBatchMode?: boolean;
  onBatchRemoveAssets?: (assetIds: number[]) => void;
  onBatchDeleteComplete?: () => void | Promise<void>;
  purgeUnavailableOnDelete?: boolean;
  duplicateGrouping?: boolean;
  autoSelectAssetIds?: () => Promise<number[]>;
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
  variant?: 'native' | 'duplicate';
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

interface MediaLayoutDriver {
  table: boolean;
  buildRows: (
    assets: Asset[],
    width: number,
    groupMode: AssetGroupMode,
    sort: SortKey,
    rowHeight: number,
    duplicateGrouping: boolean,
  ) => GridRow[];
}

const mediaLayoutDrivers: Record<MediaViewPreferences['mode'], MediaLayoutDriver> = {
  waterfall: {
    table: false,
    buildRows: (assets, width, groupMode, sort, rowHeight, duplicateGrouping) => (
      buildRows(assets, width, groupMode, sort, rowHeight, duplicateGrouping)
    ),
  },
  list: {
    table: true,
    buildRows: (assets, _width, groupMode, sort, rowHeight, duplicateGrouping) => (
      buildListRows(assets, groupMode, sort, rowHeight, duplicateGrouping)
    ),
  },
};

export default function AssetGrid({
  assets,
  loading,
  hasMore,
  hasPrevious = false,
  onLoadMore,
  onLoadPrevious,
  buildViewerUrl,
  onOpenAsset,
  onOpenViewer,
  onPressPreviewChange,
  onScrollRatioChange,
  onScrollStateChange,
  totalCount = assets.length,
  loadedStartIndex = 0,
  focusAssetId = null,
  groupMode = 'none',
  sort = 'timeline_desc',
  onSortChange,
  selectedTags = [],
  onTagFilterChange,
  scrollSignal = 0,
  scrollTarget,
  scrollTopTarget,
  enableBatchMode = true,
  onBatchRemoveAssets,
  onBatchDeleteComplete,
  purgeUnavailableOnDelete = false,
  duplicateGrouping = false,
  autoSelectAssetIds,
}: Props) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const forwardPageSentinelRef = useRef<HTMLDivElement | null>(null);
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
  const prependAnchorRef = useRef<{ firstAssetId: number | null; loadedStartIndex: number; scrollTop: number }>({
    firstAssetId: null,
    loadedStartIndex: 0,
    scrollTop: 0,
  });
  const scrollMetaRef = useRef({ loadedStartIndex: 0, totalCount: assets.length });
  const appliedFocusAssetId = useRef<number | null>(null);
  const appliedScrollTopTargetSignal = useRef<number | null>(null);
  const focusAssetIdRef = useRef<number | null>(focusAssetId);
  const onPressPreviewChangeRef = useRef(onPressPreviewChange);
  const onOpenAssetRef = useRef(onOpenAsset);
  const onOpenViewerRef = useRef(onOpenViewer);
  const onScrollRatioChangeRef = useRef(onScrollRatioChange);
  const onScrollStateChangeRef = useRef(onScrollStateChange);
  const suppressClickUntil = useRef(0);
  const resizeSettleTimer = useRef(0);
  const scrollSample = useRef({ at: performance.now(), top: 0, velocity: 0 });
  const pagingRuntime = useRef({ hasMore, hasPrevious, loading, onLoadMore, onLoadPrevious });
  const batchCommandHandlerRef = useRef<(command: AssetGridBatchCommand) => void>(() => undefined);
  const publishBatchStateRef = useRef<() => void>(() => undefined);
  const [viewport, setViewport] = useState({ height: 0, width: 0 });
  const [rowHeight, setRowHeight] = useState(() => gridRowHeightForLevel(loadGridRowHeightLevel()));
  const [mediaViewPrefs, setMediaViewPrefs] = useState<MediaViewPreferences>(() => loadMediaViewPreferences());
  const [highlightedFocusAssetId, setHighlightedFocusAssetId] = useState<number | null>(null);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedAssetIds, setSelectedAssetIds] = useState<Set<number>>(() => new Set());
  const [batchBusy, setBatchBusy] = useState(false);
  const [batchMessage, setBatchMessage] = useState('');
  const [batchProgress, setBatchProgress] = useState<{ current: number; total: number } | null>(null);
  const visibleAssets = assets;
  const width = viewport.width;
  const selectedIds = useMemo(() => Array.from(selectedAssetIds), [selectedAssetIds]);
  const layoutDriver = mediaLayoutDrivers[mediaViewPrefs.mode];
  const listColumns = useMemo(() => orderedVisibleColumns(mediaViewPrefs), [mediaViewPrefs]);
  const listWidth = useMemo(
    () => listColumns.reduce((total, column) => total + mediaColumnWidth(mediaViewPrefs, column), 0),
    [listColumns, mediaViewPrefs],
  );
  batchCommandHandlerRef.current = handleBatchCommand;
  publishBatchStateRef.current = () => {
    const detail: AssetGridBatchState = {
      available: enableBatchMode,
      busy: batchBusy,
      canAutoSelect: Boolean(autoSelectAssetIds),
      message: batchMessage,
      progress: batchProgress,
      selectedCount: selectedAssetIds.size,
      selectionMode,
    };
    window.dispatchEvent(new CustomEvent(assetGridBatchStateEvent, { detail }));
  };
  pagingRuntime.current = { hasMore, hasPrevious, loading, onLoadMore, onLoadPrevious };

  useEffect(() => {
    const handleCommand = (event: Event) => {
      const command = (event as CustomEvent<{ command: AssetGridBatchCommand }>).detail?.command;
      if (command) batchCommandHandlerRef.current(command);
    };
    const handleStateRequest = () => publishBatchStateRef.current();
    window.addEventListener(assetGridBatchCommandEvent, handleCommand);
    window.addEventListener(assetGridBatchStateRequestEvent, handleStateRequest);
    return () => {
      window.removeEventListener(assetGridBatchCommandEvent, handleCommand);
      window.removeEventListener(assetGridBatchStateRequestEvent, handleStateRequest);
      const detail: AssetGridBatchState = { available: false, busy: false, canAutoSelect: false, message: '', progress: null, selectedCount: 0, selectionMode: false };
      window.dispatchEvent(new CustomEvent(assetGridBatchStateEvent, { detail }));
    };
  }, []);

  useEffect(() => publishBatchStateRef.current(), [autoSelectAssetIds, batchBusy, batchMessage, batchProgress, enableBatchMode, selectedAssetIds.size, selectionMode]);
  useEffect(() => {
    assetsByID.current = new Map(visibleAssets.map((asset) => [asset.id, asset]));
  }, [visibleAssets]);

  useEffect(() => {
    if (autoSelectAssetIds) return;
    setSelectedAssetIds((current) => {
      if (current.size === 0) return current;
      const liveIds = new Set(visibleAssets.map((asset) => asset.id));
      let changed = false;
      const next = new Set<number>();
      current.forEach((id) => {
        if (liveIds.has(id)) {
          next.add(id);
        } else {
          changed = true;
        }
      });
      return changed ? next : current;
    });
  }, [autoSelectAssetIds, visibleAssets]);

  useEffect(() => {
    onOpenAssetRef.current = onOpenAsset;
  }, [onOpenAsset]);

  useEffect(() => {
    onOpenViewerRef.current = onOpenViewer;
  }, [onOpenViewer]);

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
    focusAssetIdRef.current = focusAssetId;
  }, [focusAssetId]);

  useEffect(() => {
    scrollMetaRef.current = { loadedStartIndex, totalCount: totalCount === assets.length ? visibleAssets.length : totalCount };
  }, [assets.length, loadedStartIndex, totalCount, visibleAssets.length]);

  useEffect(() => {
    if (!parentRef.current) return;
    const initial = parentRef.current.getBoundingClientRect();
    setViewport({ height: initial.height, width: initial.width });
    const observer = new ResizeObserver(([entry]) => {
      window.clearTimeout(resizeSettleTimer.current);
      resizeSettleTimer.current = window.setTimeout(() => {
        const { height, width: nextWidth } = entry.contentRect;
        setViewport((current) => (
          Math.abs(current.width - nextWidth) < 8 && Math.abs(current.height - height) < 8
            ? current
            : { height, width: nextWidth }
        ));
      }, waterfallResizeSettleMs);
    });
    observer.observe(parentRef.current);
    return () => {
      window.clearTimeout(resizeSettleTimer.current);
      observer.disconnect();
    };
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
    const updatePreferences = (event?: Event) => {
      const detail = (event as CustomEvent<MediaViewPreferences> | undefined)?.detail;
      setMediaViewPrefs(detail ?? loadMediaViewPreferences());
    };
    window.addEventListener(mediaViewPreferencesChanged, updatePreferences);
    window.addEventListener('storage', updatePreferences);
    return () => {
      window.removeEventListener(mediaViewPreferencesChanged, updatePreferences);
      window.removeEventListener('storage', updatePreferences);
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
      const now = performance.now();
      const elapsed = Math.max(1, now - scrollSample.current.at);
      const velocity = Math.abs(element!.scrollTop - scrollSample.current.top) / elapsed;
      scrollSample.current = { at: now, top: element!.scrollTop, velocity };
      schedulePreviewUpdate();
      emitScrollState();
      requestWaterfallPages(element!, velocity);
    }
    element.addEventListener('scroll', handleScroll, { passive: true });
    return () => element.removeEventListener('scroll', handleScroll);
  }, []);

  const gridRows = useMemo(
    () => layoutDriver.buildRows(visibleAssets, width, groupMode, sort, rowHeight, duplicateGrouping),
    [duplicateGrouping, groupMode, layoutDriver, rowHeight, sort, visibleAssets, width],
  );
  gridRowsRef.current = gridRows;
  const virtualizer = useVirtualizer({
    count: gridRows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => (gridRows[index]?.height ?? rowHeight) + gap,
    overscan: waterfallOverscanRows(viewport.height, rowHeight, gap),
  });

  useEffect(() => {
    virtualizer.measure();
  }, [rowHeight, virtualizer]);

  useEffect(() => {
    emitScrollState();
  }, [gridRows, loadedStartIndex, totalCount]);

  const rows = virtualizer.getVirtualItems();
  useLayoutEffect(() => {
    const element = parentRef.current;
    const previous = prependAnchorRef.current;
    if (element && loadedStartIndex < previous.loadedStartIndex && previous.firstAssetId !== null) {
      const anchorTop = scrollTopForAssetTop(gridRowsRef.current, previous.firstAssetId);
      if (anchorTop !== null) {
        element.scrollTop = anchorTop + previous.scrollTop;
        emitScrollState();
      }
    }
    prependAnchorRef.current = {
      firstAssetId: visibleAssets[0]?.id ?? null,
      loadedStartIndex,
      scrollTop: element?.scrollTop ?? 0,
    };
  }, [gridRows, loadedStartIndex, visibleAssets]);

  useEffect(() => {
    const element = parentRef.current;
    if (!element || gridRows.length === 0) return undefined;
    const frame = window.requestAnimationFrame(() => requestWaterfallPages(element, scrollSample.current.velocity));
    return () => window.cancelAnimationFrame(frame);
  }, [gridRows.length, hasMore, hasPrevious, loading, viewport.height, viewport.width]);

  const totalHeight = virtualizer.getTotalSize();

  useEffect(() => {
    const element = parentRef.current;
    const sentinel = forwardPageSentinelRef.current;
    if (!element || !sentinel || !hasMore) return undefined;
    const lead = Math.max(1, Math.ceil(element.clientHeight * waterfallPrefetchScreens(scrollSample.current.velocity)));
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          requestWaterfallPages(element, scrollSample.current.velocity);
        }
      },
      {
        root: element,
        rootMargin: `0px 0px ${lead}px 0px`,
        threshold: 0,
      },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, loading, totalHeight, viewport.height]);

  useEffect(() => {
    if (!parentRef.current || !focusAssetId || appliedFocusAssetId.current === focusAssetId) return;
    const element = parentRef.current;
    const targetTop = scrollTopForAsset(element, gridRowsRef.current, focusAssetId);
    if (targetTop === null) return;
    const frame = window.requestAnimationFrame(() => {
      element.scrollTop = targetTop;
      appliedFocusAssetId.current = focusAssetId;
      pulseFocusAsset(focusAssetId);
      emitScrollState();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [focusAssetId, gridRows, totalHeight]);

  useEffect(() => {
    function pulseFocusedAssetAfterViewerClose() {
      if (focusAssetIdRef.current) pulseFocusAsset(focusAssetIdRef.current);
    }
    window.addEventListener(viewerOverlayCloseCompleted, pulseFocusedAssetAfterViewerClose);
    return () => window.removeEventListener(viewerOverlayCloseCompleted, pulseFocusedAssetAfterViewerClose);
  }, []);

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
    <div className={`grid-shell media-view-${mediaViewPrefs.mode}${selectionMode ? ' batch-selection-active' : ''}${duplicateGrouping ? ' duplicate-grouping' : ''}`}>
      <div className="grid-scroll" ref={parentRef}>
      {layoutDriver.table && (
        <div className="asset-list-header" style={{ width: Math.max(listWidth, viewport.width) }}>
          {listColumns.map((column) => (
            <AssetListHeader
              column={column}
              key={column}
              preferences={mediaViewPrefs}
              selectedTags={selectedTags}
              sort={sort}
              onResize={resizeListColumn}
              onSort={onSortChange}
              onTagFilterChange={onTagFilterChange}
            />
          ))}
        </div>
      )}
      <div
        className={layoutDriver.table ? 'grid-virtual asset-list-virtual' : 'grid-virtual'}
        style={{ height: totalHeight, width: layoutDriver.table ? Math.max(listWidth, viewport.width) : undefined }}
      >
        {rows.map((row) => {
          const gridRow = gridRows[row.index];
          if (!gridRow) return null;
          if (gridRow.type === 'group') {
            return (
              <div
                className={gridRow.variant === 'duplicate' ? 'grid-group-row duplicate-group' : 'grid-group-row'}
                key={row.key}
                style={{ transform: `translateY(${row.start}px)`, height: gridRow.height }}
              >
                <span>{gridRow.label}</span>
              </div>
            );
          }
          if (layoutDriver.table) {
            const asset = gridRow.items[0]?.asset;
            if (!asset) return null;
            return (
              <a
                className={`asset-list-row ${tileClassName(asset.id)}`}
                data-asset-id={asset.id}
                draggable={false}
                href={buildViewerUrl(asset)}
                key={row.key}
                style={{ transform: `translateY(${row.start}px)`, height: gridRow.height }}
                title={asset.filename}
                onFocus={() => preloadViewerAsset(asset)}
                onMouseEnter={() => scheduleHoverPreload(asset)}
                onMouseLeave={clearHoverPreloadTimer}
                onMouseDown={(event) => startPressPreview(event, asset)}
                onDragStart={(event) => event.preventDefault()}
                onClick={(event) => handleAssetClick(event, asset)}
              >
                {listColumns.map((column) => (
                  <AssetListCell
                    asset={asset}
                    column={column}
                    key={column}
                    preferences={mediaViewPrefs}
                    rowHeight={rowHeight}
                  />
                ))}
                {selectionMode && (
                  <span className={selectedAssetIds.has(asset.id) ? 'asset-select-box selected' : 'asset-select-box'}>
                    <Square size={14} />
                  </span>
                )}
              </a>
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
                    className={tileClassName(asset.id)}
                    href={buildViewerUrl(asset)}
                    key={asset.id}
                    data-asset-id={asset.id}
                    draggable={false}
                    style={{ width: tileWidth, height: rowHeight }}
                    title={asset.filename}
                    onFocus={() => preloadViewerAsset(asset)}
                    onMouseEnter={() => scheduleHoverPreload(asset)}
                    onMouseLeave={clearHoverPreloadTimer}
                    onMouseDown={(event) => startPressPreview(event, asset)}
                    onDragStart={(event) => event.preventDefault()}
                    onClick={(event) => handleAssetClick(event, asset)}
                  >
                    <AssetTileMedia
                      asset={asset}
                      rowHeight={rowHeight}
                      tileWidth={tileWidth}
                    />
                    {asset.mediaType === 'video' && (
                      <span className="asset-video-chip" title="视频">
                        <Play size={12} fill="currentColor" />
                      </span>
                    )}
                    {selectionMode && (
                      <span className={selectedAssetIds.has(asset.id) ? 'asset-select-box selected' : 'asset-select-box'}>
                        <Square size={14} />
                      </span>
                    )}
                  </a>
                );
              })}
            </div>
          );
        })}
        <div
          aria-hidden="true"
          ref={forwardPageSentinelRef}
          style={{ bottom: 0, height: 1, left: 0, pointerEvents: 'none', position: 'absolute', width: 1 }}
        />
      </div>
      {loading && <div className="grid-loading-dot" aria-label="加载中" />}
      </div>
    </div>
  );

  function tileClassName(assetId: number) {
    const classes = ['asset-tile'];
    if (assetId === highlightedFocusAssetId) classes.push('asset-tile-viewer-focus');
    if (selectionMode) classes.push('selectable');
    if (selectedAssetIds.has(assetId)) classes.push('selected');
    return classes.join(' ');
  }

  function handleAssetClick(event: ReactMouseEvent<HTMLAnchorElement>, asset: Asset) {
    if (selectionMode) {
      event.preventDefault();
      event.stopPropagation();
      toggleAssetSelection(asset.id);
      return;
    }
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
  }

  function resizeListColumn(column: MediaColumnId, width: number, persist: boolean) {
    setMediaViewPrefs((current) => {
      const next = { ...current, columnWidths: { ...current.columnWidths, [column]: width } };
      if (persist) saveMediaViewPreferences(next);
      return next;
    });
  }

  function requestWaterfallPages(element: HTMLDivElement, velocity: number) {
    const runtime = pagingRuntime.current;
    if (runtime.loading || element.clientHeight <= 0) return;
    const remaining = Math.max(0, element.scrollHeight - element.scrollTop - element.clientHeight);
    const forwardLead = element.clientHeight * waterfallPrefetchScreens(velocity);
    if (runtime.hasMore && remaining <= forwardLead) {
      runtime.onLoadMore();
      return;
    }
    const backwardLead = element.clientHeight * waterfallPreviousPrefetchScreens;
    if (runtime.hasPrevious && element.scrollTop <= backwardLead) runtime.onLoadPrevious?.();
  }

  function toggleSelectionMode() {
    setSelectionMode((value) => {
      const next = !value;
      if (!next) {
        setSelectedAssetIds(new Set());
        setBatchMessage('');
        setBatchProgress(null);
      }
      return next;
    });
  }

  function selectAllVisible() {
    setSelectionMode(true);
    setSelectedAssetIds(new Set(visibleAssets.map((asset) => asset.id)));
  }

  function handleBatchCommand(command: AssetGridBatchCommand) {
    if (!enableBatchMode || batchBusy) return;
    switch (command) {
      case 'toggle-selection': toggleSelectionMode(); break;
      case 'auto-select': void autoSelectDuplicates(); break;
      case 'select-all': selectAllVisible(); break;
      case 'clear': setSelectedAssetIds(new Set()); break;
      case 'add-tag': void batchAddTag(); break;
      case 'set-rating': void batchSetRating(); break;
      case 'add-album': void batchAddToAlbum(); break;
      case 'rotate': void batchRotate(); break;
      case 'hide': void batchHide(); break;
      case 'delete': void batchDelete(); break;
      case 'delete-records': void batchDeleteRecords(); break;
    }
  }

  async function autoSelectDuplicates() {
    if (!autoSelectAssetIds || batchBusy) return;
    setSelectionMode(true);
    setBatchProgress(null);
    setBatchBusy(true);
    setBatchMessage('正在生成选择');
    try {
      const ids = await autoSelectAssetIds();
      setSelectedAssetIds(new Set(ids));
      setBatchMessage(ids.length > 0 ? `已选择 ${ids.length} 个重复文件` : '没有可自动选择的重复文件');
    } catch (err) {
      setBatchMessage(err instanceof Error ? err.message : '自动选择失败');
    } finally {
      setBatchBusy(false);
    }
  }

  function toggleAssetSelection(assetId: number) {
    setSelectedAssetIds((current) => {
      const next = new Set(current);
      if (next.has(assetId)) {
        next.delete(assetId);
      } else {
        next.add(assetId);
      }
      return next;
    });
  }

  async function runBatch(label: string, task: () => Promise<void>) {
    if (selectedIds.length === 0 || batchBusy) return;
    setBatchBusy(true);
    setBatchMessage(`${label}中`);
    try {
      await task();
      setBatchMessage(`${label}完成`);
    } catch (err) {
      setBatchMessage(err instanceof Error ? err.message : `${label}失败`);
    } finally {
      setBatchBusy(false);
    }
  }

  async function batchAddTag() {
    const tag = window.prompt('标签名');
    if (!tag?.trim()) return;
    await runBatch('加标签', async () => {
      await api.batchAddTags(selectedIds, [tag.trim()]);
    });
  }

  async function batchSetRating() {
    const input = window.prompt('星级 0-5');
    if (input === null) return;
    const rating = Number(input);
    if (!Number.isInteger(rating) || rating < 0 || rating > 5) {
      setBatchMessage('星级必须是 0 到 5');
      return;
    }
    await runBatch('评分', async () => {
      await api.batchSetRating(selectedIds, rating as 0 | 1 | 2 | 3 | 4 | 5);
    });
  }

  async function batchAddToAlbum() {
    const input = window.prompt('相册 ID');
    if (input === null) return;
    const albumId = Number(input);
    if (!Number.isInteger(albumId) || albumId <= 0) {
      setBatchMessage('相册 ID 无效');
      return;
    }
    await runBatch('加入相册', async () => {
      await api.batchAddToAlbum(selectedIds, albumId);
    });
  }

  async function batchRotate() {
    const input = window.prompt('旋转角度：0 / 90 / 180 / 270');
    if (input === null) return;
    const rotation = Number(input);
    if (![0, 90, 180, 270].includes(rotation)) {
      setBatchMessage('旋转角度无效');
      return;
    }
    await runBatch('旋转', async () => {
      await api.batchRotate(selectedIds, rotation);
    });
  }

  async function batchHide() {
    if (!window.confirm(`隐藏 ${selectedIds.length} 个媒体？`)) return;
    await runBatch('隐藏', async () => {
      await api.batchHide(selectedIds, true);
      onBatchRemoveAssets?.(selectedIds);
      setSelectedAssetIds(new Set());
    });
  }

  async function batchDelete() {
    const message = purgeUnavailableOnDelete
      ? `永久删除 ${selectedIds.length} 个媒体的数据库记录和缓存？此操作不可恢复。`
      : `永久删除 ${selectedIds.length} 个媒体及其数据库记录和缓存？此操作不可恢复。`;
    if (!window.confirm(message)) return;
    const ids = [...selectedIds];
    const chunkSize = 10;
    let processed = 0;
    let deleted = 0;
    let failures = 0;
    setBatchBusy(true);
    setBatchProgress({ current: 0, total: ids.length });
    setBatchMessage(`删除中 0/${ids.length}`);
    try {
      for (let offset = 0; offset < ids.length; offset += chunkSize) {
        const chunk = ids.slice(offset, offset + chunkSize);
        const finalChunk = offset + chunkSize >= ids.length;
        const result = await api.batchDelete(chunk, purgeUnavailableOnDelete, finalChunk);
        const deletedIds = result.deletedAssetIds ?? result.updatedAssetIds ?? [];
        const resultFailures = result.failures ?? [];
        deleted += deletedIds.length;
        failures += resultFailures.length;
        processed += chunk.length;
        onBatchRemoveAssets?.(deletedIds);
        setSelectedAssetIds((current) => {
          const next = new Set(current);
          deletedIds.forEach((id) => next.delete(id));
          return next;
        });
        setBatchProgress({ current: processed, total: ids.length });
        setBatchMessage(`删除中 ${processed}/${ids.length}`);
      }
      setSelectedAssetIds(new Set());
      await onBatchDeleteComplete?.();
      setBatchMessage(failures > 0 ? `已删除 ${deleted} 个，${failures} 个失败` : `已删除 ${deleted} 个`);
    } catch (err) {
      if (deleted > 0) {
        await onBatchDeleteComplete?.();
      }
      setBatchMessage(err instanceof Error ? `删除中断：${err.message}` : '删除中断');
    } finally {
      setBatchBusy(false);
    }
  }

  async function batchDeleteRecords() {
    if (!window.confirm(`删除 ${selectedIds.length} 个媒体的全部数据库记录和缓存？源文件不会删除，重新扫描后可恢复。`)) return;
    const ids = [...selectedIds];
    const chunkSize = 100;
    let processed = 0;
    let deleted = 0;
    setBatchBusy(true);
    setBatchProgress({ current: 0, total: ids.length });
    setBatchMessage(`删除记录中 0/${ids.length}`);
    try {
      for (let offset = 0; offset < ids.length; offset += chunkSize) {
        const chunk = ids.slice(offset, offset + chunkSize);
        const finalChunk = offset + chunkSize >= ids.length;
        const result = await api.batchDeleteRecords(chunk, finalChunk);
        const deletedIds = result.deletedAssetIds ?? result.updatedAssetIds ?? [];
        deleted += deletedIds.length;
        processed += chunk.length;
        onBatchRemoveAssets?.(deletedIds);
        setSelectedAssetIds((current) => {
          const next = new Set(current);
          deletedIds.forEach((id) => next.delete(id));
          return next;
        });
        setBatchProgress({ current: processed, total: ids.length });
        setBatchMessage(`删除记录中 ${processed}/${ids.length}`);
      }
      setSelectedAssetIds(new Set());
      await onBatchDeleteComplete?.();
      setBatchMessage(`已删除 ${deleted} 条记录`);
    } catch (err) {
      if (deleted > 0) await onBatchDeleteComplete?.();
      setBatchMessage(err instanceof Error ? `删除记录中断：${err.message}` : '删除记录中断');
    } finally {
      setBatchBusy(false);
    }
  }

  function startPressPreview(event: ReactMouseEvent<HTMLAnchorElement>, asset: Asset) {
    clearHoverPreloadTimer();
    preloadViewerAsset(asset, 'high');
    if (selectionMode || !onPressPreviewChangeRef.current || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey) {
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
    prependAnchorRef.current.scrollTop = element.scrollTop;
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

  function pulseFocusAsset(assetId: number) {
    setHighlightedFocusAssetId(assetId);
    window.setTimeout(() => {
      setHighlightedFocusAssetId((current) => (current === assetId ? null : current));
    }, 1500);
  }
}

function usesNativeNavigation(event: ReactMouseEvent<HTMLAnchorElement>) {
  return event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0;
}

function AssetTileMedia({
  asset,
  rowHeight,
  tileWidth,
}: {
  asset: Asset;
  rowHeight: number;
  tileWidth: number;
}) {
  const [sourceFailed, setSourceFailed] = useState(false);

  useEffect(() => {
    setSourceFailed(false);
  }, [asset.id, asset.cacheKey]);

  const thumbReady = assetReadyForThumb(asset);
  if (!thumbReady || sourceFailed) {
    return (
      <div className="asset-media-placeholder" title={asset.filename}>
        <span>{asset.filename}</span>
      </div>
    );
  }

  return (
    <img
      className="asset-media"
      src={assetThumbUrl(asset)}
      alt={asset.filename}
      loading="eager"
      decoding="async"
      draggable={false}
      style={rotatedCoverStyle(asset, { width: tileWidth, height: rowHeight })}
      onError={() => {
        setSourceFailed(true);
      }}
    />
  );
}

function AssetListHeader({
  column,
  preferences,
  selectedTags,
  sort,
  onResize,
  onSort,
  onTagFilterChange,
}: {
  column: MediaColumnId;
  preferences: MediaViewPreferences;
  selectedTags: string[];
  sort: SortKey;
  onResize: (column: MediaColumnId, width: number, persist: boolean) => void;
  onSort?: (sort: SortKey) => void;
  onTagFilterChange?: (tags: string[]) => void;
}) {
  const definition = mediaColumnDefinition(column);
  const width = mediaColumnWidth(preferences, column);
  const field = sortFieldForColumn(column);
  const sortable = column !== 'palette';
  const active = sortable && (sort === field || sort.startsWith(`${field}_`));
  const direction = active && sort.endsWith('_asc') ? 'asc' : 'desc';

  function beginResize(event: ReactPointerEvent<HTMLSpanElement>) {
    event.preventDefault();
    event.stopPropagation();
    const startX = event.clientX;
    const startWidth = width;
    const pointerID = event.pointerId;
    event.currentTarget.setPointerCapture(pointerID);
    const handle = event.currentTarget;
    const move = (moveEvent: PointerEvent) => {
      const next = Math.min(definition.maxWidth, Math.max(definition.minWidth, startWidth + moveEvent.clientX - startX));
      onResize(column, Math.round(next), false);
    };
    const end = (upEvent: PointerEvent) => {
      const next = Math.min(definition.maxWidth, Math.max(definition.minWidth, startWidth + upEvent.clientX - startX));
      handle.releasePointerCapture(pointerID);
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', end);
      onResize(column, Math.round(next), true);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', end);
    handle.addEventListener('pointercancel', end);
  }

  return (
    <div
      className={column === 'media' ? 'asset-list-head-cell media-column' : 'asset-list-head-cell'}
      style={{ width, minWidth: width }}
    >
      <button
        className={active ? 'asset-list-sort active' : 'asset-list-sort'}
        disabled={!onSort || !sortable}
        type="button"
        onClick={() => sortable && onSort?.(nextSortForColumn(column, sort))}
      >
        <span>{definition.label}</span>
        {active && <span aria-hidden="true">{direction === 'asc' ? '↑' : '↓'}</span>}
      </button>
      {column === 'aiTags' && onTagFilterChange && (
        <HierarchicalTagPicker compact selected={selectedTags} onChange={onTagFilterChange} />
      )}
      <span className="asset-list-resize" role="separator" aria-orientation="vertical" onPointerDown={beginResize} />
    </div>
  );
}

function AssetListCell({
  asset,
  column,
  preferences,
  rowHeight,
}: {
  asset: Asset;
  column: MediaColumnId;
  preferences: MediaViewPreferences;
  rowHeight: number;
}) {
  const width = mediaColumnWidth(preferences, column);
  const style = { width, minWidth: width };
  if (column === 'media') {
    const thumbHeight = Math.max(64, rowHeight - 16);
    const thumbWidth = Math.min(128, Math.max(88, Math.round(thumbHeight * 1.25)));
    const textLines = listTextLines(thumbHeight);
    return (
      <div className="asset-list-cell media-column" style={style}>
        <div className="asset-list-thumb" style={{ width: thumbWidth, height: thumbHeight }}>
          <AssetTileMedia asset={asset} rowHeight={thumbHeight} tileWidth={thumbWidth} />
          {asset.mediaType === 'video' && <span className="asset-list-video"><Play size={12} fill="currentColor" /></span>}
        </div>
        <ListText value={asset.filename} expandable maxLines={textLines} />
      </div>
    );
  }
  const textLines = listTextLines(rowHeight - 16);
  if (column === 'aiTags') {
    return (
      <div className="asset-list-cell asset-list-cell-tags" style={style}>
        <AssetListTags asset={asset} />
      </div>
    );
  }
  if (column === 'palette') {
    return (
      <div className="asset-list-cell asset-list-cell-palette" style={style}>
        <MediaPalette colors={asset.palette ?? []} />
      </div>
    );
  }
  const text = listColumnValue(asset, column);
  return (
    <div className={`asset-list-cell asset-list-cell-${column}`} style={style}>
      <ListText value={text} expandable={column === 'path' || column === 'aiDescription'} maxLines={textLines} />
    </div>
  );
}

function MediaPalette({ colors }: { colors: Array<{ hex: string; weight: number }> }) {
  return (
    <div className="media-palette" aria-label={colors.length > 0 ? `主题配色：${colors.map((item) => item.hex).join('、')}` : '暂无配色'}>
      {colors.slice(0, 5).map((item, index) => (
        <span key={`${item.hex}-${index}`} style={{ backgroundColor: item.hex }} title={`${item.hex} · ${Math.round(item.weight * 100)}%`} />
      ))}
      {colors.length === 0 && <small>—</small>}
    </div>
  );
}

function AssetListTags({ asset }: { asset: Asset }) {
  const [manualTags, setManualTags] = useState(asset.manualTags ?? []);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    setManualTags(asset.manualTags ?? []);
    setAdding(false);
    setDraft('');
    setError('');
  }, [asset.id, asset.manualTags]);

  const stop = (event: ReactMouseEvent<HTMLElement>) => {
    event.preventDefault();
    event.stopPropagation();
  };
  const add = async () => {
    const tag = draft.trim();
    if (!tag || saving) return;
    setSaving(true);
    setError('');
    try {
      const result = await api.addAssetTag(asset.id, tag);
      setManualTags(result.items);
      setDraft('');
      setAdding(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加失败');
    } finally {
      setSaving(false);
    }
  };
  const remove = async (tag: string) => {
    if (saving) return;
    setSaving(true);
    setError('');
    try {
      const result = await api.removeAssetTag(asset.id, tag);
      setManualTags(result.items);
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败');
    } finally {
      setSaving(false);
    }
  };
  const aiGroups = [...(asset.aiTags ?? []).reduce((groups, item) => {
    const key = item.categoryLabel || '其他';
    groups.set(key, [...(groups.get(key) ?? []), item]);
    return groups;
  }, new Map<string, NonNullable<Asset['aiTags']>>())];

  return (
    <div className="asset-list-tags" title={error || undefined} onClick={stop}>
      {aiGroups.map(([category, items]) => (
        <div className="asset-list-tag-group hierarchical" key={category}>
          <span className="asset-list-tag-kind">{category}</span>
          <div className="asset-list-tag-children">
            {items.map((item) => (
              <span
                className="asset-list-tag ai"
                key={item.tag}
                title={`${(item.facets ?? []).map((facet) => facet.labels.join(' / ')).join('\n')}\nAI 匹配度 ${item.confidence.toFixed(3)}`}
              >
                <small>{item.subjectLabel || category}</small><span>{item.tag}</span>
              </span>
            ))}
          </div>
        </div>
      ))}
      {aiGroups.length === 0 && <div className="asset-list-tag-group"><span className="asset-list-tag-kind">AI</span><span className="asset-list-tag-empty">—</span></div>}
      <div className="asset-list-tag-group">
        <span className="asset-list-tag-kind">自标</span>
        {manualTags.map((item) => (
          <span className="asset-list-tag manual" key={item.tag}>
            <span>{item.tag}</span>
            <button disabled={saving} type="button" title={`移除“${item.tag}”`} onClick={(event) => { stop(event); void remove(item.tag); }}>
              <X size={10} />
            </button>
          </span>
        ))}
        {adding ? (
          <form className="asset-list-tag-add-form" onSubmit={(event) => { event.preventDefault(); event.stopPropagation(); void add(); }}>
            <input
              autoFocus
              disabled={saving}
              value={draft}
              placeholder="标签"
              onChange={(event) => setDraft(event.target.value)}
              onClick={stop}
              onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); setAdding(false); setDraft(''); } }}
            />
            <button disabled={saving || !draft.trim()} title="确认添加" type="submit"><Check size={11} /></button>
          </form>
        ) : (
          <button className="asset-list-tag-add" type="button" title="添加自标" onClick={(event) => { stop(event); setAdding(true); }}>
            <Plus size={12} />
          </button>
        )}
      </div>
    </div>
  );
}

function ListText({
  value,
  expandable = false,
  maxLines = 2,
}: {
  value: string;
  expandable?: boolean;
  maxLines?: number;
}) {
  const [placement, setPlacement] = useState<'up' | 'down'>('down');
  const text = value || '—';
  const style = { '--asset-list-text-lines': maxLines } as CSSProperties;
  const preparePlacement = (event: ReactMouseEvent<HTMLSpanElement>) => {
    if (!expandable || !value) return;
    const rect = event.currentTarget.getBoundingClientRect();
    setPlacement(window.innerHeight - rect.bottom < Math.min(260, window.innerHeight * 0.45) ? 'up' : 'down');
  };
  return (
    <span
      className={expandable && value ? `asset-list-text expandable ${placement}` : 'asset-list-text'}
      style={style}
      tabIndex={expandable && value ? 0 : undefined}
      title={expandable ? undefined : text}
      onMouseEnter={preparePlacement}
    >
      <span>{text}</span>
      {expandable && value && <span className="asset-list-full-text">{value}</span>}
    </span>
  );
}

function listTextLines(availableHeight: number) {
  return Math.max(2, Math.floor(Math.max(38, availableHeight) / 19));
}

function listColumnValue(asset: Asset, column: MediaColumnId) {
  switch (column) {
    case 'path': return asset.relPath;
    case 'mediaType': return asset.mediaType === 'video' ? '视频' : '图片';
    case 'resolution': return asset.width && asset.height ? `${asset.width} × ${asset.height}` : '';
    case 'duration': return formatDuration(asset.duration);
    case 'timeline': return formatListDate(asset.takenAt ?? asset.timelineAt);
    case 'imported': return formatListDate(asset.importedAt);
    case 'modified': return formatListDate(asset.mtime);
    case 'size': return formatListBytes(asset.size);
    case 'rating': return asset.rating > 0 ? `${'★'.repeat(asset.rating)}${'☆'.repeat(5 - asset.rating)}` : '未评分';
    case 'container': return asset.container ?? '';
    case 'videoCodec': return asset.videoCodec ?? '';
    case 'audioCodec': return asset.audioCodec ?? '';
    case 'fps': return asset.fps ? `${trimNumber(asset.fps)} FPS` : '';
    case 'bitrate': return asset.overallBitrate ? `${trimNumber(asset.overallBitrate / 1_000_000)} Mbps` : '';
    case 'subtitle': return asset.hasSubtitle ? '有' : '无';
    case 'danmaku': return asset.hasDanmaku ? '有' : '无';
    case 'aiDescription': return asset.aiDescription ?? '';
    default: return '';
  }
}

function formatListDate(value: number | null | undefined) {
  if (!value) return '';
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  }).format(new Date(value * 1000));
}

function formatListBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  return `${trimNumber(bytes / 1024 ** index)} ${units[index]}`;
}

function formatDuration(seconds: number | null) {
  if (!seconds || seconds <= 0) return '';
  const rounded = Math.round(seconds);
  const hours = Math.floor(rounded / 3600);
  const minutes = Math.floor((rounded % 3600) / 60);
  const secs = rounded % 60;
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
    : `${minutes}:${String(secs).padStart(2, '0')}`;
}

function trimNumber(value: number) {
  return Number(value.toFixed(value >= 100 ? 0 : value >= 10 ? 1 : 2)).toLocaleString();
}

function sortFieldForColumn(column: MediaColumnId): SortField {
  const fields: Record<MediaColumnId, SortField> = {
    media: 'filename',
    path: 'path',
    mediaType: 'media_type',
    resolution: 'resolution',
    duration: 'duration',
    timeline: 'timeline',
    imported: 'imported',
    modified: 'modified',
    size: 'size',
    rating: 'rating',
    container: 'container',
    videoCodec: 'video_codec',
    audioCodec: 'audio_codec',
    fps: 'fps',
    bitrate: 'bitrate',
    subtitle: 'subtitle',
    danmaku: 'danmaku',
    aiDescription: 'ai_description',
    aiTags: 'ai_tag',
    palette: 'timeline',
  };
  return fields[column];
}

function nextSortForColumn(column: MediaColumnId, current: SortKey): SortKey {
  const field = sortFieldForColumn(column);
  const active = current === field || current.startsWith(`${field}_`);
  if (active) return `${field}_${current.endsWith('_asc') ? 'desc' : 'asc'}` as SortKey;
  const textFields: SortField[] = ['filename', 'path', 'media_type', 'container', 'video_codec', 'audio_codec', 'ai_description', 'ai_tag'];
  return `${field}_${textFields.includes(field) ? 'asc' : 'desc'}` as SortKey;
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
  const maxScroll = Math.max(0, element.scrollHeight - element.clientHeight);
  const clampedRatio = clampRatio(ratio);
  if (clampedRatio <= 0) return 0;
  if (clampedRatio >= 1) return maxScroll;
  if (meta.totalCount <= 1 || rows.length === 0) {
    return maxScroll > 0 ? maxScroll * clampedRatio : 0;
  }
  const targetLocalIndex = clampedRatio * (meta.totalCount - 1) - meta.loadedStartIndex;
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

function scrollTopForAssetTop(rows: GridRow[], assetId: number) {
  let offset = 0;
  for (const row of rows) {
    if (row.type === 'assets' && row.items.some((item) => item.asset.id === assetId)) {
      return offset;
    }
    offset += row.height + gap;
  }
  return null;
}

function buildListRows(
  assets: Asset[],
  groupMode: AssetGroupMode,
  sort: SortKey,
  rowHeight: number,
  duplicateGrouping: boolean,
): GridRow[] {
  const rows: GridRow[] = [];
  let currentGroup = '';
  let currentDuplicate = '';
  assets.forEach((asset, assetIndex) => {
    const groupLabel = assetGroupLabel(asset, groupMode, sort);
    if (groupLabel && groupLabel !== currentGroup) {
      currentGroup = groupLabel;
      rows.push({
        key: `list-group-${groupLabel}-${asset.id}`,
        type: 'group',
        label: groupLabel,
        height: groupHeaderHeight,
        assetIndex,
      });
    }
    if (duplicateGrouping) {
      const duplicateKey = asset.sha256 ? `${asset.sha256}:${asset.size}` : `asset-${asset.id}`;
      if (duplicateKey !== currentDuplicate) {
        currentDuplicate = duplicateKey;
        rows.push({
          key: `list-duplicate-${duplicateKey}`,
          type: 'group',
          label: duplicateGroupLabel(duplicateKey),
          height: groupHeaderHeight,
          assetIndex,
          variant: 'duplicate',
        });
      }
    }
    rows.push({
      key: `list-asset-${asset.id}`,
      type: 'assets',
      items: [{ asset, index: assetIndex, width: 0 }],
      height: rowHeight,
      startAssetIndex: assetIndex,
      endAssetIndex: assetIndex,
    });
  });
  return rows;
}

function buildRows(
  assets: Asset[],
  containerWidth: number,
  groupMode: AssetGroupMode,
  sort: SortKey,
  rowHeight: number,
  duplicateGrouping: boolean,
): GridRow[] {
  if (containerWidth <= 0) return [];
  if (duplicateGrouping) {
    return buildDuplicateRows(assets, containerWidth, groupMode, sort, rowHeight);
  }
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

function buildDuplicateRows(
  assets: Asset[],
  containerWidth: number,
  groupMode: AssetGroupMode,
  sort: SortKey,
  rowHeight: number,
): GridRow[] {
  const rows: GridRow[] = [];
  let items: RowItem[] = [];
  let usedWidth = 0;
  let rowIndex = 0;
  let currentNativeGroup = '';

  function flushRow(stretch: boolean) {
    if (items.length === 0) return;
    rows.push({
      key: `duplicate-row-${rowIndex}`,
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

  function appendAsset(asset: Asset, index: number) {
    const tileWidth = Math.min(containerWidth, Math.max(minTileWidth, Math.round(rowHeight * assetAspect(asset))));
    const nextWidth = usedWidth + (items.length > 0 ? gap : 0) + tileWidth;
    if (items.length > 0 && nextWidth > containerWidth) flushRow(true);
    items.push({ asset, index, width: tileWidth });
    usedWidth += (items.length > 1 ? gap : 0) + tileWidth;
  }

  const groups = contiguousDuplicateGroups(assets);
  groups.forEach((group, groupIndex) => {
    const representative = group.items[0];
    if (!representative) return;
    const nativeLabel = assetGroupLabel(representative.asset, groupMode, sort);
    if (nativeLabel && nativeLabel !== currentNativeGroup) {
      flushRow(false);
      currentNativeGroup = nativeLabel;
      rows.push({
        key: `native-${nativeLabel}-${group.key}`,
        type: 'group',
        label: nativeLabel,
        height: groupHeaderHeight,
        assetIndex: representative.index,
        variant: 'native',
      });
    }
    flushRow(false);
    rows.push({
      key: `duplicate-${group.key}-${groupIndex}`,
      type: 'group',
      label: duplicateGroupLabel(group.key),
      height: groupHeaderHeight,
      assetIndex: representative.index,
      variant: 'duplicate',
    });
    group.items.forEach(({ asset, index }) => appendAsset(asset, index));
    flushRow(false);
  });
  return rows;
}

function contiguousDuplicateGroups(assets: Asset[]) {
  const groups: Array<{ key: string; items: Array<{ asset: Asset; index: number }> }> = [];
  assets.forEach((asset, index) => {
    const key = asset.sha256 ? `${asset.sha256}:${asset.size}` : `asset-${asset.id}`;
    const current = groups[groups.length - 1];
    if (current?.key === key) {
      current.items.push({ asset, index });
    } else {
      groups.push({ key, items: [{ asset, index }] });
    }
  });
  return groups;
}

function duplicateGroupLabel(key: string) {
  const hash = key.split(':', 1)[0] ?? '';
  return hash.startsWith('asset-') ? '重复媒体' : `重复媒体 · SHA-256 ${hash.slice(0, 10).toUpperCase()}`;
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
