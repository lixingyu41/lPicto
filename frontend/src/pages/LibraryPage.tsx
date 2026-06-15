import { useCallback, useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { Image as ImageIcon, Images, Video } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import type { Asset, AssetKind, LibraryAnchor, SortKey } from '../types/api';
import { useRestoreSidebarState, useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import type { AssetGroupMode } from '../utils/assetGrouping';
import {
  decodeReturnState,
  encodeReturnState,
  loadPageState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { assetMatchesLibrary } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';

const pageSize = 100;
const libraryStateKey = 'library';

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
const sortKeys: SortKey[] = [
  'timeline_desc',
  'timeline_asc',
  'imported_desc',
  'imported_asc',
  'filename',
  'filename_asc',
  'filename_desc',
  'size',
  'size_desc',
  'size_asc',
];

export default function LibraryPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const decodedInitialState = decodeReturnState<LibraryPageState>(
    searchParams.get('restore'),
    loadPageState<LibraryPageState>(libraryStateKey, defaultLibraryState),
  );
  const initialStateRef = useRef(
    searchParams.has('restore') ? decodedInitialState : libraryStateFromSearchParams(searchParams, decodedInitialState),
  );
  const [type, setType] = useState<AssetKind>(initialStateRef.current.type);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [scrollTarget, setScrollTarget] = useState<{ ratio: number; signal: number } | undefined>();
  const [scrollTopTarget, setScrollTopTarget] = useState<{ scrollTop: number; signal: number } | undefined>(() =>
    initialStateRef.current.scrollTop > 0 ? { scrollTop: initialStateRef.current.scrollTop, signal: 1 } : undefined,
  );
  const [scrollResetSignal, setScrollResetSignal] = useState(0);
  const [scrollRatio, setScrollRatio] = useState(0);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const [loadedStartIndex, setLoadedStartIndex] = useState(0);
  const [, setGridUrlSignal] = useState(0);
  const gridStateRef = useRef<GridReturnState>(initialStateRef.current);
  const sidebarState = useSidebarReturnState();
  const restoreSidebarState = useRestoreSidebarState();
  const restoreRef = useRef({
    jumped: false,
    pending: initialStateRef.current.scrollTop > 0 || initialStateRef.current.loadedItemCount > pageSize || initialStateRef.current.loadedStartIndex > 0,
    signal: 0,
  });
  const indexPageRef = useRef(1);
  const seekSignalRef = useRef(0);
  const loadAssets = useCallback(
    (page: number) => api.libraryAssets(page, pageSize, type, sort, query),
    [query, sort, type],
  );
  const { items, hasMore, loading, error, loadMore, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [type, sort, query]);

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const filtered = incoming.filter((asset) => assetMatchesLibrary(asset, type, query));
      if (filtered.length === 0) return;
      mutateItems((current) => mergeSortedAssets(current, filtered, sort, { hasMore, loadedStartIndex }));
    },
    [hasMore, loadedStartIndex, mutateItems, query, sort, type],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady]);

  useEffect(() => {
    if (eventsConnected) return undefined;
    const timer = window.setInterval(() => {
      void api.libraryAssets(1, pageSize, type, sort, query).then((result) => mergeReadyAssets(result.items)).catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [eventsConnected, mergeReadyAssets, query, sort, type]);

  const currentPageState = useCallback(
    (): LibraryPageState => ({
      ...gridStateRef.current,
      groupMode,
      loadedItemCount: items.length,
      loadedStartIndex,
      query,
      sidebarCollapsed: sidebarState.sidebarCollapsed,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
      type,
    }),
    [groupMode, items.length, loadedStartIndex, query, sidebarState.sidebarCollapsed, sidebarState.sidebarExpanded, sort, type],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<LibraryPageState>(libraryStateKey, currentPageState());
  }, [currentPageState]);

  useEffect(() => {
    indexPageRef.current = 1;
    setLoadedStartIndex(0);
    setScrollTarget(undefined);
    if (restoreRef.current.pending) return;
    gridStateRef.current = resetGridState();
    setScrollResetSignal((value) => value + 1);
  }, [query, sort, type]);

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
    const options = groupOptionsForSort(sort).map((option) => option.value);
    if (!options.includes(groupMode)) {
      setGroupMode('none');
    }
  }, [groupMode, sort]);

  useEffect(() => {
    const restore = restoreRef.current;
    if (!restore.pending || loading) return;
    const saved = initialStateRef.current;
    const startIndex = Math.max(0, saved.loadedStartIndex);
    if (startIndex > 0 && !restore.jumped) {
      restore.jumped = true;
      const page = Math.floor(startIndex / pageSize) + 1;
      indexPageRef.current = page;
      setLoadedStartIndex(startIndex);
      void jumpToPage(page);
      return;
    }
    const targetCount = Math.max(pageSize, saved.loadedItemCount);
    if (items.length < targetCount && hasMore) {
      void loadMore();
      return;
    }
    restore.pending = false;
    restore.signal += 1;
    setScrollTopTarget({ scrollTop: saved.scrollTop, signal: restore.signal });
  }, [hasMore, items.length, jumpToPage, loadMore, loading]);

  const handleGridScrollState = useCallback(
    (state: { ratio: number; scrollTop: number }) => {
      gridStateRef.current = {
        ...gridStateRef.current,
        loadedItemCount: items.length,
        loadedStartIndex,
        scrollRatio: state.ratio,
        scrollTop: state.scrollTop,
      };
      setGridUrlSignal((value) => value + 1);
    },
    [items.length, loadedStartIndex],
  );

  const handleOpenAsset = useCallback(() => {
    saveCurrentState();
    saveViewerReturnPath('/library');
  }, [saveCurrentState]);

  const handleOpenViewer = useCallback(
    (_asset: Asset, viewerUrl: string) => {
      navigate(viewerUrl, { state: { backgroundLocation: location } });
    },
    [location, navigate],
  );

  const seekIndex = useCallback(
    (_anchor: LibraryAnchor, page: number, ratio: number) => {
      const signal = seekSignalRef.current + 1;
      seekSignalRef.current = signal;
      setScrollTarget({ ratio, signal });
      if (page === indexPageRef.current) return;
      indexPageRef.current = page;
      setLoadedStartIndex((Math.max(1, page) - 1) * pageSize);
      void jumpToPage(page).then(() => {
        if (seekSignalRef.current !== signal) return;
        const nextSignal = seekSignalRef.current + 1;
        seekSignalRef.current = nextSignal;
        setScrollTarget({ ratio, signal: nextSignal });
      });
    },
    [jumpToPage],
  );

  useSidebarPanel(
    'library',
    <div className="sidebar-control-stack">
      <div className="sidebar-control-title">图库</div>
      <div className="sidebar-list">
        {(['all', 'image', 'video'] as AssetKind[]).map((value) => (
          <button className={type === value ? 'sidebar-list-row active' : 'sidebar-list-row'} key={value} type="button" onClick={() => setType(value)}>
            {value === 'all' ? <Images size={14} /> : value === 'image' ? <ImageIcon size={14} /> : <Video size={14} />}
            <span>{assetKindLabel(value)}</span>
          </button>
        ))}
      </div>
      <label className="sidebar-field">
        <span>排序</span>
        <select value={sort} onChange={(event) => setSort(event.target.value as SortKey)}>
          <option value="timeline_desc">时间新到旧</option>
          <option value="timeline_asc">时间旧到新</option>
          <option value="filename">文件名</option>
          <option value="size">大小</option>
          <option value="imported_desc">导入时间</option>
        </select>
      </label>
      <label className="sidebar-field">
        <span>搜索</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名" />
      </label>
      <div className="sidebar-control-title">分组</div>
      <div className="sidebar-list">
        {groupOptionsForSort(sort).map((option) => (
          <button
            className={groupMode === option.value ? 'sidebar-list-row active' : 'sidebar-list-row'}
            key={option.value}
            type="button"
            onClick={() => setGroupMode(option.value)}
          >
            <span className="sidebar-list-marker" aria-hidden="true" />
            <span>{option.label}</span>
          </button>
        ))}
      </div>
    </div>,
    [type, sort, query, groupMode],
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
            onLoadMore={loadMore}
            onOpenAsset={handleOpenAsset}
            onOpenViewer={handleOpenViewer}
            onAssetMissing={(asset) => mutateItems((current) => removeAssetById(current, asset.id))}
            onScrollRatioChange={setScrollRatio}
            onScrollStateChange={handleGridScrollState}
            totalCount={totalCount}
            loadedStartIndex={loadedStartIndex}
            groupMode={groupMode}
            sort={sort}
            scrollSignal={scrollResetSignal}
            scrollTarget={scrollTarget}
            scrollTopTarget={scrollTopTarget}
            onPressPreviewChange={setPressPreviewAsset}
            buildViewerUrl={(asset) =>
              `/viewer/${asset.id}?context=library&type=${type}&sort=${sort}&q=${encodeURIComponent(query)}&returnPath=%2Flibrary&returnState=${encodeReturnState(
                currentPageState(),
              )}`
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

function groupOptionsForSort(sort: SortKey): Array<{ value: AssetGroupMode; label: string }> {
  if (sort === 'filename' || sort === 'filename_asc' || sort === 'filename_desc') {
    return [
      { value: 'none', label: '不分' },
      { value: 'letter', label: '首字母' },
    ];
  }
  if (sort === 'size' || sort === 'size_asc' || sort === 'size_desc') {
    return [
      { value: 'none', label: '不分' },
      { value: 'size', label: '大小' },
    ];
  }
  return [
    { value: 'none', label: '不分' },
    { value: 'day', label: '日' },
    { value: 'month', label: '月' },
    { value: 'year', label: '年' },
  ];
}

function libraryStateFromSearchParams(params: URLSearchParams, fallback: LibraryPageState): LibraryPageState {
  const type = params.get('type');
  const sort = params.get('sort');
  const q = params.get('q');
  const hasLibraryParams = params.has('type') || params.has('sort') || params.has('q');
  const base = hasLibraryParams ? { ...fallback, ...resetGridState() } : fallback;
  return {
    ...base,
    query: q ?? (hasLibraryParams ? '' : base.query),
    sort: sortKeys.includes(sort as SortKey) ? (sort as SortKey) : base.sort,
    type: assetKinds.includes(type as AssetKind) ? (type as AssetKind) : base.type,
  };
}
