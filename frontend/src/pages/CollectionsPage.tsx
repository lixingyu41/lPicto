import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { ChevronDown, ChevronRight, Trash2 } from 'lucide-react';
import { api } from '../api/client';
import AssetGrid from '../components/AssetGrid';
import { CompactAssetGroupingControls, normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarFilterIconRow, SidebarMediaTypeList, SidebarOrientationFilter, SidebarRatingFilter } from '../components/SidebarControls';
import { CompactSortControls, isSortKey } from '../components/SortControls';
import { useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { useAssetDeletedEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type { AITagSummary, Asset, AssetDeletedEvent, AssetKind, AssetRating, Collection, CollectionRule, LibraryAnchor, OrientationFilter, SortKey } from '../types/api';
import { parseAssetGroupMode, serverGroupForMode, type AssetGroupMode } from '../utils/assetGrouping';
import { removeAssetById } from '../utils/assetSort';
import {
  appendViewerReturnParams,
  decodeReturnState,
  loadPageState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { assetRatingParam, currentURLHasParam, currentURLLocation, currentURLPath, orientationParam, replaceURLState } from '../utils/urlState';
import { waterfallPageSize } from '../utils/waterfallPaging';

const pageSize = waterfallPageSize;
const collectionsStateKey = 'collections';
const collectionsURLKeys = ['collection', 'sort', 'group', 'q', 'type', 'orientation', 'rating', 'tags'];

interface CollectionsPageState extends GridReturnState {
  groupMode: AssetGroupMode;
  orientation: OrientationFilter;
  query: string;
  rating: AssetRating;
  selectedCollectionId: string;
  selectedTags: string[];
  sort: SortKey;
  type: AssetKind;
}

const defaultCollectionsState: CollectionsPageState = {
  ...resetGridState(),
  groupMode: 'none',
  orientation: 'all',
  query: '',
  rating: 0,
  selectedCollectionId: 'unclassified',
  selectedTags: [],
  sort: 'timeline_desc',
  type: 'all',
};

export default function CollectionsPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const persistedState = loadPageState<CollectionsPageState>(collectionsStateKey, defaultCollectionsState);
  const initialStateRef = useRef(initialCollectionsState(searchParams, persistedState));
  const [collections, setCollections] = useState<Collection[]>([]);
  const [collectionsError, setCollectionsError] = useState<string | null>(null);
	const [aiTags, setAITags] = useState<AITagSummary[]>([]);
	const [aiTagQuery, setAITagQuery] = useState('');
	const [aiTagCardCollapsed, setAITagCardCollapsed] = useState(false);
  const [systemCollectionsCollapsed, setSystemCollectionsCollapsed] = useState(false);
  const [selectedCollectionId, setSelectedCollectionId] = useState(initialStateRef.current.selectedCollectionId);
  const [selectedTags, setSelectedTags] = useState(initialStateRef.current.selectedTags);
  const previousCollectionIdRef = useRef(initialStateRef.current.selectedCollectionId === 'tags' ? 'unclassified' : initialStateRef.current.selectedCollectionId);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [type, setType] = useState<AssetKind>(initialStateRef.current.type);
  const [orientation, setOrientation] = useState<OrientationFilter>(initialStateRef.current.orientation);
  const [rating, setRating] = useState<AssetRating>(initialStateRef.current.rating);
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [refreshRevision, setRefreshRevision] = useState(0);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const [newCollectionName, setNewCollectionName] = useState('');
  const [creatingCollection, setCreatingCollection] = useState(false);
  const sidebarState = useSidebarReturnState();
  const serverGroup = serverGroupForMode(groupMode);
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const selectedCollection = useMemo(
	() => collections.find((collection) => collection.id === selectedCollectionId) ?? dynamicTagCollection(selectedCollectionId, selectedTags),
    [collections, selectedCollectionId, selectedTags],
  );
  const preserveMissingAssets = selectedCollectionId === 'missing' || selectedCollection?.systemKind === 'missing';
  const forceDuplicateGrouping = selectedCollectionId === 'duplicates' || selectedCollection?.systemKind === 'duplicates';
  const autoSelectDuplicateAssets = useCallback(async () => {
    const result = await api.duplicateSelection();
    return result.assetIds;
  }, []);

  const loadCollections = useCallback(async () => {
    try {
      const result = await api.collections();
      setCollections(result.items ?? []);
      setCollectionsError(null);
		if (result.items.length > 0 && !isDynamicTagCollection(selectedCollectionId) && !result.items.some((item) => item.id === selectedCollectionId)) {
        setSelectedCollectionId(result.items[0].id);
      }
    } catch (err) {
      setCollections([]);
      setCollectionsError(err instanceof Error ? err.message : '集合加载失败');
    }
  }, [selectedCollectionId]);

  useEffect(() => {
    void loadCollections();
  }, [loadCollections]);

	useEffect(() => {
		let live = true;
		const timer = window.setTimeout(() => void api.aiTags(aiTagQuery).then((result) => { if (live) setAITags(result.items ?? []); }).catch(() => { if (live) setAITags([]); }), 150);
		return () => { live = false; window.clearTimeout(timer); };
	}, [aiTagQuery, refreshRevision]);

  const loadAssets = useCallback(
    (page: number) => {
      if (!selectedCollectionId) {
        return Promise.resolve({ items: [], page, pageSize, hasMore: false });
      }
      return api.collectionAssets(selectedCollectionId, page, pageSize, sort, query, serverGroup, rating, orientation, type, selectedTags);
    },
    [orientation, query, rating, selectedCollectionId, selectedTags, serverGroup, sort, type],
  );

  const { items, hasMore, hasPrevious, loading, error, loadMore, loadPrevious, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [
    selectedCollectionId,
    selectedTags,
    query,
    rating,
    serverGroup,
    sort,
    type,
    orientation,
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
    resetKey: JSON.stringify([selectedCollectionId, selectedTags, query, groupMode, sort, type, orientation, rating]),
    searchParams,
  });

  const currentPageState = useCallback(
    (): CollectionsPageState => ({
      ...getGridState(),
      groupMode,
      orientation,
      query,
      rating,
      selectedCollectionId,
      selectedTags,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sort,
      type,
    }),
    [getGridState, groupMode, orientation, query, rating, selectedCollectionId, selectedTags, sidebarState.sidebarExpanded, sort, type],
  );
  const saveCurrentState = useCallback(() => savePageState<CollectionsPageState>(collectionsStateKey, currentPageState()), [currentPageState]);
  const scheduleCurrentStateSave = usePersistentPageState(saveCurrentState);
  const handleAssetDeleted = useCallback(
    (event: AssetDeletedEvent) => {
      if (preserveMissingAssets) return;
      mutateItems((current) => removeAssetById(current, event.id));
    },
    [mutateItems, preserveMissingAssets],
  );
  useAssetDeletedEvents(handleAssetDeleted, [handleAssetDeleted]);

  const handleBatchDeleteComplete = useCallback(async () => {
    await jumpToPage(1);
    await loadCollections();
    setRefreshRevision((value) => value + 1);
  }, [jumpToPage, loadCollections]);

  useEffect(() => {
    if (currentURLHasParam(location, 'restore')) return;
    replaceURLState(
      navigate,
      location,
      { collection: selectedCollectionId, group: groupMode, orientation, q: query, rating, sort, tags: selectedTags.length > 0 ? JSON.stringify(selectedTags) : undefined, type },
      collectionsURLKeys,
    );
  }, [groupMode, location, navigate, orientation, query, rating, searchParams, selectedCollectionId, selectedTags, sort, type]);

  useEffect(() => {
    let live = true;
    async function loadAnchors() {
      if (!selectedCollectionId) {
        setAnchors([]);
        setTotalCount(0);
        return;
      }
      try {
        const result = await api.collectionAnchors(selectedCollectionId, pageSize, sort, query, serverGroup, rating, orientation, type, selectedTags);
        if (!live) return;
        setAnchors(result.items);
        setTotalCount(result.total);
      } catch {
        if (!live) return;
        setAnchors([]);
        setTotalCount(0);
      }
    }
    void loadAnchors();
    return () => {
      live = false;
    };
  }, [orientation, query, rating, refreshRevision, selectedCollectionId, selectedTags, serverGroup, sort, type]);

  useEffect(() => {
    const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, sort);
    if (nextGroupMode !== groupMode) {
      setGroupMode(nextGroupMode);
    }
  }, [groupMode, sort]);

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

  const handleSortChange = useCallback(
    (nextSort: SortKey) => {
      const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, nextSort);
      setSort(nextSort);
      if (nextGroupMode !== groupMode) {
        setGroupMode(nextGroupMode);
      }
    },
    [groupMode],
  );

  const createSmartCollection = useCallback(async () => {
    const name = newCollectionName.trim();
    if (!name || creatingCollection) return;
    const rule: CollectionRule = {
      group: serverGroup,
      orientation,
      combinedQuery: query.trim() || undefined,
      combinedTags: selectedTags.length > 0 ? JSON.stringify(selectedTags) : undefined,
      rating,
      sort,
      type,
    };
    setCreatingCollection(true);
    setCollectionsError(null);
    try {
      const created = await api.createCollection(name, rule);
      setCollections((current) => [...current.filter((item) => item.id !== created.id), created]);
      setSelectedCollectionId(created.id);
      setNewCollectionName('');
    } catch (err) {
      setCollectionsError(err instanceof Error ? err.message : '创建智能集合失败');
    } finally {
      setCreatingCollection(false);
    }
  }, [creatingCollection, newCollectionName, orientation, query, rating, selectedTags, serverGroup, sort, type]);

  const selectCollection = useCallback((id: string) => {
    if (id !== 'tags') {
      previousCollectionIdRef.current = id;
      setSelectedTags([]);
    }
    setSelectedCollectionId(id);
  }, []);

  const toggleTag = useCallback((tag: string) => {
    const next = selectedTags.includes(tag) ? selectedTags.filter((item) => item !== tag) : [...selectedTags, tag];
    if (next.length > 0) {
      if (selectedCollectionId !== 'tags') previousCollectionIdRef.current = selectedCollectionId;
      setSelectedCollectionId('tags');
    } else {
      setSelectedCollectionId(previousCollectionIdRef.current);
    }
    setSelectedTags(next);
  }, [selectedCollectionId, selectedTags]);

  const deleteManualTag = useCallback(async (item: AITagSummary) => {
    if (!item.manualTagId || !window.confirm(`删除标签“${item.tag}”？媒体上的这个手工标签也会被移除。`)) return;
    setCollectionsError(null);
    try {
      await api.deleteTag(item.manualTagId);
      if (item.aiCount === 0 && selectedTags.includes(item.tag)) {
        const next = selectedTags.filter((tag) => tag !== item.tag);
        setSelectedTags(next);
        if (next.length === 0 && selectedCollectionId === 'tags') setSelectedCollectionId(previousCollectionIdRef.current);
      }
      setRefreshRevision((value) => value + 1);
    } catch (err) {
      setCollectionsError(err instanceof Error ? err.message : '删除标签失败');
    }
  }, [selectedCollectionId, selectedTags]);

  const systemCollections = useMemo(() => collections.filter((collection) => collection.kind === 'system'), [collections]);
  const smartCollections = useMemo(() => collections.filter((collection) => collection.kind === 'smart'), [collections]);

  useSidebarPanel(
    'collections',
    <div className="sidebar-control-stack sidebar-collections-panel">
      <SidebarFilterIconRow>
        <SidebarMediaTypeList value={type} onChange={setType} />
        <SidebarOrientationFilter value={orientation} onChange={setOrientation} />
        <SidebarRatingFilter value={rating} onChange={setRating} />
        <CompactAssetGroupingControls groupMode={groupMode} sort={sort} onChange={setGroupMode} />
        <CompactSortControls sort={sort} onChange={handleSortChange} />
      </SidebarFilterIconRow>
      <label className="sidebar-field">
        <span>集合内搜索</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名或 AI 描述" />
      </label>
      <CollectionSidebarGroup
        collapsed={systemCollectionsCollapsed}
        collapsible
        collections={systemCollections}
        emptyLabel="暂无系统集合"
        label="系统集合"
        selectedId={selectedCollectionId}
        onToggle={() => setSystemCollectionsCollapsed((collapsed) => !collapsed)}
        onSelect={selectCollection}
      />
	  <section className="sidebar-filter-card sidebar-ai-tag-card" aria-labelledby="collections-tags-title">
		<button
		  aria-expanded={!aiTagCardCollapsed}
		  className="sidebar-filter-card-toggle"
		  type="button"
		  onClick={() => setAITagCardCollapsed((collapsed) => !collapsed)}
		>
		  {aiTagCardCollapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
		  <span className="sidebar-control-title" id="collections-tags-title">标签</span>
		</button>
		{!aiTagCardCollapsed && (
		  <>
			<input value={aiTagQuery} onChange={(event) => setAITagQuery(event.target.value)} placeholder="搜索标签" />
			<div className="sidebar-tag-cloud">
			  {aiTags.map((item) => {
				const selected = selectedTags.includes(item.tag);
				const className = ['sidebar-tag-chip', item.aiCount > 0 ? 'ai-source' : '', selected ? 'active' : ''].filter(Boolean).join(' ');
				const title = item.aiCount > 0 ? (item.manualAdded ? `AI 标签；手动添加：${item.tag}` : `AI 标签：${item.tag}`) : `手动添加：${item.tag}`;
				return (
				  <div className={className} key={item.tag} title={title}>
					<button aria-pressed={selected} className="sidebar-tag-select" type="button" onClick={() => toggleTag(item.tag)}>
					  <span>{item.tag}</span><small>{item.count}</small>
					</button>
					{item.manualTagId && <button aria-label={`删除标签 ${item.tag}`} className="sidebar-tag-delete" title={`删除标签 ${item.tag}`} type="button" onClick={() => void deleteManualTag(item)}><Trash2 size={12} /></button>}
				  </div>
				);
			  })}
			</div>
		  </>
		)}
	  </section>
      <CollectionSidebarGroup
        collections={smartCollections}
        emptyLabel="暂无智能集合"
        label="智能集合"
        selectedId={selectedCollectionId}
        onSelect={selectCollection}
      />
      <label className="sidebar-field">
        <span>新建智能集合</span>
        <input value={newCollectionName} onChange={(event) => setNewCollectionName(event.target.value)} placeholder="名称" />
      </label>
      {newCollectionName.trim() && (
        <button className="sidebar-command" disabled={creatingCollection} type="button" onClick={() => void createSmartCollection()}>
          <span>{creatingCollection ? '创建中' : '保存当前规则'}</span>
        </button>
      )}
      {collectionsError && <div className="error-line">{collectionsError}</div>}
    </div>,
    [
      collectionsError,
		aiTags,
		aiTagQuery,
		aiTagCardCollapsed,
      deleteManualTag,
      createSmartCollection,
      creatingCollection,
      groupMode,
      handleSortChange,
      newCollectionName,
      orientation,
      query,
      rating,
      selectedCollectionId,
      selectedTags,
      selectCollection,
      smartCollections,
      sort,
      systemCollections,
      systemCollectionsCollapsed,
      type,
      toggleTag,
    ],
  );

  useSidebarPanel(
    'viewer',
    pressPreviewAsset ? <AssetInfoPanel asset={pressPreviewAsset} title="快速预览" /> : null,
    [pressPreviewAsset?.id],
  );

  return (
    <section className="page media-page collections-page">
      {(error || collectionsError) && <div className="error-line">{error ?? collectionsError}</div>}
      {!selectedCollectionId ? (
        <EmptyState text="没有可用集合" />
      ) : items.length === 0 && !loading ? (
        <EmptyState text={selectedCollection ? `${selectedCollection.name} 没有匹配资源` : '没有匹配资源'} />
      ) : (
        <div className="library-grid-shell">
          <AssetGrid
            key={selectedCollectionId}
            assets={items}
            loading={loading}
            hasMore={hasMore}
            hasPrevious={hasPrevious}
            onLoadMore={loadMore}
            onLoadPrevious={loadPreviousPage}
            onOpenAsset={handleOpenAsset}
            onOpenViewer={handleOpenViewer}
            onBatchRemoveAssets={(ids) => mutateItems((current) => current.filter((asset) => !ids.includes(asset.id)))}
            onBatchDeleteComplete={handleBatchDeleteComplete}
            purgeUnavailableOnDelete={preserveMissingAssets}
            onPressPreviewChange={setPressPreviewAsset}
            onScrollRatioChange={setScrollRatio}
            onScrollStateChange={handlePersistentGridScrollState}
            totalCount={totalCount}
            loadedStartIndex={loadedStartIndex}
            focusAssetId={focusAssetId}
            groupMode={groupMode}
            duplicateGrouping={forceDuplicateGrouping}
            autoSelectAssetIds={forceDuplicateGrouping ? autoSelectDuplicateAssets : undefined}
            sort={sort}
            onSortChange={handleSortChange}
            selectedTags={selectedTags}
            onTagFilterChange={setSelectedTags}
            scrollSignal={scrollResetSignal}
            scrollTarget={scrollTarget}
            scrollTopTarget={scrollTopTarget}
            buildViewerUrl={(asset) => buildViewerUrl(asset, selectedCollectionId, selectedTags, query, serverGroup, sort, type, orientation, rating, currentPageReturnPath(), currentPageState())}
          />
          {!forceDuplicateGrouping && groupMode !== 'folder' && (
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
    </section>
  );
}

function CollectionSidebarGroup({
  collapsed = false,
  collapsible = false,
  collections,
  emptyLabel,
  label,
  onSelect,
  selectedId,
  onToggle,
}: {
  collapsed?: boolean;
  collapsible?: boolean;
  collections: Collection[];
  emptyLabel: string;
  label: string;
  onSelect: (id: string) => void;
  selectedId: string;
  onToggle?: () => void;
}) {
  if (collapsible) {
    return (
      <section className="sidebar-filter-card">
        <button aria-expanded={!collapsed} className="sidebar-filter-card-toggle" type="button" onClick={onToggle}>
          {collapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
          <span className="sidebar-control-title">{label}</span>
        </button>
        {!collapsed && <CollectionSidebarRows collections={collections} emptyLabel={emptyLabel} onSelect={onSelect} selectedId={selectedId} />}
      </section>
    );
  }
  return (
    <div className="sidebar-group-section">
      <div className="sidebar-control-subtitle">{label}</div>
      <CollectionSidebarRows collections={collections} emptyLabel={emptyLabel} onSelect={onSelect} selectedId={selectedId} />
    </div>
  );
}

function CollectionSidebarRows({ collections, emptyLabel, onSelect, selectedId }: { collections: Collection[]; emptyLabel: string; onSelect: (id: string) => void; selectedId: string }) {
  if (collections.length === 0) return <div className="sidebar-empty-line">{emptyLabel}</div>;
  return <>{collections.map((collection) => (
    <button aria-current={selectedId === collection.id ? 'page' : undefined} className={selectedId === collection.id ? 'sidebar-list-row active' : 'sidebar-list-row'} key={collection.id} title={collection.description ?? collection.name} type="button" onClick={() => onSelect(collection.id)}>
      <span className="sidebar-list-marker" aria-hidden="true" />
      <span>{collection.name}</span>
      {typeof collection.assetCount === 'number' && <small>{collection.assetCount}</small>}
    </button>
  ))}</>;
}

function initialCollectionsState(searchParams: URLSearchParams, persistedState: CollectionsPageState): CollectionsPageState {
  if (searchParams.has('restore')) {
    return normalizeTagSelection(decodeReturnState<CollectionsPageState>(searchParams.get('restore'), persistedState));
  }
  if (!collectionsURLKeys.some((key) => searchParams.has(key))) {
    return normalizeTagSelection(persistedState);
  }
  const sort = searchParams.get('sort');
  return normalizeTagSelection({
    ...defaultCollectionsState,
    groupMode: parseAssetGroupMode(searchParams.get('group'), defaultCollectionsState.groupMode),
    orientation: orientationParam(searchParams.get('orientation')),
    query: searchParams.get('q') ?? '',
    rating: assetRatingParam(searchParams.get('rating')) ?? defaultCollectionsState.rating,
    selectedCollectionId: searchParams.get('collection') ?? defaultCollectionsState.selectedCollectionId,
    sort: isSortKey(sort) ? sort : defaultCollectionsState.sort,
    type: assetKindParam(searchParams.get('type')),
  }, searchParams.has('tags') ? searchParams.get('tags') : undefined);
}

function buildViewerUrl(
  asset: Asset,
  collectionId: string,
  selectedTags: string[],
  query: string,
  group: string | undefined,
  sort: SortKey,
  type: AssetKind,
  orientation: OrientationFilter,
  rating: AssetRating,
  returnPath: string,
  state: CollectionsPageState,
) {
  const params = new URLSearchParams({ collectionId, context: 'collection', sort });
	if (query.trim()) params.set('combinedQuery', query.trim());
	if (selectedTags.length > 0) params.set('combinedTags', JSON.stringify(selectedTags));
	if (collectionId.startsWith('tag:')) params.set('combinedTag', collectionId.slice(4));
	if (collectionId.startsWith('ai-tag:')) params.set('aiTag', collectionId.slice(7));
  if (group) params.set('group', group);
  if (type !== 'all') params.set('type', type);
  if (orientation !== 'all') params.set('orientation', orientation);
  params.set('rating', String(rating));
  return appendViewerReturnParams(`/viewer/${asset.id}?${params.toString()}`, returnPath, state);
}

function isDynamicTagCollection(id: string) {
  return id === 'tags' || id.startsWith('tag:') || id.startsWith('ai-tag:');
}

function dynamicTagCollection(id: string, selectedTags: string[]): Collection | null {
  if (id === 'tags') return { id, name: selectedTags.length > 0 ? selectedTags.join('、') : '标签', kind: 'smart' };
  if (id.startsWith('tag:')) return { id, name: id.slice(4), kind: 'smart' };
  if (id.startsWith('ai-tag:')) return { id, name: id.slice(7), kind: 'smart' };
  return null;
}

function normalizeTagSelection(state: CollectionsPageState, explicitTags?: string | null): CollectionsPageState {
  let selectedTags = explicitTags !== undefined ? parseSelectedTags(explicitTags) : parseSelectedTags(JSON.stringify(Array.isArray(state.selectedTags) ? state.selectedTags : []));
  let selectedCollectionId = state.selectedCollectionId;
  if (selectedCollectionId.startsWith('tag:')) selectedTags = [selectedCollectionId.slice(4)];
  if (selectedCollectionId.startsWith('ai-tag:')) selectedTags = [selectedCollectionId.slice(7)];
  if (selectedTags.length > 0) selectedCollectionId = 'tags';
  if (selectedCollectionId === 'tags' && selectedTags.length === 0) selectedCollectionId = defaultCollectionsState.selectedCollectionId;
  return { ...state, selectedCollectionId, selectedTags };
}

function parseSelectedTags(raw: string | null): string[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return [...new Set(parsed.map((value) => String(value).trim()).filter((value) => value && [...value].length <= 80))].slice(0, 32);
  } catch {
    return [];
  }
}

function assetKindParam(value: string | null): AssetKind {
  return value === 'image' || value === 'video' ? value : 'all';
}
