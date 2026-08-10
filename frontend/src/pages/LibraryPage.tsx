import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { ChevronDown, ChevronRight, RotateCcw, Save } from 'lucide-react';
import { api } from '../api/client';
import AssetGrid from '../components/AssetGrid';
import { CompactAssetGroupingControls, normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import NFOSearchFilters from '../components/NFOSearchFilters';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarAlbumList, SidebarFilterIconRow, SidebarMediaTypeList, SidebarOrientationFilter, SidebarRatingFilter } from '../components/SidebarControls';
import { CompactSortControls, isSortKey } from '../components/SortControls';
import { useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { useAssetReadyEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type {
  Album,
  AlbumGroup,
  Asset,
  AssetDeletedEvent,
  AssetKind,
  AssetRatingFilter,
  LibraryAnchor,
  NFOFilterField,
  OrientationFilter,
  LibraryFilterParams,
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
  useViewerAwareMediaState,
} from '../utils/pageState';
import { parseAssetGroupMode, serverGroupForMode, type AssetGroupMode } from '../utils/assetGrouping';
import { removeAssetById } from '../utils/assetSort';
import { assetRatingParam, currentURLHasParam, currentURLLocation, currentURLPath, replaceURLState } from '../utils/urlState';
import { waterfallPageSize } from '../utils/waterfallPaging';
import { parseTagFilters, serializeTagFilters } from '../utils/tagFilters';

const pageSize = waterfallPageSize;
const libraryURLKeys = [
  'q',
	'visible',
	'aiDescription',
	'aiTag',
  'combinedTags',
  'tagNodes',
  'nfo',
  'nfoActor',
  'nfoId',
  'nfoTag',
  'nfoTitle',
  'nfoYear',
  'type',
  'sort',
  'rating',
  'orientation',
  'albumFilter',
  'albumIds',
  'albumId',
  'album',
  'group',
  'widthMin',
  'widthMax',
  'heightMin',
  'heightMax',
  'durationMin',
  'durationMax',
  'sizeMin',
  'sizeMax',
  'from',
  'to',
];

type SearchAlbumFilterMode = 'all' | 'none' | 'albums';
type DurationUnit = 'seconds' | 'minutes' | 'hours';

interface LibraryPageState extends GridReturnState {
	aiDescriptionQuery: string;
  albumFilterMode: SearchAlbumFilterMode;
  albumFilterCardCollapsed: boolean;
  albumIds: number[];
  collapsedGroupKeys: string[];
  dateFrom: string;
  dateTo: string;
  durationMaxMinutes: string;
  durationMinMinutes: string;
  durationRange?: string;
  durationUnit: DurationUnit;
  nfoActorQuery: string;
  nfoIDQuery: string;
  nfoQuery: string;
  nfoTagQuery: string;
  nfoTitleQuery: string;
  nfoYearQuery: string;
  orientation: OrientationFilter;
  groupMode: AssetGroupMode;
  query: string;
  rating: AssetRatingFilter;
  resolutionRange?: string;
  resolutionXRange: string;
  resolutionYRange: string;
  sizeMaxMB: string;
  sizeMinMB: string;
  sort: SortKey;
  searchCardCollapsed: boolean;
  tagFilters: string[];
  type: AssetKind;
}

const defaultLibraryState: LibraryPageState = {
  ...resetGridState(),
	aiDescriptionQuery: '',
  albumFilterMode: 'all',
  albumFilterCardCollapsed: false,
  albumIds: [],
  collapsedGroupKeys: [],
  dateFrom: '',
  dateTo: '',
  durationMaxMinutes: '',
  durationMinMinutes: '',
  durationRange: '',
  durationUnit: 'minutes',
  nfoActorQuery: '',
  nfoIDQuery: '',
  nfoQuery: '',
  nfoTagQuery: '',
  nfoTitleQuery: '',
  nfoYearQuery: '',
  orientation: 'all',
  groupMode: 'none',
  query: '',
  rating: 'all',
  resolutionRange: '',
  resolutionXRange: '',
  resolutionYRange: '',
  sizeMaxMB: '',
  sizeMinMB: '',
  sort: 'timeline_desc',
  searchCardCollapsed: false,
  tagFilters: [],
  type: 'all',
};

export default function LibraryPage({ mode = 'library' }: { mode?: 'library' | 'recent' }) {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const recentMode = mode === 'recent';
  const pageStateKey = recentMode ? 'recent' : 'library';
  const defaultPageState = useMemo<LibraryPageState>(
    () => (recentMode ? { ...defaultLibraryState, sort: 'last_played_desc' } : defaultLibraryState),
    [recentMode],
  );
  const persistedState = loadPageState<LibraryPageState>(pageStateKey, defaultPageState);
  const initialStateRef = useRef(
    initialLibraryState(searchParams, persistedState, defaultPageState),
  );
  const initialResolution = initialResolutionRanges(initialStateRef.current);
  const initialDuration = initialDurationRanges(initialStateRef.current);
  const [query, setQuery] = useViewerAwareMediaState(initialStateRef.current.query);
	const [aiDescriptionQuery, setAIDescriptionQuery] = useViewerAwareMediaState(initialStateRef.current.aiDescriptionQuery ?? '');
  const [nfoQuery, setNFOQuery] = useViewerAwareMediaState(initialStateRef.current.nfoQuery);
  const [nfoActorQuery, setNFOActorQuery] = useViewerAwareMediaState(initialStateRef.current.nfoActorQuery ?? '');
  const [nfoIDQuery, setNFOIDQuery] = useViewerAwareMediaState(initialStateRef.current.nfoIDQuery ?? '');
  const [nfoTagQuery, setNFOTagQuery] = useViewerAwareMediaState(initialStateRef.current.nfoTagQuery ?? '');
  const [nfoTitleQuery, setNFOTitleQuery] = useViewerAwareMediaState(initialStateRef.current.nfoTitleQuery ?? '');
  const [nfoYearQuery, setNFOYearQuery] = useViewerAwareMediaState(initialStateRef.current.nfoYearQuery ?? '');
  const [type, setType] = useViewerAwareMediaState<AssetKind>(initialStateRef.current.type);
  const [rating, setRating] = useViewerAwareMediaState<AssetRatingFilter>(initialStateRef.current.rating ?? 'all');
  const [sort, setSort] = useViewerAwareMediaState<SortKey>(initialStateRef.current.sort);
  const [resolutionXRange, setResolutionXRange] = useViewerAwareMediaState(initialResolution.x);
  const [resolutionYRange, setResolutionYRange] = useViewerAwareMediaState(initialResolution.y);
  const [dateFrom, setDateFrom] = useViewerAwareMediaState(initialStateRef.current.dateFrom);
  const [dateTo, setDateTo] = useViewerAwareMediaState(initialStateRef.current.dateTo);
  const [durationMinMinutes, setDurationMinMinutes] = useViewerAwareMediaState(initialDuration.min);
  const [durationMaxMinutes, setDurationMaxMinutes] = useViewerAwareMediaState(initialDuration.max);
  const [durationUnit, setDurationUnit] = useViewerAwareMediaState<DurationUnit>(initialStateRef.current.durationUnit);
  const [orientation, setOrientation] = useViewerAwareMediaState<OrientationFilter>(initialStateRef.current.orientation);
  const [albumFilterMode, setAlbumFilterMode] = useViewerAwareMediaState<SearchAlbumFilterMode>(initialStateRef.current.albumFilterMode);
  const [albumFilterCardCollapsed, setAlbumFilterCardCollapsed] = useState(initialStateRef.current.albumFilterCardCollapsed ?? false);
  const [albumIds, setAlbumIds] = useViewerAwareMediaState<number[]>(initialStateRef.current.albumIds);
  const [collapsedGroupKeys, setCollapsedGroupKeys] = useState<Set<string>>(() => new Set(initialStateRef.current.collapsedGroupKeys));
  const [albums, setAlbums] = useState<Album[]>([]);
  const [groups, setGroups] = useState<AlbumGroup[]>([]);
  const [groupMode, setGroupMode] = useViewerAwareMediaState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [sizeMinMB, setSizeMinMB] = useViewerAwareMediaState(initialStateRef.current.sizeMinMB);
  const [sizeMaxMB, setSizeMaxMB] = useViewerAwareMediaState(initialStateRef.current.sizeMaxMB);
  const [searchCardCollapsed, setSearchCardCollapsed] = useState(initialStateRef.current.searchCardCollapsed ?? false);
  const [tagFilters, setTagFilters] = useViewerAwareMediaState(initialStateRef.current.tagFilters ?? []);
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const [smartCollectionName, setSmartCollectionName] = useState('');
  const [savingSmartCollection, setSavingSmartCollection] = useState(false);
  const [smartCollectionError, setSmartCollectionError] = useState<string | null>(null);
  useEffect(() => {
    if (type !== 'audio') return;
    setOrientation('all');
    setResolutionXRange('');
    setResolutionYRange('');
  }, [type]);
  const sidebarState = useSidebarReturnState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const serverGroup = serverGroupForMode(groupMode);
  const activeAlbumIds = useMemo(() => (albumFilterMode === 'albums' ? albumIds : []), [albumFilterMode, albumIds]);
  const albumIdsParam = activeAlbumIds.length > 0 ? activeAlbumIds.join(',') : undefined;
	const includeUnavailable = searchParams.get('visible') === 'all';

  const searchRequest = useMemo<LibraryFilterParams>(
    () => ({
      albumFilter: albumFilterMode === 'none' ? 'none' : undefined,
      playedOnly: recentMode ? 1 : undefined,
      albumIds: albumIdsParam,
      q: query.trim() || undefined,
		visible: includeUnavailable ? 'all' : undefined,
		aiDescription: aiDescriptionQuery.trim() || undefined,
		aiTag: searchParams.get('aiTag')?.trim() || undefined,
      tagNodes: serializeTagFilters(tagFilters),
      nfo: nfoQuery.trim() || undefined,
      nfoActor: nfoActorQuery.trim() || undefined,
      nfoId: nfoIDQuery.trim() || undefined,
      nfoTag: nfoTagQuery.trim() || undefined,
      nfoTitle: nfoTitleQuery.trim() || undefined,
      nfoYear: nfoYearQuery.trim() || undefined,
      rating: rating === 'all' ? undefined : rating,
      type,
      sort,
      ...parseResolutionRanges(resolutionXRange, resolutionYRange, orientation),
      from: datetimeLocalToUnix(dateFrom),
      to: datetimeLocalToUnix(dateTo),
      ...parseDurationRange(durationMinMinutes, durationMaxMinutes, durationUnit),
      orientation,
      group: serverGroup,
      sizeMin: mbToBytes(sizeMinMB),
      sizeMax: mbToBytes(sizeMaxMB),
    }),
    [
      dateFrom,
      recentMode,
		includeUnavailable,
		aiDescriptionQuery,
      dateTo,
      albumFilterMode,
      albumIdsParam,
      durationMaxMinutes,
      durationMinMinutes,
      durationUnit,
      nfoActorQuery,
      nfoIDQuery,
      nfoQuery,
      nfoTagQuery,
      nfoTitleQuery,
      nfoYearQuery,
      orientation,
      query,
      rating,
      resolutionXRange,
      resolutionYRange,
      serverGroup,
      sizeMaxMB,
      sizeMinMB,
      sort,
      tagFilters,
      type,
    ],
  );

  useEffect(() => {
    let live = true;
    async function loadAlbums() {
      try {
        const result = await api.albums();
        if (!live) return;
        setAlbums(result.items);
        setGroups(result.groups ?? []);
      } catch {
        if (!live) return;
        setAlbums([]);
        setGroups([]);
      }
    }
    void loadAlbums();
    return () => {
      live = false;
    };
  }, []);
  const nfoOptionQueries = useMemo<Record<NFOFilterField, string>>(
    () => ({
      actor: nfoActorQuery,
      id: nfoIDQuery,
      tag: nfoTagQuery,
      title: nfoTitleQuery,
      year: nfoYearQuery,
    }),
    [nfoActorQuery, nfoIDQuery, nfoTagQuery, nfoTitleQuery, nfoYearQuery],
  );
  const searchKey = useMemo(() => JSON.stringify(searchRequest), [searchRequest]);
  const anchorSearchRequest = useMemo<LibraryFilterParams>(() => ({ ...searchRequest }), [searchRequest]);
  const loadAssets = useCallback((page: number) => api.libraryAssets(page, pageSize, searchRequest), [searchRequest]);
  const { items, hasMore, hasPrevious, loading, error, loadMore, loadPrevious, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [searchKey]);
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
    resetKey: searchKey,
    searchParams,
  });
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((current) => removeAssetById(current, event.id)), [mutateItems]);
  const handleAssetReady = useCallback(() => {
    void jumpToPage(Math.floor(loadedStartIndex / pageSize) + 1);
  }, [jumpToPage, loadedStartIndex]);
  useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    let live = true;
    async function loadAnchors() {
      try {
        const result = await api.libraryAnchors(pageSize, anchorSearchRequest);
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
  }, [anchorSearchRequest]);

  const currentPageState = useCallback(
    (): LibraryPageState => ({
      ...getGridState(),
      albumFilterMode,
		aiDescriptionQuery,
      albumFilterCardCollapsed,
      albumIds,
      collapsedGroupKeys: Array.from(collapsedGroupKeys),
      dateFrom,
      dateTo,
      durationMaxMinutes,
      durationMinMinutes,
      durationUnit,
      nfoActorQuery,
      nfoIDQuery,
      nfoQuery,
      nfoTagQuery,
      nfoTitleQuery,
      nfoYearQuery,
      orientation,
      groupMode,
      query,
      rating,
      resolutionXRange,
      resolutionYRange,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sizeMaxMB,
      sizeMinMB,
      sort,
      searchCardCollapsed,
      tagFilters,
      type,
    }),
    [
      albumFilterMode,
      albumFilterCardCollapsed,
      albumIds,
      collapsedGroupKeys,
      dateFrom,
      dateTo,
      durationMaxMinutes,
      durationMinMinutes,
      durationUnit,
      getGridState,
      groupMode,
      nfoActorQuery,
      nfoIDQuery,
      nfoQuery,
      nfoTagQuery,
      nfoTitleQuery,
      nfoYearQuery,
      orientation,
      query,
      rating,
      resolutionXRange,
      resolutionYRange,
      sidebarState.sidebarExpanded,
      sizeMaxMB,
      sizeMinMB,
      sort,
      searchCardCollapsed,
      tagFilters,
      type,
    ],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<LibraryPageState>(pageStateKey, currentPageState());
  }, [currentPageState, pageStateKey]);
  const toggleAlbumFilterCard = useCallback(() => {
    setAlbumFilterCardCollapsed((current) => {
      const next = !current;
      savePageState<LibraryPageState>(pageStateKey, { ...currentPageState(), albumFilterCardCollapsed: next });
      return next;
    });
  }, [currentPageState, pageStateKey]);
  const toggleSearchCard = useCallback(() => {
    setSearchCardCollapsed((current) => {
      const next = !current;
      savePageState<LibraryPageState>(pageStateKey, { ...currentPageState(), searchCardCollapsed: next });
      return next;
    });
  }, [currentPageState, pageStateKey]);
  const scheduleCurrentStateSave = usePersistentPageState(saveCurrentState);

  useEffect(() => {
    if (currentURLHasParam(location, 'restore')) return;
    replaceURLState(
      navigate,
      location,
      {
        durationMax: searchRequest.durationMax,
        durationMin: searchRequest.durationMin,
        from: searchRequest.from,
        group: groupMode,
        albumFilter: searchRequest.albumFilter,
        albumIds: searchRequest.albumIds,
        heightMax: searchRequest.heightMax,
        heightMin: searchRequest.heightMin,
        nfo: nfoQuery.trim(),
		aiDescription: aiDescriptionQuery.trim(),
		aiTag: searchRequest.aiTag,
        tagNodes: searchRequest.tagNodes,
        nfoActor: nfoActorQuery.trim(),
        nfoId: nfoIDQuery.trim(),
        nfoTag: nfoTagQuery.trim(),
        nfoTitle: nfoTitleQuery.trim(),
        nfoYear: nfoYearQuery.trim(),
        orientation,
        q: query.trim(),
		visible: searchRequest.visible,
        rating: rating === 'all' ? undefined : rating,
        sizeMax: searchRequest.sizeMax,
        sizeMin: searchRequest.sizeMin,
        sort,
        to: searchRequest.to,
        type,
        widthMax: searchRequest.widthMax,
        widthMin: searchRequest.widthMin,
      },
      libraryURLKeys,
    );
  }, [
    groupMode,
		aiDescriptionQuery,
    albumFilterMode,
    albumIdsParam,
    location,
    navigate,
    nfoActorQuery,
    nfoIDQuery,
    nfoQuery,
    nfoTagQuery,
    nfoTitleQuery,
    nfoYearQuery,
    orientation,
    query,
    rating,
    searchParams,
    searchRequest,
    sort,
    tagFilters,
    type,
  ]);

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

  const saveSmartCollection = useCallback(async () => {
    const name = smartCollectionName.trim();
    if (!name || savingSmartCollection) return;
    setSavingSmartCollection(true);
    setSmartCollectionError(null);
    try {
      await api.createCollection(name, searchRequest);
      setSmartCollectionName('');
    } catch (err) {
      setSmartCollectionError(err instanceof Error ? err.message : '保存智能集合失败');
    } finally {
      setSavingSmartCollection(false);
    }
  }, [savingSmartCollection, searchRequest, smartCollectionName]);

  useEffect(() => {
    const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, sort);
    if (nextGroupMode !== groupMode) {
      setGroupMode(nextGroupMode);
    }
  }, [groupMode, sort]);

  const resetFilters = useCallback(() => {
    setQuery('');
		setAIDescriptionQuery('');
    setNFOQuery('');
    setNFOActorQuery('');
    setNFOIDQuery('');
    setNFOTagQuery('');
    setNFOTitleQuery('');
    setNFOYearQuery('');
    setType('all');
    setRating('all');
    setAlbumFilterMode('all');
    setAlbumIds([]);
    setCollapsedGroupKeys(new Set());
    setSort(recentMode ? 'last_played_desc' : 'timeline_desc');
    setTagFilters([]);
    setResolutionXRange('');
    setResolutionYRange('');
    setDateFrom('');
    setDateTo('');
    setDurationMinMinutes('');
    setDurationMaxMinutes('');
    setOrientation('all');
    setGroupMode('none');
    setSizeMinMB('');
    setSizeMaxMB('');
  }, [recentMode]);

  const handleRatingChange = useCallback((nextRating: AssetRatingFilter) => {
    setRating(nextRating);
  }, []);

  const handleDurationUnitChange = useCallback((nextUnit: DurationUnit) => {
    if (nextUnit === durationUnit) return;
    setDurationMinMinutes(convertDurationValue(durationMinMinutes, durationUnit, nextUnit));
    setDurationMaxMinutes(convertDurationValue(durationMaxMinutes, durationUnit, nextUnit));
    setDurationUnit(nextUnit);
  }, [durationMaxMinutes, durationMinMinutes, durationUnit]);

  const handleSelectAllAlbums = useCallback(() => {
    setAlbumFilterMode('all');
    setAlbumIds([]);
  }, []);

  const handleSelectUnassignedAlbums = useCallback(() => {
    setAlbumFilterMode('none');
    setAlbumIds([]);
  }, []);

  const handleToggleAlbum = useCallback(
    (album: Album) => {
      const nextAlbumIds = albumIds.includes(album.id) ? albumIds.filter((id) => id !== album.id) : [...albumIds, album.id];
      setAlbumIds(nextAlbumIds);
      setAlbumFilterMode(nextAlbumIds.length > 0 ? 'albums' : 'all');
    },
    [albumIds],
  );

  const handleToggleAlbumGroup = useCallback((key: string) => {
    setCollapsedGroupKeys((current) => {
      const next = new Set(current);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  const setNFOFieldQuery = useCallback((field: NFOFilterField, value: string) => {
    switch (field) {
      case 'actor':
        setNFOActorQuery(value);
        return;
      case 'id':
        setNFOIDQuery(value);
        return;
      case 'tag':
        setNFOTagQuery(value);
        return;
      case 'title':
        setNFOTitleQuery(value);
        return;
      case 'year':
        setNFOYearQuery(value);
        return;
    }
  }, []);

  useSidebarPanel(
    recentMode ? 'recent' : 'library',
    <div className="sidebar-control-stack sidebar-library-panel">
      <SidebarFilterIconRow>
        <SidebarMediaTypeList value={type} onChange={setType} />
        <SidebarOrientationFilter value={orientation} onChange={setOrientation} />
        <SidebarRatingFilter value={rating} onChange={handleRatingChange} />
        <CompactAssetGroupingControls groupMode={groupMode} sort={sort} onChange={setGroupMode} />
        <CompactSortControls sort={sort} onChange={setSort} />
      </SidebarFilterIconRow>
      <section className="sidebar-filter-card sidebar-album-filter-card" aria-labelledby="library-album-filter-title">
        <button
          aria-expanded={!albumFilterCardCollapsed}
          className="sidebar-filter-card-toggle"
          type="button"
          onClick={toggleAlbumFilterCard}
        >
          {albumFilterCardCollapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
          <span className="sidebar-control-title" id="library-album-filter-title">相册筛选</span>
        </button>
        {!albumFilterCardCollapsed && (
          <SidebarAlbumList
            albums={albums}
            collapsedGroupKeys={Array.from(collapsedGroupKeys)}
            forceGroupHeaders
            groups={groups}
            selectedIds={albumFilterMode === 'albums' ? albumIds : []}
            showAll
            showLabel={false}
            showUnassigned
            allActive={albumFilterMode === 'all'}
            unassignedActive={albumFilterMode === 'none'}
            onSelectAll={handleSelectAllAlbums}
            onSelectUnassigned={handleSelectUnassignedAlbums}
            onSelectAlbum={handleToggleAlbum}
            onToggleGroup={handleToggleAlbumGroup}
          />
        )}
      </section>
      <section className="sidebar-filter-card sidebar-search-card" aria-labelledby="library-search-title">
            <button
              aria-expanded={!searchCardCollapsed}
              className="sidebar-filter-card-toggle"
              type="button"
              onClick={toggleSearchCard}
            >
              {searchCardCollapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
              <span className="sidebar-control-title" id="library-search-title">搜索</span>
            </button>
            {!searchCardCollapsed && (
              <>
            <div className="sidebar-reset-row">
              <button className="sidebar-command" type="button" title="重置" aria-label="重置" onClick={resetFilters}>
                <RotateCcw size={15} />
                <span>重置</span>
              </button>
            </div>
            <label className="sidebar-field">
              <span>文件名</span>
              <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名" />
            </label>
			<label className="sidebar-field">
			  <span>AI 描述</span>
			  <input value={aiDescriptionQuery} onChange={(event) => setAIDescriptionQuery(event.target.value)} placeholder="包含或模糊匹配" />
			</label>
            <NFOSearchFilters
              nfoQuery={nfoQuery}
              nfoOptionQueries={nfoOptionQueries}
              onNFOQueryChange={setNFOQuery}
              onNFOFieldQueryChange={setNFOFieldQuery}
            />
            <label className="sidebar-field">
              <span>分辨率</span>
              <div className="sidebar-field-grid">
                <input value={resolutionXRange} onChange={(event) => setResolutionXRange(event.target.value)} placeholder="X 100-4000" />
                <input value={resolutionYRange} onChange={(event) => setResolutionYRange(event.target.value)} placeholder="Y 100-3000" />
              </div>
            </label>
            <div className="sidebar-field-grid">
              <label className="sidebar-field">
                <span>起始时间</span>
                <input type="datetime-local" value={dateFrom} onChange={(event) => setDateFrom(event.target.value)} />
              </label>
              <label className="sidebar-field">
                <span>结束时间</span>
                <input type="datetime-local" value={dateTo} onChange={(event) => setDateTo(event.target.value)} />
              </label>
            </div>
            <div className="sidebar-field">
              <span>时长单位</span>
              <div className="sidebar-segmented" role="group" aria-label="视频时长单位">
                {([
                  ['seconds', '秒'],
                  ['minutes', '分钟'],
                  ['hours', '小时'],
                ] as const).map(([unit, label]) => (
                  <button
                    className={durationUnit === unit ? 'active' : ''}
                    type="button"
                    aria-pressed={durationUnit === unit}
                    key={unit}
                    onClick={() => handleDurationUnitChange(unit)}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
            <div className="sidebar-field-grid">
              <label className="sidebar-field">
                <span>最短时长</span>
                <input inputMode="decimal" value={durationMinMinutes} onChange={(event) => setDurationMinMinutes(event.target.value)} />
              </label>
              <label className="sidebar-field">
                <span>最长时长</span>
                <input inputMode="decimal" value={durationMaxMinutes} onChange={(event) => setDurationMaxMinutes(event.target.value)} />
              </label>
            </div>
            <div className="sidebar-field-grid">
              <label className="sidebar-field">
                <span>最小 MB</span>
                <input inputMode="decimal" value={sizeMinMB} onChange={(event) => setSizeMinMB(event.target.value)} />
              </label>
              <label className="sidebar-field">
                <span>最大 MB</span>
                <input inputMode="decimal" value={sizeMaxMB} onChange={(event) => setSizeMaxMB(event.target.value)} />
              </label>
            </div>
            <div className="sidebar-group-section">
              <div className="sidebar-control-subtitle">智能相册</div>
              <label className="sidebar-field">
                <span>保存当前搜索</span>
                <input value={smartCollectionName} onChange={(event) => setSmartCollectionName(event.target.value)} placeholder="智能相册名称" />
              </label>
              <button className="sidebar-command" disabled={!smartCollectionName.trim() || savingSmartCollection} type="button" onClick={() => void saveSmartCollection()}>
                <Save size={15} />
                <span>{savingSmartCollection ? '保存中' : '保存'}</span>
              </button>
              {smartCollectionError && <div className="error-line">{smartCollectionError}</div>}
            </div>
              </>
            )}
      </section>
    </div>,
    [
      dateFrom,
		aiDescriptionQuery,
      dateTo,
      albumFilterMode,
      albumFilterCardCollapsed,
      albumIds,
      albums,
      collapsedGroupKeys,
      durationMaxMinutes,
      durationMinMinutes,
      durationUnit,
      groupMode,
      handleDurationUnitChange,
      handleRatingChange,
      handleSelectAllAlbums,
      handleSelectUnassignedAlbums,
      handleToggleAlbum,
      handleToggleAlbumGroup,
      groups,
      nfoOptionQueries,
      saveSmartCollection,
      searchCardCollapsed,
      savingSmartCollection,
      smartCollectionError,
      smartCollectionName,
      nfoActorQuery,
      nfoIDQuery,
      nfoQuery,
      nfoTagQuery,
      nfoTitleQuery,
      nfoYearQuery,
      orientation,
      query,
      rating,
      resetFilters,
      resolutionXRange,
      resolutionYRange,
      setNFOFieldQuery,
      sizeMaxMB,
      sizeMinMB,
      sort,
      toggleAlbumFilterCard,
      toggleSearchCard,
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
      {error && <div className="error-line">{error}</div>}
      {items.length === 0 && !loading ? (
        <EmptyState text={recentMode ? '暂无播放记录' : '没有匹配资源'} />
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
            buildViewerUrl={(asset) => buildViewerUrl(asset, searchRequest, currentPageState(), currentPageReturnPath(), recentMode)}
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
    </section>
  );
}

function initialLibraryState(searchParams: URLSearchParams, persistedState: LibraryPageState, defaultPageState: LibraryPageState): LibraryPageState {
  if (searchParams.has('restore')) {
    const restored = decodeReturnState<LibraryPageState>(searchParams.get('restore'), persistedState);
    return { ...restored, durationUnit: parseDurationUnit(restored.durationUnit) };
  }
  if (!hasSearchStateParams(searchParams)) {
    return { ...persistedState, durationUnit: parseDurationUnit(persistedState.durationUnit) };
  }
  return {
    ...defaultPageState,
    albumFilterCardCollapsed: persistedState.albumFilterCardCollapsed ?? false,
    searchCardCollapsed: persistedState.searchCardCollapsed ?? false,
    ...albumFilterStateFromSearchParams(searchParams),
    query: searchParams.get('q') ?? '',
		aiDescriptionQuery: searchParams.get('aiDescription') ?? '',
    nfoQuery: searchParams.get('nfo') ?? '',
    nfoActorQuery: searchParams.get('nfoActor') ?? '',
    nfoIDQuery: searchParams.get('nfoId') ?? '',
    nfoTagQuery: searchParams.get('nfoTag') ?? '',
    nfoTitleQuery: searchParams.get('nfoTitle') ?? '',
    nfoYearQuery: searchParams.get('nfoYear') ?? '',
    type: parseAssetKindParam(searchParams.get('type')),
    sort: parseSortParam(searchParams.get('sort'), defaultPageState.sort),
    orientation: parseOrientationParam(searchParams.get('orientation')),
    rating: assetRatingParam(searchParams.get('rating')) ?? defaultLibraryState.rating,
    groupMode: parseAssetGroupMode(searchParams.get('group'), 'none'),
    resolutionXRange: rangeInputFromParams(searchParams.get('widthMin'), searchParams.get('widthMax')),
    resolutionYRange: rangeInputFromParams(searchParams.get('heightMin'), searchParams.get('heightMax')),
    durationMinMinutes: secondsParamToDurationValue(searchParams.get('durationMin'), parseDurationUnit(persistedState.durationUnit)),
    durationMaxMinutes: secondsParamToDurationValue(searchParams.get('durationMax'), parseDurationUnit(persistedState.durationUnit)),
    durationUnit: parseDurationUnit(persistedState.durationUnit),
    sizeMinMB: bytesParamToMB(searchParams.get('sizeMin')),
    sizeMaxMB: bytesParamToMB(searchParams.get('sizeMax')),
    dateFrom: unixParamToDatetimeLocal(searchParams.get('from')),
    dateTo: unixParamToDatetimeLocal(searchParams.get('to')),
    tagFilters: parseTagFilters(searchParams.get('tagNodes') ?? searchParams.get('combinedTags')),
  };
}

function hasSearchStateParams(searchParams: URLSearchParams) {
  return [
    'q',
		'visible',
		'aiDescription',
		'aiTag',
    'combinedTags',
    'tagNodes',
    'nfo',
    'nfoActor',
    'nfoId',
    'nfoTag',
    'nfoTitle',
    'nfoYear',
    'type',
    'sort',
    'rating',
    'orientation',
    'albumFilter',
    'albumIds',
    'albumId',
    'album',
    'group',
    'widthMin',
    'widthMax',
    'heightMin',
    'heightMax',
    'durationMin',
    'durationMax',
    'sizeMin',
    'sizeMax',
    'from',
    'to',
  ].some((key) => searchParams.has(key));
}

function albumFilterStateFromSearchParams(params: URLSearchParams): Pick<LibraryPageState, 'albumFilterMode' | 'albumIds'> {
  const mode = (params.get('albumFilter') ?? params.get('album') ?? '').trim().toLowerCase();
  if (mode === 'none' || mode === 'unassigned') {
    return { albumFilterMode: 'none', albumIds: [] };
  }
  const albumIds = parseAlbumIds(params.get('albumIds'));
  if (albumIds.length > 0) {
    return { albumFilterMode: 'albums', albumIds };
  }
  const singleAlbumId = positiveIntParam(params.get('albumId')) ?? positiveIntParam(params.get('album'));
  if (singleAlbumId !== null) {
    return { albumFilterMode: 'albums', albumIds: [singleAlbumId] };
  }
  return { albumFilterMode: 'all', albumIds: [] };
}

function parseAlbumIds(value: string | null) {
  if (!value) return [];
  const seen = new Set<number>();
  const result: number[] = [];
  value.split(',').forEach((part) => {
    const parsed = positiveIntParam(part);
    if (parsed === null || seen.has(parsed)) return;
    seen.add(parsed);
    result.push(parsed);
  });
  return result;
}

function positiveIntParam(value: string | null) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
}

function parseAssetKindParam(value: string | null): AssetKind {
  return value === 'image' || value === 'video' || value === 'audio' ? value : 'all';
}

function parseOrientationParam(value: string | null): OrientationFilter {
  return value === 'landscape' || value === 'portrait' ? value : 'all';
}

function parseSortParam(value: string | null, fallback: SortKey = 'timeline_desc'): SortKey {
  return isSortKey(value) ? value : fallback;
}

function rangeInputFromParams(minValue: string | null, maxValue: string | null) {
  const min = positiveNumber(minValue ?? '');
  const max = positiveNumber(maxValue ?? '');
  if (min === undefined && max === undefined) return '';
  if (min !== undefined && max !== undefined && min === max) return String(min);
  return `${min ?? ''}-${max ?? ''}`;
}

function secondsParamToDurationValue(value: string | null, unit: DurationUnit) {
  const seconds = positiveNumber(value ?? '');
  if (seconds === undefined) return '';
  return formatNumberValue(seconds / durationUnitSeconds(unit));
}

function bytesParamToMB(value: string | null) {
  const bytes = positiveNumber(value ?? '');
  if (bytes === undefined) return '';
  return formatNumberValue(bytes / (1024 * 1024));
}

function unixParamToDatetimeLocal(value: string | null) {
  const seconds = positiveNumber(value ?? '');
  if (seconds === undefined) return '';
  const date = new Date(seconds * 1000);
  if (!Number.isFinite(date.getTime())) return '';
  const pad = (part: number) => String(part).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function buildViewerUrl(asset: Asset, params: LibraryFilterParams, state: LibraryPageState, returnPath: string, recentMode: boolean) {
  const query = new URLSearchParams(searchQueryEntries(params));
  query.set('context', recentMode ? 'recent' : 'library');
  return appendViewerReturnParams(`/viewer/${asset.id}?${query.toString()}`, returnPath, state);
}

function searchQueryEntries(params: LibraryFilterParams) {
  const entries: Array<[string, string]> = [];
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === '' || (value === 'all' && key !== 'visible')) return;
    entries.push([key, String(value)]);
  });
  return entries;
}

function parseResolutionRanges(
  xValue: string,
  yValue: string,
  orientation: OrientationFilter,
): Pick<LibraryFilterParams, 'widthMin' | 'widthMax' | 'heightMin' | 'heightMax' | 'dimensionMode'> {
  const width = parseNumberRange(xValue);
  const height = parseNumberRange(yValue);
  const hasResolutionFilter = width.min !== undefined || width.max !== undefined || height.min !== undefined || height.max !== undefined;
  return {
    widthMin: width.min,
    widthMax: width.max,
    heightMin: height.min,
    heightMax: height.max,
    dimensionMode: hasResolutionFilter && orientation === 'all' ? 'both' : undefined,
  };
}

function parseDurationRange(minValue: string, maxValue: string, unit: DurationUnit): Pick<LibraryFilterParams, 'durationMin' | 'durationMax'> {
  const min = positiveNumber(minValue);
  const max = positiveNumber(maxValue);
  const multiplier = durationUnitSeconds(unit);
  return {
    durationMin: min === undefined ? undefined : roundDurationSeconds(min * multiplier),
    durationMax: max === undefined ? undefined : roundDurationSeconds(max * multiplier),
  };
}

function parseNumberRange(value: string): { min?: number; max?: number } {
  const clean = value.trim();
  if (clean === '') return {};
  const parts = clean.split('-', 2);
  if (parts.length === 1) {
    const exact = positiveNumber(parts[0]);
    return exact === undefined ? {} : { min: exact, max: exact };
  }
  return { min: positiveNumber(parts[0]), max: positiveNumber(parts[1]) };
}

function positiveNumber(value: string): number | undefined {
  const clean = value.trim();
  if (clean === '') return undefined;
  const parsed = Number(clean);
  if (!Number.isFinite(parsed) || parsed < 0) return undefined;
  return parsed;
}

function datetimeLocalToUnix(value: string): number | undefined {
  if (!value) return undefined;
  const parsed = new Date(value).getTime();
  if (!Number.isFinite(parsed)) return undefined;
  return Math.floor(parsed / 1000);
}

function mbToBytes(value: string): number | undefined {
  const parsed = positiveNumber(value);
  if (parsed === undefined) return undefined;
  return Math.round(parsed * 1024 * 1024);
}

function initialResolutionRanges(state: LibraryPageState): { x: string; y: string } {
  if (state.resolutionXRange || state.resolutionYRange) {
    return { x: state.resolutionXRange ?? '', y: state.resolutionYRange ?? '' };
  }
  const legacy = state.resolutionRange?.toLowerCase().replace(/\s+/g, '') ?? '';
  const [x, y] = legacy.split(/[x×]/);
  return { x: x ?? '', y: y ?? '' };
}

function initialDurationRanges(state: LibraryPageState): { min: string; max: string } {
  if (state.durationMinMinutes || state.durationMaxMinutes) {
    return { min: state.durationMinMinutes ?? '', max: state.durationMaxMinutes ?? '' };
  }
  const range = parseNumberRange(state.durationRange ?? '');
  const multiplier = durationUnitSeconds(parseDurationUnit(state.durationUnit));
  return {
    min: range.min === undefined ? '' : formatNumberValue(range.min / multiplier),
    max: range.max === undefined ? '' : formatNumberValue(range.max / multiplier),
  };
}

function formatNumberValue(value: number): string {
  if (!Number.isFinite(value)) return '';
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(9)));
}

function parseDurationUnit(value: unknown): DurationUnit {
  return value === 'seconds' || value === 'hours' ? value : 'minutes';
}

function durationUnitSeconds(unit: DurationUnit) {
  if (unit === 'seconds') return 1;
  if (unit === 'hours') return 3600;
  return 60;
}

function roundDurationSeconds(value: number) {
  return Math.round(value * 1000) / 1000;
}

function convertDurationValue(value: string, from: DurationUnit, to: DurationUnit) {
  const parsed = positiveNumber(value);
  if (parsed === undefined) return value.trim() === '' ? '' : value;
  return formatNumberValue((parsed * durationUnitSeconds(from)) / durationUnitSeconds(to));
}
