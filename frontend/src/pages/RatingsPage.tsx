import { useCallback, useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { FolderOpen, FolderX, Image as ImageIcon, Images, Star, StarOff, Video } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import SortControls, { isSortKey } from '../components/SortControls';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type { Album, AlbumAssetFilter, Asset, AssetDeletedEvent, AssetKind, AssetRating, LibraryAnchor, SortKey } from '../types/api';
import { useRestoreSidebarState, useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { serverGroupForMode, type AssetGroupMode } from '../utils/assetGrouping';
import {
  appendViewerReturnParams,
  assetRatingChanged,
  assetRatingChangeDetail,
  decodeReturnState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { assetMatchesAlbum, assetMatchesAnyAlbum, assetMatchesRating } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';
import { ratingLabel } from '../components/RatingStars';

const pageSize = 100;
const ratingsStateKey = 'ratings';
const assetKinds: AssetKind[] = ['all', 'image', 'video'];
const ratingValues: AssetRating[] = [0, 1, 2, 3, 4, 5];
type RatingAlbumFilter = 'all' | 'none' | `album:${number}`;

interface RatingsPageState extends GridReturnState {
  albumFilter: RatingAlbumFilter;
  groupMode: AssetGroupMode;
  query: string;
  rating: AssetRating;
  sort: SortKey;
  type: AssetKind;
}

const defaultRatingsState: RatingsPageState = {
  ...resetGridState(),
  albumFilter: 'all',
  groupMode: 'none',
  query: '',
  rating: 0,
  sort: 'timeline_desc',
  type: 'all',
};

export default function RatingsPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const decodedInitialState = decodeReturnState<RatingsPageState>(searchParams.get('restore'), defaultRatingsState);
  const initialStateRef = useRef(
    searchParams.has('restore') ? decodedInitialState : ratingsStateFromSearchParams(searchParams, defaultRatingsState),
  );
  const [rating, setRating] = useState<AssetRating>(initialStateRef.current.rating);
  const [type, setType] = useState<AssetKind>(initialStateRef.current.type);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [albumFilter, setAlbumFilter] = useState<RatingAlbumFilter>(initialStateRef.current.albumFilter);
  const [albums, setAlbums] = useState<Album[]>([]);
  const [albumError, setAlbumError] = useState('');
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const serverGroup = serverGroupForMode(groupMode);
  const selectedAlbumId = albumIdFromFilter(albumFilter);
  const albumApiFilter: AlbumAssetFilter | undefined = albumFilter === 'none' ? 'none' : undefined;
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const sidebarState = useSidebarReturnState();
  const restoreSidebarState = useRestoreSidebarState();

  useEffect(() => {
    let live = true;
    async function loadAlbums() {
      try {
        const result = await api.albums();
        if (live) {
          setAlbums(result.items);
          setAlbumError('');
        }
      } catch (err) {
        if (live) {
          setAlbumError(err instanceof Error ? err.message : '读取相册失败');
        }
      }
    }
    void loadAlbums();
    return () => {
      live = false;
    };
  }, []);

  const loadAssets = useCallback(
    (page: number) => api.libraryAssets(page, pageSize, type, sort, query, serverGroup, rating, selectedAlbumId ?? undefined, albumApiFilter),
    [albumApiFilter, query, rating, selectedAlbumId, serverGroup, sort, type],
  );
  const { items, hasMore, loading, error, loadMore, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    type,
    sort,
    query,
    serverGroup,
    rating,
    albumFilter,
  ]);
  const {
    focusAssetId,
    getGridState,
    handleGridScrollState,
    loadedStartIndex,
    scrollRatio,
    scrollResetSignal,
    scrollTarget,
    scrollTopTarget,
    seekIndex,
    setScrollRatio,
  } = useWaterfallGridState({
    hasMore,
    initialState: initialStateRef.current,
    itemsLength: items.length,
    jumpToPage,
    loading,
    loadMore,
    pageSize,
    resetKey: JSON.stringify([rating, type, sort, query, groupMode, albumFilter]),
    searchParams,
  });

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const filtered = incoming.filter(
        (asset) => assetMatchesRating(asset, rating, type, query) && assetMatchesRatingAlbumFilter(asset, albumFilter, albums),
      );
      if (filtered.length === 0) return;
      mutateItems((current) => mergeSortedAssets(current, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [albumFilter, albums, groupMode, hasMore, loadedStartIndex, mutateItems, query, rating, sort, type],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((current) => removeAssetById(current, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    const handleRatingChanged = (event: Event) => {
      const detail = assetRatingChangeDetail(event);
      if (!detail) return;
      if (detail.rating !== rating) {
        setTotalCount((value) => Math.max(0, value - 1));
      }
      mutateItems((current) => {
        if (detail.rating !== rating) {
          return removeAssetById(current, detail.assetId);
        }
        return current.map((asset) => (asset.id === detail.assetId ? { ...asset, rating } : asset));
      });
    };
    window.addEventListener(assetRatingChanged, handleRatingChanged);
    return () => window.removeEventListener(assetRatingChanged, handleRatingChanged);
  }, [mutateItems, rating]);

  useEffect(() => {
    if (eventsConnected) return undefined;
    const timer = window.setInterval(() => {
      void api
        .libraryAssets(1, pageSize, type, sort, query, serverGroup, rating, selectedAlbumId ?? undefined, albumApiFilter)
        .then((result) => mergeReadyAssets(result.items))
        .catch(() => undefined);
    }, 8000);
    return () => window.clearInterval(timer);
  }, [albumApiFilter, eventsConnected, mergeReadyAssets, query, rating, selectedAlbumId, serverGroup, sort, type]);

  const currentPageState = useCallback(
    (): RatingsPageState => ({
      ...getGridState(),
      albumFilter,
      groupMode,
      query,
      rating,
      sidebarCollapsed: sidebarState.sidebarCollapsed,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
      type,
    }),
    [albumFilter, getGridState, groupMode, query, rating, sidebarState.sidebarCollapsed, sidebarState.sidebarExpanded, sort, type],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<RatingsPageState>(ratingsStateKey, currentPageState());
  }, [currentPageState]);

  useEffect(() => {
    let live = true;
    async function loadAnchors() {
      try {
        const result = await api.libraryAnchors(pageSize, type, sort, query, serverGroup, rating, selectedAlbumId ?? undefined, albumApiFilter);
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
  }, [albumApiFilter, query, rating, selectedAlbumId, serverGroup, sort, type]);

  useEffect(() => {
    const options = groupOptionsForSort(sort).map((option) => option.value);
    if (!options.includes(groupMode)) {
      setGroupMode('none');
    }
  }, [groupMode, sort]);

  const handleOpenAsset = useCallback(() => {
    saveCurrentState();
    saveViewerReturnPath('/ratings');
  }, [saveCurrentState]);

  const handleOpenViewer = useCallback(
    (asset: Asset, viewerUrl: string) => {
      navigate(viewerUrl, { state: { backgroundLocation: location, initialAsset: asset } });
    },
    [location, navigate],
  );

  useSidebarPanel(
    'ratings',
    <div className="sidebar-control-stack">
      <div className="sidebar-list">
        {ratingValues.map((value) => (
          <button className={rating === value ? 'sidebar-list-row active' : 'sidebar-list-row'} key={value} type="button" onClick={() => setRating(value)}>
            {value === 0 ? <StarOff size={14} /> : <Star size={14} fill="currentColor" />}
            <span>{ratingLabel(value)}</span>
          </button>
        ))}
      </div>
      <div className="sidebar-list">
        {assetKinds.map((value) => (
          <button className={type === value ? 'sidebar-list-row active' : 'sidebar-list-row'} key={value} type="button" onClick={() => setType(value)}>
            {value === 'all' ? <Images size={14} /> : value === 'image' ? <ImageIcon size={14} /> : <Video size={14} />}
            <span>{assetKindLabel(value)}</span>
          </button>
        ))}
      </div>
      <div className="sidebar-control-title">相册</div>
      <div className="sidebar-list">
        <button className={albumFilter === 'all' ? 'sidebar-list-row active' : 'sidebar-list-row'} type="button" onClick={() => setAlbumFilter('all')}>
          <Images size={14} />
          <span>全部</span>
        </button>
        <button className={albumFilter === 'none' ? 'sidebar-list-row active' : 'sidebar-list-row'} type="button" onClick={() => setAlbumFilter('none')}>
          <FolderX size={14} />
          <span>不在相册</span>
        </button>
        {albums.map((album) => (
          <button
            className={albumFilter === albumFilterForId(album.id) ? 'sidebar-list-row active' : 'sidebar-list-row'}
            key={album.id}
            title={album.name}
            type="button"
            onClick={() => setAlbumFilter(albumFilterForId(album.id))}
          >
            <FolderOpen size={14} />
            <span>{album.name}</span>
            <span>{album.assetCount}</span>
          </button>
        ))}
      </div>
      <SortControls sort={sort} onChange={setSort} />
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
    [albumFilter, albums, type, sort, query, groupMode, rating],
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
      {(error || albumError) && <div className="error-line">{error || albumError}</div>}
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
            focusAssetId={focusAssetId}
            groupMode={groupMode}
            sort={sort}
            scrollSignal={scrollResetSignal}
            scrollTarget={scrollTarget}
            scrollTopTarget={scrollTopTarget}
            onPressPreviewChange={setPressPreviewAsset}
            buildViewerUrl={(asset) =>
              appendViewerReturnParams(
                `/viewer/${asset.id}?context=rating&rating=${rating}&type=${type}&sort=${sort}&q=${encodeURIComponent(query)}${
                  serverGroup ? `&group=${serverGroup}` : ''
                }${albumViewerParams(albumFilter)}`,
                '/ratings',
                currentPageState(),
              )
            }
          />
          <LibraryIndexRail anchors={anchors} sort={sort} scrollRatio={scrollRatio} totalCount={totalCount} pageSize={pageSize} onSeek={seekIndex} />
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
      { value: 'folder', label: '文件夹' },
      { value: 'letter', label: '首字母' },
    ];
  }
  if (sort === 'size' || sort === 'size_asc' || sort === 'size_desc') {
    return [
      { value: 'none', label: '不分' },
      { value: 'folder', label: '文件夹' },
      { value: 'size', label: '大小' },
    ];
  }
  return [
    { value: 'none', label: '不分' },
    { value: 'folder', label: '文件夹' },
    { value: 'day', label: '日' },
    { value: 'month', label: '月' },
    { value: 'year', label: '年' },
  ];
}

function albumFilterForId(id: number): RatingAlbumFilter {
  return `album:${id}`;
}

function albumIdFromFilter(value: RatingAlbumFilter) {
  if (!value.startsWith('album:')) return null;
  const parsed = Number(value.slice('album:'.length));
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
}

function albumViewerParams(value: RatingAlbumFilter) {
  if (value === 'none') return '&albumFilter=none';
  const albumId = albumIdFromFilter(value);
  return albumId === null ? '' : `&albumId=${albumId}`;
}

function assetMatchesRatingAlbumFilter(asset: Asset, albumFilter: RatingAlbumFilter, albums: Album[]) {
  if (albumFilter === 'all') return true;
  if (albumFilter === 'none') return !assetMatchesAnyAlbum(asset, albums);
  const albumId = albumIdFromFilter(albumFilter);
  const album = albumId === null ? null : albums.find((item) => item.id === albumId) ?? null;
  return assetMatchesAlbum(asset, album, '');
}

function ratingsStateFromSearchParams(params: URLSearchParams, fallback: RatingsPageState): RatingsPageState {
  const type = params.get('type');
  const sort = params.get('sort');
  const group = params.get('group');
  const q = params.get('q');
  const rating = ratingFromSearchParam(params.get('rating'));
  const albumFilter = albumFilterFromSearchParams(params);
  const hasRatingParams =
    params.has('rating') ||
    params.has('type') ||
    params.has('sort') ||
    params.has('q') ||
    params.has('group') ||
    params.has('albumId') ||
    params.has('albumFilter') ||
    params.has('album');
  const base = hasRatingParams ? { ...fallback, ...resetGridState() } : fallback;
  return {
    ...base,
    albumFilter: albumFilter ?? base.albumFilter,
    groupMode: group === 'folder' ? 'folder' : base.groupMode,
    query: q ?? (hasRatingParams ? '' : base.query),
    rating: rating ?? base.rating,
    sort: isSortKey(sort) ? sort : base.sort,
    type: assetKinds.includes(type as AssetKind) ? (type as AssetKind) : base.type,
  };
}

function albumFilterFromSearchParams(params: URLSearchParams): RatingAlbumFilter | null {
  const mode = (params.get('albumFilter') ?? params.get('album') ?? '').trim().toLowerCase();
  if (mode === 'none' || mode === 'unassigned') return 'none';
  const parsed = Number(params.get('albumId'));
  if (Number.isInteger(parsed) && parsed > 0) return albumFilterForId(parsed);
  return null;
}

function ratingFromSearchParam(value: string | null): AssetRating | null {
  const parsed = Number(value);
  if (parsed === 0 || parsed === 1 || parsed === 2 || parsed === 3 || parsed === 4 || parsed === 5) {
    return parsed;
  }
  return null;
}
