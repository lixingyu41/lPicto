import { useCallback, useEffect, useState } from 'react';
import { Check, ChevronRight, FolderPlus, RotateCw, Trash2, X } from 'lucide-react';
import Toolbar from '../components/Toolbar';
import { api } from '../api/client';
import type { ProcessingProgress, ScanLibrary, ScanRun, ScanStatus, SourceFolder, WorkStatusCounts } from '../types/api';
import { formatDateTime } from '../utils/format';
import { loadViewerPrefs, saveViewerPrefs, type ViewerPrefs } from '../utils/viewerPrefs';

export default function SettingsPage() {
  const [status, setStatus] = useState<ScanStatus | null>(null);
  const [progress, setProgress] = useState<ProcessingProgress | null>(null);
  const [runs, setRuns] = useState<ScanRun[]>([]);
  const [libraries, setLibraries] = useState<ScanLibrary[]>([]);
  const [configured, setConfigured] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [viewerPrefs, setViewerPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());

  const refreshStatus = useCallback(async () => {
    const [statusResult, runsResult, progressResult] = await Promise.all([
      api.scanStatus(),
      api.scanRuns(1, 8),
      api.settingsProgress(),
    ]);
    setStatus(statusResult);
    setRuns(runsResult.items);
    setProgress(progressResult);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [statusResult, runsResult, progressResult, librariesResult] = await Promise.all([
        api.scanStatus(),
        api.scanRuns(1, 8),
        api.settingsProgress(),
        api.scanLibraries(),
      ]);
      setStatus(statusResult);
      setRuns(runsResult.items);
      setProgress(progressResult);
      setLibraries(librariesResult.items);
      setConfigured(librariesResult.configured);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取设置失败');
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      void refreshStatus().catch((err) => {
        setError(err instanceof Error ? err.message : '刷新进度失败');
      });
    }, 2500);
    return () => window.clearInterval(timer);
  }, [refreshStatus]);

  async function createLibrary(name: string, relPaths: string[]) {
    try {
      const result = await api.createScanLibrary(name, relPaths);
      setLibraries(result.items);
      setConfigured(true);
      setAddOpen(false);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加 LIB 失败');
    }
  }

  async function removeLibrary(id: string) {
    try {
      const result = await api.removeScanLibrary(id);
      setLibraries(result.items);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除 LIB 失败');
    }
  }

  async function rescanLibrary(id: string) {
    try {
      await api.scanLibrary(id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : '扫描 LIB 失败');
    }
  }

  async function triggerScan() {
    await api.triggerScan();
    await refresh();
  }

  function updateViewerPrefs(next: ViewerPrefs) {
    setViewerPrefs(next);
    saveViewerPrefs(next);
  }

  const lastRun = status?.lastRun;
  const liveProgress = status?.progress;
  const scanSeen = status?.running ? liveProgress?.totalSeen : lastRun?.totalSeen;
  const scanAdded = status?.running ? liveProgress?.assetsAdded : lastRun?.assetsAdded;
  const scanUpdated = status?.running ? liveProgress?.assetsUpdated : lastRun?.assetsUpdated;
  const scanDeleted = status?.running ? liveProgress?.assetsDeleted : lastRun?.assetsDeleted;
  const scanErrors = status?.running ? liveProgress?.errors : lastRun?.errors;

  return (
    <section className="page settings-page">
      <Toolbar title="设置" onScanStarted={() => void refresh()} />
      <div className="settings-scroll">
        {error && <div className="error-line">{error}</div>}
        <section className="settings-grid">
          <div className="settings-panel">
            <div className="settings-panel-title">扫描状态</div>
            <div className="metric-grid">
              <Metric label="状态" value={status?.running ? '处理中' : '空闲'} />
              <Metric label="最近开始" value={formatDateTime(status?.lastStart ?? null) || '-'} />
              <Metric label="最近结果" value={lastRun?.status ?? '-'} />
              <Metric label="发现资源" value={String(scanSeen ?? 0)} />
              <Metric label="新增" value={String(scanAdded ?? 0)} />
              <Metric label="更新" value={String(scanUpdated ?? 0)} />
              <Metric label="删除" value={String(scanDeleted ?? 0)} />
              <Metric label="错误" value={String(scanErrors ?? 0)} />
            </div>
            {status?.running && liveProgress && (
              <div className="settings-note scan-live-line">
                <span>当前根：{displayRelPath(liveProgress.currentRoot)}</span>
                {liveProgress.currentRelPath && <span>当前文件：{displayRelPath(liveProgress.currentRelPath)}</span>}
              </div>
            )}
            {lastRun?.lastError && <div className="settings-warning">{lastRun.lastError}</div>}
            <button className="command-button settings-action" type="button" onClick={() => void triggerScan()}>
              <RotateCw size={16} />
              重新扫描全部
            </button>
          </div>

          <div className="settings-panel">
            <div className="settings-panel-title">处理进度</div>
            <div className="metric-grid">
              <Metric label="媒体总数" value={String(progress?.assetTotal ?? 0)} />
              <Metric label="图片" value={String(progress?.imageTotal ?? 0)} />
              <Metric label="视频" value={String(progress?.videoTotal ?? 0)} />
              <Metric label="队列" value={`图 ${progress?.queue.thumbQueued ?? 0} / 视 ${progress?.queue.videoQueued ?? 0}`} />
            </div>
            <div className="progress-list">
              <ProgressRow label="图片缩略图" counts={progress?.thumb} />
              <ProgressRow label="图片预览图" counts={progress?.preview} />
              <ProgressRow label="视频封面" counts={progress?.videoPoster} />
              <ProgressRow label="视频代理" counts={progress?.videoProxy} />
            </div>
          </div>

          <div className="settings-panel">
            <div className="settings-panel-title">最近扫描</div>
            <div className="run-list">
              {runs.map((run) => (
                <div className="run-row" key={run.id}>
                  <span>{formatDateTime(run.startedAt)}</span>
                  <span>{run.status}</span>
                  <span>新增 {run.assetsAdded}</span>
                  <span>更新 {run.assetsUpdated}</span>
                  <span>删除 {run.assetsDeleted}</span>
                  <span>错误 {run.errors}</span>
                </div>
              ))}
              {runs.length === 0 && <div className="muted-line">暂无扫描记录</div>}
            </div>
          </div>
        </section>

        <section className="settings-panel">
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

        <section className="settings-panel">
          <div className="settings-panel-title">LIB</div>
          <div className="library-list">
            {libraries.map((library) => (
              <div className="library-row" key={library.id}>
                <div className="library-info">
                  <strong>{library.name}</strong>
                  <small>{library.exists ? '可访问' : '部分不可访问'} · {library.folders.length} 个文件夹</small>
                  <div className="library-paths">
                    {library.folders.map((folder) => (
                      <span key={folder.relPath || 'root'}>{displayRelPath(folder.relPath)}</span>
                    ))}
                  </div>
                </div>
                <button type="button" title="重新扫描" onClick={() => void rescanLibrary(library.id)}>
                  <RotateCw size={15} />
                </button>
                <button type="button" title="删除" onClick={() => void removeLibrary(library.id)}>
                  <Trash2 size={15} />
                </button>
              </div>
            ))}
            {libraries.length === 0 && <div className="muted-line">暂无 LIB</div>}
          </div>
          {!configured && <div className="settings-note">当前使用默认 LIB：扫描整个 /photos。</div>}
          <div className="selected-folder-actions">
            <button className="command-button" type="button" onClick={() => setAddOpen(true)}>
              <FolderPlus size={16} />
              添加
            </button>
          </div>
        </section>
      </div>
      {addOpen && (
        <FolderPickerModal
          onClose={() => setAddOpen(false)}
          onConfirm={(name, relPaths) => void createLibrary(name, relPaths)}
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

function ProgressRow({ label, counts }: { label: string; counts: WorkStatusCounts | null | undefined }) {
  const value = counts ?? emptyCounts;
  const done = value.ready + value.notRequired;
  const percent = value.total > 0 ? Math.round((done / value.total) * 100) : 0;
  return (
    <div className="progress-row">
      <div className="progress-row-title">
        <span>{label}</span>
        <strong>
          {done}/{value.total} · {percent}%
        </strong>
      </div>
      <div className="progress-bar" aria-label={`${label} ${percent}%`}>
        <div className="progress-fill" style={{ width: `${percent}%` }} />
      </div>
      <div className="progress-meta">
        <span>待处理 {value.pending}</span>
        <span>处理中 {value.processing}</span>
        <span>错误 {value.error}</span>
        {value.notRequired > 0 && <span>跳过 {value.notRequired}</span>}
      </div>
    </div>
  );
}

function FolderPickerModal({
  onClose,
  onConfirm,
}: {
  onClose: () => void;
  onConfirm: (name: string, relPaths: string[]) => void;
}) {
  const [children, setChildren] = useState<Record<string, SourceFolder[]>>({});
  const [rootFolder, setRootFolder] = useState<SourceFolder | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [libraryName, setLibraryName] = useState('');
  const [loading, setLoading] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);

  const loadChildren = useCallback(async (relPath: string) => {
    setLoading((prev) => new Set(prev).add(relPath));
    try {
      const result = await api.sourceFolders(relPath);
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
    if (!children[relPath]) {
      void loadChildren(relPath);
    }
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
  const canFinish = selectedPaths.length > 0 && libraryName.trim().length > 0;

  return (
    <div className="modal-backdrop" role="presentation">
      <div className="folder-picker" role="dialog" aria-modal="true" aria-label="添加 LIB">
        <div className="modal-title">
          <span>添加 LIB</span>
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
          <label htmlFor="library-name">LIB 名称</label>
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
            完成并扫描
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
      ? '已在 LIB 中'
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
    if ((selectedPath === '' && relPath !== '') || relPath.startsWith(`${selectedPath}/`)) {
      return true;
    }
  }
  return false;
}

function displayRelPath(relPath: string) {
  return relPath ? `/${relPath}` : '/photos';
}
