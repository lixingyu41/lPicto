import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { ChevronRight, Folder as FolderIcon, FolderTree } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import SortControls from '../components/SortControls';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type { Asset, AssetDeletedEvent, Folder, LibraryAnchor, SortKey } from '../types/api';
import { useRestoreSidebarState, useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import {
  appendViewerReturnParams,
  decodeReturnState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { serverGroupForMode, type AssetGroupMode } from '../utils/assetGrouping';
import { assetMatchesFolder } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';

const pageSize = 100;
const foldersStateKey = 'folders';
const mediaGroupOptions: Array<{ value: AssetGroupMode; label: string }> = [
  { value: 'none', label: '不分' },
  { value: 'folder', label: '文件夹' },
];

interface FoldersPageState extends GridReturnState {
  currentId: number;
  expandedRelPaths: string[];
  groupMode: AssetGroupMode;
  includeSubfolders: boolean;
  query: string;
  sort: SortKey;
}

const defaultFoldersState: FoldersPageState = {
  ...resetGridState(),
  currentId: 0,
  expandedRelPaths: [''],
  groupMode: 'none',
  includeSubfolders: true,
  query: '',
  sort: 'timeline_desc',
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
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [folderSearchFocused, setFolderSearchFocused] = useState(false);
  const [includeSubfolders, setIncludeSubfolders] = useState(initialStateRef.current.includeSubfolders);
  const [expandedRelPaths, setExpandedRelPaths] = useState<Set<string>>(() => new Set(initialStateRef.current.expandedRelPaths));
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const sidebarState = useSidebarReturnState();
  const restoreSidebarState = useRestoreSidebarState();
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const serverGroup = serverGroupForMode(groupMode);

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
    (page: number) => api.folderAssets(currentId, page, pageSize, sort, query, includeSubfolders, serverGroup),
    [currentId, includeSubfolders, query, serverGroup, sort],
  );
  const { items, hasMore, loading, error, loadMore, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    currentId,
    groupMode,
    includeSubfolders,
    sort,
    query,
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
    resetKey: JSON.stringify([currentId, includeSubfolders, sort, query, groupMode]),
    searchParams,
  });

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const folderRelPath = current?.relPath ?? '';
      const filtered = incoming.filter((asset) => assetMatchesFolder(asset, folderRelPath, includeSubfolders, query));
      if (filtered.length === 0) return;
      mutateItems((value) => mergeSortedAssets(value, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [current?.relPath, groupMode, hasMore, includeSubfolders, loadedStartIndex, mutateItems, query, sort],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((value) => removeAssetById(value, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected || !current) return undefined;
    const timer = window.setInterval(() => {
      void api.folderAssets(currentId, 1, pageSize, sort, query, includeSubfolders, serverGroup).then((result) => mergeReadyAssets(result.items)).catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [current, currentId, eventsConnected, includeSubfolders, mergeReadyAssets, query, serverGroup, sort]);

  const currentPageState = useCallback(
    (): FoldersPageState => ({
      ...getGridState(),
      currentId,
      expandedRelPaths: Array.from(expandedRelPaths),
      groupMode,
      includeSubfolders,
      query,
      sidebarCollapsed: sidebarState.sidebarCollapsed,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
    }),
    [currentId, expandedRelPaths, getGridState, groupMode, includeSubfolders, query, sidebarState.sidebarCollapsed, sidebarState.sidebarExpanded, sort],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<FoldersPageState>(foldersStateKey, currentPageState());
  }, [currentPageState]);

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
      <SortControls sort={sort} onChange={setSort} />
      <div className="sidebar-control-title">分组</div>
      <div className="sidebar-list">
        {mediaGroupOptions.map((option) => (
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
      groupMode,
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
              focusAssetId={focusAssetId}
              groupMode={groupMode}
              sort={sort}
              scrollSignal={scrollResetSignal}
              scrollTarget={scrollTarget}
              scrollTopTarget={scrollTopTarget}
              buildViewerUrl={(asset) =>
                appendViewerReturnParams(
                  `/viewer/${asset.id}?context=folder&folderId=${currentId}&sort=${sort}&q=${encodeURIComponent(query)}&recursive=${
                    includeSubfolders ? 1 : 0
                  }${serverGroup ? `&group=${serverGroup}` : ''}`,
                  '/folders',
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
