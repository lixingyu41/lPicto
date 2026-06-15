import { useCallback, useEffect, useState } from 'react';
import { Check, ChevronRight, FolderPlus, Pencil, RotateCw, Trash2, X } from 'lucide-react';
import Toolbar from '../components/Toolbar';
import { api } from '../api/client';
import type { CleanupStatus, ProcessingProgress, ScanLibrary, ScanLibraryProgress, ScanStatus, SourceFolder, WorkStatusCounts } from '../types/api';
import { formatBytes } from '../utils/format';
import { loadGridRowHeightLevel, saveGridRowHeightLevel, type GridRowHeightLevel } from '../utils/gridPrefs';
import { loadThemeMode, saveThemeMode, type ThemeMode } from '../utils/themePrefs';
import { loadViewerPrefs, playbackRates, saveViewerPrefs, type ViewerPrefs } from '../utils/viewerPrefs';

const settingsSections = [
  { id: 'libraries', label: '图库' },
  { id: 'appearance', label: '外观' },
  { id: 'viewer', label: '查看器' },
] as const;

type SettingsSectionId = (typeof settingsSections)[number]['id'];

export default function SettingsPage() {
  const [status, setStatus] = useState<ScanStatus | null>(null);
  const [progress, setProgress] = useState<ProcessingProgress | null>(null);
  const [libraries, setLibraries] = useState<ScanLibrary[]>([]);
  const [cleanup, setCleanup] = useState<CleanupStatus | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [editingLibrary, setEditingLibrary] = useState<ScanLibrary | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [rowHeightLevel, setRowHeightLevel] = useState<GridRowHeightLevel>(() => loadGridRowHeightLevel());
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => loadThemeMode());
  const [viewerPrefs, setViewerPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());
  const [activeSettingsSection, setActiveSettingsSection] = useState<SettingsSectionId>('libraries');

  const refreshLibraries = useCallback(async () => {
    const librariesResult = await api.scanLibraries();
    setLibraries(librariesResult.items);
  }, []);

  const refreshActivity = useCallback(async () => {
    const activity = await api.settingsActivity();
    setStatus(activity.scan);
    setProgress(activity.progress);
    setCleanup(activity.cleanup);
  }, []);

  const refreshInitial = useCallback(async () => {
    try {
      await Promise.all([refreshActivity(), refreshLibraries()]);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取设置失败');
    }
  }, [refreshActivity, refreshLibraries]);

  useEffect(() => {
    void refreshInitial();
  }, [refreshInitial]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      void Promise.all([refreshActivity(), refreshLibraries()]).catch((err) => {
        setError(err instanceof Error ? err.message : '刷新进度失败');
      });
    }, 2500);
    return () => window.clearInterval(timer);
  }, [refreshActivity, refreshLibraries]);

  async function createLibrary(name: string, relPaths: string[]) {
    const tempId = `pending-${Date.now()}`;
    const optimistic: ScanLibrary = {
      id: tempId,
      name,
      exists: true,
      folders: relPaths.map((relPath) => ({
        relPath,
        name: relPath.split('/').filter(Boolean).pop() ?? '全部存储',
        parentRelPath: relPath.includes('/') ? relPath.slice(0, relPath.lastIndexOf('/')) : relPath ? '' : null,
        depth: relPath ? relPath.split('/').length : 0,
        exists: true,
      })),
      progress: emptyLibraryProgress,
    };
    setLibraries((value) => [...value, optimistic]);
    setAddOpen(false);
    setError(null);
    try {
      const result = await api.createScanLibrary(name, relPaths);
      setLibraries(result.items);
    } catch (err) {
      setLibraries((value) => value.filter((library) => library.id !== tempId));
      setError(err instanceof Error ? err.message : '添加来源失败');
    }
  }

  async function updateLibrary(id: string, name: string, relPaths: string[]) {
    setError(null);
    try {
      const result = await api.updateScanLibrary(id, name, relPaths);
      setLibraries(result.items);
      setEditingLibrary(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新来源失败');
    }
  }

  async function removeLibrary(id: string) {
    const previous = libraries;
    setLibraries((value) => value.filter((library) => library.id !== id));
    setError(null);
    try {
      const result = await api.removeScanLibrary(id);
      setLibraries(result.items);
      if (result.cleanupQueued) {
        setCleanup({ running: true, status: 'running', lastError: '', updatedAt: Math.floor(Date.now() / 1000) });
      }
    } catch (err) {
      setLibraries(previous);
      setError(err instanceof Error ? err.message : '删除来源失败');
    }
  }

  async function rescanLibrary(id: string) {
    try {
      await api.scanLibrary(id);
      await Promise.all([refreshActivity(), refreshLibraries()]);
    } catch (err) {
      setError(err instanceof Error ? err.message : '扫描来源失败');
    }
  }

  function updateViewerPrefs(next: ViewerPrefs) {
    setViewerPrefs(next);
    saveViewerPrefs(next);
  }

  function updateThemeMode(next: ThemeMode) {
    setThemeMode(next);
    saveThemeMode(next);
  }

  function updateRowHeightLevel(next: GridRowHeightLevel) {
    setRowHeightLevel(next);
    saveGridRowHeightLevel(next);
  }

  const liveProgress = status?.progress;
  const totalMedia = progress?.assetTotal ?? libraries.reduce((sum, library) => sum + library.progress.assetTotal, 0);
  const statusLabel = cleanup?.running ? '清理中' : status?.running ? scanPhaseLabel(liveProgress?.phase) : '空闲';

  return (
    <section className="page settings-page">
      <Toolbar title="设置" showScanAction={false} />
      <div className="settings-scroll">
        <div className="settings-layout">
          <nav className="settings-section-nav" aria-label="设置分区">
            {settingsSections.map((section) => (
              <button
                className={activeSettingsSection === section.id ? 'active' : ''}
                key={section.id}
                type="button"
                onClick={() => setActiveSettingsSection(section.id)}
              >
                {section.label}
              </button>
            ))}
          </nav>
          <div className="settings-content">
            {error && <div className="error-line">{error}</div>}

            {activeSettingsSection === 'libraries' && (
              <section className="settings-section library-scan-section">
                <div className="settings-panel">
                  <div className="settings-panel-title">总扫描</div>
                  <div className="metric-grid scan-summary-grid">
                    <Metric label="状态" value={statusLabel} />
                    <Metric label="总媒体" value={String(totalMedia)} />
                    <Metric label="缓存" value={cacheSizeLabel(progress)} />
                    <Metric label="图库个数" value={String(libraries.length)} />
                  </div>
                </div>
                <div className="settings-panel">
                  <div className="settings-panel-title">图库</div>
                  <div className="library-list">
                    {libraries.map((library) => (
                      <div className={library.progress.active ? 'library-row active-scan' : 'library-row'} key={library.id}>
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
                        <button className="library-scan-button" type="button" title="扫描此图库" onClick={() => void rescanLibrary(library.id)}>
                          <RotateCw size={15} />
                          <span>扫描</span>
                        </button>
                        <button type="button" title="编辑" onClick={() => setEditingLibrary(library)}>
                          <Pencil size={15} />
                        </button>
                        <button type="button" title="删除" onClick={() => void removeLibrary(library.id)}>
                          <Trash2 size={15} />
                        </button>
                      </div>
                    ))}
                    {libraries.length === 0 && <div className="muted-line">默认扫描全部存储</div>}
                  </div>
                  <div className="selected-folder-actions">
                    <button className="command-button" type="button" onClick={() => setAddOpen(true)}>
                      <FolderPlus size={16} />
                      添加来源
                    </button>
                  </div>
                </div>
              </section>
            )}

            {activeSettingsSection === 'appearance' && (
              <section className="settings-panel settings-section">
                <div className="settings-panel-title">外观</div>
                <div className="settings-field settings-field-wide">
                  <span>主题</span>
                  <div className="settings-segmented three-options">
                    <button
                      className={themeMode === 'system' ? 'active' : ''}
                      type="button"
                      onClick={() => updateThemeMode('system')}
                    >
                      跟随系统
                    </button>
                    <button
                      className={themeMode === 'light' ? 'active' : ''}
                      type="button"
                      onClick={() => updateThemeMode('light')}
                    >
                      浅色
                    </button>
                    <button
                      className={themeMode === 'dark' ? 'active' : ''}
                      type="button"
                      onClick={() => updateThemeMode('dark')}
                    >
                      深色
                    </button>
                  </div>
                </div>
                <div className="settings-field settings-field-wide settings-field-spaced">
                  <span>单行高度</span>
                  <div className="settings-segmented five-options">
                    {rowHeightOptions.map((option) => (
                      <button
                        className={rowHeightLevel === option.value ? 'active' : ''}
                        key={option.value}
                        type="button"
                        onClick={() => updateRowHeightLevel(option.value)}
                      >
                        {option.label}
                      </button>
                    ))}
                  </div>
                </div>
              </section>
            )}

            {activeSettingsSection === 'viewer' && (
              <section className="settings-panel settings-section">
                <div className="settings-panel-title">查看器</div>
                <div className="viewer-settings-grid">
                  <label className="settings-check-row settings-field-wide">
                    <input
                      type="checkbox"
                      checked={viewerPrefs.videoAutoplay}
                      onChange={(event) => updateViewerPrefs({ ...viewerPrefs, videoAutoplay: event.target.checked })}
                    />
                    <span>视频自动播放</span>
                  </label>
                  <label className="settings-check-row settings-field-wide">
                    <input
                      type="checkbox"
                      checked={viewerPrefs.subtitlesEnabled}
                      onChange={(event) => updateViewerPrefs({ ...viewerPrefs, subtitlesEnabled: event.target.checked })}
                    />
                    <span>弹幕默认开启</span>
                  </label>
                  <div className="settings-field settings-field-wide">
                    <span>视频倍速</span>
                    <div className="settings-segmented five-options">
                      {playbackRates.map((rate) => (
                        <button
                          className={viewerPrefs.playbackRate === rate ? 'active' : ''}
                          key={rate}
                          type="button"
                          onClick={() => updateViewerPrefs({ ...viewerPrefs, playbackRate: rate })}
                        >
                          {rate}x
                        </button>
                      ))}
                    </div>
                  </div>
                  <div className="settings-field settings-field-wide">
                    <span>按住放大模式</span>
                    <div className="settings-segmented">
                      <button
                        className={viewerPrefs.zoomMode === 'scale' ? 'active' : ''}
                        type="button"
                        onClick={() => updateViewerPrefs({ ...viewerPrefs, zoomMode: 'scale' })}
                      >
                        固定倍数
                      </button>
                      <button
                        className={viewerPrefs.zoomMode === 'pixels' ? 'active' : ''}
                        type="button"
                        onClick={() => updateViewerPrefs({ ...viewerPrefs, zoomMode: 'pixels' })}
                      >
                        固定显示像素
                      </button>
                    </div>
                  </div>
                  <label className="settings-field">
                    <span>固定倍数</span>
                    <input
                      disabled={viewerPrefs.zoomMode !== 'scale'}
                      max={8}
                      min={1.5}
                      step={0.1}
                      type="number"
                      value={viewerPrefs.zoomScale}
                      onChange={(event) =>
                        updateViewerPrefs({ ...viewerPrefs, zoomScale: Number(event.target.value) })
                      }
                    />
                  </label>
                  <label className="settings-field">
                    <span>固定显示像素</span>
                    <input
                      disabled={viewerPrefs.zoomMode !== 'pixels'}
                      max={2000}
                      min={50}
                      step={50}
                      type="number"
                      value={viewerPrefs.zoomPixelArea}
                      onChange={(event) =>
                        updateViewerPrefs({ ...viewerPrefs, zoomPixelArea: Number(event.target.value) })
                      }
                    />
                  </label>
                </div>
              </section>
            )}
          </div>
        </div>
      </div>
      {addOpen && (
        <FolderPickerModal
          confirmLabel="完成"
          title="添加来源"
          onClose={() => setAddOpen(false)}
          onConfirm={(name, relPaths) => void createLibrary(name, relPaths)}
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
          onClose={() => setEditingLibrary(null)}
          onConfirm={(name, relPaths) => void updateLibrary(editingLibrary.id, name, relPaths)}
        />
      )}
    </section>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

const emptyCounts: WorkStatusCounts = {
  error: 0,
  notRequired: 0,
  pending: 0,
  processing: 0,
  ready: 0,
  total: 0,
};

const emptyLibraryProgress: ScanLibraryProgress = {
  active: false,
  assetTotal: 0,
  scannedFiles: 0,
  thumb: emptyCounts,
  transcode: emptyCounts,
  unscannedFiles: 0,
};

const rowHeightOptions: Array<{ label: string; value: GridRowHeightLevel }> = [
  { label: '紧凑', value: 'compact' },
  { label: '小', value: 'small' },
  { label: '中', value: 'medium' },
  { label: '大', value: 'large' },
  { label: '超大', value: 'extra' },
];

function LibraryProgress({ progress }: { progress: ScanLibraryProgress }) {
  return (
    <div className="library-progress">
      <div className="library-stat-strip">
        <span>
          <em>已扫描</em>
          <strong>{progress.scannedFiles}</strong>
        </span>
        <span>
          <em>未扫描</em>
          <strong>{progress.unscannedFiles}</strong>
        </span>
        <span>
          <em>媒体</em>
          <strong>{progress.assetTotal}</strong>
        </span>
      </div>
      <div className="library-progress-bars">
        <ProgressRow label="缩略图" counts={progress.thumb} />
        <ProgressRow label="转码" counts={progress.transcode} />
      </div>
    </div>
  );
}

function ProgressRow({ label, counts }: { label: string; counts: WorkStatusCounts | null | undefined }) {
  const value = counts ?? emptyCounts;
  const done = value.ready + value.notRequired;
  const percent = value.total > 0 ? Math.round((done / value.total) * 100) : 0;
  return (
    <div className="progress-row">
      <div className="progress-row-title">
        <span>{label}</span>
        <strong>已完成 {done}/{value.total}</strong>
      </div>
      <div className="progress-bar" aria-label={`${label} 已完成 ${done}/${value.total}`}>
        <div className="progress-fill" style={{ width: `${percent}%` }} />
      </div>
      <div className="progress-meta">
        <span>待处理 {value.pending}</span>
        <span>处理中 {value.processing}</span>
        <span>错误 {value.error}</span>
      </div>
    </div>
  );
}

function scanPhaseLabel(phase: string | undefined) {
  switch (phase) {
    case 'counting':
      return '统计中';
    case 'scanning':
      return '扫描中';
    case 'finished':
      return '完成';
    case 'paused':
      return '已暂停';
    case 'pausing':
      return '暂停中';
    default:
      return '处理中';
  }
}

function cacheSizeLabel(progress: ProcessingProgress | null) {
  if (!progress?.cache) return '0 B';
  if (progress.cache.refreshing && progress.cache.updatedAt === 0) return '统计中';
  return formatBytes(progress.cache.sizeBytes);
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

function displayRelPath(relPath: string) {
  return relPath ? `/${relPath}` : '全部存储';
}

function displayLibraryName(name: string) {
  return name === '默认 LIB' ? '默认来源' : name;
}
