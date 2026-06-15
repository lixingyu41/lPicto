import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { Check, ChevronRight, FolderPlus, Images, Pencil, Plus, RefreshCw, Trash2, X } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { useRestoreSidebarState, useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import type {
  Album,
  AlbumGroup,
  AlbumMediaFilter,
  AlbumOrientationFilter,
  AlbumSource,
  AlbumSourceInput,
  Asset,
  LibraryAnchor,
  SortKey,
  SourceFolder,
} from '../types/api';
import {
  decodeReturnState,
  encodeReturnState,
  loadPageState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { assetMatchesAlbum } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';

const pageSize = 100;
const albumsStateKey = 'albums';

type AlbumSortField = 'timeline' | 'imported' | 'size' | 'filename';
type AlbumSortDirection = 'asc' | 'desc';

interface AlbumsPageState extends GridReturnState {
  collapsedGroupKeys: string[];
  query: string;
  selectedId: number | null;
  sort: SortKey;
}

const defaultAlbumsState: AlbumsPageState = {
  ...resetGridState(),
  collapsedGroupKeys: [],
  query: '',
  selectedId: null,
  sort: 'timeline_desc',
};

export default function AlbumsPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const initialStateRef = useRef(
    decodeReturnState<AlbumsPageState>(searchParams.get('restore'), loadPageState<AlbumsPageState>(albumsStateKey, defaultAlbumsState)),
  );
  const [albums, setAlbums] = useState<Album[]>([]);
  const [groups, setGroups] = useState<AlbumGroup[]>([]);
  const [selectedId, setSelectedId] = useState<number | null>(initialStateRef.current.selectedId);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const albumSort = albumSortFromKey(sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [addOpen, setAddOpen] = useState(false);
  const [editingAlbum, setEditingAlbum] = useState<Album | null>(null);
  const [groupDraftOpen, setGroupDraftOpen] = useState(false);
  const [groupName, setGroupName] = useState('');
  const [collapsedGroupKeys, setCollapsedGroupKeys] = useState<Set<string>>(
    () => new Set(initialStateRef.current.collapsedGroupKeys),
  );
  const [error, setError] = useState<string | null>(null);
  const [scrollTopTarget, setScrollTopTarget] = useState<{ scrollTop: number; signal: number } | undefined>(() =>
    initialStateRef.current.scrollTop > 0 ? { scrollTop: initialStateRef.current.scrollTop, signal: 1 } : undefined,
  );
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [scrollTarget, setScrollTarget] = useState<{ ratio: number; signal: number } | undefined>();
  const [scrollResetSignal, setScrollResetSignal] = useState(0);
  const [scrollRatio, setScrollRatio] = useState(0);
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
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);

  const selectedAlbum = useMemo(
    () => albums.find((album) => album.id === selectedId) ?? albums[0] ?? null,
    [albums, selectedId],
  );
  const albumBuckets = useMemo(() => buildAlbumBuckets(albums, groups), [albums, groups]);

  const loadAlbums = useCallback(async () => {
    try {
      const result = await api.albums();
      setAlbums(result.items);
      setGroups(result.groups ?? []);
      setSelectedId((current) => (current && result.items.some((album) => album.id === current) ? current : result.items[0]?.id ?? null));
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
      return api.albumAssets(selectedAlbum.id, page, pageSize, sort, query);
    },
    [query, selectedAlbum, sort],
  );

  const { items, hasMore, loading, error: loadError, loadMore, reset, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    selectedAlbum?.id,
    sort,
    query,
  ]);

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const filtered = incoming.filter((asset) => assetMatchesAlbum(asset, selectedAlbum, query));
      if (filtered.length === 0) return;
      mutateItems((current) => mergeSortedAssets(current, filtered, sort, { hasMore, loadedStartIndex }));
    },
    [hasMore, loadedStartIndex, mutateItems, query, selectedAlbum, sort],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady]);

  useEffect(() => {
    if (eventsConnected || !selectedAlbum) return undefined;
    const timer = window.setInterval(() => {
      void api.albumAssets(selectedAlbum.id, 1, pageSize, sort, query).then((result) => mergeReadyAssets(result.items)).catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [eventsConnected, mergeReadyAssets, query, selectedAlbum, sort]);

  useEffect(() => {
    let live = true;
    async function loadAnchors(albumId: number) {
      try {
        const result = await api.albumAnchors(albumId, pageSize, sort, query);
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
  }, [query, selectedAlbum?.id, sort]);

  const currentPageState = useCallback(
    (): AlbumsPageState => ({
      ...gridStateRef.current,
      collapsedGroupKeys: Array.from(collapsedGroupKeys),
      loadedItemCount: items.length,
      loadedStartIndex,
      query,
      selectedId: selectedAlbum?.id ?? selectedId,
      sidebarCollapsed: sidebarState.sidebarCollapsed,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
    }),
    [collapsedGroupKeys, items.length, loadedStartIndex, query, selectedAlbum?.id, selectedId, sidebarState.sidebarCollapsed, sidebarState.sidebarExpanded, sort],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<AlbumsPageState>(albumsStateKey, currentPageState());
  }, [currentPageState]);

  useEffect(() => {
    indexPageRef.current = 1;
    setLoadedStartIndex(0);
    setScrollTarget(undefined);
    if (restoreRef.current.pending) return;
    gridStateRef.current = resetGridState();
    setScrollResetSignal((value) => value + 1);
  }, [query, selectedAlbum?.id, sort]);

  useEffect(() => {
    if (!restoreRef.current.pending || loading) return;
    const startIndex = Math.max(0, initialStateRef.current.loadedStartIndex);
    if (startIndex > 0 && !restoreRef.current.jumped) {
      restoreRef.current.jumped = true;
      const page = Math.floor(startIndex / pageSize) + 1;
      indexPageRef.current = page;
      setLoadedStartIndex(startIndex);
      void jumpToPage(page);
      return;
    }
    const targetCount = Math.max(pageSize, initialStateRef.current.loadedItemCount);
    if (items.length < targetCount && hasMore) {
      void loadMore();
      return;
    }
    restoreRef.current.pending = false;
    restoreRef.current.signal += 1;
    setScrollTopTarget({ scrollTop: initialStateRef.current.scrollTop, signal: restoreRef.current.signal });
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
    saveViewerReturnPath('/albums');
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

  const toggleAlbumGroup = useCallback((key: string) => {
    setCollapsedGroupKeys((value) => {
      const next = new Set(value);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

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
      setGroupName('');
      setGroupDraftOpen(false);
      setCollapsedGroupKeys((value) => {
        const next = new Set(value);
        next.delete(albumGroupKey(group.id));
        return next;
      });
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
        <div className="sidebar-control-title">相册</div>
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
        <div className="album-list">
          {albumBuckets.map((bucket) => {
            const collapsed = collapsedGroupKeys.has(bucket.key);
            return (
              <div className="album-group-block" key={bucket.key}>
                <button className="album-group-row" type="button" onClick={() => toggleAlbumGroup(bucket.key)}>
                  <span className={collapsed ? 'folder-expand-button' : 'folder-expand-button expanded'}>
                    <ChevronRight size={15} />
                  </span>
                  <span>{bucket.name}</span>
                  <small>{bucket.albums.length}</small>
                </button>
                {!collapsed &&
                  bucket.albums.map((album) => (
                    <button
                      className={selectedAlbum?.id === album.id ? 'album-row active' : 'album-row'}
                      key={album.id}
                      type="button"
                      onClick={() => setSelectedId(album.id)}
                    >
                      <Images size={15} />
                      <span>{album.name}</span>
                    </button>
                  ))}
              </div>
            );
          })}
          {albums.length === 0 && groups.length === 0 && <div className="muted-line">暂无相册</div>}
        </div>
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
          <label className="sidebar-field">
            <span>排序</span>
            <select
              value={albumSort.field}
              onChange={(event) => setSort(albumSortKey(event.target.value as AlbumSortField, albumSort.direction))}
            >
              <option value="timeline">时间</option>
              <option value="imported">导入时间</option>
              <option value="size">大小</option>
              <option value="filename">文件名</option>
            </select>
          </label>
          <label className="sidebar-field">
            <span>顺序</span>
            <select
              value={albumSort.direction}
              onChange={(event) => setSort(albumSortKey(albumSort.field, event.target.value as AlbumSortDirection))}
            >
              <option value="desc">倒序</option>
              <option value="asc">正序</option>
            </select>
          </label>
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
      albumBuckets,
      collapsedGroupKeys,
      groupDraftOpen,
      groupName,
      groups.length,
      query,
      albumSort.direction,
      albumSort.field,
      selectedAlbum?.id,
      selectedAlbum?.updatedAt,
      sort,
      toggleAlbumGroup,
    ],
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
            onLoadMore={loadMore}
            onOpenAsset={handleOpenAsset}
            onOpenViewer={handleOpenViewer}
            onAssetMissing={(asset) => mutateItems((current) => removeAssetById(current, asset.id))}
            onPressPreviewChange={setPressPreviewAsset}
            onScrollRatioChange={setScrollRatio}
            onScrollStateChange={handleGridScrollState}
            totalCount={totalCount}
            loadedStartIndex={loadedStartIndex}
            scrollSignal={scrollResetSignal}
            scrollTarget={scrollTarget}
            scrollTopTarget={scrollTopTarget}
            buildViewerUrl={(asset) =>
              `/viewer/${asset.id}?context=album&albumId=${selectedAlbum.id}&sort=${sort}&q=${encodeURIComponent(
                query,
              )}&returnPath=%2Falbums&returnState=${encodeReturnState(currentPageState())}`
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
      {addOpen && (
        <AlbumPickerModal
          groups={groups}
          onClose={() => setAddOpen(false)}
          onConfirm={(name, sources, groupId) => void createAlbum(name, sources, groupId)}
        />
      )}
      {editingAlbum && (
        <AlbumPickerModal
          groups={groups}
          initialAlbum={editingAlbum}
          onClose={() => setEditingAlbum(null)}
          onConfirm={(name, sources, groupId) => void updateAlbum(editingAlbum.id, name, sources, groupId)}
        />
      )}
    </section>
  );
}

function AlbumPickerModal({
  groups,
  initialAlbum,
  onClose,
  onConfirm,
}: {
  groups: AlbumGroup[];
  initialAlbum?: Album | null;
  onClose: () => void;
  onConfirm: (name: string, sources: AlbumSourceInput[], groupId: number | null) => void;
}) {
  const [children, setChildren] = useState<Record<string, SourceFolder[]>>({});
  const [rootFolder, setRootFolder] = useState<SourceFolder | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [albumName, setAlbumName] = useState(initialAlbum?.name ?? '');
  const [groupId, setGroupId] = useState<number | null>(initialAlbum?.groupId ?? null);
  const [mediaFilter, setMediaFilter] = useState<AlbumMediaFilter>('all');
  const [orientationFilter, setOrientationFilter] = useState<AlbumOrientationFilter>('all');
  const [recursive, setRecursive] = useState(true);
  const [sourceRules, setSourceRules] = useState<AlbumSourceInput[]>(() =>
    initialAlbum?.sources.map((source) => ({
      relPath: source.relPath,
      recursive: source.recursive,
      mediaTypeFilter: source.mediaTypeFilter,
      orientationFilter: source.orientationFilter,
    })) ?? [],
  );
  const [loading, setLoading] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const title = initialAlbum ? '编辑相册' : '添加相册';

  const loadChildren = useCallback(async (relPath: string) => {
    setLoading((prev) => new Set(prev).add(relPath));
    try {
      const result = await api.albumSourceFolders(relPath);
      if (relPath === '') setRootFolder(result.current);
      setChildren((prev) => ({ ...prev, [relPath]: result.items }));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取文件夹失败');
    } finally {
      setLoading((prev) => {
        const next = new Set(prev);
        next.delete(relPath);
        return next;
      });
    }
  }, []);

  useEffect(() => {
    void loadChildren('');
  }, [loadChildren]);

  function toggleExpanded(relPath: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(relPath)) next.delete(relPath);
      else next.add(relPath);
      return next;
    });
    if (!children[relPath]) void loadChildren(relPath);
  }

  function toggleSelected(relPath: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(relPath)) next.delete(relPath);
      else next.add(relPath);
      return next;
    });
  }

  const selectedPaths = Array.from(selected);
  const draftSources = selectedPaths.map((relPath) => ({
    relPath,
    recursive,
    mediaTypeFilter: mediaFilter,
    orientationFilter,
  }));
  const allSources = [...sourceRules, ...draftSources];
  const canFinish = albumName.trim().length > 0 && allSources.length > 0;

  function addSourceRules() {
    if (draftSources.length === 0) return;
    setSourceRules((prev) => [...prev, ...draftSources]);
    setSelected(new Set());
  }

  function removeSourceRule(index: number) {
    setSourceRules((prev) => prev.filter((_, itemIndex) => itemIndex !== index));
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <div className="folder-picker" role="dialog" aria-modal="true" aria-label={title}>
        <div className="modal-title">
          <span>{title}</span>
          <button type="button" onClick={onClose} title="关闭">
            <X size={17} />
          </button>
        </div>
        {error && <div className="error-line">{error}</div>}
        <div className="album-form-grid">
          <label className="settings-field">
            <span>名称</span>
            <input value={albumName} placeholder="例如：竖屏视频" onChange={(event) => setAlbumName(event.target.value)} />
          </label>
          <label className="settings-field">
            <span>分组</span>
            <select
              value={groupId ?? ''}
              onChange={(event) => setGroupId(event.target.value ? Number(event.target.value) : null)}
            >
              <option value="">未分组</option>
              {groups.map((group) => (
                <option key={group.id} value={group.id}>
                  {group.name}
                </option>
              ))}
            </select>
          </label>
          <label className="settings-field">
            <span>类型</span>
            <select value={mediaFilter} onChange={(event) => setMediaFilter(event.target.value as AlbumMediaFilter)}>
              <option value="all">全部</option>
              <option value="image">照片</option>
              <option value="video">视频</option>
            </select>
          </label>
          <label className="settings-field">
            <span>方向</span>
            <select
              value={orientationFilter}
              onChange={(event) => setOrientationFilter(event.target.value as AlbumOrientationFilter)}
            >
              <option value="all">全部</option>
              <option value="portrait">竖屏</option>
              <option value="landscape">横屏</option>
            </select>
          </label>
          <label className="settings-check-row">
            <input type="checkbox" checked={recursive} onChange={(event) => setRecursive(event.target.checked)} />
            <span>包含子文件夹</span>
          </label>
        </div>
        <div className="album-rule-toolbar">
          <button className="text-button" type="button" disabled={draftSources.length === 0} onClick={addSourceRules}>
            <Plus size={15} />
            加入筛选
          </button>
          <span>{sourceRules.length > 0 ? `已加入 ${sourceRules.length} 条筛选` : '可重复加入不同筛选'}</span>
        </div>
        {sourceRules.length > 0 && (
          <div className="album-rule-list">
            {sourceRules.map((source, index) => (
              <div className="album-rule-row" key={`${source.relPath}-${source.mediaTypeFilter}-${source.orientationFilter}-${source.recursive}-${index}`}>
                <span>{displayRelPath(source.relPath)} · {sourceFilterLabel(source)}</span>
                <button type="button" title="移除筛选" onClick={() => removeSourceRule(index)}>
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
          </div>
        )}
        <div className="folder-tree-picker">
          {rootFolder && (
            <AlbumFolderTreeNode
              childrenMap={children}
              expanded={expanded}
              folder={rootFolder}
              key={rootFolder.relPath || 'album-root'}
              loading={loading}
              selected={selected}
              onExpand={toggleExpanded}
              onSelect={toggleSelected}
            />
          )}
          {!rootFolder && loading.has('') && <div className="muted-line">读取中</div>}
          {rootFolder && children['']?.length === 0 && !rootFolder.included && (
            <div className="muted-line">没有可选的来源文件夹</div>
          )}
        </div>
        <div className="modal-actions">
          <span>{allSources.length > 0 ? `${allSources.length} 条筛选` : '未选择文件夹'}</span>
          <button className="text-button" type="button" onClick={onClose}>
            取消
          </button>
          <button
            className="command-button"
            type="button"
            disabled={!canFinish}
            onClick={() => onConfirm(albumName.trim(), allSources, groupId)}
          >
            <Check size={16} />
            {initialAlbum ? '保存' : '完成'}
          </button>
        </div>
      </div>
    </div>
  );
}

function AlbumFolderTreeNode({
  folder,
  childrenMap,
  expanded,
  loading,
  selected,
  onExpand,
  onSelect,
}: {
  folder: SourceFolder;
  childrenMap: Record<string, SourceFolder[]>;
  expanded: Set<string>;
  loading: Set<string>;
  selected: Set<string>;
  onExpand: (relPath: string) => void;
  onSelect: (relPath: string) => void;
}) {
  const isExpanded = expanded.has(folder.relPath);
  const children = childrenMap[folder.relPath] ?? [];
  const checkboxDisabled = !folder.included;
  const checked = selected.has(folder.relPath);
  const note = !folder.included
    ? '展开'
    : folder.selected
        ? '来源'
        : loading.has(folder.relPath)
          ? '读取中'
          : '';
  return (
    <div className="picker-node-group">
      <div className="picker-node" style={{ paddingLeft: 10 + Math.max(0, folder.depth - 1) * 18 }}>
        <button className={isExpanded ? 'expand-button expanded' : 'expand-button'} type="button" onClick={() => onExpand(folder.relPath)}>
          <ChevronRight size={15} />
        </button>
        <label>
          <input type="checkbox" checked={checked} disabled={checkboxDisabled} onChange={() => onSelect(folder.relPath)} />
          <span>{folder.relPath ? folder.name : displayRelPath(folder.relPath)}</span>
        </label>
        <small>{note}</small>
      </div>
      {isExpanded &&
        children.map((child) => (
          <AlbumFolderTreeNode
            childrenMap={childrenMap}
            expanded={expanded}
            folder={child}
            key={child.relPath}
            loading={loading}
            selected={selected}
            onExpand={onExpand}
            onSelect={onSelect}
          />
        ))}
    </div>
  );
}

interface AlbumBucket {
  key: string;
  name: string;
  albums: Album[];
}

function buildAlbumBuckets(albums: Album[], groups: AlbumGroup[]): AlbumBucket[] {
  const byGroup = new Map<number | null, Album[]>();
  albums.forEach((album) => {
    const key = album.groupId ?? null;
    const items = byGroup.get(key) ?? [];
    items.push(album);
    byGroup.set(key, items);
  });
  const buckets = groups.map((group) => ({
    key: albumGroupKey(group.id),
    name: group.name,
    albums: byGroup.get(group.id) ?? [],
  }));
  const knownGroupIds = new Set(groups.map((group) => group.id));
  const orphanGroupIds = Array.from(
    new Set(albums.map((album) => album.groupId).filter((id): id is number => id !== null && !knownGroupIds.has(id))),
  );
  orphanGroupIds.forEach((id) => {
    buckets.push({ key: albumGroupKey(id), name: '未命名组', albums: byGroup.get(id) ?? [] });
  });
  const ungrouped = byGroup.get(null) ?? [];
  if (ungrouped.length > 0 || groups.length === 0) {
    buckets.push({ key: albumGroupKey(null), name: '未分组', albums: ungrouped });
  }
  return buckets;
}

function albumGroupKey(groupId: number | null) {
  return groupId === null ? 'ungrouped' : `group-${groupId}`;
}

function albumSortFromKey(sort: SortKey): { field: AlbumSortField; direction: AlbumSortDirection } {
  switch (sort) {
    case 'timeline_asc':
      return { field: 'timeline', direction: 'asc' };
    case 'imported_asc':
      return { field: 'imported', direction: 'asc' };
    case 'imported_desc':
      return { field: 'imported', direction: 'desc' };
    case 'filename':
    case 'filename_asc':
      return { field: 'filename', direction: 'asc' };
    case 'filename_desc':
      return { field: 'filename', direction: 'desc' };
    case 'size_asc':
      return { field: 'size', direction: 'asc' };
    case 'size':
    case 'size_desc':
      return { field: 'size', direction: 'desc' };
    default:
      return { field: 'timeline', direction: 'desc' };
  }
}

function albumSortKey(field: AlbumSortField, direction: AlbumSortDirection): SortKey {
  if (field === 'timeline') return direction === 'asc' ? 'timeline_asc' : 'timeline_desc';
  if (field === 'imported') return direction === 'asc' ? 'imported_asc' : 'imported_desc';
  if (field === 'filename') return direction === 'asc' ? 'filename_asc' : 'filename_desc';
  return direction === 'asc' ? 'size_asc' : 'size_desc';
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

function sourceFilterLabel(source: Pick<AlbumSource, 'mediaTypeFilter' | 'orientationFilter' | 'recursive'>) {
  const type = source.mediaTypeFilter === 'image' ? '照片' : source.mediaTypeFilter === 'video' ? '视频' : '全部';
  const orientation =
    source.orientationFilter === 'portrait' ? '竖屏' : source.orientationFilter === 'landscape' ? '横屏' : '全部方向';
  return `${type} · ${orientation} · ${source.recursive ? '含子文件夹' : '仅本层'}`;
}

function displayRelPath(relPath: string) {
  return relPath ? `/${relPath}` : '全部存储';
}
