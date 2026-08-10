import { Fragment, useEffect, useState, type CSSProperties, type ReactNode } from 'react';
import { ChevronRight, Database, EyeOff, Image as ImageIcon, Images, ListChecks, MonitorSmartphone, Music, RectangleHorizontal, RectangleVertical, RotateCw, Sparkles, Square, Star, StarOff, Tags, Trash2, Video } from 'lucide-react';
import type { Album, AlbumGroup, AssetKind, AssetRatingFilter, OrientationFilter } from '../types/api';
import { CompactSidebarMenu, CompactSidebarMenuGroup } from './CompactSidebarMenu';
import {
  assetGridBatchStateEvent,
  assetGridBatchStateRequestEvent,
  dispatchAssetGridBatchCommand,
  type AssetGridBatchState,
} from '../utils/batchSelection';

export interface SidebarSelectOption {
  disabled?: boolean;
  label: string;
  value: string;
}

export interface SidebarSelectGroup {
  label: string;
  options: SidebarSelectOption[];
}

export function SidebarSelect({
  disabled = false,
  emptyLabel = '暂无可选项',
  groups,
  label,
  onChange,
  options = [],
  value,
}: {
  disabled?: boolean;
  emptyLabel?: string;
  groups?: SidebarSelectGroup[];
  label: string;
  onChange: (value: string) => void;
  options?: SidebarSelectOption[];
  value: string;
}) {
  const activeGroups = groups?.filter((group) => group.options.length > 0) ?? [];
  const hasOptions = options.length > 0 || activeGroups.length > 0;

  return (
    <label className="sidebar-field sidebar-select-field">
      <span>{label}</span>
      <select value={hasOptions ? value : ''} disabled={disabled || !hasOptions} onChange={(event) => onChange(event.target.value)}>
        {!hasOptions && <option value="">{emptyLabel}</option>}
        {options.map((option) => (
          <option disabled={option.disabled} key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
        {activeGroups.map((group) => (
          <optgroup key={group.label} label={group.label}>
            {group.options.map((option) => (
              <option disabled={option.disabled} key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </optgroup>
        ))}
      </select>
    </label>
  );
}

export function SidebarMediaTypeList({ onChange, value }: { onChange: (value: AssetKind) => void; value: AssetKind }) {
  return <SidebarIconMenu label="类型" value={value} options={assetKindOptions} onChange={onChange} />;
}

export function SidebarOrientationFilter({ onChange, value }: { onChange: (value: OrientationFilter) => void; value: OrientationFilter }) {
  return <SidebarIconMenu label="方向" value={value} options={orientationFilterOptions} onChange={onChange} />;
}

export function SidebarRatingFilter({ onChange, value }: { onChange: (value: AssetRatingFilter) => void; value: AssetRatingFilter }) {
  return <SidebarIconMenu label="星级" value={value} options={ratingFilterOptions} onChange={onChange} />;
}

export function SidebarFilterIconRow({ children }: { children: ReactNode }) {
  return (
    <CompactSidebarMenuGroup>
      <div className="sidebar-filter-icon-row">
        {children}
        <SidebarBatchSelectionMenu />
      </div>
    </CompactSidebarMenuGroup>
  );
}

const emptyBatchState: AssetGridBatchState = {
  available: false,
  busy: false,
  canAutoSelect: false,
  message: '',
  progress: null,
  selectedCount: 0,
  selectionMode: false,
};

function SidebarBatchSelectionMenu() {
  const [state, setState] = useState<AssetGridBatchState>(emptyBatchState);
  useEffect(() => {
    const handleState = (event: Event) => setState((event as CustomEvent<AssetGridBatchState>).detail);
    window.addEventListener(assetGridBatchStateEvent, handleState);
    window.dispatchEvent(new Event(assetGridBatchStateRequestEvent));
    return () => window.removeEventListener(assetGridBatchStateEvent, handleState);
  }, []);
  const unavailable = !state.available || state.busy;
  const selectionDisabled = unavailable || state.selectedCount === 0;
  const options = [
    {
      active: state.selectionMode,
      disabled: !state.available || state.busy,
      icon: <Square size={18} fill={state.selectionMode ? 'currentColor' : 'none'} />,
      key: 'toggle-selection',
      label: state.selectionMode ? '退出选择' : '进入选择',
      onSelect: () => dispatchAssetGridBatchCommand('toggle-selection'),
    },
    ...(state.canAutoSelect ? [{
      disabled: unavailable,
      icon: <Sparkles size={18} />,
      key: 'auto-select',
      label: '自动选择重复项',
      onSelect: () => dispatchAssetGridBatchCommand('auto-select'),
    }] : []),
    {
      disabled: unavailable,
      icon: <ListChecks size={18} />,
      key: 'select-all',
      label: '全选已加载',
      onSelect: () => dispatchAssetGridBatchCommand('select-all'),
    },
    {
      disabled: unavailable || state.selectedCount === 0,
      icon: <Square size={18} />,
      key: 'clear',
      label: `清空选择 (${state.selectedCount})`,
      onSelect: () => dispatchAssetGridBatchCommand('clear'),
    },
    { disabled: selectionDisabled, icon: <Tags size={18} />, key: 'add-tag', label: '批量加标签', onSelect: () => dispatchAssetGridBatchCommand('add-tag') },
    { disabled: selectionDisabled, icon: <Star size={18} />, key: 'set-rating', label: '批量评分', onSelect: () => dispatchAssetGridBatchCommand('set-rating') },
    { disabled: selectionDisabled, icon: <Images size={18} />, key: 'add-album', label: '批量加入相册', onSelect: () => dispatchAssetGridBatchCommand('add-album') },
    { disabled: selectionDisabled, icon: <RotateCw size={18} />, key: 'rotate', label: '批量旋转', onSelect: () => dispatchAssetGridBatchCommand('rotate') },
    { disabled: selectionDisabled, icon: <EyeOff size={18} />, key: 'hide', label: '批量隐藏', onSelect: () => dispatchAssetGridBatchCommand('hide') },
    { disabled: selectionDisabled, icon: <Trash2 size={18} />, key: 'delete', label: '批量删除', onSelect: () => dispatchAssetGridBatchCommand('delete') },
    { disabled: selectionDisabled, icon: <Database size={18} />, key: 'delete-records', label: '删除记录', onSelect: () => dispatchAssetGridBatchCommand('delete-records') },
  ];
  return (
    <CompactSidebarMenu
      ariaLabel={`多选，已选择 ${state.selectedCount} 个`}
      title={`多选 · ${state.selectedCount} 个`}
      trigger={<ListChecks size={18} />}
      options={options}
      footer={(state.message || state.progress) && (
        <div className="sidebar-batch-status">
          {state.message && <span>{state.message}</span>}
          {state.progress && (
            <span className="batch-progress" role="progressbar" aria-valuemin={0} aria-valuemax={state.progress.total} aria-valuenow={state.progress.current}>
              <i style={{ width: `${state.progress.total > 0 ? (state.progress.current / state.progress.total) * 100 : 0}%` }} />
              <small>{state.progress.current}/{state.progress.total}</small>
            </span>
          )}
        </div>
      )}
    />
  );
}

interface SidebarIconMenuOption<T extends string | number> {
  badge?: string;
  label: string;
  renderIcon: (active: boolean) => ReactNode;
  value: T;
}

function SidebarIconMenu<T extends string | number>({
  label,
  onChange,
  options,
  value,
}: {
  label: string;
  onChange: (value: T) => void;
  options: Array<SidebarIconMenuOption<T>>;
  value: T;
}) {
  const active = options.find((option) => option.value === value) ?? options[0];
  return (
    <CompactSidebarMenu
      ariaLabel={`${label}: ${active.label}`}
      title={`${label}: ${active.label}`}
      trigger={
        <>
          {active.renderIcon(true)}
          {active.badge && <small>{active.badge}</small>}
        </>
      }
      options={options.map((option) => ({
        active: value === option.value,
        icon: option.renderIcon(value === option.value),
        key: String(option.value),
        label: option.label,
        onSelect: () => onChange(option.value),
      }))}
    />
  );
}

export interface SidebarButtonGroupOption<T extends string> {
  disabled?: boolean;
  label: string;
  value: T;
}

export function SidebarButtonGroup<T extends string>({
  columns,
  label,
  onChange,
  options,
  value,
}: {
  columns?: number;
  label: string;
  onChange: (value: T) => void;
  options: Array<SidebarButtonGroupOption<T>>;
  value: T;
}) {
  const style = { '--sidebar-button-columns': String(columns ?? options.length) } as CSSProperties;
  return (
    <div className="sidebar-field">
      <span>{label}</span>
      <div className="sidebar-button-group" style={style}>
        {options.map((option) => (
          <button
            aria-pressed={value === option.value}
            className={value === option.value ? 'active' : ''}
            disabled={option.disabled}
            key={option.value}
            type="button"
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  );
}

export function SidebarAlbumList({
  allActive = false,
  albums,
  collapsed = false,
  collapsedGroupKeys = [],
  collapsible = false,
  emptyLabel = '暂无相册',
  forceGroupHeaders = false,
  groups = [],
  label = '相册',
  onSelectAlbum,
  onSelectAll,
  onSelectUnassigned,
  onToggleCollapsed,
  onToggleGroup,
  selectedIds,
  showAll = false,
  showEmptyGroups = false,
  showLabel = true,
  showUnassigned = false,
  unassignedActive = false,
}: {
  allActive?: boolean;
  albums: Album[];
  collapsed?: boolean;
  collapsedGroupKeys?: string[];
  collapsible?: boolean;
  emptyLabel?: string;
  forceGroupHeaders?: boolean;
  groups?: AlbumGroup[];
  label?: string;
  onSelectAlbum: (album: Album) => void;
  onSelectAll?: () => void;
  onSelectUnassigned?: () => void;
  onToggleCollapsed?: () => void;
  onToggleGroup?: (key: string) => void;
  selectedIds: number[];
  showAll?: boolean;
  showEmptyGroups?: boolean;
  showLabel?: boolean;
  showUnassigned?: boolean;
  unassignedActive?: boolean;
}) {
  const selected = new Set(selectedIds);
  const collapsedGroups = new Set(collapsedGroupKeys);
  const buckets = buildSidebarAlbumBuckets(albums, groups).filter((bucket) => showEmptyGroups || bucket.albums.length > 0);
  return (
    <div className="sidebar-group-section sidebar-album-list">
      {collapsible ? (
        <button aria-expanded={!collapsed} className="album-group-row" type="button" onClick={onToggleCollapsed}>
          <span className={collapsed ? 'folder-expand-button' : 'folder-expand-button expanded'}>
            <ChevronRight size={15} />
          </span>
          <span>{label}</span>
          <small>{albums.length}</small>
        </button>
      ) : showLabel ? (
        <div className="sidebar-control-subtitle">{label}</div>
      ) : null}
      {!collapsed && showAll && (
        <button className={allActive ? 'album-row active' : 'album-row'} type="button" onClick={onSelectAll}>
          <i className="sidebar-list-marker" aria-hidden="true" />
          <span>全部</span>
          <small />
        </button>
      )}
      {!collapsed && showUnassigned && (
        <button className={unassignedActive ? 'album-row active' : 'album-row'} type="button" onClick={onSelectUnassigned}>
          <i className="sidebar-list-marker" aria-hidden="true" />
          <span>不在相册</span>
          <small />
        </button>
      )}
      {!collapsed &&
        buckets.map((bucket) => {
          const groupCollapsed = collapsedGroups.has(bucket.key);
          const showGroupHeader = forceGroupHeaders || buckets.length > 1;
          return (
            <Fragment key={bucket.key}>
              {showGroupHeader &&
                (onToggleGroup ? (
                  <button aria-expanded={!groupCollapsed} className="album-group-row" type="button" onClick={() => onToggleGroup(bucket.key)}>
                    <span className={groupCollapsed ? 'folder-expand-button' : 'folder-expand-button expanded'}>
                      <ChevronRight size={15} />
                    </span>
                    <span>{bucket.name}</span>
                    <small>{bucket.albums.length}</small>
                  </button>
                ) : (
                  <div className="sidebar-control-subtitle">{bucket.name}</div>
                ))}
              {!groupCollapsed &&
                bucket.albums.map((album) => (
                  <button
                    aria-pressed={selected.has(album.id)}
                    className={selected.has(album.id) ? 'album-row active' : 'album-row'}
                    key={album.id}
                    type="button"
                    onClick={() => onSelectAlbum(album)}
                  >
                    <i className="sidebar-list-marker" aria-hidden="true" />
                    <span>{album.name}</span>
                    <small>{album.assetCount}</small>
                  </button>
                ))}
            </Fragment>
          );
        })}
      {!collapsed && albums.length === 0 && <div className="muted-line">{emptyLabel}</div>}
    </div>
  );
}

const ratingValues: AssetRatingFilter[] = ['all', 0, 1, 2, 3, 4, 5];
export const sidebarOrientationOptions: Array<SidebarButtonGroupOption<OrientationFilter>> = [
  { value: 'all', label: '任意' },
  { value: 'landscape', label: '横屏' },
  { value: 'portrait', label: '竖屏' },
];

const assetKindOptions: Array<SidebarIconMenuOption<AssetKind>> = [
  { value: 'all', label: '全部', renderIcon: () => <Images size={18} /> },
  { value: 'video', label: '视频', renderIcon: () => <Video size={18} /> },
  { value: 'image', label: '图片', renderIcon: () => <ImageIcon size={18} /> },
  { value: 'audio', label: '音频', renderIcon: () => <Music size={18} /> },
];

const orientationFilterOptions: Array<SidebarIconMenuOption<OrientationFilter>> = [
  { value: 'all', label: '任意方向', renderIcon: () => <MonitorSmartphone size={18} /> },
  { value: 'landscape', label: '横屏', renderIcon: () => <RectangleHorizontal size={18} /> },
  { value: 'portrait', label: '竖屏', renderIcon: () => <RectangleVertical size={18} /> },
];

const ratingFilterOptions: Array<SidebarIconMenuOption<AssetRatingFilter>> = ratingValues.map((rating) => ({
  badge: rating === 'all' ? '全' : String(rating),
  label: rating === 'all' ? '全部' : rating === 0 ? '未评级' : `${rating} 星`,
  renderIcon: (active) =>
    rating === 0 ? <StarOff size={18} /> : <Star size={18} fill={rating !== 'all' && active ? 'currentColor' : 'none'} />,
  value: rating,
}));

interface SidebarAlbumBucket {
  key: string;
  name: string;
  albums: Album[];
}

function buildSidebarAlbumBuckets(albums: Album[], groups: AlbumGroup[]): SidebarAlbumBucket[] {
  const byGroup = new Map<number | null, Album[]>();
  albums.forEach((album) => {
    const key = album.groupId ?? null;
    const items = byGroup.get(key) ?? [];
    items.push(album);
    byGroup.set(key, items);
  });
  const buckets = groups.map((group) => ({
    key: albumGroupKey(group.id),
    name: group.name,
    albums: byGroup.get(group.id) ?? [],
  }));
  const knownGroupIds = new Set(groups.map((group) => group.id));
  Array.from(new Set(albums.map((album) => album.groupId).filter((id): id is number => id !== null && !knownGroupIds.has(id)))).forEach((id) => {
    buckets.push({ key: albumGroupKey(id), name: '未命名组', albums: byGroup.get(id) ?? [] });
  });
  const ungrouped = byGroup.get(null) ?? [];
  if (ungrouped.length > 0 || groups.length === 0) {
    buckets.push({ key: albumGroupKey(null), name: '未分组', albums: ungrouped });
  }
  return buckets;
}

function albumGroupKey(groupId: number | null) {
  return groupId === null ? 'ungrouped' : `group-${groupId}`;
}

export function assetKindLabel(value: AssetKind) {
  switch (value) {
    case 'image':
      return '图片';
    case 'video':
      return '视频';
    case 'audio':
      return '音频';
    default:
      return '全部';
  }
}
