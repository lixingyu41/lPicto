import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { RotateCcw } from 'lucide-react';
import { api } from '../api/client';
import AssetGrid from '../components/AssetGrid';
import AssetGroupingControls, { normalizeAssetGroupModeForSort } from '../components/AssetGroupingControls';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import SortControls from '../components/SortControls';
import { useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { useAssetDeletedEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import { usePersistentPageState } from '../hooks/usePersistentPageState';
import { useWaterfallGridState } from '../hooks/useWaterfallGridState';
import type { Asset, AssetDeletedEvent, AssetKind, LibraryAnchor, NFOFilterField, OrientationFilter, SearchAssetsParams, SortKey } from '../types/api';
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
import { removeAssetById } from '../utils/assetSort';
import { currentURLHasParam, currentURLLocation, currentURLPath, replaceURLState } from '../utils/urlState';

const pageSize = 100;
const searchStateKey = 'search';
const searchURLKeys = [
  'q',
  'nfo',
  'nfoActor',
  'nfoId',
  'nfoTag',
  'nfoTitle',
  'nfoYear',
  'type',
  'sort',
  'orientation',
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

interface SearchPageState extends GridReturnState {
  dateFrom: string;
  dateTo: string;
  durationMaxMinutes: string;
  durationMinMinutes: string;
  durationRange?: string;
  nfoActorQuery: string;
  nfoIDQuery: string;
  nfoQuery: string;
  nfoTagQuery: string;
  nfoTitleQuery: string;
  nfoYearQuery: string;
  orientation: OrientationFilter;
  groupMode: AssetGroupMode;
  query: string;
  resolutionRange?: string;
  resolutionXRange: string;
  resolutionYRange: string;
  sizeMaxMB: string;
  sizeMinMB: string;
  sort: SortKey;
  type: AssetKind;
}

const defaultSearchState: SearchPageState = {
  ...resetGridState(),
  dateFrom: '',
  dateTo: '',
  durationMaxMinutes: '',
  durationMinMinutes: '',
  durationRange: '',
  nfoActorQuery: '',
  nfoIDQuery: '',
  nfoQuery: '',
  nfoTagQuery: '',
  nfoTitleQuery: '',
  nfoYearQuery: '',
  orientation: 'all',
  groupMode: 'none',
  query: '',
  resolutionRange: '',
  resolutionXRange: '',
  resolutionYRange: '',
  sizeMaxMB: '',
  sizeMinMB: '',
  sort: 'timeline_desc',
  type: 'all',
};

const nfoFilterFields: Array<{ key: NFOFilterField; label: string; placeholder: string }> = [
  { key: 'actor', label: 'NFO 演员', placeholder: '选择或输入演员' },
  { key: 'id', label: 'NFO ID', placeholder: '选择或输入 ID' },
  { key: 'tag', label: 'NFO 标签/类型', placeholder: '选择或输入标签/类型' },
  { key: 'title', label: 'NFO 标题', placeholder: '选择或输入标题' },
  { key: 'year', label: 'NFO 年份', placeholder: '选择或输入年份' },
];

const emptyNFOOptions: Record<NFOFilterField, string[]> = {
  actor: [],
  id: [],
  tag: [],
  title: [],
  year: [],
};

export default function SearchPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const persistedState = loadPageState<SearchPageState>(searchStateKey, defaultSearchState);
  const initialStateRef = useRef(
    initialSearchState(searchParams, persistedState),
  );
  const initialResolution = initialResolutionRanges(initialStateRef.current);
  const initialDuration = initialDurationMinuteRanges(initialStateRef.current);
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [nfoQuery, setNFOQuery] = useState(initialStateRef.current.nfoQuery);
  const [nfoActorQuery, setNFOActorQuery] = useState(initialStateRef.current.nfoActorQuery ?? '');
  const [nfoIDQuery, setNFOIDQuery] = useState(initialStateRef.current.nfoIDQuery ?? '');
  const [nfoTagQuery, setNFOTagQuery] = useState(initialStateRef.current.nfoTagQuery ?? '');
  const [nfoTitleQuery, setNFOTitleQuery] = useState(initialStateRef.current.nfoTitleQuery ?? '');
  const [nfoYearQuery, setNFOYearQuery] = useState(initialStateRef.current.nfoYearQuery ?? '');
  const [nfoOptions, setNFOOptions] = useState<Record<NFOFilterField, string[]>>(emptyNFOOptions);
  const [type, setType] = useState<AssetKind>(initialStateRef.current.type);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [resolutionXRange, setResolutionXRange] = useState(initialResolution.x);
  const [resolutionYRange, setResolutionYRange] = useState(initialResolution.y);
  const [dateFrom, setDateFrom] = useState(initialStateRef.current.dateFrom);
  const [dateTo, setDateTo] = useState(initialStateRef.current.dateTo);
  const [durationMinMinutes, setDurationMinMinutes] = useState(initialDuration.min);
  const [durationMaxMinutes, setDurationMaxMinutes] = useState(initialDuration.max);
  const [orientation, setOrientation] = useState<OrientationFilter>(initialStateRef.current.orientation);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>(initialStateRef.current.groupMode);
  const [sizeMinMB, setSizeMinMB] = useState(initialStateRef.current.sizeMinMB);
  const [sizeMaxMB, setSizeMaxMB] = useState(initialStateRef.current.sizeMaxMB);
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const sidebarState = useSidebarReturnState();
  const currentPageReturnPath = useCallback(() => currentURLPath(location), [location]);
  const serverGroup = serverGroupForMode(groupMode);

  const searchRequest = useMemo<SearchAssetsParams>(
    () => ({
      q: query.trim() || undefined,
      nfo: nfoQuery.trim() || undefined,
      nfoActor: nfoActorQuery.trim() || undefined,
      nfoId: nfoIDQuery.trim() || undefined,
      nfoTag: nfoTagQuery.trim() || undefined,
      nfoTitle: nfoTitleQuery.trim() || undefined,
      nfoYear: nfoYearQuery.trim() || undefined,
      type,
      sort,
      ...parseResolutionRanges(resolutionXRange, resolutionYRange, orientation),
      from: datetimeLocalToUnix(dateFrom),
      to: datetimeLocalToUnix(dateTo),
      ...parseDurationMinuteRange(durationMinMinutes, durationMaxMinutes),
      orientation,
      group: serverGroup,
      sizeMin: mbToBytes(sizeMinMB),
      sizeMax: mbToBytes(sizeMaxMB),
    }),
    [
      dateFrom,
      dateTo,
      durationMaxMinutes,
      durationMinMinutes,
      nfoActorQuery,
      nfoIDQuery,
      nfoQuery,
      nfoTagQuery,
      nfoTitleQuery,
      nfoYearQuery,
      orientation,
      query,
      resolutionXRange,
      resolutionYRange,
      serverGroup,
      sizeMaxMB,
      sizeMinMB,
      sort,
      type,
    ],
  );
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
  const anchorSearchRequest = useMemo<SearchAssetsParams>(() => ({ ...searchRequest, group: undefined }), [searchRequest]);
  const loadAssets = useCallback((page: number) => api.searchAssets(page, pageSize, searchRequest), [searchRequest]);
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
  useAssetDeletedEvents(handleAssetDeleted, [handleAssetDeleted]);

  useEffect(() => {
    let live = true;
    async function loadAnchors() {
      try {
        const result = await api.searchAnchors(pageSize, anchorSearchRequest);
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

  useEffect(() => {
    const controller = new AbortController();
    void Promise.all(
      nfoFilterFields.map(async ({ key }) => {
        try {
          const response = await api.searchNFOOptions(key, nfoOptionQueries[key].trim(), controller.signal);
          return [key, response.items ?? []] as const;
        } catch {
          return [key, []] as const;
        }
      }),
    ).then((entries) => {
      if (controller.signal.aborted) return;
      setNFOOptions(Object.fromEntries(entries) as Record<NFOFilterField, string[]>);
    });
    return () => controller.abort();
  }, [nfoOptionQueries]);

  const currentPageState = useCallback(
    (): SearchPageState => ({
      ...getGridState(),
      dateFrom,
      dateTo,
      durationMaxMinutes,
      durationMinMinutes,
      nfoActorQuery,
      nfoIDQuery,
      nfoQuery,
      nfoTagQuery,
      nfoTitleQuery,
      nfoYearQuery,
      orientation,
      groupMode,
      query,
      resolutionXRange,
      resolutionYRange,
      sidebarExpanded: sidebarState.sidebarExpanded,
      sizeMaxMB,
      sizeMinMB,
      sort,
      type,
    }),
    [
      dateFrom,
      dateTo,
      durationMaxMinutes,
      durationMinMinutes,
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
      resolutionXRange,
      resolutionYRange,
      sidebarState.sidebarExpanded,
      sizeMaxMB,
      sizeMinMB,
      sort,
      type,
    ],
  );

  const saveCurrentState = useCallback(() => {
    savePageState<SearchPageState>(searchStateKey, currentPageState());
  }, [currentPageState]);
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
        heightMax: searchRequest.heightMax,
        heightMin: searchRequest.heightMin,
        nfo: nfoQuery.trim(),
        nfoActor: nfoActorQuery.trim(),
        nfoId: nfoIDQuery.trim(),
        nfoTag: nfoTagQuery.trim(),
        nfoTitle: nfoTitleQuery.trim(),
        nfoYear: nfoYearQuery.trim(),
        orientation,
        q: query.trim(),
        sizeMax: searchRequest.sizeMax,
        sizeMin: searchRequest.sizeMin,
        sort,
        to: searchRequest.to,
        type,
        widthMax: searchRequest.widthMax,
        widthMin: searchRequest.widthMin,
      },
      searchURLKeys,
    );
  }, [
    groupMode,
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
    searchParams,
    searchRequest,
    sort,
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

  useEffect(() => {
    const nextGroupMode = normalizeAssetGroupModeForSort(groupMode, sort);
    if (nextGroupMode !== groupMode) {
      setGroupMode(nextGroupMode);
    }
  }, [groupMode, sort]);

  const resetFilters = useCallback(() => {
    setQuery('');
    setNFOQuery('');
    setNFOActorQuery('');
    setNFOIDQuery('');
    setNFOTagQuery('');
    setNFOTitleQuery('');
    setNFOYearQuery('');
    setType('all');
    setSort('timeline_desc');
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
    'search',
    <div className="sidebar-control-stack sidebar-search-panel">
      <div className="sidebar-mode-row">
        <div className="sidebar-segmented">
          {(['all', 'image', 'video'] as AssetKind[]).map((value) => (
            <button className={type === value ? 'active' : ''} key={value} type="button" onClick={() => setType(value)}>
              {value === 'all' ? '全部' : value === 'image' ? '照片' : '视频'}
            </button>
          ))}
        </div>
        <button className="sidebar-square-button" type="button" title="重置" aria-label="重置" onClick={resetFilters}>
          <RotateCcw size={15} />
        </button>
      </div>
      <label className="sidebar-field">
        <span>文件名</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名" />
      </label>
      <div className="sidebar-field-grid">
        {nfoFilterFields.map((field) => (
          <NFOFilterInput
            key={field.key}
            field={field.key}
            label={field.label}
            listID={`search-nfo-${field.key}`}
            onChange={setNFOFieldQuery}
            options={nfoOptions[field.key] ?? []}
            placeholder={field.placeholder}
            value={nfoOptionQueries[field.key]}
          />
        ))}
      </div>
      <label className="sidebar-field">
        <span>NFO 全文</span>
        <input value={nfoQuery} onChange={(event) => setNFOQuery(event.target.value)} placeholder="任意 NFO 文本" />
      </label>
      <SortControls sort={sort} onChange={setSort} />
      <AssetGroupingControls groupMode={groupMode} sort={sort} onChange={setGroupMode} />
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
      <div className="sidebar-field-grid">
        <label className="sidebar-field">
          <span>最短分钟</span>
          <input inputMode="decimal" value={durationMinMinutes} onChange={(event) => setDurationMinMinutes(event.target.value)} />
        </label>
        <label className="sidebar-field">
          <span>最长分钟</span>
          <input inputMode="decimal" value={durationMaxMinutes} onChange={(event) => setDurationMaxMinutes(event.target.value)} />
        </label>
      </div>
      <div className="sidebar-segmented">
        {(['all', 'landscape', 'portrait'] as OrientationFilter[]).map((value) => (
          <button className={orientation === value ? 'active' : ''} key={value} type="button" onClick={() => setOrientation(value)}>
            {value === 'all' ? '方向' : value === 'landscape' ? '横屏' : '竖屏'}
          </button>
        ))}
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
    </div>,
    [
      dateFrom,
      dateTo,
      durationMaxMinutes,
      durationMinMinutes,
      groupMode,
      nfoOptionQueries,
      nfoOptions,
      nfoActorQuery,
      nfoIDQuery,
      nfoQuery,
      nfoTagQuery,
      nfoTitleQuery,
      nfoYearQuery,
      orientation,
      query,
      resetFilters,
      resolutionXRange,
      resolutionYRange,
      setNFOFieldQuery,
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
      {error && <div className="error-line">{error}</div>}
      {items.length === 0 && !loading ? (
        <EmptyState text="没有匹配资源" />
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
            onAssetMissing={(asset) => mutateItems((current) => removeAssetById(current, asset.id))}
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
            buildViewerUrl={(asset) => buildViewerUrl(asset, searchRequest, currentPageState(), currentPageReturnPath())}
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
    </section>
  );
}

function NFOFilterInput({
  field,
  label,
  listID,
  onChange,
  options,
  placeholder,
  value,
}: {
  field: NFOFilterField;
  label: string;
  listID: string;
  onChange: (field: NFOFilterField, value: string) => void;
  options: string[];
  placeholder: string;
  value: string;
}) {
  return (
    <label className="sidebar-field">
      <span>{label}</span>
      <input list={listID} value={value} onChange={(event) => onChange(field, event.target.value)} placeholder={placeholder} />
      <datalist id={listID}>
        {(options ?? []).map((option) => (
          <option key={option} value={option} />
        ))}
      </datalist>
    </label>
  );
}

function initialSearchState(searchParams: URLSearchParams, persistedState: SearchPageState): SearchPageState {
  if (searchParams.has('restore')) {
    return decodeReturnState<SearchPageState>(searchParams.get('restore'), persistedState);
  }
  if (!hasSearchStateParams(searchParams)) {
    return persistedState;
  }
  return {
    ...defaultSearchState,
    query: searchParams.get('q') ?? '',
    nfoQuery: searchParams.get('nfo') ?? '',
    nfoActorQuery: searchParams.get('nfoActor') ?? '',
    nfoIDQuery: searchParams.get('nfoId') ?? '',
    nfoTagQuery: searchParams.get('nfoTag') ?? '',
    nfoTitleQuery: searchParams.get('nfoTitle') ?? '',
    nfoYearQuery: searchParams.get('nfoYear') ?? '',
    type: parseAssetKindParam(searchParams.get('type')),
    sort: parseSortParam(searchParams.get('sort')),
    orientation: parseOrientationParam(searchParams.get('orientation')),
    groupMode: parseAssetGroupMode(searchParams.get('group'), 'none'),
    resolutionXRange: rangeInputFromParams(searchParams.get('widthMin'), searchParams.get('widthMax')),
    resolutionYRange: rangeInputFromParams(searchParams.get('heightMin'), searchParams.get('heightMax')),
    durationMinMinutes: secondsParamToMinutes(searchParams.get('durationMin')),
    durationMaxMinutes: secondsParamToMinutes(searchParams.get('durationMax')),
    sizeMinMB: bytesParamToMB(searchParams.get('sizeMin')),
    sizeMaxMB: bytesParamToMB(searchParams.get('sizeMax')),
    dateFrom: unixParamToDatetimeLocal(searchParams.get('from')),
    dateTo: unixParamToDatetimeLocal(searchParams.get('to')),
  };
}

function hasSearchStateParams(searchParams: URLSearchParams) {
  return [
    'q',
    'nfo',
    'nfoActor',
    'nfoId',
    'nfoTag',
    'nfoTitle',
    'nfoYear',
    'type',
    'sort',
    'orientation',
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

function parseAssetKindParam(value: string | null): AssetKind {
  return value === 'image' || value === 'video' ? value : 'all';
}

function parseOrientationParam(value: string | null): OrientationFilter {
  return value === 'landscape' || value === 'portrait' ? value : 'all';
}

function parseSortParam(value: string | null): SortKey {
  if (
    value === 'timeline_desc' ||
    value === 'timeline_asc' ||
    value === 'imported_desc' ||
    value === 'imported_asc' ||
    value === 'filename' ||
    value === 'filename_asc' ||
    value === 'filename_desc' ||
    value === 'size' ||
    value === 'size_desc' ||
    value === 'size_asc'
  ) {
    return value;
  }
  return 'timeline_desc';
}

function rangeInputFromParams(minValue: string | null, maxValue: string | null) {
  const min = positiveNumber(minValue ?? '');
  const max = positiveNumber(maxValue ?? '');
  if (min === undefined && max === undefined) return '';
  if (min !== undefined && max !== undefined && min === max) return String(min);
  return `${min ?? ''}-${max ?? ''}`;
}

function secondsParamToMinutes(value: string | null) {
  const seconds = positiveNumber(value ?? '');
  if (seconds === undefined) return '';
  return formatMinuteValue(seconds / 60);
}

function bytesParamToMB(value: string | null) {
  const bytes = positiveNumber(value ?? '');
  if (bytes === undefined) return '';
  return formatMinuteValue(bytes / (1024 * 1024));
}

function unixParamToDatetimeLocal(value: string | null) {
  const seconds = positiveNumber(value ?? '');
  if (seconds === undefined) return '';
  const date = new Date(seconds * 1000);
  if (!Number.isFinite(date.getTime())) return '';
  const pad = (part: number) => String(part).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function buildViewerUrl(asset: Asset, params: SearchAssetsParams, state: SearchPageState, returnPath: string) {
  const query = new URLSearchParams(searchQueryEntries(params));
  query.set('context', 'search');
  return appendViewerReturnParams(`/viewer/${asset.id}?${query.toString()}`, returnPath, state);
}

function searchQueryEntries(params: SearchAssetsParams) {
  const entries: Array<[string, string]> = [];
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === '' || value === 'all') return;
    entries.push([key, String(value)]);
  });
  return entries;
}

function parseResolutionRanges(
  xValue: string,
  yValue: string,
  orientation: OrientationFilter,
): Pick<SearchAssetsParams, 'widthMin' | 'widthMax' | 'heightMin' | 'heightMax' | 'dimensionMode'> {
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

function parseDurationMinuteRange(minValue: string, maxValue: string): Pick<SearchAssetsParams, 'durationMin' | 'durationMax'> {
  const min = positiveNumber(minValue);
  const max = positiveNumber(maxValue);
  return {
    durationMin: min === undefined ? undefined : min * 60,
    durationMax: max === undefined ? undefined : max * 60,
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

function initialResolutionRanges(state: SearchPageState): { x: string; y: string } {
  if (state.resolutionXRange || state.resolutionYRange) {
    return { x: state.resolutionXRange ?? '', y: state.resolutionYRange ?? '' };
  }
  const legacy = state.resolutionRange?.toLowerCase().replace(/\s+/g, '') ?? '';
  const [x, y] = legacy.split(/[x×]/);
  return { x: x ?? '', y: y ?? '' };
}

function initialDurationMinuteRanges(state: SearchPageState): { min: string; max: string } {
  if (state.durationMinMinutes || state.durationMaxMinutes) {
    return { min: state.durationMinMinutes ?? '', max: state.durationMaxMinutes ?? '' };
  }
  const range = parseNumberRange(state.durationRange ?? '');
  return {
    min: range.min === undefined ? '' : formatMinuteValue(range.min / 60),
    max: range.max === undefined ? '' : formatMinuteValue(range.max / 60),
  };
}

function formatMinuteValue(value: number): string {
  if (!Number.isFinite(value)) return '';
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(3)));
}
