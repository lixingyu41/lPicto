import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { Check, FolderPlus, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react';
import AlbumEditor from '../components/AlbumEditor';
import AssetGrid from '../components/AssetGrid';
import AssetGroupingControls, { normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarAlbumList, SidebarButtonGroup, SidebarMediaTypeList, SidebarRatingFilter, sidebarOrientationOptions } from '../components/SidebarControls';
import SortControls, { isSortKey } from '../components/SortControls';
import { useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type {
  Album,
  AlbumGroup,
  AlbumSourceInput,
  Asset,
  AssetDeletedEvent,
  AssetKind,
  AssetRating,
  LibraryAnchor,
  OrientationFilter,
  SortKey,
} from '../types/api';
import {
  appendViewerReturnParams,
  decodeReturnState,
  loadPageState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { parseAssetGroupMode, serverGroupForMode, type AssetGroupMode } from '../utils/assetGrouping';
import { assetMatchesAlbum } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';
import { assetRatingParam, currentURLHasParam, currentURLLocation, currentURLPath, orientationParam, positiveIntParam, replaceURLState } from '../utils/urlState';

const pageSize = 100;
const albumsStateKey = 'albums';
const albumsURLKeys = ['albumId', 'album', 'type', 'rating', 'orientation', 'sort', 'group', 'q'];
const assetKinds: AssetKind[] = ['all', 'image', 'video'];

interface AlbumsPageState extends GridReturnState {
  albumListCollapsed: boolean;
  collapsedGroupKeys: string[];
  groupMode: AssetGroupMode;
  orientation: OrientationFilter;
  query: string;
  rating: AssetRating;
  selectedId: number | null;
  sort: SortKey;
  type: AssetKind;
}

const defaultAlbumsState: AlbumsPageState = {
  ...resetGridState(),
  albumListCollapsed: false,
  collapsedGroupKeys: [],
  groupMode: 'none',
  orientation: 'all',
  query: '',
  rating: 0,
  selectedId: null,
  sort: 'timeline_desc',
  type: 'all',
};

export default function AlbumsPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const persistedState = loadPageState<AlbumsPageState>(albumsStateKey, defaultAlbumsState);
  const decodedInitialState = decodeReturnState<AlbumsPageState>(searchParams.get('restore'), persistedState);
  const initialStateRef = useRef(
    searchParams.has('restore') ? decodedInitialState : albumsStateFromSearchParams(searchParams, persistedState),
  );
  const initialAlbumNameRef = useRef(searchParams.get('album') ?? '');
  const [albums, setAlbums] = useState<Album[]>([]);
  const [groups, setGroups] = useState<AlbumGroup[]>([]);
  const [type, setType] = useState<AssetKind>(initialStateRef.current.type);
  const [selectedId, setSelectedId] = useState<number | null>(initialStateRef.current.selectedId);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [rating, setRating] = useState<AssetRating>(initialStateRef.current.rating ?? 0);
  const [orientation, setOrientation] = useState<OrientationFilter>(initialStateRef.current.orientation);
  const [addOpen, setAddOpen] = useState(false);
  const [editingAlbum, setEditingAlbum] = useState<Album | null>(null);
  const [groupDraftOpen, setGroupDraftOpen] = useState(false);
  const [groupName, setGroupName] = useState('');
  const [albumListCollapsed, setAlbumListCollapsed] = useState(initialStateRef.current.albumListCollapsed);
  const [collapsedGroupKeys, setCollapsedGroupKeys] = useState<Set<string>>(() => new Set(initialStateRef.current.collapsedGroupKeys));
  const [error, setError] = useState<string | null>(null);
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const sidebarState = useSidebarReturnState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const serverGroup = serverGroupForMode(groupMode);

  const selectedAlbum = useMemo(
    () => albums.find((album) => album.id === selectedId) ?? albums[0] ?? null,
    [albums, selectedId],
  );
  const loadAlbums = useCallback(async () => {
    try {
      const result = await api.albums();
      setAlbums(result.items);
      setGroups(result.groups ?? []);
      setSelectedId((current) => {
        if (current && result.items.some((album) => album.id === current)) return current;
        const requestedName = initialAlbumNameRef.current.trim();
        const byName = requestedName && !positiveIntParam(requestedName) ? result.items.find((album) => album.name === requestedName)?.id : null;
        return byName ?? result.items[0]?.id ?? null;
      });
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取相册失败');
    }
  }, []);

  useEffect(() => {
    void loadAlbums();
  }, [loadAlbums]);

  const loadAssets = useCallback(
    (page: number) => {
      if (!selectedAlbum) {
        return Promise.resolve({ items: [], page, pageSize, hasMore: false });
      }
      return api.albumAssets(selectedAlbum.id, page, pageSize, sort, query, serverGroup, rating, orientation, type);
    },
    [orientation, query, rating, selectedAlbum, serverGroup, sort, type],
  );

  const { items, hasMore, hasPrevious, loading, error: loadError, loadMore, loadPrevious, reset, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    groupMode,
    orientation,
    rating,
    selectedAlbum?.id,
    sort,
    type,
    query,
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
    resetKey: JSON.stringify([selectedAlbum?.id ?? null, type, rating, orientation, sort, query, groupMode]),
    restoreReady: Boolean(selectedAlbum),
    searchParams,
  });

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const filtered = incoming.filter((asset) => assetMatchesAlbum(asset, selectedAlbum, query, rating, orientation, type));
      if (filtered.length === 0) return;
      mutateItems((current) => mergeSortedAssets(current, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [groupMode, hasMore, loadedStartIndex, mutateItems, orientation, query, rating, selectedAlbum, sort, type],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((current) => removeAssetById(current, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected || !selectedAlbum) return undefined;
    const timer = window.setInterval(() => {
      void api
        .albumAssets(selectedAlbum.id, 1, pageSize, sort, query, serverGroup, rating, orientation, type)
        .then((result) => mergeReadyAssets(result.items))
        .catch(() => undefined);
    }, 30000);
    return () => window.clearInterval(timer);
  }, [eventsConnected, mergeReadyAssets, orientation, query, rating, selectedAlbum, serverGroup, sort, type]);

  useEffect(() => {
    let live = true;
    async function loadAnchors(albumId: number) {
      try {
        const result = await api.albumAnchors(albumId, pageSize, sort, query, serverGroup, rating, orientation, type);
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
    if (selectedAlbum) {
      void loadAnchors(selectedAlbum.id);
    } else {
      setAnchors([]);
      setTotalCount(0);
    }
    return () => {
      live = false;
    };
  }, [orientation, query, rating, selectedAlbum?.id, serverGroup, sort, type]);

  const currentPageState = useCallback(
    (): AlbumsPageState => ({
      ...getGridState(),
      albumListCollapsed,
      collapsedGroupKeys: Array.from(collapsedGroupKeys),
      groupMode,
      orientation,
      query,
      rating,
      selectedId: selectedAlbum?.id ?? selectedId,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
      type,
    }),
    [albumListCollapsed, collapsedGroupKeys, getGridState, groupMode, orientation, query, rating, selectedAlbum?.id, selectedId, sidebarState.sidebarExpanded, sort, type],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<AlbumsPageState>(albumsStateKey, currentPageState());
  }, [currentPageState]);
  const scheduleCurrentStateSave = usePersistentPageState(saveCurrentState);

  useEffect(() => {
    if (currentURLHasParam(location, 'restore') || !selectedAlbum) return;
    replaceURLState(
      navigate,
      location,
      {
        album: selectedAlbum.name,
        albumId: selectedAlbum.id,
        group: groupMode,
        orientation: orientation === 'all' ? undefined : orientation,
        q: query,
        rating,
        sort,
        type,
      },
      albumsURLKeys,
    );
  }, [groupMode, location, navigate, orientation, query, rating, searchParams, selectedAlbum, sort, type]);
  const handlePersistentGridScrollState = useCallback(
    (state: { ratio: number; scrollTop: number }) => {
      handleGridScrollState(state);
      scheduleCurrentStateSave();
    },
    [handleGridScrollState, scheduleCurrentStateSave],
  );

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

  const handleRatingChange = useCallback((nextRating: AssetRating) => {
    setRating(nextRating);
  }, []);

  const handleToggleAlbumGroup = useCallback((key: string) => {
    setCollapsedGroupKeys((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  useEffect(() => {
    const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, sort);
    if (nextGroupMode !== groupMode) {
      setGroupMode(nextGroupMode);
    }
  }, [groupMode, sort]);

  async function createAlbum(
    name: string,
    sources: AlbumSourceInput[],
    groupId: number | null,
  ) {
    try {
      const album = await api.createAlbum(name, sources, groupId);
      setAddOpen(false);
      await loadAlbums();
      setSelectedId(album.id);
      reset();
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建相册失败');
    }
  }

  async function updateAlbum(
    id: number,
    name: string,
    sources: AlbumSourceInput[],
    groupId: number | null,
  ) {
    try {
      const album = await api.updateAlbum(id, name, sources, groupId);
      setEditingAlbum(null);
      await loadAlbums();
      setSelectedId(album.id);
      reset();
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存相册失败');
    }
  }

  async function createAlbumGroup() {
    const name = groupName.trim();
    if (!name) return;
    try {
      const group = await api.createAlbumGroup(name);
      setGroups((value) => [...value, group]);
      setCollapsedGroupKeys((value) => {
        const next = new Set(value);
        next.delete(`group-${group.id}`);
        return next;
      });
      setGroupName('');
      setGroupDraftOpen(false);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建相册组失败');
    }
  }

  async function deleteAlbum(id: number) {
    try {
      await api.deleteAlbum(id);
      await loadAlbums();
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除相册失败');
    }
  }

  async function refreshAlbum(id: number) {
    try {
      await api.refreshAlbum(id);
      await loadAlbums();
      reset();
    } catch (err) {
      setError(err instanceof Error ? err.message : '刷新相册失败');
    }
  }

  useSidebarPanel(
    'albums',
    <div className="sidebar-control-stack">
      <div className="album-toolbar">
        <button className="sidebar-command" type="button" onClick={() => setAddOpen(true)}>
          <Plus size={16} />
          添加相册
        </button>
        <button className="sidebar-command" type="button" onClick={() => setGroupDraftOpen((value) => !value)}>
          <FolderPlus size={16} />
          新建组
        </button>
      </div>
      {groupDraftOpen && (
        <div className="album-group-create">
          <input value={groupName} placeholder="组名称" onChange={(event) => setGroupName(event.target.value)} />
          <button type="button" title="创建" disabled={groupName.trim().length === 0} onClick={() => void createAlbumGroup()}>
            <Check size={15} />
          </button>
        </div>
      )}
      <SidebarAlbumList
        albums={albums}
        collapsed={albumListCollapsed}
        collapsedGroupKeys={Array.from(collapsedGroupKeys)}
        collapsible
        forceGroupHeaders
        groups={groups}
        selectedIds={selectedAlbum ? [selectedAlbum.id] : []}
        onSelectAlbum={(album) => setSelectedId(album.id)}
        onToggleCollapsed={() => setAlbumListCollapsed((value) => !value)}
        onToggleGroup={handleToggleAlbumGroup}
      />
      {selectedAlbum && (
        <>
          <div className="sidebar-icon-actions">
            <button type="button" title="编辑相册" onClick={() => setEditingAlbum(selectedAlbum)}>
              <Pencil size={15} />
            </button>
            <button type="button" title="刷新相册" onClick={() => void refreshAlbum(selectedAlbum.id)}>
              <RefreshCw size={15} />
            </button>
            <button type="button" title="删除相册" onClick={() => void deleteAlbum(selectedAlbum.id)}>
              <Trash2 size={15} />
            </button>
            <span>{albumFilterLabel(selectedAlbum)}</span>
          </div>
          <SidebarMediaTypeList value={type} onChange={setType} />
          <SidebarRatingFilter value={rating} onChange={handleRatingChange} />
          <SidebarButtonGroup columns={3} label="方向" value={orientation} options={sidebarOrientationOptions} onChange={setOrientation} />
          <SortControls sort={sort} onChange={setSort} />
          <AssetGroupingControls groupMode={groupMode} sort={sort} onChange={setGroupMode} />
          <label className="sidebar-field">
            <span>搜索</span>
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名" />
          </label>
          <div className="library-paths">
            {selectedAlbum.sources.map((source) => (
              <span key={source.id}>{displayRelPath(source.relPath)} · {sourceFilterLabel(source)}</span>
            ))}
          </div>
        </>
      )}
    </div>,
    [
      albums,
      albumListCollapsed,
      collapsedGroupKeys,
      groups,
      groupDraftOpen,
      groupName,
      groupMode,
      handleRatingChange,
      handleToggleAlbumGroup,
      orientation,
      query,
      rating,
      selectedAlbum?.id,
      selectedAlbum?.updatedAt,
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
      {(error || loadError) && <div className="error-line">{error || loadError}</div>}
      {!selectedAlbum ? (
        <EmptyState text="左侧添加相册" />
      ) : items.length === 0 && !loading ? (
        <EmptyState text="当前相册没有媒体" />
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
            onPressPreviewChange={setPressPreviewAsset}
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
            buildViewerUrl={(asset) =>
              appendViewerReturnParams(
                `/viewer/${asset.id}?context=album&albumId=${selectedAlbum.id}&type=${type}&sort=${sort}&q=${encodeURIComponent(query)}${ratingViewerParam(
                  rating,
                )}${orientationViewerParam(orientation)}${serverGroup ? `&group=${serverGroup}` : ''}`,
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
      {addOpen && (
        <AlbumEditor
          groups={groups}
          onClose={() => setAddOpen(false)}
          onConfirm={(name, sources, groupId) => void createAlbum(name, sources, groupId)}
        />
      )}
      {editingAlbum && (
        <AlbumEditor
          groups={groups}
          initialAlbum={editingAlbum}
          onClose={() => setEditingAlbum(null)}
          onConfirm={(name, sources, groupId) => void updateAlbum(editingAlbum.id, name, sources, groupId)}
        />
      )}
    </section>
  );
}

function albumsStateFromSearchParams(params: URLSearchParams, fallback: AlbumsPageState): AlbumsPageState {
  const selectedId = positiveIntParam(params.get('albumId')) ?? positiveIntParam(params.get('album'));
  const sort = params.get('sort');
  const type = params.get('type');
  const hasAlbumParams = albumsURLKeys.some((key) => params.has(key));
  const base = hasAlbumParams ? { ...fallback, ...resetGridState() } : fallback;
  return {
    ...base,
    groupMode: parseAssetGroupMode(params.get('group'), base.groupMode),
    orientation: params.has('orientation') ? orientationParam(params.get('orientation')) : base.orientation,
    query: params.get('q') ?? (hasAlbumParams ? '' : base.query),
    rating: params.has('rating') ? assetRatingParam(params.get('rating')) ?? base.rating : base.rating,
    selectedId: selectedId ?? base.selectedId,
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

function sourceFilterLabel(source: { mediaTypeFilter: string; orientationFilter: string; recursive: boolean }) {
  const type = source.mediaTypeFilter === 'image' ? '照片' : source.mediaTypeFilter === 'video' ? '视频' : '全部';
  const orientation =
    source.orientationFilter === 'portrait' ? '竖屏' : source.orientationFilter === 'landscape' ? '横屏' : '全部方向';
  return `${type} · ${orientation} · ${source.recursive ? '含子文件夹' : '仅本层'}`;
}

function displayRelPath(relPath: string) {
  return relPath ? `/${relPath}` : '全部存储';
}

function albumFilterLabel(album: Album) {
  if (album.sources.some((source) => source.mediaTypeFilter !== 'all' || source.orientationFilter !== 'all' || !source.recursive)) {
    return `${album.sources.length} 条筛选`;
  }
  const type = album.mediaTypeFilter === 'image' ? '照片' : album.mediaTypeFilter === 'video' ? '视频' : '全部';
  const orientation =
    album.orientationFilter === 'portrait' ? '竖屏' : album.orientationFilter === 'landscape' ? '横屏' : '全部方向';
  return `${type} · ${orientation}`;
}
