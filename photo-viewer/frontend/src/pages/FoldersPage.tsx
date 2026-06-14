import { useCallback, useEffect, useMemo, useState } from 'react';
import { Folder as FolderIcon } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { api } from '../api/client';
import { usePagedLoader } from '../hooks/usePagedLoader';
import type { Asset, Folder, SortKey } from '../types/api';
import { useSidebarPanel } from '../components/SidebarContext';

const pageSize = 100;

export default function FoldersPage() {
  const [tree, setTree] = useState<Folder[]>([]);
  const [children, setChildren] = useState<Folder[]>([]);
  const [currentId, setCurrentId] = useState(0);
  const [current, setCurrent] = useState<Folder | null>(null);
  const [sort, setSort] = useState<SortKey>('filename');
  const [query, setQuery] = useState('');
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);

  useEffect(() => {
    let live = true;
    async function load() {
      const [treeResult, childrenResult, folderResult] = await Promise.all([
        api.folderTree(),
        api.folders(currentId),
        api.folder(currentId),
      ]);
      if (!live) return;
      setTree(treeResult.items);
      setChildren(childrenResult.items);
      setCurrent(folderResult);
    }
    void load();
    return () => {
      live = false;
    };
  }, [currentId]);

  const loadAssets = useCallback(
    (page: number) => api.folderAssets(currentId, page, pageSize, sort, query),
    [currentId, query, sort],
  );
  const { items, hasMore, loading, error, loadMore } = usePagedLoader<Asset>(loadAssets, [currentId, sort, query]);
  const currentPath = current?.relPath || '根目录';
  const parent = useMemo(() => {
    if (!current?.parentRelPath) return null;
    return tree.find((item) => item.relPath === current.parentRelPath) ?? null;
  }, [current, tree]);
  useSidebarPanel(
    'folders',
    <div className="sidebar-control-stack">
      <div className="sidebar-control-title">文件夹</div>
      <div className="sidebar-meta">
        <strong>{currentPath}</strong>
        <span>{current?.recursiveAssetCount ?? 0} 个资源</span>
      </div>
      {parent && (
        <button className="sidebar-command" type="button" onClick={() => setCurrentId(parent.id)}>
          上一级
        </button>
      )}
      <label className="sidebar-field">
        <span>排序</span>
        <select value={sort} onChange={(event) => setSort(event.target.value as SortKey)}>
          <option value="filename">文件名</option>
          <option value="timeline_desc">时间新到旧</option>
          <option value="timeline_asc">时间旧到新</option>
          <option value="size">大小</option>
        </select>
      </label>
      <label className="sidebar-field">
        <span>搜索</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="当前文件夹" />
      </label>
      <div className="sidebar-folder-tree">
        {tree.map((folder) => (
          <button
            className={folder.id === currentId ? 'sidebar-folder-node active' : 'sidebar-folder-node'}
            key={folder.id}
            type="button"
            onClick={() => setCurrentId(folder.id)}
            style={{ paddingLeft: 8 + folder.depth * 12 }}
          >
            <FolderIcon size={14} />
            <span>{folder.name}</span>
            <small>{folder.recursiveAssetCount}</small>
          </button>
        ))}
      </div>
    </div>,
    [currentId, currentPath, current?.recursiveAssetCount, parent?.id, query, sort, tree],
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
        <EmptyState text={children.length > 0 ? '请选择左侧子文件夹' : '当前文件夹没有媒体'} />
      ) : (
        <>
          <AssetGrid
            assets={items}
            loading={loading}
            hasMore={hasMore}
            onLoadMore={loadMore}
            onPressPreviewChange={setPressPreviewAsset}
            buildViewerUrl={(asset) =>
              `/viewer/${asset.id}?context=folder&folderId=${currentId}&sort=${sort}&q=${encodeURIComponent(query)}`
            }
          />
          <PressPreviewOverlay asset={pressPreviewAsset} />
        </>
      )}
    </section>
  );
}
