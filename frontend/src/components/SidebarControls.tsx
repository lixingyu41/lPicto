import type { CSSProperties } from 'react';
import { ChevronRight, Image as ImageIcon, Images, Star, Video } from 'lucide-react';
import type { Album, AlbumGroup, AssetKind, AssetRating, OrientationFilter } from '../types/api';

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
  return (
    <div className="sidebar-list">
      {assetKinds.map((kind) => (
        <button className={value === kind ? 'sidebar-list-row active' : 'sidebar-list-row'} key={kind} type="button" onClick={() => onChange(kind)}>
          {kind === 'all' ? <Images size={14} /> : kind === 'image' ? <ImageIcon size={14} /> : <Video size={14} />}
          <span>{assetKindLabel(kind)}</span>
        </button>
      ))}
    </div>
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

export function SidebarRatingFilter({ onChange, value }: { onChange: (value: AssetRating) => void; value: AssetRating }) {
  return (
    <div className="sidebar-field sidebar-rating-filter">
      <span>星级</span>
      <div className="rating-stars sidebar-rating-stars" role="radiogroup" aria-label="星级">
        {ratingValues.map((rating) => (
          <button
            aria-checked={value === rating}
            aria-label={rating === 0 ? '未评级' : `${rating} 星`}
            className={rating === 0 ? ratingZeroClass(value) : ratingStarClass(value, rating)}
            key={rating}
            role="radio"
            title={rating === 0 ? '未评级' : `${rating} 星`}
            type="button"
            onClick={() => onChange(rating)}
          >
            {rating === 0 ? <span className="rating-zero-label">0</span> : <Star size={15} fill={rating <= value && value > 0 ? 'currentColor' : 'none'} />}
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
  showUnassigned?: boolean;
  unassignedActive?: boolean;
}) {
  const selected = new Set(selectedIds);
  const collapsedGroups = new Set(collapsedGroupKeys);
  const buckets = buildSidebarAlbumBuckets(albums, groups).filter((bucket) => bucket.albums.length > 0);
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
      ) : (
        <div className="sidebar-control-subtitle">{label}</div>
      )}
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
            <div className="album-group-block" key={bucket.key}>
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
            </div>
          );
        })}
      {!collapsed && albums.length === 0 && <div className="muted-line">{emptyLabel}</div>}
    </div>
  );
}

const assetKinds: AssetKind[] = ['all', 'video', 'image'];
const ratingValues: AssetRating[] = [0, 1, 2, 3, 4, 5];
export const sidebarOrientationOptions: Array<SidebarButtonGroupOption<OrientationFilter>> = [
  { value: 'all', label: '任意' },
  { value: 'landscape', label: '横屏' },
  { value: 'portrait', label: '竖屏' },
];

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

function ratingZeroClass(value: AssetRating) {
  return value === 0 ? 'rating-star-button active zero' : 'rating-star-button zero';
}

function ratingStarClass(value: AssetRating, rating: AssetRating) {
  return rating <= value && value > 0 ? 'rating-star-button active' : 'rating-star-button';
}

export function assetKindLabel(value: AssetKind) {
  switch (value) {
    case 'image':
      return '图片';
    case 'video':
      return '视频';
    default:
      return '全部';
  }
}
