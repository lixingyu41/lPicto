import { useCallback, useEffect, useRef, useState } from 'react';
import AssetGrid from '../components/AssetGrid';
import AssetInfoPanel from '../components/AssetInfoPanel';
import EmptyState from '../components/EmptyState';
import LibraryIndexRail from '../components/LibraryIndexRail';
import PressPreviewOverlay from '../components/PressPreviewOverlay';
import { api } from '../api/client';
import { usePagedLoader } from '../hooks/usePagedLoader';
import type { Asset, AssetKind, LibraryAnchor, SortKey } from '../types/api';
import { useSidebarPanel } from '../components/SidebarContext';
import type { AssetGroupMode } from '../utils/assetGrouping';

const pageSize = 100;

export default function LibraryPage() {
  const [type, setType] = useState<AssetKind>('all');
  const [sort, setSort] = useState<SortKey>('timeline_desc');
  const [query, setQuery] = useState('');
  const [anchors, setAnchors] = useState<LibraryAnchor[]>([]);
  const [scrollTarget, setScrollTarget] = useState<{ ratio: number; signal: number } | undefined>();
  const [scrollRatio, setScrollRatio] = useState(0);
  const [groupMode, setGroupMode] = useState<AssetGroupMode>('none');
  const [pressPreviewAsset, setPressPreviewAsset] = useState<Asset | null>(null);
  const indexPageRef = useRef(1);
  const seekSignalRef = useRef(0);
  const loadAssets = useCallback(
    (page: number) => api.libraryAssets(page, pageSize, type, sort, query),
    [query, sort, type],
  );
  const { items, hasMore, loading, error, loadMore, jumpToPage } = usePagedLoader<Asset>(loadAssets, [type, sort, query]);

  useEffect(() => {
    indexPageRef.current = 1;
    setScrollTarget(undefined);
  }, [query, sort, type]);

  useEffect(() => {
    let live = true;
    async function loadAnchors() {
      try {
        const result = await api.libraryAnchors(pageSize, type, sort, query);
        if (live) setAnchors(result.items);
      } catch {
        if (live) setAnchors([]);
      }
    }
    void loadAnchors();
    return () => {
      live = false;
    };
  }, [query, sort, type]);

  useEffect(() => {
    const options = groupOptionsForSort(sort).map((option) => option.value);
    if (!options.includes(groupMode)) {
      setGroupMode('none');
    }
  }, [groupMode, sort]);

  const seekIndex = useCallback(
    (_anchor: LibraryAnchor, page: number, ratio: number) => {
      const signal = seekSignalRef.current + 1;
      seekSignalRef.current = signal;
      setScrollTarget({ ratio, signal });
      if (page === indexPageRef.current) return;
      indexPageRef.current = page;
      void jumpToPage(page).then(() => {
        if (seekSignalRef.current !== signal) return;
        const nextSignal = seekSignalRef.current + 1;
        seekSignalRef.current = nextSignal;
        setScrollTarget({ ratio, signal: nextSignal });
      });
    },
    [jumpToPage],
  );

  useSidebarPanel(
    'library',
    <div className="sidebar-control-stack">
      <div className="sidebar-control-title">图库筛选</div>
      <div className="sidebar-segmented">
        {(['all', 'image', 'video'] as AssetKind[]).map((value) => (
          <button className={type === value ? 'active' : ''} key={value} type="button" onClick={() => setType(value)}>
            {value === 'all' ? '全部' : value === 'image' ? '照片' : '视频'}
          </button>
        ))}
      </div>
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
        <span>搜索</span>
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="文件名" />
      </label>
      <div className="sidebar-control-title">分组</div>
      <div className="sidebar-segmented group-mode-segmented">
        {groupOptionsForSort(sort).map((option) => (
          <button
            className={groupMode === option.value ? 'active' : ''}
            key={option.value}
            type="button"
            onClick={() => setGroupMode(option.value)}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>,
    [type, sort, query, groupMode],
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
            onLoadMore={loadMore}
            onScrollRatioChange={setScrollRatio}
            groupMode={groupMode}
            sort={sort}
            scrollTarget={scrollTarget}
            onPressPreviewChange={setPressPreviewAsset}
            buildViewerUrl={(asset) =>
              `/viewer/${asset.id}?context=library&type=${type}&sort=${sort}&q=${encodeURIComponent(query)}`
            }
          />
          <LibraryIndexRail anchors={anchors} sort={sort} scrollRatio={scrollRatio} onSeek={seekIndex} />
          <PressPreviewOverlay asset={pressPreviewAsset} />
        </div>
      )}
    </section>
  );
}

function groupOptionsForSort(sort: SortKey): Array<{ value: AssetGroupMode; label: string }> {
  if (sort === 'filename') {
    return [
      { value: 'none', label: '不分' },
      { value: 'letter', label: '首字母' },
    ];
  }
  if (sort === 'size') {
    return [
      { value: 'none', label: '不分' },
      { value: 'size', label: '大小' },
    ];
  }
  return [
    { value: 'none', label: '不分' },
    { value: 'day', label: '日' },
    { value: 'month', label: '月' },
    { value: 'year', label: '年' },
  ];
}
