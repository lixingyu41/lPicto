import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { RotateCcw } from 'lucide-react';
import { api } from '../api/client';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { useRestoreSidebarState, useSidebarPanel, useSidebarReturnState } from '../components/SidebarContext';
import { usePagedLoader } from '../hooks/usePagedLoader';
import type { Asset, AssetKind, OrientationFilter, SearchAssetsParams, SortKey } from '../types/api';
import {
  decodeReturnState,
  encodeReturnState,
  loadPageState,
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
  durationRange: string;
  nfoQuery: string;
  orientation: OrientationFilter;
  query: string;
  resolutionRange: string;
  sizeMaxMB: string;
  sizeMinMB: string;
  sort: SortKey;
  type: AssetKind;
}

const defaultSearchState: SearchPageState = {
  ...resetGridState(),
  dateFrom: '',
  dateTo: '',
  durationRange: '',
  nfoQuery: '',
  orientation: 'all',
  query: '',
  resolutionRange: '',
  sizeMaxMB: '',
  sizeMinMB: '',
  sort: 'timeline_desc',
  type: 'all',
};

export default function SearchPage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const initialStateRef = useRef(
    decodeReturnState<SearchPageState>(searchParams.get('restore'), loadPageState<SearchPageState>(searchStateKey, defaultSearchState)),
  );
  const [query, setQuery] = useState(initialStateRef.current.query);
  const [nfoQuery, setNFOQuery] = useState(initialStateRef.current.nfoQuery);
  const [type, setType] = useState<AssetKind>(initialStateRef.current.type);
  const [sort, setSort] = useState<SortKey>(initialStateRef.current.sort);
  const [resolutionRange, setResolutionRange] = useState(initialStateRef.current.resolutionRange);
  const [dateFrom, setDateFrom] = useState(initialStateRef.current.dateFrom);
  const [dateTo, setDateTo] = useState(initialStateRef.current.dateTo);
  const [durationRange, setDurationRange] = useState(initialStateRef.current.durationRange);
  const [orientation, setOrientation] = useState<OrientationFilter>(initialStateRef.current.orientation);
  const [sizeMinMB, setSizeMinMB] = useState(initialStateRef.current.sizeMinMB);
  const [sizeMaxMB, setSizeMaxMB] = useState(initialStateRef.current.sizeMaxMB);
  const [scrollTopTarget, setScrollTopTarget] = useState<{ scrollTop: number; signal: number } | undefined>(() =>
    initialStateRef.current.scrollTop > 0 ? { scrollTop: initialStateRef.current.scrollTop, signal: 1 } : undefined,
  );
  const [scrollResetSignal, setScrollResetSignal] = useState(0);
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const [, setGridUrlSignal] = useState(0);
  const gridStateRef = useRef<GridReturnState>(initialStateRef.current);
  const sidebarState = useSidebarReturnState();
  const restoreSidebarState = useRestoreSidebarState();
  const restoreRef = useRef({
    pending: initialStateRef.current.scrollTop > 0 || initialStateRef.current.loadedItemCount > pageSize,
    signal: 0,
  });

  const searchRequest = useMemo<SearchAssetsParams>(
    () => ({
      q: query.trim() || undefined,
      nfo: nfoQuery.trim() || undefined,
      type,
      sort,
      ...parseResolutionRange(resolutionRange),
      from: datetimeLocalToUnix(dateFrom),
      to: datetimeLocalToUnix(dateTo),
      ...parseDurationRange(durationRange),
      orientation,
      sizeMin: mbToBytes(sizeMinMB),
      sizeMax: mbToBytes(sizeMaxMB),
    }),
    [dateFrom, dateTo, durationRange, nfoQuery, orientation, query, resolutionRange, sizeMaxMB, sizeMinMB, sort, type],
  );
  const searchKey = useMemo(() => JSON.stringify(searchRequest), [searchRequest]);
  const loadAssets = useCallback((page: number) => api.searchAssets(page, pageSize, searchRequest), [searchRequest]);
  const { items, hasMore, loading, error, loadMore, mutateItems } = usePagedLoader<Asset>(loadAssets, [searchKey]);

  const currentPageState = useCallback(
    (): SearchPageState => ({
      ...gridStateRef.current,
      dateFrom,
      dateTo,
      durationRange,
      loadedItemCount: items.length,
      loadedStartIndex: 0,
      nfoQuery,
      orientation,
      query,
      resolutionRange,
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
      durationRange,
      items.length,
      nfoQuery,
      orientation,
      query,
      resolutionRange,
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
    if (restoreRef.current.pending) return;
    gridStateRef.current = resetGridState();
    setScrollResetSignal((value) => value + 1);
  }, [searchKey]);

  useEffect(() => {
    if (!restoreRef.current.pending || loading) return;
    const targetCount = Math.max(pageSize, initialStateRef.current.loadedItemCount);
    if (items.length < targetCount && hasMore) {
      void loadMore();
      return;
    }
    restoreRef.current.pending = false;
    restoreRef.current.signal += 1;
    setScrollTopTarget({ scrollTop: initialStateRef.current.scrollTop, signal: restoreRef.current.signal });
  }, [hasMore, items.length, loadMore, loading]);

  const handleGridScrollState = useCallback(
    (state: { ratio: number; scrollTop: number }) => {
      gridStateRef.current = {
        ...gridStateRef.current,
        loadedItemCount: items.length,
        loadedStartIndex: 0,
        scrollRatio: state.ratio,
        scrollTop: state.scrollTop,
      };
      setGridUrlSignal((value) => value + 1);
    },
    [items.length],
  );

  const handleOpenAsset = useCallback(() => {
    saveCurrentState();
    saveViewerReturnPath('/search');
  }, [saveCurrentState]);

  const handleOpenViewer = useCallback(
    (_asset: Asset, viewerUrl: string) => {
      navigate(viewerUrl, { state: { backgroundLocation: location } });
    },
    [location, navigate],
  );

  const resetFilters = useCallback(() => {
    setQuery('');
    setNFOQuery('');
    setType('all');
    setSort('timeline_desc');
    setResolutionRange('');
    setDateFrom('');
    setDateTo('');
    setDurationRange('');
    setOrientation('all');
    setSizeMinMB('');
    setSizeMaxMB('');
  }, []);

  useSidebarPanel(
    'search',
    <div className="sidebar-control-stack sidebar-search-panel">
      <div className="sidebar-panel-title-row">
        <div className="sidebar-control-title">搜索</div>
        <button className="sidebar-square-button" type="button" title="重置" aria-label="重置" onClick={resetFilters}>
          <RotateCcw size={15} />
        </button>
      </div>
      <div className="sidebar-segmented">
        {(['all', 'image', 'video'] as AssetKind[]).map((value) => (
          <button className={type === value ? 'active' : ''} key={value} type="button" onClick={() => setType(value)}>
            {value === 'all' ? '全部' : value === 'image' ? '照片' : '视频'}
          </button>
        ))}
      </div>
      <label className="sidebar-field">
        <span>文件名</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名" />
      </label>
      <label className="sidebar-field">
        <span>NFO</span>
        <input value={nfoQuery} onChange={(event) => setNFOQuery(event.target.value)} placeholder="演员 / ID / 标签 / 年份 / 标题" />
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
        <input value={resolutionRange} onChange={(event) => setResolutionRange(event.target.value)} placeholder="100-4000x100-3000" />
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
      <label className="sidebar-field">
        <span>视频时长（秒）</span>
        <input value={durationRange} onChange={(event) => setDurationRange(event.target.value)} placeholder="0-600" />
      </label>
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
      durationRange,
      nfoQuery,
      orientation,
      query,
      resetFilters,
      resolutionRange,
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
            onScrollStateChange={handleGridScrollState}
            scrollSignal={scrollResetSignal}
            scrollTopTarget={scrollTopTarget}
            buildViewerUrl={(asset) => buildViewerUrl(asset, searchRequest, currentPageState())}
          />
          <PressPreviewOverlay asset={pressPreviewAsset} />
        </div>
      )}
    </section>
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

function parseResolutionRange(value: string): Pick<SearchAssetsParams, 'widthMin' | 'widthMax' | 'heightMin' | 'heightMax'> {
  const [widthRange, heightRange] = value.toLowerCase().replace(/\s+/g, '').split(/[x×]/);
  if (!widthRange || !heightRange) return {};
  const width = parseNumberRange(widthRange);
  const height = parseNumberRange(heightRange);
  return { widthMin: width.min, widthMax: width.max, heightMin: height.min, heightMax: height.max };
}

function parseDurationRange(value: string): Pick<SearchAssetsParams, 'durationMin' | 'durationMax'> {
  const range = parseNumberRange(value);
  return { durationMin: range.min, durationMax: range.max };
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
