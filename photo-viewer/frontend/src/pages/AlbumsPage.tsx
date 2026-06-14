import { useCallback, useEffect, useMemo, useState } from 'react';
import { Check, ChevronRight, Images, Plus, RefreshCw, Trash2, X } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { useSidebarPanel } from '../components/SidebarContext';
import { api } from '../api/client';
import { usePagedLoader } from '../hooks/usePagedLoader';
import type {
  Album,
  AlbumMediaFilter,
  AlbumOrientationFilter,
  AlbumSource,
  AlbumSourceInput,
  Asset,
  SortKey,
  SourceFolder,
} from '../types/api';

const pageSize = 100;

export default function AlbumsPage() {
  const [albums, setAlbums] = useState<Album[]>([]);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [sort, setSort] = useState<SortKey>('timeline_desc');
  const [query, setQuery] = useState('');
  const [addOpen, setAddOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);

  const selectedAlbum = useMemo(
    () => albums.find((album) => album.id === selectedId) ?? albums[0] ?? null,
    [albums, selectedId],
  );

  const loadAlbums = useCallback(async () => {
    try {
      const result = await api.albums();
      setAlbums(result.items);
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

  const { items, hasMore, loading, error: loadError, loadMore, reset } = usePagedLoader<Asset>(loadAssets, [
    selectedAlbum?.id,
    sort,
    query,
  ]);

  async function createAlbum(
    name: string,
    sources: AlbumSourceInput[],
  ) {
    try {
      const album = await api.createAlbum(name, sources);
      setAddOpen(false);
      await loadAlbums();
      setSelectedId(album.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建相册失败');
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
      <div className="album-list">
        {albums.map((album) => (
          <button
            className={selectedAlbum?.id === album.id ? 'album-row active' : 'album-row'}
            key={album.id}
            type="button"
            onClick={() => setSelectedId(album.id)}
          >
            <Images size={15} />
            <span>{album.name}</span>
            <small>{album.assetCount}</small>
          </button>
        ))}
        {albums.length === 0 && <div className="muted-line">暂无相册</div>}
      </div>
      <button className="sidebar-command" type="button" onClick={() => setAddOpen(true)}>
        <Plus size={16} />
        添加相册
      </button>
      {selectedAlbum && (
        <>
          <div className="sidebar-icon-actions">
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
          <div className="library-paths">
            {selectedAlbum.sources.map((source) => (
              <span key={source.id}>{displayRelPath(source.relPath)} · {sourceFilterLabel(source)}</span>
            ))}
          </div>
        </>
      )}
    </div>,
    [albums, query, selectedAlbum?.id, selectedAlbum?.assetCount, selectedAlbum?.updatedAt, sort],
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
        <>
          <AssetGrid
            assets={items}
            loading={loading}
            hasMore={hasMore}
            onLoadMore={loadMore}
            onPressPreviewChange={setPressPreviewAsset}
            buildViewerUrl={(asset) =>
              `/viewer/${asset.id}?context=album&albumId=${selectedAlbum.id}&sort=${sort}&q=${encodeURIComponent(query)}`
            }
          />
          <PressPreviewOverlay asset={pressPreviewAsset} />
        </>
      )}
      {addOpen && (
        <AlbumPickerModal
          onClose={() => setAddOpen(false)}
          onConfirm={(name, sources) => void createAlbum(name, sources)}
        />
      )}
    </section>
  );
}

function AlbumPickerModal({
  onClose,
  onConfirm,
}: {
  onClose: () => void;
  onConfirm: (name: string, sources: AlbumSourceInput[]) => void;
}) {
  const [children, setChildren] = useState<Record<string, SourceFolder[]>>({});
  const [rootFolder, setRootFolder] = useState<SourceFolder | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [albumName, setAlbumName] = useState('');
  const [mediaFilter, setMediaFilter] = useState<AlbumMediaFilter>('all');
  const [orientationFilter, setOrientationFilter] = useState<AlbumOrientationFilter>('all');
  const [recursive, setRecursive] = useState(true);
  const [sourceRules, setSourceRules] = useState<AlbumSourceInput[]>([]);
  const [loading, setLoading] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);

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
      <div className="folder-picker" role="dialog" aria-modal="true" aria-label="添加相册">
        <div className="modal-title">
          <span>添加相册</span>
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
            <div className="muted-line">没有可选的 LIB 文件夹</div>
          )}
        </div>
        <div className="modal-actions">
          <span>{allSources.length > 0 ? `将创建 ${allSources.length} 条筛选` : '未选择文件夹'}</span>
          <button className="text-button" type="button" onClick={onClose}>
            取消
          </button>
          <button
            className="command-button"
            type="button"
            disabled={!canFinish}
            onClick={() => onConfirm(albumName.trim(), allSources)}
          >
            <Check size={16} />
            完成
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
        ? 'LIB'
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
  return relPath ? `/${relPath}` : '/photos';
}
