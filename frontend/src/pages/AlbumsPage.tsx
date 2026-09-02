import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { Check, FolderInput, FolderPlus, Pencil, Plus, RefreshCw, RotateCcw, Trash2 } from 'lucide-react';
import AlbumEditor from '../components/AlbumEditor';
import AssetGrid from '../components/AssetGrid';
import { CompactAssetGroupingControls, normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import NFOSearchFilters from '../components/NFOSearchFilters';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { SidebarAlbumList, SidebarFilterIconRow, SidebarMediaTypeList, SidebarOrientationFilter, SidebarRatingFilter } from '../components/SidebarControls';
import { CompactSortControls, isSortKey } from '../components/SortControls';
import { useSidebarBrowseTools, useSidebarPanel, useSidebarQueryChips, useSidebarReturnState, useSidebarScopeTitle, type BrowseQueryChip, type BrowseTools } from '../components/SidebarContext';
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
  LibraryFilterParams,
  NFOFilterField,
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
import { removeAssetById } from '../utils/assetSort';
import { assetRatingParam, currentURLHasParam, currentURLLocation, currentURLPath, orientationParam, positiveIntParam, replaceURLState } from '../utils/urlState';
import { waterfallPageSize } from '../utils/waterfallPaging';
import { parseTagFilters, serializeTagFilters } from '../utils/tagFilters';
import {
  bytesParamToMB,
  convertDurationValue,
  datetimeLocalToUnix,
  mbToBytes,
  parseDurationRange,
  parseDurationUnit,
  parseResolutionRanges,
  rangeInputFromParams,
  secondsParamToDurationValue,
  unixParamToDatetimeLocal,
  type DurationUnit,
} from '../utils/mediaFilterValues';

const pageSize = waterfallPageSize;
const albumsStateKey = 'albums';
const albumsURLKeys = [
  'albumId', 'album', 'type', 'rating', 'orientation', 'sort', 'group', 'q', 'combinedTags', 'tagNodes',
  'aiDescription', 'nfo', 'nfoActor', 'nfoId', 'nfoTag', 'nfoTitle', 'nfoYear',
  'widthMin', 'widthMax', 'heightMin', 'heightMax', 'durationMin', 'durationMax', 'sizeMin', 'sizeMax', 'from', 'to',
];
const pendingAlbumEditorKey = 'lpicto:pending-album-editor';
const assetKinds: AssetKind[] = ['all', 'image', 'video', 'audio'];

type PendingAlbumEditor = { mode: 'add' } | { mode: 'edit'; albumId: number };

interface AlbumsPageState extends GridReturnState {
  aiDescriptionQuery: string;
  collapsedGroupKeys: string[];
  dateFrom: string;
  dateTo: string;
  durationMax: string;
  durationMin: string;
  durationUnit: DurationUnit;
  groupMode: AssetGroupMode;
  nfoActorQuery: string;
  nfoIDQuery: string;
  nfoQuery: string;
  nfoTagQuery: string;
  nfoTitleQuery: string;
  nfoYearQuery: string;
  orientation: OrientationFilter;
  query: string;
  rating: AssetRatingFilter;
  resolutionXRange: string;
  resolutionYRange: string;
  selectedId: number | null;
  sizeMaxMB: string;
  sizeMinMB: string;
  sort: SortKey;
  tagFilters: string[];
  type: AssetKind;
}

const defaultAlbumsState: AlbumsPageState = {
  ...resetGridState(),
  aiDescriptionQuery: '',
  collapsedGroupKeys: [],
  dateFrom: '',
  dateTo: '',
  durationMax: '',
  durationMin: '',
  durationUnit: 'minutes',
  groupMode: 'none',
  nfoActorQuery: '',
  nfoIDQuery: '',
  nfoQuery: '',
  nfoTagQuery: '',
  nfoTitleQuery: '',
  nfoYearQuery: '',
  orientation: 'all',
  query: '',
  rating: 'all',
  resolutionXRange: '',
  resolutionYRange: '',
  selectedId: null,
  sizeMaxMB: '',
  sizeMinMB: '',
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
  const [aiDescriptionQuery, setAIDescriptionQuery] = useViewerAwareMediaState(initialStateRef.current.aiDescriptionQuery);
  const [nfoQuery, setNFOQuery] = useViewerAwareMediaState(initialStateRef.current.nfoQuery);
  const [nfoActorQuery, setNFOActorQuery] = useViewerAwareMediaState(initialStateRef.current.nfoActorQuery);
  const [nfoIDQuery, setNFOIDQuery] = useViewerAwareMediaState(initialStateRef.current.nfoIDQuery);
  const [nfoTagQuery, setNFOTagQuery] = useViewerAwareMediaState(initialStateRef.current.nfoTagQuery);
  const [nfoTitleQuery, setNFOTitleQuery] = useViewerAwareMediaState(initialStateRef.current.nfoTitleQuery);
  const [nfoYearQuery, setNFOYearQuery] = useViewerAwareMediaState(initialStateRef.current.nfoYearQuery);
  const [resolutionXRange, setResolutionXRange] = useViewerAwareMediaState(initialStateRef.current.resolutionXRange);
  const [resolutionYRange, setResolutionYRange] = useViewerAwareMediaState(initialStateRef.current.resolutionYRange);
  const [dateFrom, setDateFrom] = useViewerAwareMediaState(initialStateRef.current.dateFrom);
  const [dateTo, setDateTo] = useViewerAwareMediaState(initialStateRef.current.dateTo);
  const [durationMin, setDurationMin] = useViewerAwareMediaState(initialStateRef.current.durationMin);
  const [durationMax, setDurationMax] = useViewerAwareMediaState(initialStateRef.current.durationMax);
  const [durationUnit, setDurationUnit] = useViewerAwareMediaState<DurationUnit>(initialStateRef.current.durationUnit);
  const [sizeMinMB, setSizeMinMB] = useViewerAwareMediaState(initialStateRef.current.sizeMinMB);
  const [sizeMaxMB, setSizeMaxMB] = useViewerAwareMediaState(initialStateRef.current.sizeMaxMB);
  const [rating, setRating] = useViewerAwareMediaState<AssetRatingFilter>(initialStateRef.current.rating ?? 'all');
  const [orientation, setOrientation] = useViewerAwareMediaState<OrientationFilter>(initialStateRef.current.orientation);
  useEffect(() => {
    if (type !== 'audio') return;
    setOrientation('all');
    setResolutionXRange('');
    setResolutionYRange('');
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
  const searchRequest = useMemo<LibraryFilterParams>(() => ({
    q: query.trim() || undefined,
    aiDescription: aiDescriptionQuery.trim() || undefined,
    nfo: nfoQuery.trim() || undefined,
    nfoActor: nfoActorQuery.trim() || undefined,
    nfoId: nfoIDQuery.trim() || undefined,
    nfoTag: nfoTagQuery.trim() || undefined,
    nfoTitle: nfoTitleQuery.trim() || undefined,
    nfoYear: nfoYearQuery.trim() || undefined,
    tagNodes: serializeTagFilters(tagFilters),
    rating: activeRating,
    type,
    sort,
    ...parseResolutionRanges(resolutionXRange, resolutionYRange, orientation),
    from: datetimeLocalToUnix(dateFrom),
    to: datetimeLocalToUnix(dateTo),
    ...parseDurationRange(durationMin, durationMax, durationUnit),
    orientation,
    group: serverGroup,
    sizeMin: mbToBytes(sizeMinMB),
    sizeMax: mbToBytes(sizeMaxMB),
  }), [
    activeRating, aiDescriptionQuery, dateFrom, dateTo, durationMax, durationMin, durationUnit, nfoActorQuery,
    nfoIDQuery, nfoQuery, nfoTagQuery, nfoTitleQuery, nfoYearQuery, orientation, query, resolutionXRange,
    resolutionYRange, serverGroup, sizeMaxMB, sizeMinMB, sort, tagFilters, type,
  ]);
  const searchKey = useMemo(() => JSON.stringify(searchRequest), [searchRequest]);
  const nfoOptionQueries = useMemo<Record<NFOFilterField, string>>(() => ({
    actor: nfoActorQuery,
    id: nfoIDQuery,
    tag: nfoTagQuery,
    title: nfoTitleQuery,
    year: nfoYearQuery,
  }), [nfoActorQuery, nfoIDQuery, nfoTagQuery, nfoTitleQuery, nfoYearQuery]);

  const clearNFOSearch = useCallback(() => {
    setNFOQuery('');
    setNFOActorQuery('');
    setNFOIDQuery('');
    setNFOTagQuery('');
    setNFOTitleQuery('');
    setNFOYearQuery('');
  }, []);
  const handleNFOFieldQueryChange = useCallback((field: NFOFilterField, value: string) => {
    if (field === 'actor') setNFOActorQuery(value);
    else if (field === 'id') setNFOIDQuery(value);
    else if (field === 'tag') setNFOTagQuery(value);
    else if (field === 'title') setNFOTitleQuery(value);
    else setNFOYearQuery(value);
  }, []);
  const handleDurationUnitChange = useCallback((next: DurationUnit) => {
    if (next === durationUnit) return;
    setDurationMin((value) => convertDurationValue(value, durationUnit, next));
    setDurationMax((value) => convertDurationValue(value, durationUnit, next));
    setDurationUnit(next);
  }, [durationUnit]);
  const resetRangeFilters = useCallback(() => {
    setResolutionXRange('');
    setResolutionYRange('');
    setDateFrom('');
    setDateTo('');
    setDurationMin('');
    setDurationMax('');
    setSizeMinMB('');
    setSizeMaxMB('');
  }, []);

  const selectedAlbum = useMemo(
    () => albums.find((album) => album.id === selectedId) ?? albums[0] ?? null,
    [albums, selectedId],
  );
  useSidebarScopeTitle('albums', selectedAlbum ? `相册 / ${selectedAlbum.name}` : '相册', [selectedAlbum?.id, selectedAlbum?.name]);
  const queryChips = useMemo<BrowseQueryChip[]>(() => {
    const chips: BrowseQueryChip[] = [];
    if (query.trim()) chips.push({ id: 'search', label: `文件名: ${query.trim()}`, onRemove: () => setQuery('') });
    if (aiDescriptionQuery.trim()) chips.push({ id: 'ai-description', label: `AI 描述: ${aiDescriptionQuery.trim()}`, onRemove: () => setAIDescriptionQuery('') });
    const nfoConditions = [nfoQuery, nfoActorQuery, nfoIDQuery, nfoTagQuery, nfoTitleQuery, nfoYearQuery].filter((value) => value.trim()).length;
    if (nfoConditions > 0) chips.push({ id: 'nfo', label: `元数据 ${nfoConditions}`, onRemove: clearNFOSearch });
    if (resolutionXRange.trim() || resolutionYRange.trim()) chips.push({ id: 'resolution', label: '分辨率', onRemove: () => { setResolutionXRange(''); setResolutionYRange(''); } });
    if (dateFrom || dateTo) chips.push({ id: 'date', label: '时间范围', onRemove: () => { setDateFrom(''); setDateTo(''); } });
    if (durationMin.trim() || durationMax.trim()) chips.push({ id: 'duration', label: '时长', onRemove: () => { setDurationMin(''); setDurationMax(''); } });
    if (sizeMinMB.trim() || sizeMaxMB.trim()) chips.push({ id: 'size', label: '文件大小', onRemove: () => { setSizeMinMB(''); setSizeMaxMB(''); } });
    if (tagFilters.length > 0) chips.push({ id: 'tags', label: `标签 ${tagFilters.length}`, onRemove: () => setTagFilters([]) });
    return chips;
  }, [aiDescriptionQuery, dateFrom, dateTo, durationMax, durationMin, nfoActorQuery, nfoIDQuery, nfoQuery, nfoTagQuery, nfoTitleQuery, nfoYearQuery, query, resolutionXRange, resolutionYRange, sizeMaxMB, sizeMinMB, tagFilters]);
  useSidebarQueryChips('albums', queryChips, [queryChips]);
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
      return api.albumAssets(selectedAlbum.id, page, pageSize, searchRequest);
    },
    [searchRequest, selectedAlbum],
  );
  const selectAllAssetIds = useCallback(async () => (
    selectedAlbum ? (await api.albumSelection(selectedAlbum.id, searchRequest)).assetIds : []
  ), [searchRequest, selectedAlbum?.id]);

  const { items, hasMore, hasPrevious, loading, error: loadError, loadMore, loadPrevious, reset, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [selectedAlbum?.id, searchKey]);
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
    resetKey: JSON.stringify([selectedAlbum?.id ?? null, searchKey]),
    restoreReady: Boolean(selectedAlbum),
    searchParams,
  });

  const handleAssetReady = useCallback(() => {
    void jumpToPage(Math.floor(loadedStartIndex / pageSize) + 1);
  }, [jumpToPage, loadedStartIndex]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((current) => removeAssetById(current, event.id)), [mutateItems]);
  const eventsConnected = useAssetReadyEvents(handleAssetReady, [handleAssetReady, handleAssetDeleted], handleAssetDeleted);

  useEffect(() => {
    if (eventsConnected || !selectedAlbum) return undefined;
    const timer = window.setInterval(() => {
      void api
        .albumAssets(selectedAlbum.id, Math.floor(loadedStartIndex / pageSize) + 1, pageSize, searchRequest)
        .then((result) => mutateItems(() => result.items))
        .catch(() => undefined);
    }, 30000);
    return () => window.clearInterval(timer);
  }, [eventsConnected, loadedStartIndex, mutateItems, searchRequest, selectedAlbum]);

  useEffect(() => {
    let live = true;
    async function loadAnchors(albumId: number) {
      try {
        const result = await api.albumAnchors(albumId, pageSize, searchRequest);
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
  }, [searchRequest, selectedAlbum?.id]);

  const currentPageState = useCallback(
    (): AlbumsPageState => ({
      ...getGridState(),
      aiDescriptionQuery,
      collapsedGroupKeys: Array.from(collapsedGroupKeys),
      dateFrom,
      dateTo,
      durationMax,
      durationMin,
      durationUnit,
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
      selectedId: selectedAlbum?.id ?? selectedId,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sizeMaxMB,
      sizeMinMB,
      sort,
      tagFilters,
      type,
    }),
    [aiDescriptionQuery, collapsedGroupKeys, dateFrom, dateTo, durationMax, durationMin, durationUnit, getGridState, groupMode, nfoActorQuery, nfoIDQuery, nfoQuery, nfoTagQuery, nfoTitleQuery, nfoYearQuery, orientation, query, rating, resolutionXRange, resolutionYRange, selectedAlbum?.id, selectedId, sidebarState.sidebarExpanded, sizeMaxMB, sizeMinMB, sort, tagFilters, type],
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
        aiDescription: searchRequest.aiDescription,
        album: selectedAlbum.name,
        albumId: selectedAlbum.id,
        durationMax: searchRequest.durationMax,
        durationMin: searchRequest.durationMin,
        from: searchRequest.from,
        group: groupMode,
        heightMax: searchRequest.heightMax,
        heightMin: searchRequest.heightMin,
        nfo: searchRequest.nfo,
        nfoActor: searchRequest.nfoActor,
        nfoId: searchRequest.nfoId,
        nfoTag: searchRequest.nfoTag,
        nfoTitle: searchRequest.nfoTitle,
        nfoYear: searchRequest.nfoYear,
        orientation: orientation === 'all' ? undefined : orientation,
        q: query,
        rating: activeRating,
        sizeMax: searchRequest.sizeMax,
        sizeMin: searchRequest.sizeMin,
        sort,
        tagNodes: serializeTagFilters(tagFilters),
        to: searchRequest.to,
        type,
        widthMax: searchRequest.widthMax,
        widthMin: searchRequest.widthMin,
      },
      albumsURLKeys,
    );
  }, [groupMode, location, navigate, orientation, query, rating, searchParams, searchRequest, selectedAlbum, sort, tagFilters, type]);
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
  useSidebarBrowseTools('albums', browseTools, [browseTools]);

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
      <div className="sidebar-panel-section sidebar-panel-scope">
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
      <div className="album-toolbar album-scope-actions" role="toolbar" aria-label="相册操作">
        <button className="sidebar-compact-trigger" type="button" title="新建相册" aria-label="新建相册" onClick={handleAddAlbum}>
          <Plus size={18} />
        </button>
        <button className="sidebar-compact-trigger" type="button" title="新建组" aria-label="新建组" onClick={() => {
          setMoveGroupOpen(false);
          setGroupDraftOpen((value) => !value);
        }}>
          <FolderPlus size={18} />
        </button>
        <button className="sidebar-compact-trigger" type="button" title="编辑相册" aria-label="编辑相册" disabled={!selectedAlbum} onClick={() => selectedAlbum && handleEditAlbum(selectedAlbum)}>
          <Pencil size={18} />
        </button>
        <button
          className="sidebar-compact-trigger"
          type="button"
          title="刷新相册"
          aria-label="刷新相册"
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
          aria-label="删除相册"
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
          aria-label="放到组"
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
      </div>
      {selectedAlbum && (
        <section className="sidebar-filter-card sidebar-search-card sidebar-panel-section sidebar-panel-search" aria-labelledby="album-search-title">
          <div className="sidebar-panel-card-title" id="album-search-title">搜索</div>
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
            onNFOFieldQueryChange={handleNFOFieldQueryChange}
          />
        </section>
      )}
      {selectedAlbum && (
        <section className="sidebar-filter-card sidebar-panel-section sidebar-panel-filters" aria-labelledby="album-filter-title">
          <div className="sidebar-panel-card-title" id="album-filter-title">范围筛选</div>
          <div className="sidebar-reset-row">
            <button className="sidebar-command" type="button" title="重置范围筛选" aria-label="重置范围筛选" onClick={resetRangeFilters}>
              <RotateCcw size={15} /><span>重置</span>
            </button>
          </div>
          <label className="sidebar-field">
            <span>分辨率</span>
            <div className="sidebar-field-grid">
              <input value={resolutionXRange} onChange={(event) => setResolutionXRange(event.target.value)} placeholder="X 100-4000" />
              <input value={resolutionYRange} onChange={(event) => setResolutionYRange(event.target.value)} placeholder="Y 100-3000" />
            </div>
          </label>
          <div className="sidebar-field-grid">
            <label className="sidebar-field"><span>起始时间</span><input type="datetime-local" value={dateFrom} onChange={(event) => setDateFrom(event.target.value)} /></label>
            <label className="sidebar-field"><span>结束时间</span><input type="datetime-local" value={dateTo} onChange={(event) => setDateTo(event.target.value)} /></label>
          </div>
          <div className="sidebar-field">
            <span>时长单位</span>
            <div className="sidebar-segmented" role="group" aria-label="媒体时长单位">
              {([['seconds', '秒'], ['minutes', '分钟'], ['hours', '小时']] as const).map(([unit, label]) => (
                <button className={durationUnit === unit ? 'active' : ''} type="button" aria-pressed={durationUnit === unit} key={unit} onClick={() => handleDurationUnitChange(unit)}>{label}</button>
              ))}
            </div>
          </div>
          <div className="sidebar-field-grid">
            <label className="sidebar-field"><span>最短时长</span><input inputMode="decimal" value={durationMin} onChange={(event) => setDurationMin(event.target.value)} /></label>
            <label className="sidebar-field"><span>最长时长</span><input inputMode="decimal" value={durationMax} onChange={(event) => setDurationMax(event.target.value)} /></label>
          </div>
          <div className="sidebar-field-grid">
            <label className="sidebar-field"><span>最小 MB</span><input inputMode="decimal" value={sizeMinMB} onChange={(event) => setSizeMinMB(event.target.value)} /></label>
            <label className="sidebar-field"><span>最大 MB</span><input inputMode="decimal" value={sizeMaxMB} onChange={(event) => setSizeMaxMB(event.target.value)} /></label>
          </div>
        </section>
      )}
    </div>,
    [
      aiDescriptionQuery,
      albums,
      collapsedGroupKeys,
      dateFrom,
      dateTo,
      durationMax,
      durationMin,
      durationUnit,
      groups,
      groupDraftOpen,
      groupName,
      groupMode,
      handleAddAlbum,
      handleDurationUnitChange,
      handleEditAlbum,
      handleNFOFieldQueryChange,
      handleRatingChange,
      handleToggleAlbumGroup,
      nfoOptionQueries,
      nfoQuery,
      orientation,
      query,
      rating,
      moveGroupId,
      moveGroupOpen,
      resetRangeFilters,
      resolutionXRange,
      resolutionYRange,
      selectedAlbum?.id,
      selectedAlbum?.updatedAt,
      sizeMaxMB,
      sizeMinMB,
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
                buildAlbumViewerPath(asset.id, selectedAlbum.id, searchRequest),
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
    aiDescriptionQuery: params.get('aiDescription') ?? (hasAlbumParams ? '' : base.aiDescriptionQuery),
    dateFrom: params.has('from') ? unixParamToDatetimeLocal(params.get('from')) : base.dateFrom,
    dateTo: params.has('to') ? unixParamToDatetimeLocal(params.get('to')) : base.dateTo,
    durationMax: params.has('durationMax') ? secondsParamToDurationValue(params.get('durationMax'), base.durationUnit) : base.durationMax,
    durationMin: params.has('durationMin') ? secondsParamToDurationValue(params.get('durationMin'), base.durationUnit) : base.durationMin,
    durationUnit: parseDurationUnit(base.durationUnit),
    groupMode: parseAssetGroupMode(params.get('group'), base.groupMode),
    nfoActorQuery: params.get('nfoActor') ?? (hasAlbumParams ? '' : base.nfoActorQuery),
    nfoIDQuery: params.get('nfoId') ?? (hasAlbumParams ? '' : base.nfoIDQuery),
    nfoQuery: params.get('nfo') ?? (hasAlbumParams ? '' : base.nfoQuery),
    nfoTagQuery: params.get('nfoTag') ?? (hasAlbumParams ? '' : base.nfoTagQuery),
    nfoTitleQuery: params.get('nfoTitle') ?? (hasAlbumParams ? '' : base.nfoTitleQuery),
    nfoYearQuery: params.get('nfoYear') ?? (hasAlbumParams ? '' : base.nfoYearQuery),
    orientation: params.has('orientation') ? orientationParam(params.get('orientation')) : base.orientation,
    query: params.get('q') ?? (hasAlbumParams ? '' : base.query),
    rating: params.has('rating') ? assetRatingParam(params.get('rating')) ?? base.rating : base.rating,
    resolutionXRange: params.has('widthMin') || params.has('widthMax') ? rangeInputFromParams(params.get('widthMin'), params.get('widthMax')) : base.resolutionXRange,
    resolutionYRange: params.has('heightMin') || params.has('heightMax') ? rangeInputFromParams(params.get('heightMin'), params.get('heightMax')) : base.resolutionYRange,
    selectedId: selectedId ?? base.selectedId,
    sizeMaxMB: params.has('sizeMax') ? bytesParamToMB(params.get('sizeMax')) : base.sizeMaxMB,
    sizeMinMB: params.has('sizeMin') ? bytesParamToMB(params.get('sizeMin')) : base.sizeMinMB,
    sort: isSortKey(sort) ? sort : base.sort,
    tagFilters: params.has('tagNodes') || params.has('combinedTags') ? parseTagFilters(params.get('tagNodes') ?? params.get('combinedTags')) : base.tagFilters ?? [],
    type: assetKinds.includes(type as AssetKind) ? (type as AssetKind) : base.type,
  };
}

function buildAlbumViewerPath(assetId: number, albumId: number, params: LibraryFilterParams) {
  const query = new URLSearchParams();
  query.set('context', 'album');
  query.set('albumId', String(albumId));
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === '' || value === 'all') return;
    query.set(key, String(value));
  });
  return `/viewer/${assetId}?${query.toString()}`;
}
