import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { RotateCcw } from 'lucide-react';
import { api } from '../api/client';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { useRestoreSidebarState, useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { useAssetDeletedEvents } from '../hooks/useAssetReadyEvents';
import { usePagedLoader } from '../hooks/usePagedLoader';
import type { Asset, AssetDeletedEvent, AssetKind, LibraryAnchor, NFOFilterField, OrientationFilter, SearchAssetsParams, SortKey } from '../types/api';
import {
  clearRestoreParamFromLocation,
  decodeReturnState,
  encodeReturnState,
  resetGridState,
  savePageState,
  saveViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { removeAssetById } from '../utils/assetSort';

const pageSize = 100;
const searchStateKey = 'search';

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
  { key: 'tag', label: 'NFO 标签', placeholder: '选择或输入标签' },
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
  const initialStateRef = useRef(
    decodeReturnState<SearchPageState>(searchParams.get('restore'), defaultSearchState),
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
  const [sizeMinMB, setSizeMinMB] = useState(initialStateRef.current.sizeMinMB);
  const [sizeMaxMB, setSizeMaxMB] = useState(initialStateRef.current.sizeMaxMB);
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
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
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
  const loadAssets = useCallback((page: number) => api.searchAssets(page, pageSize, searchRequest), [searchRequest]);
  const { items, hasMore, loading, error, loadMore, jumpToPage, mutateItems } = usePagedLoader<Asset>(loadAssets, [searchKey]);
  const handleAssetDeleted = useCallback((event: AssetDeletedEvent) => mutateItems((current) => removeAssetById(current, event.id)), [mutateItems]);
  useAssetDeletedEvents(handleAssetDeleted, [handleAssetDeleted]);

  useEffect(() => {
    let live = true;
    async function loadAnchors() {
      try {
        const result = await api.searchAnchors(pageSize, searchRequest);
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
  }, [searchRequest]);

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
      ...gridStateRef.current,
      dateFrom,
      dateTo,
      durationMaxMinutes,
      durationMinMinutes,
      focusAssetId: null,
      loadedItemCount: items.length,
      loadedStartIndex,
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
      sidebarCollapsed: sidebarState.sidebarCollapsed,
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
      items.length,
      loadedStartIndex,
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
      sidebarState.sidebarCollapsed,
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
  }, [searchKey]);

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
    saveViewerReturnPath('/search');
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

  useEffect(() => {
    restoreSidebarState({ sidebarCollapsed: initialStateRef.current.sidebarCollapsed });
  }, [restoreSidebarState]);

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
            onLoadMore={loadMore}
            onOpenAsset={handleOpenAsset}
            onOpenViewer={handleOpenViewer}
            onAssetMissing={(asset) => mutateItems((current) => removeAssetById(current, asset.id))}
            onPressPreviewChange={setPressPreviewAsset}
            onScrollRatioChange={setScrollRatio}
            onScrollStateChange={handleGridScrollState}
            totalCount={totalCount}
            loadedStartIndex={loadedStartIndex}
            focusAssetId={initialStateRef.current.focusAssetId}
            scrollSignal={scrollResetSignal}
            scrollTarget={scrollTarget}
            scrollTopTarget={scrollTopTarget}
            buildViewerUrl={(asset) => buildViewerUrl(asset, searchRequest, currentPageState())}
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

function buildViewerUrl(asset: Asset, params: SearchAssetsParams, state: SearchPageState) {
  const query = new URLSearchParams(searchQueryEntries(params));
  query.set('context', 'search');
  query.set('returnPath', '/search');
  return `/viewer/${asset.id}?${query.toString()}&returnState=${encodeReturnState(state)}`;
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
