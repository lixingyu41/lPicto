import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import AssetGrid from '../components/AssetGrid';
import AssetGroupingControls, { normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarAlbumList, SidebarButtonGroup, SidebarMediaTypeList, SidebarRatingFilter, sidebarOrientationOptions } from '../components/SidebarControls';
import SortControls, { isSortKey } from '../components/SortControls';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type {
  Album,
  AlbumAssetFilter,
  AlbumGroup,
  Asset,
  AssetDeletedEvent,
  AssetKind,
  AssetRating,
  LibraryAnchor,
  OrientationFilter,
  SortKey,
} from '../types/api';
import { useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { parseAssetGroupMode, serverGroupForMode, type AssetGroupMode } from '../utils/assetGrouping';
import {
  appendViewerReturnParams,
  assetRatingChanged,
  assetRatingChangeDetail,
  decodeReturnState,
  loadPageState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { assetMatchesAlbum, assetMatchesAnyAlbum, assetMatchesRating } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';
import { currentURLHasParam, currentURLLocation, currentURLPath, orientationParam, positiveIntParam, replaceURLState } from '../utils/urlState';

const pageSize = 100;
const ratingsStateKey = 'ratings';
const ratingsURLKeys = ['rating', 'type', 'orientation', 'sort', 'group', 'q', 'albumFilter', 'albumId', 'albumIds', 'album'];
const assetKinds: AssetKind[] = ['all', 'image', 'video'];
type RatingAlbumFilterMode = 'all' | 'none' | 'albums';

interface RatingsPageState extends GridReturnState {
  albumFilterMode: RatingAlbumFilterMode;
  albumIds: number[];
  albumListCollapsed: boolean;
  collapsedGroupKeys: string[];
  groupMode: AssetGroupMode;
  orientation: OrientationFilter;
  query: string;
  rating: AssetRating;
  sort: SortKey;
  type: AssetKind;
}

interface LegacyRatingsPageState extends Partial<RatingsPageState> {
  albumFilter?: string;
}

const defaultRatingsState: RatingsPageState = {
  ...resetGridState(),
  albumFilterMode: 'all',
  albumIds: [],
  albumListCollapsed: false,
  collapsedGroupKeys: [],
  groupMode: 'none',
  orientation: 'all',
  query: '',
  rating: 0,
  sort: 'timeline_desc',
  type: 'all',
};

export default function RatingsPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const persistedState = normalizeRatingsState(loadPageState<LegacyRatingsPageState>(ratingsStateKey, defaultRatingsState));
  const decodedInitialState = normalizeRatingsState(decodeReturnState<LegacyRatingsPageState>(searchParams.get('restore'), persistedState));
  const initialStateRef = useRef(
    searchParams.has('restore') ? decodedInitialState : ratingsStateFromSearchParams(searchParams, persistedState),
  );
  const [rating, setRating] = useState<AssetRating>(initialStateRef.current.rating ?? 0);
  const [type, setType] = useState<AssetKind>(initialStateRef.current.type);
  const [orientation, setOrientation] = useState<OrientationFilter>(initialStateRef.current.orientation);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [albumFilterMode, setAlbumFilterMode] = useState<RatingAlbumFilterMode>(initialStateRef.current.albumFilterMode);
  const [albumIds, setAlbumIds] = useState<number[]>(initialStateRef.current.albumIds);
  const [albumListCollapsed, setAlbumListCollapsed] = useState(initialStateRef.current.albumListCollapsed);
  const [collapsedGroupKeys, setCollapsedGroupKeys] = useState<Set<string>>(() => new Set(initialStateRef.current.collapsedGroupKeys));
  const [albums, setAlbums] = useState<Album[]>([]);
  const [groups, setGroups] = useState<AlbumGroup[]>([]);
  const [albumError, setAlbumError] = useState('');
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const serverGroup = serverGroupForMode(groupMode);
  const activeAlbumIds = useMemo(() => (albumFilterMode === 'albums' ? albumIds : []), [albumFilterMode, albumIds]);
  const albumIdsKey = activeAlbumIds.join(',');
  const singleAlbumId = activeAlbumIds.length === 1 ? activeAlbumIds[0] : undefined;
  const multiAlbumIds = activeAlbumIds.length > 1 ? activeAlbumIds : undefined;
  const selectedAlbum = useMemo(() => (singleAlbumId === undefined ? null : albums.find((album) => album.id === singleAlbumId) ?? null), [albums, singleAlbumId]);
  const albumApiFilter: AlbumAssetFilter | undefined = albumFilterMode === 'none' ? 'none' : undefined;
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const sidebarState = useSidebarReturnState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);

  useEffect(() => {
    let live = true;
    async function loadAlbums() {
      try {
        const result = await api.albums();
        if (live) {
          setAlbums(result.items);
          setGroups(result.groups ?? []);
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
    (page: number) => api.libraryAssets(page, pageSize, type, sort, query, serverGroup, rating, singleAlbumId, albumApiFilter, multiAlbumIds, orientation),
    [albumApiFilter, multiAlbumIds, orientation, query, rating, serverGroup, singleAlbumId, sort, type],
  );
  const { items, hasMore, hasPrevious, loading, error, loadMore, loadPrevious, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    type,
    sort,
    query,
    serverGroup,
    rating,
    orientation,
    albumFilterMode,
    albumIdsKey,
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
    resetKey: JSON.stringify([rating, type, orientation, sort, query, groupMode, albumFilterMode, albumIdsKey]),
    searchParams,
  });

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const filtered = incoming.filter(
        (asset) => assetMatchesRating(asset, rating, type, query, orientation) && assetMatchesRatingAlbumFilter(asset, albumFilterMode, activeAlbumIds, albums),
      );
      if (filtered.length === 0) return;
      mutateItems((current) => mergeSortedAssets(current, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [activeAlbumIds, albumFilterMode, albums, groupMode, hasMore, loadedStartIndex, mutateItems, orientation, query, rating, sort, type],
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
        .libraryAssets(1, pageSize, type, sort, query, serverGroup, rating, singleAlbumId, albumApiFilter, multiAlbumIds, orientation)
        .then((result) => mergeReadyAssets(result.items))
        .catch(() => undefined);
    }, 30000);
    return () => window.clearInterval(timer);
  }, [albumApiFilter, eventsConnected, mergeReadyAssets, multiAlbumIds, orientation, query, rating, serverGroup, singleAlbumId, sort, type]);

  const currentPageState = useCallback(
    (): RatingsPageState => ({
      ...getGridState(),
      albumFilterMode,
      albumIds,
      albumListCollapsed,
      collapsedGroupKeys: Array.from(collapsedGroupKeys),
      groupMode,
      orientation,
      query,
      rating,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
      type,
    }),
    [albumFilterMode, albumIds, albumListCollapsed, collapsedGroupKeys, getGridState, groupMode, orientation, query, rating, sidebarState.sidebarExpanded, sort, type],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<RatingsPageState>(ratingsStateKey, currentPageState());
  }, [currentPageState]);
  const scheduleCurrentStateSave = usePersistentPageState(saveCurrentState);

  useEffect(() => {
    if (currentURLHasParam(location, 'restore')) return;
    replaceURLState(
      navigate,
      location,
      {
        album: selectedAlbum?.name,
        albumFilter: albumFilterMode === 'all' || albumFilterMode === 'none' ? albumFilterMode : undefined,
        albumId: singleAlbumId,
        albumIds: multiAlbumIds?.join(','),
        group: groupMode,
        orientation: orientation === 'all' ? undefined : orientation,
        q: query,
        rating,
        sort,
        type,
      },
      ratingsURLKeys,
    );
  }, [albumFilterMode, groupMode, location, multiAlbumIds, navigate, orientation, query, rating, searchParams, selectedAlbum, singleAlbumId, sort, type]);

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
        const result = await api.libraryAnchors(pageSize, type, sort, query, serverGroup, rating, singleAlbumId, albumApiFilter, multiAlbumIds, orientation);
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
  }, [albumApiFilter, multiAlbumIds, orientation, query, rating, serverGroup, singleAlbumId, sort, type]);

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

  const handleSelectAllAlbums = useCallback(() => {
    setAlbumFilterMode('all');
    setAlbumIds([]);
  }, []);

  const handleSelectUnassignedAlbums = useCallback(() => {
    setAlbumFilterMode('none');
    setAlbumIds([]);
  }, []);

  const handleToggleAlbum = useCallback(
    (album: Album) => {
      const nextAlbumIds = albumIds.includes(album.id) ? albumIds.filter((id) => id !== album.id) : [...albumIds, album.id];
      setAlbumIds(nextAlbumIds);
      setAlbumFilterMode(nextAlbumIds.length > 0 ? 'albums' : 'all');
    },
    [albumIds],
  );

  const handleToggleAlbumListCollapsed = useCallback(() => {
    setAlbumListCollapsed((value) => !value);
  }, []);

  const handleToggleAlbumGroup = useCallback((key: string) => {
    setCollapsedGroupKeys((current) => {
      const next = new Set(current);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  useSidebarPanel(
    'ratings',
    <div className="sidebar-control-stack">
      <SidebarRatingFilter value={rating} onChange={setRating} />
      <SidebarMediaTypeList value={type} onChange={setType} />
      <SidebarButtonGroup columns={3} label="方向" value={orientation} options={sidebarOrientationOptions} onChange={setOrientation} />
      <SidebarAlbumList
        albums={albums}
        collapsed={albumListCollapsed}
        collapsedGroupKeys={Array.from(collapsedGroupKeys)}
        collapsible
        forceGroupHeaders
        groups={groups}
        selectedIds={albumFilterMode === 'albums' ? albumIds : []}
        showAll
        showUnassigned
        allActive={albumFilterMode === 'all'}
        unassignedActive={albumFilterMode === 'none'}
        onSelectAll={handleSelectAllAlbums}
        onSelectUnassigned={handleSelectUnassignedAlbums}
        onSelectAlbum={handleToggleAlbum}
        onToggleCollapsed={handleToggleAlbumListCollapsed}
        onToggleGroup={handleToggleAlbumGroup}
      />
      <SortControls sort={sort} onChange={setSort} />
      <label className="sidebar-field">
        <span>搜索</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名" />
      </label>
      <AssetGroupingControls groupMode={groupMode} sort={sort} onChange={setGroupMode} />
    </div>,
    [
      albumFilterMode,
      albumIds,
      albumListCollapsed,
      albums,
      collapsedGroupKeys,
      groups,
      handleSelectAllAlbums,
      handleSelectUnassignedAlbums,
      handleToggleAlbumGroup,
      handleToggleAlbumListCollapsed,
      handleToggleAlbum,
      type,
      orientation,
      sort,
      query,
      groupMode,
      rating,
    ],
  );

  useSidebarPanel(
    'viewer',
    pressPreviewAsset ? <AssetInfoPanel asset={pressPreviewAsset} title="快速预览" /> : null,
    [pressPreviewAsset?.id],
  );

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
                `/viewer/${asset.id}?context=rating&rating=${rating}&type=${type}&sort=${sort}&q=${encodeURIComponent(query)}${
                  serverGroup ? `&group=${serverGroup}` : ''
                }${orientationViewerParam(orientation)}${albumViewerParams(albumFilterMode, activeAlbumIds)}`,
                currentPageReturnPath(),
                currentPageState(),
              )
            }
          />
          {groupMode !== 'folder' && (
            <LibraryIndexRail anchors={anchors} sort={sort} scrollRatio={scrollRatio} totalCount={totalCount} pageSize={pageSize} onSeek={seekIndex} />
          )}
          <PressPreviewOverlay asset={pressPreviewAsset} />
        </div>
      )}
    </section>
  );
}

function albumViewerParams(mode: RatingAlbumFilterMode, albumIds: number[]) {
  if (mode === 'none') return '&albumFilter=none';
  if (mode !== 'albums' || albumIds.length === 0) return '';
  if (albumIds.length === 1) return `&albumId=${albumIds[0]}`;
  return `&albumIds=${albumIds.join(',')}`;
}

function assetMatchesRatingAlbumFilter(asset: Asset, mode: RatingAlbumFilterMode, albumIds: number[], albums: Album[]) {
  if (mode === 'all') return true;
  if (mode === 'none') return !assetMatchesAnyAlbum(asset, albums);
  const selected = new Set(albumIds);
  return albums.some((album) => selected.has(album.id) && assetMatchesAlbum(asset, album, ''));
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
    params.has('orientation') ||
    params.has('sort') ||
    params.has('q') ||
    params.has('group') ||
    params.has('albumId') ||
    params.has('albumIds') ||
    params.has('albumFilter') ||
    params.has('album');
  const base = hasRatingParams ? { ...fallback, ...resetGridState() } : fallback;
  return {
    ...base,
    albumFilterMode: albumFilter?.mode ?? base.albumFilterMode,
    albumIds: albumFilter?.albumIds ?? base.albumIds,
    groupMode: parseAssetGroupMode(group, base.groupMode),
    orientation: params.has('orientation') ? orientationParam(params.get('orientation')) : base.orientation,
    query: q ?? (hasRatingParams ? '' : base.query),
    rating: rating ?? base.rating,
    sort: isSortKey(sort) ? sort : base.sort,
    type: assetKinds.includes(type as AssetKind) ? (type as AssetKind) : base.type,
  };
}

function orientationViewerParam(orientation: OrientationFilter) {
  return orientation === 'all' ? '' : `&orientation=${orientation}`;
}

function albumFilterFromSearchParams(params: URLSearchParams): { mode: RatingAlbumFilterMode; albumIds: number[] } | null {
  const mode = (params.get('albumFilter') ?? params.get('album') ?? '').trim().toLowerCase();
  if (mode === 'all') return { mode: 'all', albumIds: [] };
  if (mode === 'none' || mode === 'unassigned') return { mode: 'none', albumIds: [] };
  const albumIds = parseAlbumIds(params.get('albumIds'));
  if (albumIds.length > 0) return { mode: 'albums', albumIds };
  const parsed = positiveIntParam(params.get('albumId')) ?? positiveIntParam(params.get('album'));
  if (parsed) return { mode: 'albums', albumIds: [parsed] };
  return null;
}

function normalizeRatingsState(value: LegacyRatingsPageState): RatingsPageState {
  const legacyAlbumFilter = legacyAlbumFilterFromValue(value.albumFilter);
  const albumIds = normalizeAlbumIds(legacyAlbumFilter?.albumIds ?? value.albumIds);
  const albumFilterMode = legacyAlbumFilter?.mode ?? (isRatingAlbumFilterMode(value.albumFilterMode) ? value.albumFilterMode : albumIds.length > 0 ? 'albums' : 'all');
  return {
    ...defaultRatingsState,
    ...value,
    albumFilterMode: albumFilterMode === 'albums' && albumIds.length === 0 ? 'all' : albumFilterMode,
    albumIds,
    rating: ratingFromSearchParam(String(value.rating)) ?? defaultRatingsState.rating,
  };
}

function legacyAlbumFilterFromValue(value: string | undefined) {
  if (!value) return null;
  if (value === 'all') return { mode: 'all' as const, albumIds: [] };
  if (value === 'none') return { mode: 'none' as const, albumIds: [] };
  if (!value.startsWith('album:')) return null;
  const parsed = positiveIntParam(value.slice('album:'.length));
  return parsed ? { mode: 'albums' as const, albumIds: [parsed] } : null;
}

function parseAlbumIds(value: string | null) {
  if (!value) return [];
  return normalizeAlbumIds(value.split(',').map((part) => Number(part.trim())));
}

function normalizeAlbumIds(value: unknown) {
  if (!Array.isArray(value)) return [];
  const seen = new Set<number>();
  const result: number[] = [];
  value.forEach((item) => {
    const parsed = Number(item);
    if (!Number.isInteger(parsed) || parsed <= 0 || seen.has(parsed)) return;
    seen.add(parsed);
    result.push(parsed);
  });
  return result;
}

function isRatingAlbumFilterMode(value: unknown): value is RatingAlbumFilterMode {
  return value === 'all' || value === 'none' || value === 'albums';
}

function ratingFromSearchParam(value: string | null): AssetRating | null {
  const parsed = Number(value);
  if (parsed === 0 || parsed === 1 || parsed === 2 || parsed === 3 || parsed === 4 || parsed === 5) {
    return parsed;
  }
  return null;
}
