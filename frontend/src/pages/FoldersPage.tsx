import { type MutableRefObject, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { ChevronsUp, ChevronRight, Folder as FolderIcon } from 'lucide-react';
import AssetGrid from '../components/AssetGrid';
import { CompactAssetGroupingControls, normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarFilterIconRow, SidebarMediaTypeList, SidebarOrientationFilter, SidebarRatingFilter, SidebarSelect } from '../components/SidebarControls';
import { CompactSortControls, isSortKey } from '../components/SortControls';
import { api } from '../api/client';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type { Asset, AssetDeletedEvent, AssetKind, AssetRatingFilter, Folder, LibraryAnchor, OrientationFilter, SortKey } from '../types/api';
import { useSidebarBrowseTools, useSidebarPanel, useSidebarQueryChips, useSidebarReturnState, useSidebarScopeTitle, type BrowseQueryChip, type BrowseTools } from '../components/SidebarContext';
import {
  appendViewerReturnParams,
  decodeReturnState,
  loadPageState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
  useViewerAwareMediaState,
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
import { waterfallPageSize } from '../utils/waterfallPaging';
import { parseTagFilters, serializeTagFilters } from '../utils/tagFilters';

const pageSize = waterfallPageSize;
const foldersStateKey = 'folders';
const folderScopePreferenceKey = 'lpicto:folders:include-subfolders';
const folderSortPreferenceKey = 'lpicto:folders:tree-sort';
const foldersURLKeys = ['folderId', 'folder', 'type', 'rating', 'orientation', 'sort', 'group', 'q', 'recursive', 'combinedTags', 'tagNodes'];

type FolderSortField = 'name' | 'created' | 'modified' | 'size' | 'count';
type FolderSortDirection = 'asc' | 'desc';

interface FolderSortPreference {
  field: FolderSortField;
  direction: FolderSortDirection;
}

interface FoldersPageState extends GridReturnState {
  collapsedFolderKeys: string[];
  currentId: number;
  groupMode: AssetGroupMode;
  includeSubfolders: boolean;
  orientation: OrientationFilter;
  query: string;
  rating: AssetRatingFilter;
  sort: SortKey;
  tagFilters: string[];
  type: AssetKind;
}

const defaultFoldersState: FoldersPageState = {
  ...resetGridState(),
  collapsedFolderKeys: [],
  currentId: 0,
  groupMode: 'none',
  includeSubfolders: true,
  orientation: 'all',
  query: '',
  rating: 'all',
  sort: 'timeline_desc',
  tagFilters: [],
  type: 'all',
};

let folderTreeCache: Folder[] | null = null;

export default function FoldersPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const liveSearchText = typeof window === 'undefined' ? location.search : window.location.search;
  const liveSearchParams = useMemo(() => new URLSearchParams(liveSearchText), [liveSearchText]);
  const persistedState = loadPageState<FoldersPageState>(foldersStateKey, defaultFoldersState);
  const rememberedState = { ...persistedState, includeSubfolders: loadFolderScopePreference(persistedState.includeSubfolders) };
  const decodedInitialState = decodeReturnState<FoldersPageState>(liveSearchParams.get('restore'), rememberedState);
  const initialStateRef = useRef(
    liveSearchParams.has('restore')
      ? decodedInitialState
      : { ...foldersStateFromSearchParams(liveSearchParams, rememberedState), includeSubfolders: rememberedState.includeSubfolders },
  );
  const requestedFolderRelPath = liveSearchParams.has('folder') ? liveSearchParams.get('folder') ?? '' : null;
  const [tree, setTree] = useState<Folder[]>(() => folderTreeCache ?? []);
  const [treeLoading, setTreeLoading] = useState(folderTreeCache === null);
  const [treeError, setTreeError] = useState('');
  const [currentId, setCurrentId] = useViewerAwareMediaState(initialStateRef.current.currentId);
  const [current, setCurrent] = useState<Folder | null>(null);
  const [sort, setSort] = useViewerAwareMediaState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useViewerAwareMediaState(initialStateRef.current.query);
  const [groupMode, setGroupMode] = useViewerAwareMediaState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [includeSubfolders, setIncludeSubfolders] = useViewerAwareMediaState(initialStateRef.current.includeSubfolders);
  const [rating, setRating] = useViewerAwareMediaState<AssetRatingFilter>(initialStateRef.current.rating ?? 'all');
  const [orientation, setOrientation] = useViewerAwareMediaState<OrientationFilter>(initialStateRef.current.orientation);
  const [type, setType] = useViewerAwareMediaState<AssetKind>(initialStateRef.current.type);
  const [folderSort, setFolderSort] = useState<FolderSortPreference>(loadFolderSortPreference);
  useEffect(() => {
    if (type === 'audio') setOrientation('all');
  }, [type]);
  const [tagFilters, setTagFilters] = useViewerAwareMediaState(initialStateRef.current.tagFilters ?? []);
  const [collapsedFolderKeys, setCollapsedFolderKeys] = useState<Set<string>>(() => {
    if (initialStateRef.current.collapsedFolderKeys.length > 0 || !folderTreeCache) {
      return new Set(initialStateRef.current.collapsedFolderKeys);
    }
    const selected = requestedFolderRelPath !== null
      ? folderTreeCache.find((folder) => folder.relPath === requestedFolderRelPath)
      : folderTreeCache.find((folder) => folderID(folder) === initialStateRef.current.currentId);
    return initialCollapsedFolderKeys(folderTreeCache, selected);
  });
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const sidebarState = useSidebarReturnState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const folderTreeRef = useRef<HTMLDivElement | null>(null);
  const initializeFolderTreeRef = useRef(folderTreeCache === null);
  const serverGroup = serverGroupForMode(groupMode);
  const activeRating = rating === 'all' ? undefined : rating;
  const currentLookupId = requestedFolderRelPath === null ? currentId : null;
  const resolvingRequestedFolder = requestedFolderRelPath !== null && current?.relPath !== requestedFolderRelPath;
  useSidebarScopeTitle('folders', current ? `文件夹 / ${current.name}` : '文件夹', [current?.id, current?.name]);
  const queryChips = useMemo<BrowseQueryChip[]>(() => {
    const chips: BrowseQueryChip[] = [];
    if (query.trim()) chips.push({ id: 'search', label: `搜索: ${query.trim()}`, onRemove: () => setQuery('') });
    if (tagFilters.length > 0) chips.push({ id: 'tags', label: `标签 ${tagFilters.length}`, onRemove: () => setTagFilters([]) });
    return chips;
  }, [query, tagFilters]);
  useSidebarQueryChips('folders', queryChips, [queryChips]);

  useEffect(() => {
    saveFolderScopePreference(includeSubfolders);
  }, [includeSubfolders]);

  useEffect(() => {
    saveFolderSortPreference(folderSort);
  }, [folderSort]);

  useEffect(() => {
    let live = true;
    async function loadTree() {
      if (folderTreeCache === null) setTreeLoading(true);
      setTreeError('');
      try {
        const treeResult = await api.folderTree();
        if (!live) return;
        if (initializeFolderTreeRef.current) {
          const selected = requestedFolderRelPath !== null
            ? treeResult.items.find((folder) => folder.relPath === requestedFolderRelPath)
            : treeResult.items.find((folder) => folderID(folder) === initialStateRef.current.currentId);
          setCollapsedFolderKeys(initialCollapsedFolderKeys(treeResult.items, selected));
          initializeFolderTreeRef.current = false;
        }
        folderTreeCache = treeResult.items;
        setTree(treeResult.items);
      } catch (err) {
        if (live) {
          setTreeError(err instanceof Error ? err.message : '读取文件夹失败');
        }
      } finally {
        if (live) setTreeLoading(false);
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

  const childrenByParent = useMemo(
    () => buildFolderChildren(tree, folderSort, includeSubfolders),
    [folderSort, includeSubfolders, tree],
  );
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
      return api.folderAssets(currentId, page, pageSize, sort, query, includeSubfolders, serverGroup, activeRating, orientation, type, undefined, serializeTagFilters(tagFilters));
    },
    [activeRating, currentId, includeSubfolders, orientation, query, resolvingRequestedFolder, serverGroup, sort, tagFilters, type],
  );
  const selectAllAssetIds = useCallback(async () => (
    await api.folderSelection(currentId, sort, query, includeSubfolders, serverGroup, activeRating, orientation, type, undefined, serializeTagFilters(tagFilters))
  ).assetIds, [activeRating, currentId, includeSubfolders, orientation, query, serverGroup, sort, tagFilters, type]);
  const { items, hasMore, hasPrevious, loading, error, loadMore, loadPrevious, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    currentId,
    groupMode,
    includeSubfolders,
    orientation,
    rating,
    resolvingRequestedFolder,
    sort,
    tagFilters,
    query,
    type,
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
    resetKey: JSON.stringify([currentId, resolvingRequestedFolder, includeSubfolders, rating, orientation, type, sort, query, groupMode, tagFilters]),
    searchParams: liveSearchParams,
  });

  const mergeReadyAssets = useCallback(
    (incoming: Asset[]) => {
      const folderRelPath = current?.relPath ?? '';
      const filtered = incoming.filter((asset) => assetMatchesFolder(asset, folderRelPath, includeSubfolders, query, activeRating, orientation, type));
      if (filtered.length === 0) return;
      mutateItems((value) => mergeSortedAssets(value, filtered, sort, { hasMore, loadedStartIndex, groupMode }));
    },
    [activeRating, current?.relPath, groupMode, hasMore, includeSubfolders, loadedStartIndex, mutateItems, orientation, query, sort, type],
  );

  const handleAssetReady = useCallback((asset: Asset) => mergeReadyAssets([asset]), [mergeReadyAssets]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((value) => removeAssetById(value, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected || !current || resolvingRequestedFolder) return undefined;
    const timer = window.setInterval(() => {
      void api
        .folderAssets(currentId, 1, pageSize, sort, query, includeSubfolders, serverGroup, activeRating, orientation, type, undefined, serializeTagFilters(tagFilters))
        .then((result) => mergeReadyAssets(result.items))
        .catch(() => undefined);
    }, 30000);
    return () => window.clearInterval(timer);
  }, [activeRating, current, currentId, eventsConnected, includeSubfolders, mergeReadyAssets, orientation, query, resolvingRequestedFolder, serverGroup, sort, tagFilters, type]);

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
      tagFilters,
      type,
    }),
    [collapsedFolderKeys, currentId, getGridState, groupMode, includeSubfolders, orientation, query, rating, sidebarState.sidebarExpanded, sort, tagFilters, type],
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
        rating: activeRating,
        recursive: includeSubfolders ? 1 : 0,
        sort,
        tagNodes: serializeTagFilters(tagFilters),
        type: type === 'all' ? undefined : type,
      },
      foldersURLKeys,
    );
  }, [activeRating, current, currentId, groupMode, includeSubfolders, liveSearchText, location, navigate, orientation, query, requestedFolderRelPath, sort, tagFilters, type]);

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
        const result = await api.folderAnchors(currentId, pageSize, sort, query, includeSubfolders, serverGroup, activeRating, orientation, type, undefined, serializeTagFilters(tagFilters));
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
  }, [activeRating, currentId, includeSubfolders, orientation, query, resolvingRequestedFolder, serverGroup, sort, tagFilters, type]);

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
    const ancestorKeys = folderAncestorPathKeys(selected, folderByRelPath);
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
            rating: activeRating,
            recursive: includeSubfolders ? 1 : 0,
            sort,
            tagNodes: serializeTagFilters(tagFilters),
            type: type === 'all' ? undefined : type,
          },
          foldersURLKeys,
        );
      }
    },
    [activeRating, groupMode, includeSubfolders, location, navigate, orientation, query, sort, tagFilters, type],
  );

  useEffect(() => {
    const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, sort);
    if (nextGroupMode !== groupMode) {
      setGroupMode(nextGroupMode);
    }
  }, [groupMode, sort]);

  const handleRatingChange = useCallback((nextRating: AssetRatingFilter) => {
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
    const keepOpen = new Set(selected ? folderAncestorPathKeys(selected, folderByRelPath) : []);
    setCollapsedFolderKeys(new Set(tree.filter((folder) => (childrenByParent.get(folder.relPath)?.length ?? 0) > 0 && !keepOpen.has(folder.relPath)).map((folder) => folder.relPath)));
  }, [childrenByParent, currentId, folderByRelPath, tree]);

  const browseTools = useMemo<BrowseTools>(() => ({
    groupMode,
    onGroupChange: setGroupMode,
    onOrientationChange: setOrientation,
    onRatingChange: handleRatingChange,
    onSortChange: setSort,
    onTagFilterChange: setTagFilters,
    onTypeChange: setType,
    orientation,
    panelModes: ['search', 'filters'],
    rating,
    sort,
    tagFilters,
    type,
  }), [groupMode, handleRatingChange, orientation, rating, sort, tagFilters, type]);
  useSidebarBrowseTools('folders', browseTools, [browseTools]);

  useSidebarPanel(
    'folders',
    <div className="sidebar-control-stack sidebar-folder-panel">
      <SidebarFilterIconRow>
        <SidebarMediaTypeList value={type} onChange={setType} />
        <SidebarOrientationFilter value={orientation} onChange={setOrientation} />
        <SidebarRatingFilter value={rating} onChange={handleRatingChange} />
        <CompactAssetGroupingControls groupMode={groupMode} sort={sort} onChange={setGroupMode} />
        <CompactSortControls sort={sort} onChange={setSort} />
      </SidebarFilterIconRow>
      <div className="sidebar-panel-section sidebar-panel-scope">
        <div className="sidebar-field-grid sidebar-folder-sort-controls">
          <SidebarSelect
            label="文件夹排序"
            value={folderSort.field}
            options={[
              { value: 'name', label: '文件名' },
              { value: 'created', label: '创建时间' },
              { value: 'modified', label: '修改时间' },
              { value: 'size', label: '大小' },
              { value: 'count', label: '媒体数量' },
            ]}
            onChange={(value) => setFolderSort((current) => ({ ...current, field: parseFolderSortField(value) }))}
          />
          <SidebarSelect
            label="顺序"
            value={folderSort.direction}
            options={[
              { value: 'asc', label: '正序' },
              { value: 'desc', label: '倒序' },
            ]}
            onChange={(value) => setFolderSort((current) => ({ ...current, direction: value === 'desc' ? 'desc' : 'asc' }))}
          />
        </div>
        <SidebarSelect
          label="文件夹范围"
          value={includeSubfolders ? 'recursive' : 'direct'}
          options={[
            { value: 'recursive', label: '含子文件夹' },
            { value: 'direct', label: '仅本层' },
          ]}
          onChange={(value) => setIncludeSubfolders(value === 'recursive')}
        />
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
          error={treeError}
          includeSubfolders={includeSubfolders}
          loading={treeLoading}
          onSelect={selectFolder}
          onToggle={toggleFolderCollapsed}
          treeRef={folderTreeRef}
        />
      </div>
      <label className="sidebar-field sidebar-folder-search-field sidebar-panel-section sidebar-panel-search">
        <span>搜索</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="当前文件夹" />
      </label>
    </div>,
    [
      childrenByParent,
      collapsedFolderKeys,
      currentId,
      treeError,
      treeLoading,
      groupMode,
      handleRatingChange,
      includeSubfolders,
      folderSort,
      orientation,
      query,
      rating,
      collapseOtherFolders,
      selectFolder,
      sort,
      toggleFolderCollapsed,
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
              onBatchRemoveAssets={(ids) => mutateItems((current) => current.filter((asset) => !ids.includes(asset.id)))}
              selectAllAssetIds={selectAllAssetIds}
              onPressPreviewChange={setPressPreviewAsset}
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
                  `/viewer/${asset.id}?context=folder&folderId=${currentId}&sort=${sort}&q=${encodeURIComponent(query)}&recursive=${
                    includeSubfolders ? 1 : 0
                  }${serializeTagFilters(tagFilters) ? `&tagNodes=${encodeURIComponent(serializeTagFilters(tagFilters)!)}` : ''}${typeViewerParam(type)}${ratingViewerParam(rating)}${orientationViewerParam(orientation)}${serverGroup ? `&group=${serverGroup}` : ''}`,
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
      </div>
    </section>
  );
}

function folderID(folder: Folder) {
  return folder.relPath === '' ? 0 : folder.id;
}

function loadFolderScopePreference(fallback: boolean) {
  if (typeof window === 'undefined') return fallback;
  try {
    const stored = window.localStorage.getItem(folderScopePreferenceKey);
    if (stored === 'true') return true;
    if (stored === 'false') return false;
  } catch {
    // The page-state and URL fallbacks still preserve the current view.
  }
  return fallback;
}

function saveFolderScopePreference(includeSubfolders: boolean) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(folderScopePreferenceKey, String(includeSubfolders));
  } catch {
    // Browser storage may be unavailable in private or restricted contexts.
  }
}

function loadFolderSortPreference(): FolderSortPreference {
  const fallback: FolderSortPreference = { field: 'name', direction: 'asc' };
  if (typeof window === 'undefined') return fallback;
  try {
    const parsed = JSON.parse(window.localStorage.getItem(folderSortPreferenceKey) ?? '') as Partial<FolderSortPreference>;
    return {
      field: parseFolderSortField(parsed.field),
      direction: parsed.direction === 'desc' ? 'desc' : 'asc',
    };
  } catch {
    return fallback;
  }
}

function saveFolderSortPreference(preference: FolderSortPreference) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(folderSortPreferenceKey, JSON.stringify(preference));
  } catch {
    // Browser storage may be unavailable in private or restricted contexts.
  }
}

function parseFolderSortField(value: unknown): FolderSortField {
  return value === 'created' || value === 'modified' || value === 'size' || value === 'count' ? value : 'name';
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
    rating: params.has('rating')
      ? params.get('rating') === 'all' ? 'all' : assetRatingParam(params.get('rating')) ?? base.rating
      : base.rating,
    sort: isSortKey(sort) ? sort : base.sort,
    tagFilters: params.has('tagNodes') || params.has('combinedTags') ? parseTagFilters(params.get('tagNodes') ?? params.get('combinedTags')) : base.tagFilters ?? [],
    type: params.has('type') ? parseAssetKind(params.get('type')) : base.type,
  };
}

function parseAssetKind(value: string | null): AssetKind {
  return value === 'image' || value === 'video' || value === 'audio' ? value : 'all';
}

function typeViewerParam(type: AssetKind) {
  return type === 'all' ? '' : `&type=${type}`;
}

function ratingViewerParam(rating: AssetRatingFilter) {
  return rating === 'all' ? '' : `&rating=${rating}`;
}

function orientationViewerParam(orientation: OrientationFilter) {
  return orientation === 'all' ? '' : `&orientation=${orientation}`;
}

function buildFolderChildren(tree: Folder[], preference: FolderSortPreference, includeSubfolders: boolean) {
  const result = new Map<string | null, Folder[]>();
  tree.forEach((folder) => {
    const key = folder.parentRelPath ?? null;
    const items = result.get(key) ?? [];
    items.push(folder);
    result.set(key, items);
  });
  result.forEach((items) => {
    items.sort((a, b) => compareFolders(a, b, preference, includeSubfolders));
  });
  return result;
}

function compareFolders(a: Folder, b: Folder, preference: FolderSortPreference, includeSubfolders: boolean) {
  let result = 0;
  switch (preference.field) {
    case 'created':
      result = a.createdAt - b.createdAt;
      break;
    case 'modified':
      result = a.modifiedAt - b.modifiedAt;
      break;
    case 'size':
      result = (includeSubfolders ? a.recursiveSizeBytes : a.sizeBytes) - (includeSubfolders ? b.recursiveSizeBytes : b.sizeBytes);
      break;
    case 'count':
      result = (includeSubfolders ? a.recursiveAssetCount : a.assetCount) - (includeSubfolders ? b.recursiveAssetCount : b.assetCount);
      break;
    default:
      result = naturalFolderNameCompare(a.name, b.name);
  }
  if (result !== 0) return preference.direction === 'desc' ? -result : result;
  return naturalFolderNameCompare(a.name, b.name);
}

function naturalFolderNameCompare(a: string, b: string) {
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' });
}

function SidebarFolderTree({
  childrenByParent,
  collapsedKeys,
  currentId,
  error,
  includeSubfolders,
  loading,
  onSelect,
  onToggle,
  treeRef,
}: {
  childrenByParent: Map<string | null, Folder[]>;
  collapsedKeys: Set<string>;
  currentId: number;
  error: string;
  includeSubfolders: boolean;
  loading: boolean;
  onSelect: (folder: Folder) => void;
  onToggle: (key: string) => void;
  treeRef: MutableRefObject<HTMLDivElement | null>;
}) {
  const roots = visibleSidebarFolderRoots(childrenByParent);
  const scrollViewportRef = useRef<HTMLDivElement | null>(null);
  const contentRef = useRef<HTMLDivElement | null>(null);
  const topScrollRef = useRef<HTMLDivElement | null>(null);
  const [contentWidth, setContentWidth] = useState(0);
  const [hasHorizontalOverflow, setHasHorizontalOverflow] = useState(false);

  const updateScrollDimensions = useCallback(() => {
    const viewport = scrollViewportRef.current;
    const content = contentRef.current;
    if (!viewport || !content) return;
    const nextWidth = Math.ceil(content.scrollWidth);
    setContentWidth((value) => (value === nextWidth ? value : nextWidth));
    setHasHorizontalOverflow(nextWidth > viewport.clientWidth + 1);
  }, []);

  useEffect(() => {
    const viewport = scrollViewportRef.current;
    const content = contentRef.current;
    if (!viewport || !content) return undefined;
    const observer = new ResizeObserver(updateScrollDimensions);
    observer.observe(viewport);
    observer.observe(content);
    const frame = window.requestAnimationFrame(updateScrollDimensions);
    window.addEventListener('resize', updateScrollDimensions);
    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener('resize', updateScrollDimensions);
      observer.disconnect();
    };
  }, [updateScrollDimensions]);

  useEffect(() => {
    const frame = window.requestAnimationFrame(updateScrollDimensions);
    const settleTimer = window.setTimeout(updateScrollDimensions, 240);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(settleTimer);
    };
  }, [collapsedKeys, contentWidth, updateScrollDimensions]);

  const setTreeViewport = useCallback((node: HTMLDivElement | null) => {
    scrollViewportRef.current = node;
    treeRef.current = node;
  }, [treeRef]);

  const syncHorizontalScroll = useCallback((source: HTMLDivElement | null, target: HTMLDivElement | null) => {
    if (!source || !target || target.scrollLeft === source.scrollLeft) return;
    target.scrollLeft = source.scrollLeft;
  }, []);

  return (
    <div className={`sidebar-folder-tree-shell${hasHorizontalOverflow ? ' has-horizontal-overflow' : ''}`}>
      <div
        aria-hidden="true"
        className="sidebar-folder-tree-top-scroll"
        ref={topScrollRef}
        onScroll={() => syncHorizontalScroll(topScrollRef.current, scrollViewportRef.current)}
      >
        <div className="sidebar-folder-tree-top-track" style={{ width: `${contentWidth}px` }} />
      </div>
      <div
        className="sidebar-folder-tree"
        role="tree"
        aria-label="文件夹"
        ref={setTreeViewport}
        onScroll={() => syncHorizontalScroll(scrollViewportRef.current, topScrollRef.current)}
      >
        <div className="sidebar-folder-tree-content" ref={contentRef}>
          {roots.length === 0 ? (
            <div className={error ? 'error-line' : 'muted-line'}>{loading ? '正在加载文件夹' : error || '暂无文件夹'}</div>
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
      </div>
    </div>
  );
}

function visibleSidebarFolderRoots(childrenByParent: Map<string | null, Folder[]>) {
  const roots = childrenByParent.get(null) ?? [];
  const virtualRoot = roots.find((folder) => folder.relPath === '');
  const visibleRoots = virtualRoot
    ? [...(childrenByParent.get(virtualRoot.relPath) ?? []), ...roots.filter((folder) => folder.relPath !== virtualRoot.relPath)]
    : roots;
  return visibleRoots.flatMap((folder) => {
    const children = childrenByParent.get(folder.relPath) ?? [];
    return folder.name.trim().toLowerCase() === 'nas' && children.length > 0 ? children : [folder];
  });
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
        style={{ paddingInlineStart: `${depth * 16}px` }}
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

function folderAncestorPathKeys(folder: Folder, folderByRelPath: Map<string, Folder>) {
  const result: string[] = [];
  let parentKey = folder.parentRelPath;
  while (parentKey !== null) {
    result.push(parentKey);
    parentKey = folderByRelPath.get(parentKey)?.parentRelPath ?? null;
  }
  return result;
}

function initialCollapsedFolderKeys(tree: Folder[], selected?: Folder) {
  const foldersByRelPath = new Map(tree.map((folder) => [folder.relPath, folder]));
  const keepOpen = new Set(selected ? folderAncestorPathKeys(selected, foldersByRelPath) : []);
  const parentKeys = new Set(tree.map((folder) => folder.parentRelPath).filter((key): key is string => key !== null));
  return new Set(Array.from(parentKeys).filter((key) => !keepOpen.has(key)));
}
