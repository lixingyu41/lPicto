import { useCallback, useEffect, useState } from 'react';
import { Check, ChevronRight, Plus, Trash2, X } from 'lucide-react';
import { api } from '../api/client';
import type {
  Album,
  AlbumGroup,
  AlbumMediaFilter,
  AlbumOrientationFilter,
  AlbumSource,
  AlbumSourceInput,
  SourceFolder,
} from '../types/api';

interface AlbumEditorProps {
  groups: AlbumGroup[];
  initialAlbum?: Album | null;
  onClose: () => void;
  onConfirm: (name: string, sources: AlbumSourceInput[], groupId: number | null) => void;
}

export default function AlbumEditor({
  groups,
  initialAlbum,
  onClose,
  onConfirm,
}: AlbumEditorProps) {
  const [children, setChildren] = useState<Record<string, SourceFolder[]>>({});
  const [rootFolder, setRootFolder] = useState<SourceFolder | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [albumName, setAlbumName] = useState(initialAlbum?.name ?? '');
  const [groupId, setGroupId] = useState<number | null>(initialAlbum?.groupId ?? null);
  const [mediaFilter, setMediaFilter] = useState<AlbumMediaFilter>('all');
  const [orientationFilter, setOrientationFilter] = useState<AlbumOrientationFilter>('all');
  const [recursive, setRecursive] = useState(true);
  const [sourceRules, setSourceRules] = useState<AlbumSourceInput[]>(() =>
    initialAlbum?.sources.map((source) => ({
      relPath: source.relPath,
      recursive: source.recursive,
      mediaTypeFilter: source.mediaTypeFilter,
      orientationFilter: source.orientationFilter,
    })) ?? [],
  );
  const [loading, setLoading] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const title = initialAlbum ? '编辑相册' : '添加相册';

  const loadChildren = useCallback(async (relPath: string) => {
    setLoading((prev) => new Set(prev).add(relPath));
    try {
      const result = await api.albumSourceFolders(relPath);
      if (relPath === '') setRootFolder(result.current);
      setChildren((prev) => ({ ...prev, [relPath]: result.items }));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取文件夹失败');
    } finally {
      setLoading((prev) => {
        const next = new Set(prev);
        next.delete(relPath);
        return next;
      });
    }
  }, []);

  useEffect(() => {
    void loadChildren('');
  }, [loadChildren]);

  function toggleExpanded(relPath: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(relPath)) next.delete(relPath);
      else next.add(relPath);
      return next;
    });
    if (!children[relPath]) void loadChildren(relPath);
  }

  function toggleSelected(relPath: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(relPath)) next.delete(relPath);
      else next.add(relPath);
      return next;
    });
  }

  const selectedPaths = Array.from(selected);
  const draftSources = selectedPaths.map((relPath) => ({
    relPath,
    recursive,
    mediaTypeFilter: mediaFilter,
    orientationFilter,
  }));
  const allSources = [...sourceRules, ...draftSources];
  const canFinish = albumName.trim().length > 0 && allSources.length > 0;

  function addSourceRules() {
    if (draftSources.length === 0) return;
    setSourceRules((prev) => [...prev, ...draftSources]);
    setSelected(new Set());
  }

  function removeSourceRule(index: number) {
    setSourceRules((prev) => prev.filter((_, itemIndex) => itemIndex !== index));
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <div className="folder-picker" role="dialog" aria-modal="true" aria-label={title}>
        <div className="modal-title">
          <span>{title}</span>
          <button type="button" onClick={onClose} title="关闭">
            <X size={17} />
          </button>
        </div>
        {error && <div className="error-line">{error}</div>}
        <div className="album-form-grid">
          <label className="settings-field">
            <span>名称</span>
            <input value={albumName} placeholder="例如：竖屏视频" onChange={(event) => setAlbumName(event.target.value)} />
          </label>
          <label className="settings-field">
            <span>分组</span>
            <select
              value={groupId ?? ''}
              onChange={(event) => setGroupId(event.target.value ? Number(event.target.value) : null)}
            >
              <option value="">未分组</option>
              {groups.map((group) => (
                <option key={group.id} value={group.id}>
                  {group.name}
                </option>
              ))}
            </select>
          </label>
          <label className="settings-field">
            <span>类型</span>
            <select value={mediaFilter} onChange={(event) => setMediaFilter(event.target.value as AlbumMediaFilter)}>
              <option value="all">全部</option>
              <option value="image">照片</option>
              <option value="video">视频</option>
              <option value="audio">音频</option>
            </select>
          </label>
          <label className="settings-field">
            <span>方向</span>
            <select
              value={orientationFilter}
              onChange={(event) => setOrientationFilter(event.target.value as AlbumOrientationFilter)}
            >
              <option value="all">全部</option>
              <option value="portrait">竖屏</option>
              <option value="landscape">横屏</option>
            </select>
          </label>
          <label className="settings-check-row">
            <input type="checkbox" checked={recursive} onChange={(event) => setRecursive(event.target.checked)} />
            <span>包含子文件夹</span>
          </label>
        </div>
        <div className="album-rule-toolbar">
          <button className="text-button" type="button" disabled={draftSources.length === 0} onClick={addSourceRules}>
            <Plus size={15} />
            加入筛选
          </button>
          <span>{sourceRules.length > 0 ? `已加入 ${sourceRules.length} 条筛选` : '可重复加入不同筛选'}</span>
        </div>
        {sourceRules.length > 0 && (
          <div className="album-rule-list">
            {sourceRules.map((source, index) => (
              <div className="album-rule-row" key={`${source.relPath}-${source.mediaTypeFilter}-${source.orientationFilter}-${source.recursive}-${index}`}>
                <span>{displayRelPath(source.relPath)} · {sourceFilterLabel(source)}</span>
                <button type="button" title="移除筛选" onClick={() => removeSourceRule(index)}>
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
          </div>
        )}
        <div className="folder-tree-picker">
          {rootFolder?.included ? (
            <AlbumFolderTreeNode
              childrenMap={children}
              expanded={expanded}
              folder={rootFolder}
              key={rootFolder.relPath || 'album-root'}
              loading={loading}
              selected={selected}
              onExpand={toggleExpanded}
              onSelect={toggleSelected}
            />
          ) : (
            (children[''] ?? []).map((folder) => (
              <AlbumFolderTreeNode
                childrenMap={children}
                expanded={expanded}
                folder={folder}
                key={folder.relPath}
                loading={loading}
                selected={selected}
                onExpand={toggleExpanded}
                onSelect={toggleSelected}
              />
            ))
          )}
          {!rootFolder && loading.has('') && <div className="muted-line">读取中</div>}
          {rootFolder && children['']?.length === 0 && !rootFolder.included && (
            <div className="muted-line">没有可选的来源文件夹</div>
          )}
        </div>
        <div className="modal-actions">
          <span>{allSources.length > 0 ? `${allSources.length} 条筛选` : '未选择文件夹'}</span>
          <button className="text-button" type="button" onClick={onClose}>
            取消
          </button>
          <button
            className="command-button"
            type="button"
            disabled={!canFinish}
            onClick={() => onConfirm(albumName.trim(), allSources, groupId)}
          >
            <Check size={16} />
            {initialAlbum ? '保存' : '完成'}
          </button>
        </div>
      </div>
    </div>
  );
}

function AlbumFolderTreeNode({
  folder,
  childrenMap,
  expanded,
  loading,
  selected,
  onExpand,
  onSelect,
}: {
  folder: SourceFolder;
  childrenMap: Record<string, SourceFolder[]>;
  expanded: Set<string>;
  loading: Set<string>;
  selected: Set<string>;
  onExpand: (relPath: string) => void;
  onSelect: (relPath: string) => void;
}) {
  const isExpanded = expanded.has(folder.relPath);
  const children = childrenMap[folder.relPath] ?? [];
  const checkboxDisabled = !folder.included;
  const checked = selected.has(folder.relPath);
  const note = !folder.included
    ? '展开'
    : folder.selected
        ? '来源'
        : loading.has(folder.relPath)
          ? '读取中'
          : '';
  return (
    <div className="picker-node-group">
      <div className="picker-node" style={{ paddingLeft: 10 + Math.max(0, folder.depth - 1) * 18 }}>
        <button className={isExpanded ? 'expand-button expanded' : 'expand-button'} type="button" onClick={() => onExpand(folder.relPath)}>
          <ChevronRight size={15} />
        </button>
        <label>
          <input type="checkbox" checked={checked} disabled={checkboxDisabled} onChange={() => onSelect(folder.relPath)} />
          <span>{folder.relPath ? folder.name : displayRelPath(folder.relPath)}</span>
        </label>
        <small>{note}</small>
      </div>
      {isExpanded &&
        children.map((child) => (
          <AlbumFolderTreeNode
            childrenMap={childrenMap}
            expanded={expanded}
            folder={child}
            key={child.relPath}
            loading={loading}
            selected={selected}
            onExpand={onExpand}
            onSelect={onSelect}
          />
        ))}
    </div>
  );
}

function sourceFilterLabel(source: Pick<AlbumSource, 'mediaTypeFilter' | 'orientationFilter' | 'recursive'>) {
  const type = source.mediaTypeFilter === 'image' ? '照片' : source.mediaTypeFilter === 'video' ? '视频' : source.mediaTypeFilter === 'audio' ? '音频' : '全部';
  const orientation =
    source.orientationFilter === 'portrait' ? '竖屏' : source.orientationFilter === 'landscape' ? '横屏' : '全部方向';
  return `${type} · ${orientation} · ${source.recursive ? '含子文件夹' : '仅本层'}`;
}

function displayRelPath(relPath: string) {
  return relPath ? `/${relPath}` : '全部存储';
}
