import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { ChevronRight, Folder as FolderIcon, FolderTree } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import type { Asset, AssetDeletedEvent, Folder, LibraryAnchor, SortKey } from '../types/api';
import { useRestoreSidebarState, useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import {
  clearRestoreParamFromLocation,
  decodeReturnState,
  encodeReturnState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { assetMatchesFolder } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';

const pageSize = 100;
const foldersStateKey = 'folders';

interface FoldersPageState extends GridReturnState {
  currentId: number;
  expandedRelPaths: string[];
  includeSubfolders: boolean;
  query: string;
  sort: SortKey;
}

const defaultFoldersState: FoldersPageState = {
  ...resetGridState(),
  currentId: 0,
  expandedRelPaths: [''],
  includeSubfolders: true,
  query: '',
  sort: 'filename',
};

export default function FoldersPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const initialStateRef = useRef(
    decodeReturnState<FoldersPageState>(searchParams.get('restore'), defaultFoldersState),
  );
  const [tree, setTree] = useState<Folder[]>([]);
  const [currentId, setCurrentId] = useState(initialStateRef.current.currentId);
  const [current, setCurrent] = useState<Folder | null>(null);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [folderSearchFocused, setFolderSearchFocused] = useState(false);
  const [includeSubfolders, setIncludeSubfolders] = useState(initialStateRef.current.includeSubfolders);
  const [expandedRelPaths, setExpandedRelPaths] = useState<Set<string>>(() => new Set(initialStateRef.current.expandedRelPaths));
  const [scrollTopTarget, setScrollTopTarget] = useState<{ scrollTop: number; signal: number } | undefined>(() =>
    initialStateRef.current.scrollTop > 0 && !initialStateRef.current.focusAssetId
      ? { scrollTop: initialStateRef.current.scrollTop, signal: 1 }
      : undefined,
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
    pending:
      initialStateRef.current.scrollTop > 0 ||
      initialStateRef.current.loadedItemCount > pageSize ||
      initialStateRef.current.loadedStartIndex > 0 ||
      Boolean(initialStateRef.current.focusAssetId),
    signal: 0,
  });
  const indexPageRef = useRef(1);
  const seekSignalRef = useRef(0);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);

  useEffect(() => {
    let live = true;
    async function load() {
      const [treeResult, folderResult] = await Promise.all([api.folderTree(), api.folder(currentId)]);
      if (!live) return;
      setTree(treeResult.items);
      setCurrent(folderResult);
    }
    void load();
    return () => {
      live = false;
    };
  }, [currentId]);

  const childrenByParent = useMemo(() => buildFolderChildren(tree), [tree]);

  useEffect(() => {
    if (!current) return;
    setExpandedRelPaths((value) => mergeExpandedAncestors(value, current.relPath));
  }, [current?.relPath]);

  const visibleTree = useMemo(
    () => flattenVisibleFolders(tree, childrenByParent, expandedRelPaths),
    [childrenByParent, expandedRelPaths, tree],
  );

  const loadAssets = useCallback(
    (page: number) => api.folderAssets(currentId, page, pageSize, sort, query, includeSubfolders),
    [currentId, includeSubfolders, query, sort],
  );
  const { items, hasMore, loading, error, loadMore, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    currentId,
    includeSubfolders,
    sort,
    query,
  ]);

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const folderRelPath = current?.relPath ?? '';
      const filtered = incoming.filter((asset) => assetMatchesFolder(asset, folderRelPath, includeSubfolders, query));
      if (filtered.length === 0) return;
      mutateItems((value) => mergeSortedAssets(value, filtered, sort, { hasMore, loadedStartIndex }));
    },
    [current?.relPath, hasMore, includeSubfolders, loadedStartIndex, mutateItems, query, sort],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((value) => removeAssetById(value, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected || !current) return undefined;
    const timer = window.setInterval(() => {
      void api.folderAssets(currentId, 1, pageSize, sort, query, includeSubfolders).then((result) => mergeReadyAssets(result.items)).catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [current, currentId, eventsConnected, includeSubfolders, mergeReadyAssets, query, sort]);

  const currentPageState = useCallback(
    (): FoldersPageState => ({
      ...gridStateRef.current,
      currentId,
      expandedRelPaths: Array.from(expandedRelPaths),
      focusAssetId: null,
      includeSubfolders,
      loadedItemCount: items.length,
      loadedStartIndex,
      query,
      sidebarCollapsed: sidebarState.sidebarCollapsed,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
    }),
    [currentId, expandedRelPaths, includeSubfolders, items.length, loadedStartIndex, query, sidebarState.sidebarCollapsed, sidebarState.sidebarExpanded, sort],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<FoldersPageState>(foldersStateKey, currentPageState());
  }, [currentPageState]);

  useEffect(() => {
    if (!searchParams.has('restore')) return;
    clearRestoreParamFromLocation();
  }, [searchParams]);

  useEffect(() => {
    indexPageRef.current = 1;
    setLoadedStartIndex(0);
    setScrollTarget(undefined);
    if (restoreRef.current.pending) return;
    gridStateRef.current = resetGridState();
    setScrollResetSignal((value) => value + 1);
  }, [currentId, includeSubfolders, query, sort]);

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
    if (!initialStateRef.current.focusAssetId) {
      restoreRef.current.signal += 1;
      setScrollTopTarget({ scrollTop: initialStateRef.current.scrollTop, signal: restoreRef.current.signal });
    }
  }, [hasMore, items.length, jumpToPage, loadMore, loading]);

  useEffect(() => {
    let live = true;
    async function loadAnchors() {
      try {
        const result = await api.folderAnchors(currentId, pageSize, sort, query, includeSubfolders);
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
  }, [currentId, includeSubfolders, query, sort]);

  const handleGridScrollState = useCallback(
    (state: { ratio: number; scrollTop: number }) => {
      gridStateRef.current = {
        ...gridStateRef.current,
        focusAssetId: null,
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
    saveViewerReturnPath('/folders');
  }, [saveCurrentState]);

  const handleOpenViewer = useCallback(
    (asset: Asset, viewerUrl: string) => {
      navigate(viewerUrl, { state: { backgroundLocation: location, initialAsset: asset } });
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

  const hasCurrentChildren = current ? (childrenByParent.get(current.relPath)?.length ?? 0) > 0 : false;

  const toggleFolder = useCallback((relPath: string) => {
    setExpandedRelPaths((value) => {
      const next = new Set(value);
      if (next.has(relPath)) next.delete(relPath);
      else next.add(relPath);
      return next;
    });
  }, []);

  const selectFolder = useCallback((folder: Folder) => {
    setCurrentId(folderID(folder));
  }, []);

  useSidebarPanel(
    'folders',
    <div className="sidebar-control-stack sidebar-folder-panel">
      <label className="sidebar-field">
        <span>排序</span>
        <select value={sort} onChange={(event) => setSort(event.target.value as SortKey)}>
          <option value="filename">文件名</option>
          <option value="timeline_desc">时间新到旧</option>
          <option value="timeline_asc">时间旧到新</option>
          <option value="size">大小</option>
        </select>
      </label>
      <div className={folderSearchFocused ? 'sidebar-folder-search-row search-focused' : 'sidebar-folder-search-row'}>
        <label className="sidebar-field sidebar-folder-search-field">
          <span>搜索</span>
          <input
            value={query}
            onBlur={() => setFolderSearchFocused(false)}
            onChange={(event) => setQuery(event.target.value)}
            onFocus={() => setFolderSearchFocused(true)}
            placeholder="当前文件夹"
          />
        </label>
        {!folderSearchFocused && (
          <button
            aria-label={includeSubfolders ? '隐藏子文件夹图' : '显示子文件夹图'}
            aria-pressed={includeSubfolders}
            className={includeSubfolders ? 'sidebar-square-button sidebar-folder-recursive-button active' : 'sidebar-square-button sidebar-folder-recursive-button'}
            title={includeSubfolders ? '隐藏子文件夹图' : '显示子文件夹图'}
            type="button"
            onClick={() => setIncludeSubfolders((value) => !value)}
          >
            <FolderTree size={16} />
          </button>
        )}
      </div>
      <div className="sidebar-folder-tree">
        {visibleTree.map((folder) => {
          const hasChildren = (childrenByParent.get(folder.relPath)?.length ?? 0) > 0;
          const expanded = expandedRelPaths.has(folder.relPath);
          const active = folderID(folder) === currentId;
          const count = includeSubfolders ? folder.recursiveAssetCount : folder.assetCount;
          return (
            <div
              className={active ? 'sidebar-folder-node active' : 'sidebar-folder-node'}
              key={folder.id}
              style={{ paddingLeft: 4 + folder.depth * 12 }}
            >
              <button
                aria-label={expanded ? '收起文件夹' : '展开文件夹'}
                className={expanded ? 'folder-expand-button expanded' : 'folder-expand-button'}
                disabled={!hasChildren}
                title={expanded ? '收起' : '展开'}
                type="button"
                onClick={() => toggleFolder(folder.relPath)}
              >
                {hasChildren && <ChevronRight size={15} />}
              </button>
              <button className="sidebar-folder-node-main" type="button" onClick={() => selectFolder(folder)}>
                <FolderIcon size={14} />
                <span>{folder.name}</span>
                <small>{count}</small>
              </button>
            </div>
          );
        })}
      </div>
    </div>,
    [
      childrenByParent,
      currentId,
      expandedRelPaths,
      folderSearchFocused,
      includeSubfolders,
      query,
      selectFolder,
      sort,
      toggleFolder,
      visibleTree,
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
      <div className="folder-content">
        {error && <div className="error-line">{error}</div>}
        {items.length === 0 && !loading ? (
          <EmptyState text={includeSubfolders || !hasCurrentChildren ? '当前文件夹没有媒体' : '当前文件夹没有本层媒体'} />
        ) : (
          <div className="library-grid-shell">
            <AssetGrid
              assets={items}
              loading={loading}
              hasMore={hasMore}
              onLoadMore={loadMore}
              onOpenAsset={handleOpenAsset}
              onOpenViewer={handleOpenViewer}
              onAssetMissing={(asset) => mutateItems((value) => removeAssetById(value, asset.id))}
              onPressPreviewChange={setPressPreviewAsset}
              onScrollRatioChange={setScrollRatio}
              onScrollStateChange={handleGridScrollState}
              totalCount={totalCount}
              loadedStartIndex={loadedStartIndex}
              focusAssetId={initialStateRef.current.focusAssetId}
              scrollSignal={scrollResetSignal}
              scrollTarget={scrollTarget}
              scrollTopTarget={scrollTopTarget}
              buildViewerUrl={(asset) =>
                `/viewer/${asset.id}?context=folder&folderId=${currentId}&sort=${sort}&q=${encodeURIComponent(query)}&recursive=${
                  includeSubfolders ? 1 : 0
                }&returnPath=%2Ffolders&returnState=${encodeReturnState(currentPageState())}`
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
      </div>
    </section>
  );
}

function folderID(folder: Folder) {
  return folder.relPath === '' ? 0 : folder.id;
}

function buildFolderChildren(tree: Folder[]) {
  const result = new Map<string | null, Folder[]>();
  tree.forEach((folder) => {
    const key = folder.parentRelPath ?? null;
    const items = result.get(key) ?? [];
    items.push(folder);
    result.set(key, items);
  });
  result.forEach((items) => {
    items.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }));
  });
  return result;
}

function flattenVisibleFolders(tree: Folder[], childrenByParent: Map<string | null, Folder[]>, expandedRelPaths: Set<string>) {
  const roots = childrenByParent.get(null) ?? tree.filter((folder) => folder.parentRelPath === null);
  const result: Folder[] = [];
  const visit = (folder: Folder) => {
    result.push(folder);
    if (!expandedRelPaths.has(folder.relPath)) return;
    childrenByParent.get(folder.relPath)?.forEach(visit);
  };
  roots.forEach(visit);
  return result;
}

function mergeExpandedAncestors(value: Set<string>, relPath: string) {
  let changed = false;
  const next = new Set(value);
  const add = (folderRel: string) => {
    if (next.has(folderRel)) return;
    next.add(folderRel);
    changed = true;
  };
  add('');
  if (relPath) {
    let current = '';
    const parts = relPath.split('/');
    for (let index = 0; index < parts.length - 1; index += 1) {
      current = current ? `${current}/${parts[index]}` : parts[index];
      add(current);
    }
  }
  return changed ? next : value;
}
