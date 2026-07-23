import { useCallback, useEffect, useState } from 'react';
import { Check, ChevronRight, FolderPlus, Pencil, Trash2, X } from 'lucide-react';
import type { ScanLibrary, ScanLibraryProgress, SourceFolder } from '../types/api';
import { api } from '../api/client';

export interface ScanManagerProps {
  libraries: ScanLibrary[];
  addOpen: boolean;
  editingLibrary: ScanLibrary | null;
  onRemoveLibrary: (id: string) => void;
  onSetEditingLibrary: (library: ScanLibrary | null) => void;
  onSetAddOpen: (open: boolean) => void;
  onCreateLibrary: (name: string, relPaths: string[]) => void;
  onUpdateLibrary: (id: string, name: string, relPaths: string[]) => void;
}

export function ScanManager({
  libraries,
  addOpen,
  editingLibrary,
  onRemoveLibrary,
  onSetEditingLibrary,
  onSetAddOpen,
  onCreateLibrary,
  onUpdateLibrary,
}: ScanManagerProps) {
  return (
    <>
      <div className="settings-panel">
        <div className="settings-panel-title">图库来源</div>
        <div className="library-list">
          {libraries.map((library) => (
            <div className="library-row" key={library.id}>
              <div className="library-info">
                <strong>{displayLibraryName(library.name)}</strong>
                <small>{library.exists ? '已连接' : '不可访问'} · {library.folders.length} 个文件夹</small>
                <div className="library-paths">
                  {library.folders.map((folder) => (
                    <span key={folder.relPath || 'root'}>{displayRelPath(folder.relPath)}</span>
                  ))}
                </div>
                <LibraryProgress progress={library.progress} />
              </div>
              <button className="library-manage-button" type="button" title="编辑" onClick={() => onSetEditingLibrary(library)}>
                <Pencil size={15} />
              </button>
              <button className="library-manage-button" type="button" title="删除" onClick={() => onRemoveLibrary(library.id)}>
                <Trash2 size={15} />
              </button>
            </div>
          ))}
          {libraries.length === 0 && <div className="muted-line">未添加图库</div>}
        </div>
        <div className="selected-folder-actions">
          <button className="command-button" type="button" onClick={() => onSetAddOpen(true)}>
            <FolderPlus size={16} />
            添加来源
          </button>
        </div>
      </div>
      {addOpen && (
        <FolderPickerModal
          confirmLabel="完成"
          title="添加来源"
          onClose={() => onSetAddOpen(false)}
          onConfirm={(name, relPaths) => onCreateLibrary(name, relPaths)}
        />
      )}
      {editingLibrary && (
        <FolderPickerModal
          confirmLabel="保存"
          excludeLibraryId={editingLibrary.id}
          initialName={editingLibrary.name}
          initialRelPaths={editingLibrary.folders.map((folder) => folder.relPath)}
          key={editingLibrary.id}
          title="编辑来源"
          onClose={() => onSetEditingLibrary(null)}
          onConfirm={(name, relPaths) => onUpdateLibrary(editingLibrary.id, name, relPaths)}
        />
      )}
    </>
  );
}

function LibraryProgress({ progress }: {
  progress: ScanLibraryProgress;
}) {
  const discovered = Math.max(progress.discoveredFiles, progress.scannedFiles + progress.unscannedFiles, progress.scannedFiles);
  const scanned = Math.min(progress.scannedFiles, discovered);
  const mediaReady = progress.thumb.ready;
  const proxiedVideos = progress.videoProxy?.ready ?? 0;
  const thumbTotal = Math.max(0, progress.thumb.total - progress.thumb.notRequired);
  const thumbPercent = thumbTotal > 0 ? Math.min(100, Math.round((mediaReady / thumbTotal) * 100)) : 0;
  return (
    <div className="library-progress">
      <div className="library-stat-strip">
        <span>
          <em>已发现</em>
          <strong>{discovered}</strong>
        </span>
        <span>
          <em>已扫描</em>
          <strong>{scanned}</strong>
        </span>
        <span>
          <em>已建缩略图</em>
          <strong>{mediaReady}</strong>
        </span>
        <span>
          <em>已代理视频</em>
          <strong>{proxiedVideos}</strong>
        </span>
      </div>
      <div className="library-progress-bars">
        <div className="progress-row">
          <div className="progress-row-title">
            <span>上次清点</span>
            <strong>{discovered.toLocaleString()}{progress.discoveredAt ? ` · ${timeLabel(progress.discoveredAt)}` : ' · 尚未执行'}</strong>
          </div>
        </div>
        <div className="progress-row">
          <div className="progress-row-title">
            <span>缩略图</span>
            <strong>{Math.min(mediaReady, thumbTotal).toLocaleString()}/{thumbTotal.toLocaleString()}</strong>
          </div>
          <div className="progress-bar" aria-label={`缩略图 ${mediaReady}/${thumbTotal}`}>
            <div className="progress-fill" style={{ width: `${thumbPercent}%` }} />
          </div>
        </div>
      </div>
    </div>
  );
}

function timeLabel(value: number) {
  return new Date(value * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function displayRelPath(relPath: string) {
  return relPath ? `/${relPath}` : '全部存储';
}

function displayLibraryName(name: string) {
  return name === '默认 LIB' ? '默认来源' : name;
}

function FolderPickerModal({
  confirmLabel,
  excludeLibraryId,
  initialName,
  initialRelPaths,
  onClose,
  onConfirm,
  title,
}: {
  confirmLabel: string;
  excludeLibraryId?: string;
  initialName?: string;
  initialRelPaths?: string[];
  onClose: () => void;
  onConfirm: (name: string, relPaths: string[]) => void;
  title: string;
}) {
  const [children, setChildren] = useState<Record<string, SourceFolder[]>>({});
  const [rootFolder, setRootFolder] = useState<SourceFolder | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<Set<string>>(() => new Set(initialRelPaths ?? []));
  const [libraryName, setLibraryName] = useState(initialName ?? '');
  const [loading, setLoading] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);

  const loadChildren = useCallback(async (relPath: string) => {
    setLoading((prev) => new Set(prev).add(relPath));
    try {
      const result = await api.sourceFolders(relPath, excludeLibraryId);
      if (relPath === '') {
        setRootFolder(result.current);
      }
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
  }, [excludeLibraryId]);

  useEffect(() => {
    void loadChildren('');
  }, [loadChildren]);

  useEffect(() => {
    const ancestors = new Set<string>();
    for (const relPath of initialRelPaths ?? []) {
      for (const ancestor of folderAncestorPaths(relPath)) {
        ancestors.add(ancestor);
      }
    }
    if (ancestors.size === 0) return;
    setExpanded((prev) => {
      const next = new Set(prev);
      ancestors.forEach((ancestor) => next.add(ancestor));
      return next;
    });
    ancestors.forEach((ancestor) => void loadChildren(ancestor));
  }, [initialRelPaths, loadChildren]);

  function toggleExpanded(relPath: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(relPath)) next.delete(relPath);
      else next.add(relPath);
      return next;
    });
    if (!children[relPath]) {
      void loadChildren(relPath);
    }
  }

  function toggleSelected(relPath: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(relPath)) next.delete(relPath);
      else {
        next.add(relPath);
        for (const selectedPath of Array.from(next)) {
          if (selectedPath !== relPath && isDescendantPath(selectedPath, relPath)) {
            next.delete(selectedPath);
          }
        }
      }
      return next;
    });
  }

  const selectedPaths = Array.from(selected);
  const canFinish = selectedPaths.length > 0 && libraryName.trim().length > 0;

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
        <div className="folder-tree-picker">
          {rootFolder && (
            <FolderTreeNode
              childrenMap={children}
              expanded={expanded}
              folder={rootFolder}
              key={rootFolder.relPath || 'photo-root'}
              loading={loading}
              selected={selected}
              onExpand={toggleExpanded}
              onSelect={toggleSelected}
            />
          )}
          {!rootFolder && loading.has('') && <div className="muted-line">读取中</div>}
          {!rootFolder && children['']?.length === 0 && <div className="muted-line">没有可选择的文件夹</div>}
        </div>
        <div className="library-name-field">
          <label htmlFor="library-name">来源名称</label>
          <input
            id="library-name"
            value={libraryName}
            placeholder="例如：家庭照片"
            onChange={(event) => setLibraryName(event.target.value)}
          />
        </div>
        <div className="modal-actions">
          <span>{selectedPaths.length > 0 ? `已选 ${selectedPaths.length} 个文件夹` : '未选择文件夹'}</span>
          <button className="text-button" type="button" onClick={onClose}>
            取消
          </button>
          <button className="command-button" type="button" disabled={!canFinish} onClick={() => onConfirm(libraryName.trim(), selectedPaths)}>
            <Check size={16} />
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

function FolderTreeNode({
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
  const includedBySelectedParent = hasSelectedAncestor(folder.relPath, selected);
  const checkboxDisabled = folder.included || includedBySelectedParent;
  const checked = folder.included || includedBySelectedParent || selected.has(folder.relPath);
  const note = folder.included
    ? folder.selected
      ? '已添加'
      : '已被上级包含'
    : includedBySelectedParent
      ? '已被上级选择'
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
          <FolderTreeNode
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

function hasSelectedAncestor(relPath: string, selected: Set<string>) {
  for (const selectedPath of selected) {
    if (selectedPath === relPath) {
      continue;
    }
    if (isDescendantPath(relPath, selectedPath)) {
      return true;
    }
  }
  return false;
}

function isDescendantPath(relPath: string, ancestorPath: string) {
  return (ancestorPath === '' && relPath !== '') || relPath.startsWith(`${ancestorPath}/`);
}

function folderAncestorPaths(relPath: string) {
  const parts = relPath.split('/').filter(Boolean);
  const ancestors = [''];
  for (let index = 1; index < parts.length; index += 1) {
    ancestors.push(parts.slice(0, index).join('/'));
  }
  return ancestors;
}
