import { useCallback, useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { Image as ImageIcon, Images, Video } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetGroupingControls, { normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import SortControls, { isSortKey } from '../components/SortControls';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type { Asset, AssetDeletedEvent, AssetKind, LibraryAnchor, SortKey } from '../types/api';
import { useRestoreSidebarState, useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
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
import { currentURLHasParam, currentURLLocation, currentURLPath, replaceURLState } from '../utils/urlState';

const pageSize = 100;
const libraryStateKey = 'library';
const libraryURLKeys = ['type', 'sort', 'group', 'q'];
type LibraryControlState = Pick<LibraryPageState, 'groupMode' | 'query' | 'sort' | 'type'>;

interface LibraryPageState extends GridReturnState {
  groupMode: AssetGroupMode;
  query: string;
  sort: SortKey;
  type: AssetKind;
}

const defaultLibraryState: LibraryPageState = {
  ...resetGridState(),
  groupMode: 'none',
  query: '',
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
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const serverGroup = serverGroupForMode(groupMode);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const sidebarState = useSidebarReturnState();
  const restoreSidebarState = useRestoreSidebarState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const loadAssets = useCallback(
    (page: number) => api.libraryAssets(page, pageSize, type, sort, query, serverGroup),
    [query, serverGroup, sort, type],
  );
  const { items, hasMore, hasPrevious, loading, error, loadMore, loadPrevious, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [type, sort, query, serverGroup]);
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
    resetKey: JSON.stringify([type, sort, query, groupMode]),
    searchParams,
  });

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const filtered = incoming.filter((asset) => assetMatchesLibrary(asset, type, query));
      if (filtered.length === 0) return;
      mutateItems((current) => mergeSortedAssets(current, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [groupMode, hasMore, loadedStartIndex, mutateItems, query, sort, type],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((current) => removeAssetById(current, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected) return undefined;
    const timer = window.setInterval(() => {
      void api.libraryAssets(1, pageSize, type, sort, query, serverGroup).then((result) => mergeReadyAssets(result.items)).catch(() => undefined);
    }, 30000);
    return () => window.clearInterval(timer);
  }, [eventsConnected, mergeReadyAssets, query, serverGroup, sort, type]);

  const currentPageState = useCallback(
    (): LibraryPageState => ({
      ...getGridState(),
      groupMode,
      query,
      sidebarCollapsed: sidebarState.sidebarCollapsed,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
      type,
    }),
    [getGridState, groupMode, query, sidebarState.sidebarCollapsed, sidebarState.sidebarExpanded, sort, type],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<LibraryPageState>(libraryStateKey, { ...currentPageState(), ...(pendingControlStateRef.current ?? {}) });
  }, [currentPageState]);
  const saveControlState = useCallback(
    (patch: Partial<LibraryControlState>) => {
      const controls: LibraryControlState = {
        groupMode,
        query,
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
        query: controls.query,
        sidebarCollapsed: current.sidebarCollapsed,
        sidebarExpanded: current.sidebarExpanded,
        sort: controls.sort,
        type: controls.type,
      });
    },
    [currentPageState, groupMode, query, sort, type],
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
    replaceURLState(navigate, location, { group: groupMode, q: query, sort, type }, libraryURLKeys);
  }, [groupMode, location, navigate, query, searchParams, sort, type]);

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
        const result = await api.libraryAnchors(pageSize, type, sort, query);
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
  }, [query, sort, type]);

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
      <div className="sidebar-list">
        {(['all', 'image', 'video'] as AssetKind[]).map((value) => (
          <button className={type === value ? 'sidebar-list-row active' : 'sidebar-list-row'} key={value} type="button" onClick={() => handleTypeChange(value)}>
            {value === 'all' ? <Images size={14} /> : value === 'image' ? <ImageIcon size={14} /> : <Video size={14} />}
            <span>{assetKindLabel(value)}</span>
          </button>
        ))}
      </div>
      <SortControls sort={sort} onChange={handleSortChange} />
      <label className="sidebar-field">
        <span>搜索</span>
        <input value={query} onChange={(event) => handleQueryChange(event.target.value)} placeholder="文件名" />
      </label>
      <AssetGroupingControls groupMode={groupMode} sort={sort} onChange={handleGroupModeChange} />
    </div>,
    [groupMode, handleGroupModeChange, handleQueryChange, handleSortChange, handleTypeChange, query, sort, type],
  );

  useSidebarPanel(
    'viewer',
    pressPreviewAsset ? <AssetInfoPanel asset={pressPreviewAsset} title="快速预览" /> : null,
    [pressPreviewAsset?.id],
  );

  useEffect(() => {
    restoreSidebarState({ sidebarCollapsed: initialStateRef.current.sidebarCollapsed });
  }, [restoreSidebarState]);

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
                `/viewer/${asset.id}?context=library&type=${type}&sort=${sort}&q=${encodeURIComponent(query)}${serverGroup ? `&group=${serverGroup}` : ''}`,
                currentPageReturnPath(),
                currentPageState(),
              )
            }
          />
          <LibraryIndexRail
            anchors={anchors}
            sort={sort}
            scrollRatio={scrollRatio}
            totalCount={totalCount}
            pageSize={pageSize}
            onSeek={seekIndex}
          />
          <PressPreviewOverlay asset={pressPreviewAsset} />
        </div>
      )}
    </section>
  );
}

function assetKindLabel(value: AssetKind) {
  switch (value) {
    case 'image':
      return '照片';
    case 'video':
      return '视频';
    default:
      return '全部';
  }
}

function libraryStateFromSearchParams(params: URLSearchParams, fallback: LibraryPageState): LibraryPageState {
  const type = params.get('type');
  const sort = params.get('sort');
  const q = params.get('q');
  const hasLibraryParams = params.has('type') || params.has('sort') || params.has('q') || params.has('group');
  const base = hasLibraryParams ? { ...fallback, ...resetGridState() } : fallback;
  return {
    ...base,
    groupMode: parseAssetGroupMode(params.get('group'), base.groupMode),
    query: q ?? (hasLibraryParams ? '' : base.query),
    sort: isSortKey(sort) ? sort : base.sort,
    type: assetKinds.includes(type as AssetKind) ? (type as AssetKind) : base.type,
  };
}
