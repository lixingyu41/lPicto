import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { Check, FolderInput, FolderPlus, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react';
import AlbumEditor from '../components/AlbumEditor';
import AssetGrid from '../components/AssetGrid';
import { CompactAssetGroupingControls, normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarAlbumList, SidebarFilterIconRow, SidebarMediaTypeList, SidebarOrientationFilter, SidebarRatingFilter } from '../components/SidebarControls';
import { CompactSortControls, isSortKey } from '../components/SortControls';
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
  AssetRatingFilter,
  LibraryAnchor,
  OrientationFilter,
  SortKey,
} from '../types/api';
import {
  appendViewerReturnParams,
  decodeReturnState,
  loadPageState,
  requestViewerOverlayClose,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
  useViewerAwareMediaState,
  viewerOverlayCloseCompleted,
} from '../utils/pageState';
import { parseAssetGroupMode, serverGroupForMode, type AssetGroupMode } from '../utils/assetGrouping';
import { assetMatchesAlbum } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';
import { assetRatingParam, currentURLHasParam, currentURLLocation, currentURLPath, orientationParam, positiveIntParam, replaceURLState } from '../utils/urlState';
import { waterfallPageSize } from '../utils/waterfallPaging';
import { parseTagFilters, serializeTagFilters } from '../utils/tagFilters';

const pageSize = waterfallPageSize;
const albumsStateKey = 'albums';
const albumsURLKeys = ['albumId', 'album', 'type', 'rating', 'orientation', 'sort', 'group', 'q', 'combinedTags', 'tagNodes'];
const pendingAlbumEditorKey = 'lpicto:pending-album-editor';
const assetKinds: AssetKind[] = ['all', 'image', 'video', 'audio'];

type PendingAlbumEditor = { mode: 'add' } | { mode: 'edit'; albumId: number };

interface AlbumsPageState extends GridReturnState {
  collapsedGroupKeys: string[];
  groupMode: AssetGroupMode;
  orientation: OrientationFilter;
  query: string;
  rating: AssetRatingFilter;
  selectedId: number | null;
  sort: SortKey;
  tagFilters: string[];
  type: AssetKind;
}

const defaultAlbumsState: AlbumsPageState = {
  ...resetGridState(),
  collapsedGroupKeys: [],
  groupMode: 'none',
  orientation: 'all',
  query: '',
  rating: 'all',
  selectedId: null,
  sort: 'timeline_desc',
  tagFilters: [],
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
  const [type, setType] = useViewerAwareMediaState<AssetKind>(initialStateRef.current.type);
  const [selectedId, setSelectedId] = useViewerAwareMediaState<number | null>(initialStateRef.current.selectedId);
  const [sort, setSort] = useViewerAwareMediaState<SortKey>(initialStateRef.current.sort);
  const [groupMode, setGroupMode] = useViewerAwareMediaState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [query, setQuery] = useViewerAwareMediaState(initialStateRef.current.query);
  const [rating, setRating] = useViewerAwareMediaState<AssetRatingFilter>(initialStateRef.current.rating ?? 'all');
  const [orientation, setOrientation] = useViewerAwareMediaState<OrientationFilter>(initialStateRef.current.orientation);
  useEffect(() => {
    if (type === 'audio') setOrientation('all');
  }, [type]);
  const [tagFilters, setTagFilters] = useViewerAwareMediaState(initialStateRef.current.tagFilters ?? []);
  const [addOpen, setAddOpen] = useState(false);
  const [editingAlbum, setEditingAlbum] = useState<Album | null>(null);
  const [groupDraftOpen, setGroupDraftOpen] = useState(false);
  const [groupName, setGroupName] = useState('');
  const [moveGroupOpen, setMoveGroupOpen] = useState(false);
  const [moveGroupId, setMoveGroupId] = useState('');
  const [collapsedGroupKeys, setCollapsedGroupKeys] = useState<Set<string>>(() => new Set(initialStateRef.current.collapsedGroupKeys));
  const [error, setError] = useState<string | null>(null);
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const sidebarState = useSidebarReturnState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const serverGroup = serverGroupForMode(groupMode);
  const activeRating = rating === 'all' ? undefined : rating;

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
      return api.albumAssets(selectedAlbum.id, page, pageSize, sort, query, serverGroup, activeRating, orientation, type, undefined, serializeTagFilters(tagFilters));
    },
    [activeRating, orientation, query, selectedAlbum, serverGroup, sort, tagFilters, type],
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
      const filtered = incoming.filter((asset) => assetMatchesAlbum(asset, selectedAlbum, query, activeRating, orientation, type));
      if (filtered.length === 0) return;
      mutateItems((current) => mergeSortedAssets(current, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [activeRating, groupMode, hasMore, loadedStartIndex, mutateItems, orientation, query, selectedAlbum, sort, type],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((current) => removeAssetById(current, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected || !selectedAlbum) return undefined;
    const timer = window.setInterval(() => {
      void api
        .albumAssets(selectedAlbum.id, 1, pageSize, sort, query, serverGroup, activeRating, orientation, type)
        .then((result) => mergeReadyAssets(result.items))
        .catch(() => undefined);
    }, 30000);
    return () => window.clearInterval(timer);
  }, [activeRating, eventsConnected, mergeReadyAssets, orientation, query, selectedAlbum, serverGroup, sort, type]);

  useEffect(() => {
    let live = true;
    async function loadAnchors(albumId: number) {
      try {
        const result = await api.albumAnchors(albumId, pageSize, sort, query, serverGroup, activeRating, orientation, type, undefined, serializeTagFilters(tagFilters));
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
  }, [activeRating, orientation, query, selectedAlbum?.id, serverGroup, sort, tagFilters, type]);

  const currentPageState = useCallback(
    (): AlbumsPageState => ({
      ...getGridState(),
      collapsedGroupKeys: Array.from(collapsedGroupKeys),
      groupMode,
      orientation,
      query,
      rating,
      selectedId: selectedAlbum?.id ?? selectedId,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
      tagFilters,
      type,
    }),
    [collapsedGroupKeys, getGridState, groupMode, orientation, query, rating, selectedAlbum?.id, selectedId, sidebarState.sidebarExpanded, sort, tagFilters, type],
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
        rating: activeRating,
        sort,
        tagNodes: serializeTagFilters(tagFilters),
        type,
      },
      albumsURLKeys,
    );
  }, [groupMode, location, navigate, orientation, query, rating, searchParams, selectedAlbum, sort, tagFilters, type]);
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

  const handleRatingChange = useCallback((nextRating: AssetRatingFilter) => {
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

  const queueAlbumEditorAfterViewerClose = useCallback((pending: PendingAlbumEditor) => {
    const saved = savePendingAlbumEditor(pending);
    if (requestViewerOverlayClose()) return saved;
    clearPendingAlbumEditor();
    return false;
  }, []);

  const handleAddAlbum = useCallback(() => {
    if (queueAlbumEditorAfterViewerClose({ mode: 'add' })) return;
    setEditingAlbum(null);
    setAddOpen(true);
  }, [queueAlbumEditorAfterViewerClose]);

  const handleEditAlbum = useCallback(
    (album: Album) => {
      if (queueAlbumEditorAfterViewerClose({ mode: 'edit', albumId: album.id })) return;
      setAddOpen(false);
      setEditingAlbum(album);
    },
    [queueAlbumEditorAfterViewerClose],
  );

  const applyPendingAlbumEditor = useCallback(() => {
    const pending = readPendingAlbumEditor();
    if (!pending) return;
    if (pending.mode === 'add') {
      clearPendingAlbumEditor();
      setEditingAlbum(null);
      setAddOpen(true);
      return;
    }
    if (albums.length === 0) return;
    clearPendingAlbumEditor();
    const album = albums.find((item) => item.id === pending.albumId);
    if (album) {
      setAddOpen(false);
      setEditingAlbum(album);
    }
  }, [albums]);

  useEffect(() => {
    applyPendingAlbumEditor();
  }, [applyPendingAlbumEditor]);

  useEffect(() => {
    window.addEventListener(viewerOverlayCloseCompleted, applyPendingAlbumEditor);
    return () => window.removeEventListener(viewerOverlayCloseCompleted, applyPendingAlbumEditor);
  }, [applyPendingAlbumEditor]);

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

  async function moveSelectedAlbumToGroup() {
    if (!selectedAlbum) return;
    const groupId = moveGroupId === '' ? null : Number(moveGroupId);
    if (groupId !== null && (!Number.isInteger(groupId) || !groups.some((group) => group.id === groupId))) {
      setError('相册组无效');
      return;
    }
    try {
      await api.updateAlbum(
        selectedAlbum.id,
        selectedAlbum.name,
        selectedAlbum.sources.map((source) => ({
          relPath: source.relPath,
          recursive: source.recursive,
          mediaTypeFilter: source.mediaTypeFilter,
          orientationFilter: source.orientationFilter,
        })),
        groupId,
      );
      setMoveGroupOpen(false);
      await loadAlbums();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '移动相册失败');
    }
  }

  useSidebarPanel(
    'albums',
    <div className="sidebar-control-stack">
      {selectedAlbum && (
        <SidebarFilterIconRow>
          <SidebarMediaTypeList value={type} onChange={setType} />
          <SidebarOrientationFilter value={orientation} onChange={setOrientation} />
          <SidebarRatingFilter value={rating} onChange={handleRatingChange} />
          <CompactAssetGroupingControls groupMode={groupMode} sort={sort} onChange={setGroupMode} />
          <CompactSortControls sort={sort} onChange={setSort} />
        </SidebarFilterIconRow>
      )}
      <SidebarAlbumList
        albums={albums}
        collapsedGroupKeys={Array.from(collapsedGroupKeys)}
        forceGroupHeaders
        groups={groups}
        selectedIds={selectedAlbum ? [selectedAlbum.id] : []}
        showEmptyGroups
        showLabel={false}
        onSelectAlbum={(album) => setSelectedId(album.id)}
        onToggleGroup={handleToggleAlbumGroup}
      />
      <div className="album-toolbar sidebar-filter-icon-row">
        <button className="sidebar-compact-trigger" type="button" title="新建相册" onClick={handleAddAlbum}>
          <Plus size={18} />
        </button>
        <button className="sidebar-compact-trigger" type="button" title="新建组" onClick={() => {
          setMoveGroupOpen(false);
          setGroupDraftOpen((value) => !value);
        }}>
          <FolderPlus size={18} />
        </button>
        <button className="sidebar-compact-trigger" type="button" title="编辑相册" disabled={!selectedAlbum} onClick={() => selectedAlbum && handleEditAlbum(selectedAlbum)}>
          <Pencil size={18} />
        </button>
        <button
          className="sidebar-compact-trigger"
          type="button"
          title="刷新相册"
          disabled={!selectedAlbum}
          onClick={() => {
            if (selectedAlbum) void refreshAlbum(selectedAlbum.id);
          }}
        >
          <RefreshCw size={18} />
        </button>
        <button
          className="sidebar-compact-trigger"
          type="button"
          title="删除相册"
          disabled={!selectedAlbum}
          onClick={() => {
            if (selectedAlbum) void deleteAlbum(selectedAlbum.id);
          }}
        >
          <Trash2 size={18} />
        </button>
        <button
          className="sidebar-compact-trigger"
          type="button"
          title="放到组"
          disabled={!selectedAlbum}
          onClick={() => {
            if (!selectedAlbum) return;
            setGroupDraftOpen(false);
            setMoveGroupId(selectedAlbum.groupId === null ? '' : String(selectedAlbum.groupId));
            setMoveGroupOpen((value) => !value);
          }}
        >
          <FolderInput size={18} />
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
      {moveGroupOpen && selectedAlbum && (
        <div className="album-group-create">
          <select aria-label="选择相册组" value={moveGroupId} onChange={(event) => setMoveGroupId(event.target.value)}>
            <option value="">未分组</option>
            {groups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}
          </select>
          <button type="button" title="确认放到组" onClick={() => void moveSelectedAlbumToGroup()}>
            <Check size={15} />
          </button>
        </div>
      )}
      {selectedAlbum && (
        <label className="sidebar-field">
          <span>搜索</span>
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名" />
        </label>
      )}
    </div>,
    [
      albums,
      collapsedGroupKeys,
      groups,
      groupDraftOpen,
      groupName,
      groupMode,
      handleAddAlbum,
      handleEditAlbum,
      handleRatingChange,
      handleToggleAlbumGroup,
      orientation,
      query,
      rating,
      moveGroupId,
      moveGroupOpen,
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
            onBatchRemoveAssets={(ids) => mutateItems((current) => current.filter((asset) => !ids.includes(asset.id)))}
            onPressPreviewChange={setPressPreviewAsset}
            onScrollRatioChange={setScrollRatio}
            onScrollStateChange={handlePersistentGridScrollState}
            totalCount={totalCount}
            loadedStartIndex={loadedStartIndex}
            focusAssetId={focusAssetId}
            groupMode={groupMode}
            sort={sort}
            onSortChange={setSort}
            selectedTags={tagFilters}
            onTagFilterChange={setTagFilters}
            scrollSignal={scrollResetSignal}
            scrollTarget={scrollTarget}
            scrollTopTarget={scrollTopTarget}
            buildViewerUrl={(asset) =>
              appendViewerReturnParams(
                `/viewer/${asset.id}?context=album&albumId=${selectedAlbum.id}&type=${type}&sort=${sort}&q=${encodeURIComponent(query)}${serializeTagFilters(tagFilters) ? `&tagNodes=${encodeURIComponent(serializeTagFilters(tagFilters)!)}` : ''}${ratingViewerParam(
                  rating,
                )}${orientationViewerParam(orientation)}${serverGroup ? `&group=${serverGroup}` : ''}`,
                currentPageReturnPath(),
                currentPageState(),
              )
            }
          />
          <LibraryIndexRail
            anchors={anchors}
            hideLabels={groupMode === 'folder'}
            scrollRatio={scrollRatio}
            totalCount={totalCount}
            pageSize={pageSize}
            onSeek={seekIndex}
          />
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

function savePendingAlbumEditor(pending: PendingAlbumEditor) {
  if (typeof window === 'undefined') return false;
  try {
    window.sessionStorage.setItem(pendingAlbumEditorKey, JSON.stringify(pending));
    return true;
  } catch {
    return false;
  }
}

function readPendingAlbumEditor(): PendingAlbumEditor | null {
  if (typeof window === 'undefined') return null;
  try {
    const raw = window.sessionStorage.getItem(pendingAlbumEditorKey);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<PendingAlbumEditor> | null;
    if (!parsed || typeof parsed !== 'object') return null;
    if (parsed.mode === 'add') return { mode: 'add' };
    const albumId = Number((parsed as { albumId?: unknown }).albumId);
    if (parsed.mode === 'edit' && Number.isFinite(albumId) && albumId > 0) {
      return { mode: 'edit', albumId };
    }
    return null;
  } catch {
    return null;
  }
}

function clearPendingAlbumEditor() {
  if (typeof window === 'undefined') return;
  try {
    window.sessionStorage.removeItem(pendingAlbumEditorKey);
  } catch {
    // Ignore storage failures.
  }
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
    tagFilters: params.has('tagNodes') || params.has('combinedTags') ? parseTagFilters(params.get('tagNodes') ?? params.get('combinedTags')) : base.tagFilters ?? [],
    type: assetKinds.includes(type as AssetKind) ? (type as AssetKind) : base.type,
  };
}

function ratingViewerParam(rating: AssetRatingFilter) {
  return rating === 'all' ? '' : `&rating=${rating}`;
}

function orientationViewerParam(orientation: OrientationFilter) {
  return orientation === 'all' ? '' : `&orientation=${orientation}`;
}
