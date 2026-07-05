import { type Ref, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { ChevronsUp, ChevronRight, Folder as FolderIcon } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import AssetGroupingControls, { normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarButtonGroup, SidebarRatingFilter, SidebarSelect, sidebarOrientationOptions } from '../components/SidebarControls';
import SortControls, { isSortKey } from '../components/SortControls';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type { Asset, AssetDeletedEvent, AssetRating, Folder, LibraryAnchor, OrientationFilter, SortKey } from '../types/api';
import { useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
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
import { assetMatchesFolder } from '../utils/assetFilters';
import { mergeSortedAssets, removeAssetById } from '../utils/assetSort';
import {
  assetRatingParam,
  booleanParam,
  currentURLHasParam,
  currentURLLocation,
  currentURLPath,
  nonNegativeIntParam,
  orientationParam,
  replaceURLState,
} from '../utils/urlState';

const pageSize = 100;
const foldersStateKey = 'folders';
const foldersURLKeys = ['folderId', 'folder', 'rating', 'orientation', 'sort', 'group', 'q', 'recursive'];

interface FoldersPageState extends GridReturnState {
  collapsedFolderKeys: string[];
  currentId: number;
  groupMode: AssetGroupMode;
  includeSubfolders: boolean;
  orientation: OrientationFilter;
  query: string;
  rating: AssetRating;
  sort: SortKey;
}

const defaultFoldersState: FoldersPageState = {
  ...resetGridState(),
  collapsedFolderKeys: [],
  currentId: 0,
  groupMode: 'none',
  includeSubfolders: true,
  orientation: 'all',
  query: '',
  rating: 0,
  sort: 'timeline_desc',
};

let folderTreeCache: Folder[] | null = null;

export default function FoldersPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const persistedState = loadPageState<FoldersPageState>(foldersStateKey, defaultFoldersState);
  const decodedInitialState = decodeReturnState<FoldersPageState>(searchParams.get('restore'), persistedState);
  const initialStateRef = useRef(searchParams.has('restore') ? decodedInitialState : foldersStateFromSearchParams(searchParams, persistedState));
  const requestedFolderRelPath = searchParams.has('folder') ? searchParams.get('folder') ?? '' : null;
  const [tree, setTree] = useState<Folder[]>(() => folderTreeCache ?? []);
  const [currentId, setCurrentId] = useState(initialStateRef.current.currentId);
  const [current, setCurrent] = useState<Folder | null>(null);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [includeSubfolders, setIncludeSubfolders] = useState(initialStateRef.current.includeSubfolders);
  const [rating, setRating] = useState<AssetRating>(initialStateRef.current.rating ?? 0);
  const [orientation, setOrientation] = useState<OrientationFilter>(initialStateRef.current.orientation);
  const [collapsedFolderKeys, setCollapsedFolderKeys] = useState<Set<string>>(() => new Set(initialStateRef.current.collapsedFolderKeys));
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const sidebarState = useSidebarReturnState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const folderTreeRef = useRef<HTMLDivElement | null>(null);
  const serverGroup = serverGroupForMode(groupMode);
  const currentLookupId = requestedFolderRelPath === null ? currentId : null;
  const resolvingRequestedFolder = requestedFolderRelPath !== null && current?.relPath !== requestedFolderRelPath;

  useEffect(() => {
    let live = true;
    async function loadTree() {
      try {
        const treeResult = await api.folderTree();
        if (!live) return;
        folderTreeCache = treeResult.items;
        setTree(treeResult.items);
      } catch {
        if (live) {
          folderTreeCache = null;
          setTree([]);
        }
      }
    }
    void loadTree();
    return () => {
      live = false;
    };
  }, []);

  useEffect(() => {
    let live = true;
    async function loadCurrent() {
      try {
        const folderResult = requestedFolderRelPath !== null ? await api.folderByPath(requestedFolderRelPath) : await api.folder(currentLookupId ?? 0);
        if (!live) return;
        setCurrent(folderResult);
        setCurrentId(folderID(folderResult));
      } catch {
        if (!live) return;
        if (currentLookupId !== null && currentLookupId !== 0) {
          setCurrentId(0);
          return;
        }
        setCurrent(null);
      }
    }
    void loadCurrent();
    return () => {
      live = false;
    };
  }, [currentLookupId, requestedFolderRelPath]);

  const childrenByParent = useMemo(() => buildFolderChildren(tree), [tree]);
  const folderByRelPath = useMemo(() => new Map(tree.map((folder) => [folder.relPath, folder])), [tree]);

  useEffect(() => {
    if (requestedFolderRelPath === null || tree.length === 0) return;
    const selected = folderByRelPath.get(requestedFolderRelPath);
    if (!selected) return;
    const nextId = folderID(selected);
    if (nextId === currentId && current?.relPath === selected.relPath) return;
    setCurrent(selected);
    setCurrentId(nextId);
  }, [current?.relPath, currentId, folderByRelPath, requestedFolderRelPath, tree.length]);

  const loadAssets = useCallback(
    (page: number) => {
      if (resolvingRequestedFolder) {
        return Promise.resolve({ items: [], page, pageSize, hasMore: false });
      }
      return api.folderAssets(currentId, page, pageSize, sort, query, includeSubfolders, serverGroup, rating, orientation);
    },
    [currentId, includeSubfolders, orientation, query, rating, resolvingRequestedFolder, serverGroup, sort],
  );
  const { items, hasMore, hasPrevious, loading, error, loadMore, loadPrevious, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    currentId,
    groupMode,
    includeSubfolders,
    orientation,
    rating,
    resolvingRequestedFolder,
    sort,
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
    resetKey: JSON.stringify([currentId, resolvingRequestedFolder, includeSubfolders, rating, orientation, sort, query, groupMode]),
    searchParams,
  });

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const folderRelPath = current?.relPath ?? '';
      const filtered = incoming.filter((asset) => assetMatchesFolder(asset, folderRelPath, includeSubfolders, query, rating, orientation));
      if (filtered.length === 0) return;
      mutateItems((value) => mergeSortedAssets(value, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [current?.relPath, groupMode, hasMore, includeSubfolders, loadedStartIndex, mutateItems, orientation, query, rating, sort],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((value) => removeAssetById(value, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected || !current || resolvingRequestedFolder) return undefined;
    const timer = window.setInterval(() => {
      void api
        .folderAssets(currentId, 1, pageSize, sort, query, includeSubfolders, serverGroup, rating, orientation)
        .then((result) => mergeReadyAssets(result.items))
        .catch(() => undefined);
    }, 30000);
    return () => window.clearInterval(timer);
  }, [current, currentId, eventsConnected, includeSubfolders, mergeReadyAssets, orientation, query, rating, resolvingRequestedFolder, serverGroup, sort]);

  const currentPageState = useCallback(
    (): FoldersPageState => ({
      ...getGridState(),
      collapsedFolderKeys: Array.from(collapsedFolderKeys),
      currentId,
      groupMode,
      includeSubfolders,
      orientation,
      query,
      rating,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
    }),
    [collapsedFolderKeys, currentId, getGridState, groupMode, includeSubfolders, orientation, query, rating, sidebarState.sidebarExpanded, sort],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<FoldersPageState>(foldersStateKey, currentPageState());
  }, [currentPageState]);
  const scheduleCurrentStateSave = usePersistentPageState(saveCurrentState);

  useEffect(() => {
    if (currentURLHasParam(location, 'restore') || !current || folderID(current) !== currentId) return;
    if (requestedFolderRelPath !== null && current.relPath !== requestedFolderRelPath) return;
    replaceURLState(
      navigate,
      location,
      {
        folder: current.relPath,
        folderId: currentId,
        group: groupMode,
        orientation: orientation === 'all' ? undefined : orientation,
        q: query,
        rating,
        recursive: includeSubfolders ? 1 : 0,
        sort,
      },
      foldersURLKeys,
    );
  }, [current, currentId, groupMode, includeSubfolders, location, navigate, orientation, query, rating, requestedFolderRelPath, searchParams, sort]);

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
      if (resolvingRequestedFolder) {
        setAnchors([]);
        setTotalCount(0);
        return;
      }
      try {
        const result = await api.folderAnchors(currentId, pageSize, sort, query, includeSubfolders, serverGroup, rating, orientation);
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
  }, [currentId, includeSubfolders, orientation, query, rating, resolvingRequestedFolder, serverGroup, sort]);

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

  const hasCurrentChildren = current ? (childrenByParent.get(current.relPath)?.length ?? 0) > 0 : false;

  useEffect(() => {
    const selected = tree.find((folder) => folderID(folder) === currentId);
    if (!selected) return;
    const ancestorKeys = folderOpenPathKeys(selected, folderByRelPath);
    if (ancestorKeys.length === 0) return;
    setCollapsedFolderKeys((value) => {
      const next = new Set(value);
      let changed = false;
      ancestorKeys.forEach((key) => {
        if (next.delete(key)) changed = true;
      });
      return changed ? next : value;
    });
  }, [currentId, folderByRelPath, tree]);

  useEffect(() => {
    if (tree.length === 0) return undefined;
    const frame = window.requestAnimationFrame(() => {
      const node = folderTreeRef.current?.querySelector<HTMLElement>(`[data-folder-id="${currentId}"]`);
      node?.scrollIntoView({ block: 'center' });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [collapsedFolderKeys, currentId, tree.length]);

  const selectFolder = useCallback(
    (folder: Folder) => {
      const nextId = folderID(folder);
      setCurrent(folder);
      setCurrentId(nextId);
      if (!currentURLHasParam(location, 'restore')) {
        replaceURLState(
          navigate,
          location,
          {
            folder: folder.relPath,
            folderId: nextId,
            group: groupMode,
            orientation: orientation === 'all' ? undefined : orientation,
            q: query,
            rating,
            recursive: includeSubfolders ? 1 : 0,
            sort,
          },
          foldersURLKeys,
        );
      }
    },
    [groupMode, includeSubfolders, location, navigate, orientation, query, rating, sort],
  );

  useEffect(() => {
    const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, sort);
    if (nextGroupMode !== groupMode) {
      setGroupMode(nextGroupMode);
    }
  }, [groupMode, sort]);

  const handleRatingChange = useCallback((nextRating: AssetRating) => {
    setRating(nextRating);
  }, []);

  const toggleFolderCollapsed = useCallback((key: string) => {
    setCollapsedFolderKeys((value) => {
      const next = new Set(value);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  const collapseOtherFolders = useCallback(() => {
    const selected = tree.find((folder) => folderID(folder) === currentId);
    const keepOpen = new Set(selected ? folderOpenPathKeys(selected, folderByRelPath) : []);
    setCollapsedFolderKeys(new Set(tree.filter((folder) => (childrenByParent.get(folder.relPath)?.length ?? 0) > 0 && !keepOpen.has(folder.relPath)).map((folder) => folder.relPath)));
  }, [childrenByParent, currentId, folderByRelPath, tree]);

  useSidebarPanel(
    'folders',
    <div className="sidebar-control-stack sidebar-folder-panel">
      <SidebarRatingFilter value={rating} onChange={handleRatingChange} />
      <SidebarButtonGroup columns={3} label="方向" value={orientation} options={sidebarOrientationOptions} onChange={setOrientation} />
      <SortControls sort={sort} onChange={setSort} />
      <AssetGroupingControls groupMode={groupMode} sort={sort} onChange={setGroupMode} />
      <div className="sidebar-folder-action-row">
        <button className="sidebar-command" type="button" onClick={collapseOtherFolders}>
          <ChevronsUp size={15} />
          <span>全部折叠</span>
        </button>
      </div>
      <SidebarFolderTree
        childrenByParent={childrenByParent}
        collapsedKeys={collapsedFolderKeys}
        currentId={currentId}
        includeSubfolders={includeSubfolders}
        onSelect={selectFolder}
        onToggle={toggleFolderCollapsed}
        treeRef={folderTreeRef}
      />
      <SidebarSelect
        label="范围"
        value={includeSubfolders ? 'recursive' : 'direct'}
        options={[
          { value: 'recursive', label: '含子文件夹' },
          { value: 'direct', label: '仅本层' },
        ]}
        onChange={(value) => setIncludeSubfolders(value === 'recursive')}
      />
      <label className="sidebar-field sidebar-folder-search-field">
        <span>搜索</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="当前文件夹" />
      </label>
    </div>,
    [
      childrenByParent,
      collapsedFolderKeys,
      currentId,
      groupMode,
      handleRatingChange,
      includeSubfolders,
      orientation,
      query,
      rating,
      collapseOtherFolders,
      selectFolder,
      sort,
      toggleFolderCollapsed,
    ],
  );

  useSidebarPanel(
    'viewer',
    pressPreviewAsset ? <AssetInfoPanel asset={pressPreviewAsset} title="快速预览" /> : null,
    [pressPreviewAsset?.id],
  );

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
              hasPrevious={hasPrevious}
              onLoadMore={loadMore}
              onLoadPrevious={loadPreviousPage}
              onOpenAsset={handleOpenAsset}
              onOpenViewer={handleOpenViewer}
              onAssetMissing={(asset) => mutateItems((value) => removeAssetById(value, asset.id))}
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
                  `/viewer/${asset.id}?context=folder&folderId=${currentId}&sort=${sort}&q=${encodeURIComponent(query)}&recursive=${
                    includeSubfolders ? 1 : 0
                  }${ratingViewerParam(rating)}${orientationViewerParam(orientation)}${serverGroup ? `&group=${serverGroup}` : ''}`,
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
      </div>
    </section>
  );
}

function folderID(folder: Folder) {
  return folder.relPath === '' ? 0 : folder.id;
}

function foldersStateFromSearchParams(params: URLSearchParams, fallback: FoldersPageState): FoldersPageState {
  const currentId = nonNegativeIntParam(params.get('folderId'));
  const currentRelPath = params.has('folder') ? params.get('folder') ?? '' : null;
  const sort = params.get('sort');
  const hasFolderParams = foldersURLKeys.some((key) => params.has(key));
  const base = hasFolderParams ? { ...fallback, ...resetGridState() } : fallback;
  return {
    ...base,
    currentId: currentId ?? (currentRelPath !== null ? 0 : base.currentId),
    groupMode: parseAssetGroupMode(params.get('group'), base.groupMode),
    includeSubfolders: booleanParam(params.get('recursive'), base.includeSubfolders),
    orientation: params.has('orientation') ? orientationParam(params.get('orientation')) : base.orientation,
    query: params.get('q') ?? (hasFolderParams ? '' : base.query),
    rating: params.has('rating') ? assetRatingParam(params.get('rating')) ?? base.rating : base.rating,
    sort: isSortKey(sort) ? sort : base.sort,
  };
}

function ratingViewerParam(rating: AssetRating) {
  return `&rating=${rating}`;
}

function orientationViewerParam(orientation: OrientationFilter) {
  return orientation === 'all' ? '' : `&orientation=${orientation}`;
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

function SidebarFolderTree({
  childrenByParent,
  collapsedKeys,
  currentId,
  includeSubfolders,
  onSelect,
  onToggle,
  treeRef,
}: {
  childrenByParent: Map<string | null, Folder[]>;
  collapsedKeys: Set<string>;
  currentId: number;
  includeSubfolders: boolean;
  onSelect: (folder: Folder) => void;
  onToggle: (key: string) => void;
  treeRef: Ref<HTMLDivElement>;
}) {
  const roots = childrenByParent.get(null) ?? [];
  return (
    <div className="sidebar-folder-tree" role="tree" aria-label="文件夹" ref={treeRef}>
      {roots.length === 0 ? (
        <div className="muted-line">暂无文件夹</div>
      ) : (
        roots.map((folder) => (
          <SidebarFolderNode
            childrenByParent={childrenByParent}
            collapsedKeys={collapsedKeys}
            currentId={currentId}
            depth={0}
            folder={folder}
            includeSubfolders={includeSubfolders}
            key={folder.relPath || 'root'}
            onSelect={onSelect}
            onToggle={onToggle}
          />
        ))
      )}
    </div>
  );
}

function SidebarFolderNode({
  childrenByParent,
  collapsedKeys,
  currentId,
  depth,
  folder,
  includeSubfolders,
  onSelect,
  onToggle,
}: {
  childrenByParent: Map<string | null, Folder[]>;
  collapsedKeys: Set<string>;
  currentId: number;
  depth: number;
  folder: Folder;
  includeSubfolders: boolean;
  onSelect: (folder: Folder) => void;
  onToggle: (key: string) => void;
}) {
  const children = childrenByParent.get(folder.relPath) ?? [];
  const hasChildren = children.length > 0;
  const expanded = !collapsedKeys.has(folder.relPath);
  const active = folderID(folder) === currentId;
  const count = includeSubfolders ? folder.recursiveAssetCount : folder.assetCount;
  return (
    <>
      <div
        aria-expanded={hasChildren ? expanded : undefined}
        aria-selected={active}
        className={active ? 'sidebar-folder-node active' : 'sidebar-folder-node'}
        data-folder-id={folderID(folder)}
        aria-level={depth + 1}
        role="treeitem"
      >
        <button
          aria-label={expanded ? '折叠文件夹' : '展开文件夹'}
          className={hasChildren && expanded ? 'folder-expand-button expanded' : 'folder-expand-button'}
          disabled={!hasChildren}
          type="button"
          onClick={() => onToggle(folder.relPath)}
        >
          <ChevronRight size={15} />
        </button>
        <button className="sidebar-folder-node-main" type="button" onClick={() => onSelect(folder)}>
          <FolderIcon size={15} />
          <span>{folder.relPath === '' ? '全部存储' : folder.name}</span>
          <small>{count}</small>
        </button>
      </div>
      {hasChildren &&
        expanded &&
        children.map((child) => (
          <SidebarFolderNode
            childrenByParent={childrenByParent}
            collapsedKeys={collapsedKeys}
            currentId={currentId}
            depth={depth + 1}
            folder={child}
            includeSubfolders={includeSubfolders}
            key={child.relPath}
            onSelect={onSelect}
            onToggle={onToggle}
          />
        ))}
    </>
  );
}

function folderOpenPathKeys(folder: Folder, folderByRelPath: Map<string, Folder>) {
  const result: string[] = [folder.relPath];
  let parentKey = folder.parentRelPath;
  while (parentKey !== null) {
    result.push(parentKey);
    parentKey = folderByRelPath.get(parentKey)?.parentRelPath ?? null;
  }
  return result;
}
