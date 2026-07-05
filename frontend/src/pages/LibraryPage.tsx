import { useCallback, useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import AssetGrid from '../components/AssetGrid';
import AssetGroupingControls, { normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarButtonGroup, SidebarMediaTypeList, SidebarRatingFilter, sidebarOrientationOptions } from '../components/SidebarControls';
import SortControls, { isSortKey } from '../components/SortControls';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type { Asset, AssetDeletedEvent, AssetKind, AssetRating, LibraryAnchor, OrientationFilter, SortKey } from '../types/api';
import { useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { parseAssetGroupMode, serverGroupForMode, type AssetGroupMode } from '../utils/assetGrouping';
import {
  appendViewerReturnParams,
  decodeReturnState,
  loadPageState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { assetMatchesLibrary } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';
import { assetRatingParam, currentURLHasParam, currentURLLocation, currentURLPath, orientationParam, replaceURLState } from '../utils/urlState';

const pageSize = 100;
const libraryStateKey = 'library';
const libraryURLKeys = ['type', 'rating', 'orientation', 'sort', 'group', 'q'];
type LibraryControlState = Pick<LibraryPageState, 'groupMode' | 'orientation' | 'query' | 'rating' | 'sort' | 'type'>;

interface LibraryPageState extends GridReturnState {
  groupMode: AssetGroupMode;
  orientation: OrientationFilter;
  query: string;
  rating: AssetRating;
  sort: SortKey;
  type: AssetKind;
}

const defaultLibraryState: LibraryPageState = {
  ...resetGridState(),
  groupMode: 'none',
  orientation: 'all',
  query: '',
  rating: 0,
  sort: 'timeline_desc',
  type: 'all',
};

const assetKinds: AssetKind[] = ['all', 'image', 'video'];
export default function LibraryPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const persistedState = loadPageState<LibraryPageState>(libraryStateKey, defaultLibraryState);
  const decodedInitialState = decodeReturnState<LibraryPageState>(
    searchParams.get('restore'),
    persistedState,
  );
  const initialStateRef = useRef(
    searchParams.has('restore') ? decodedInitialState : libraryStateFromSearchParams(searchParams, persistedState),
  );
  const pendingControlStateRef = useRef<Partial<LibraryControlState> | null>(null);
  const [type, setType] = useState<AssetKind>(initialStateRef.current.type);
  const [rating, setRating] = useState<AssetRating>(initialStateRef.current.rating ?? 0);
  const [orientation, setOrientation] = useState<OrientationFilter>(initialStateRef.current.orientation);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const serverGroup = serverGroupForMode(groupMode);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const sidebarState = useSidebarReturnState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const loadAssets = useCallback(
    (page: number) => api.libraryAssets(page, pageSize, type, sort, query, serverGroup, rating, undefined, undefined, undefined, orientation),
    [orientation, query, rating, serverGroup, sort, type],
  );
  const { items, hasMore, hasPrevious, loading, error, loadMore, loadPrevious, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    type,
    rating,
    orientation,
    sort,
    query,
    serverGroup,
  ]);
  const {
    focusAssetId,
    getGridState,
    handleGridScrollState,
    loadedStartIndex,
    loadPreviousPage,
    scrollRatio,
    scrollResetSignal,
    scrollTarget,
    scrollTopTarget,
    seekIndex,
    setScrollRatio,
  } = useWaterfallGridState({
    hasMore,
    hasPrevious,
    initialState: initialStateRef.current,
    itemsLength: items.length,
    jumpToPage,
    loading,
    loadMore,
    loadPrevious,
    pageSize,
    resetKey: JSON.stringify([type, rating, orientation, sort, query, groupMode]),
    searchParams,
  });

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const filtered = incoming.filter((asset) => assetMatchesLibrary(asset, type, query, rating, orientation));
      if (filtered.length === 0) return;
      mutateItems((current) => mergeSortedAssets(current, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [groupMode, hasMore, loadedStartIndex, mutateItems, orientation, query, rating, sort, type],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((current) => removeAssetById(current, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected) return undefined;
    const timer = window.setInterval(() => {
      void api
        .libraryAssets(1, pageSize, type, sort, query, serverGroup, rating, undefined, undefined, undefined, orientation)
        .then((result) => mergeReadyAssets(result.items))
        .catch(() => undefined);
    }, 30000);
    return () => window.clearInterval(timer);
  }, [eventsConnected, mergeReadyAssets, orientation, query, rating, serverGroup, sort, type]);

  const currentPageState = useCallback(
    (): LibraryPageState => ({
      ...getGridState(),
      groupMode,
      orientation,
      query,
      rating,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
      type,
    }),
    [getGridState, groupMode, orientation, query, rating, sidebarState.sidebarExpanded, sort, type],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<LibraryPageState>(libraryStateKey, { ...currentPageState(), ...(pendingControlStateRef.current ?? {}) });
  }, [currentPageState]);
  const saveControlState = useCallback(
    (patch: Partial<LibraryControlState>) => {
      const controls: LibraryControlState = {
        groupMode,
        orientation,
        query,
        rating,
        sort,
        type,
        ...(pendingControlStateRef.current ?? {}),
        ...patch,
      };
      pendingControlStateRef.current = controls;
      const current = currentPageState();
      const reset = resetGridState();
      savePageState<LibraryPageState>(libraryStateKey, {
        ...current,
        ...reset,
        groupMode: controls.groupMode,
        orientation: controls.orientation,
        query: controls.query,
        rating: controls.rating,
        sidebarExpanded: current.sidebarExpanded,
        sort: controls.sort,
        type: controls.type,
      });
    },
    [currentPageState, groupMode, orientation, query, rating, sort, type],
  );
  const handleTypeChange = useCallback(
    (nextType: AssetKind) => {
      setType(nextType);
      saveControlState({ type: nextType });
    },
    [saveControlState],
  );
  const handleSortChange = useCallback(
    (nextSort: SortKey) => {
      const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, nextSort);
      setSort(nextSort);
      if (nextGroupMode !== groupMode) {
        setGroupMode(nextGroupMode);
      }
      saveControlState({ groupMode: nextGroupMode, sort: nextSort });
    },
    [groupMode, saveControlState],
  );
  const handleRatingChange = useCallback(
    (nextRating: AssetRating) => {
      setRating(nextRating);
      saveControlState({ rating: nextRating });
    },
    [saveControlState],
  );
  const handleOrientationChange = useCallback(
    (nextOrientation: OrientationFilter) => {
      setOrientation(nextOrientation);
      saveControlState({ orientation: nextOrientation });
    },
    [saveControlState],
  );
  const handleQueryChange = useCallback(
    (nextQuery: string) => {
      setQuery(nextQuery);
      saveControlState({ query: nextQuery });
    },
    [saveControlState],
  );
  const handleGroupModeChange = useCallback(
    (nextGroupMode: AssetGroupMode) => {
      setGroupMode(nextGroupMode);
      saveControlState({ groupMode: nextGroupMode });
    },
    [saveControlState],
  );
  const scheduleCurrentStateSave = usePersistentPageState(saveCurrentState);

  useEffect(() => {
    if (currentURLHasParam(location, 'restore')) return;
    replaceURLState(
      navigate,
      location,
      { group: groupMode, orientation: orientation === 'all' ? undefined : orientation, q: query, rating, sort, type },
      libraryURLKeys,
    );
  }, [groupMode, location, navigate, orientation, query, rating, searchParams, sort, type]);

  const handlePersistentGridScrollState = useCallback(
    (state: { ratio: number; scrollTop: number }) => {
      handleGridScrollState(state);
      scheduleCurrentStateSave();
    },
    [handleGridScrollState, scheduleCurrentStateSave],
  );

  useEffect(() => {
    let live = true;
    async function loadAnchors() {
      try {
        const result = await api.libraryAnchors(pageSize, type, sort, query, serverGroup, rating, undefined, undefined, undefined, orientation);
        if (live) {
          setAnchors(result.items);
          setTotalCount(result.total);
        }
      } catch {
        if (live) {
          setAnchors([]);
          setTotalCount(0);
        }
      }
    }
    void loadAnchors();
    return () => {
      live = false;
    };
  }, [orientation, query, rating, serverGroup, sort, type]);

  useEffect(() => {
    const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, sort);
    if (nextGroupMode !== groupMode) {
      setGroupMode(nextGroupMode);
    }
  }, [groupMode, sort]);

  const handleOpenAsset = useCallback(() => {
    saveCurrentState();
    saveViewerReturnPath(currentPageReturnPath());
  }, [currentPageReturnPath, saveCurrentState]);

  const handleOpenViewer = useCallback(
    (asset: Asset, viewerUrl: string) => {
      navigate(viewerUrl, { state: { backgroundLocation: currentURLLocation(location), initialAsset: asset } });
    },
    [location, navigate],
  );

  useSidebarPanel(
    'library',
    <div className="sidebar-control-stack">
      <SidebarMediaTypeList value={type} onChange={handleTypeChange} />
      <SidebarRatingFilter value={rating} onChange={handleRatingChange} />
      <SidebarButtonGroup columns={3} label="方向" value={orientation} options={sidebarOrientationOptions} onChange={handleOrientationChange} />
      <SortControls sort={sort} onChange={handleSortChange} />
      <label className="sidebar-field">
        <span>搜索</span>
        <input value={query} onChange={(event) => handleQueryChange(event.target.value)} placeholder="文件名" />
      </label>
      <AssetGroupingControls groupMode={groupMode} sort={sort} onChange={handleGroupModeChange} />
    </div>,
    [
      groupMode,
      handleGroupModeChange,
      handleOrientationChange,
      handleQueryChange,
      handleRatingChange,
      handleSortChange,
      handleTypeChange,
      orientation,
      query,
      rating,
      sort,
      type,
    ],
  );

  useSidebarPanel(
    'viewer',
    pressPreviewAsset ? <AssetInfoPanel asset={pressPreviewAsset} title="快速预览" /> : null,
    [pressPreviewAsset?.id],
  );

  return (
    <section className="page media-page">
      {error && <div className="error-line">{error}</div>}
      {items.length === 0 && !loading ? (
        <EmptyState text="没有匹配资源" />
      ) : (
        <div className="library-grid-shell">
          <AssetGrid
            assets={items}
            loading={loading}
            hasMore={hasMore}
            hasPrevious={hasPrevious}
            onLoadMore={loadMore}
            onLoadPrevious={loadPreviousPage}
            onOpenAsset={handleOpenAsset}
            onOpenViewer={handleOpenViewer}
            onAssetMissing={(asset) => mutateItems((current) => removeAssetById(current, asset.id))}
            onScrollRatioChange={setScrollRatio}
            onScrollStateChange={handlePersistentGridScrollState}
            totalCount={totalCount}
            loadedStartIndex={loadedStartIndex}
            focusAssetId={focusAssetId}
            groupMode={groupMode}
            sort={sort}
            scrollSignal={scrollResetSignal}
            scrollTarget={scrollTarget}
            scrollTopTarget={scrollTopTarget}
            onPressPreviewChange={setPressPreviewAsset}
            buildViewerUrl={(asset) =>
              appendViewerReturnParams(
                `/viewer/${asset.id}?context=library&type=${type}&sort=${sort}&q=${encodeURIComponent(query)}${ratingViewerParam(rating)}${orientationViewerParam(
                  orientation,
                )}${serverGroup ? `&group=${serverGroup}` : ''}`,
                currentPageReturnPath(),
                currentPageState(),
              )
            }
          />
          {groupMode !== 'folder' && (
            <LibraryIndexRail
              anchors={anchors}
              sort={sort}
              scrollRatio={scrollRatio}
              totalCount={totalCount}
              pageSize={pageSize}
              onSeek={seekIndex}
            />
          )}
          <PressPreviewOverlay asset={pressPreviewAsset} />
        </div>
      )}
    </section>
  );
}

function libraryStateFromSearchParams(params: URLSearchParams, fallback: LibraryPageState): LibraryPageState {
  const type = params.get('type');
  const sort = params.get('sort');
  const q = params.get('q');
  const hasLibraryParams = libraryURLKeys.some((key) => params.has(key));
  const base = hasLibraryParams ? { ...fallback, ...resetGridState() } : fallback;
  return {
    ...base,
    groupMode: parseAssetGroupMode(params.get('group'), base.groupMode),
    orientation: params.has('orientation') ? orientationParam(params.get('orientation')) : base.orientation,
    query: q ?? (hasLibraryParams ? '' : base.query),
    rating: params.has('rating') ? assetRatingParam(params.get('rating')) ?? base.rating : base.rating,
    sort: isSortKey(sort) ? sort : base.sort,
    type: assetKinds.includes(type as AssetKind) ? (type as AssetKind) : base.type,
  };
}

function ratingViewerParam(rating: AssetRating) {
  return `&rating=${rating}`;
}

function orientationViewerParam(orientation: OrientationFilter) {
  return orientation === 'all' ? '' : `&orientation=${orientation}`;
}
