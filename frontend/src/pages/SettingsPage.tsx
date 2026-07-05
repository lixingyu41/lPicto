import { useCallback, useEffect, useRef, useState } from 'react';
import Toolbar from '../components/Toolbar';
import { useSidebarPanel } from '../components/SidebarContext';
import { api } from '../api/client';
import type { CleanupStatus, ProcessingProgress, ScanLibrary, ScanLibraryProgress, ScanStatus, WorkStatusCounts } from '../types/api';
import { useAssetReadyEvents, useScanStatusEvents } from '../hooks/useAssetReadyEvents';
import { loadGridRowHeightLevel, saveGridRowHeightLevel, type GridRowHeightLevel } from '../utils/gridPrefs';
import { loadThemeMode, saveThemeMode, type ThemeMode } from '../utils/themePrefs';
import { loadViewerPrefs, playbackRates, saveViewerPrefs, type ViewerPrefs } from '../utils/viewerPrefs';
import { CacheManager } from '../components/CacheManager';
import { ScanManager } from '../components/ScanManager';

const settingsSections = [
  { id: 'libraries', label: '图库' },
  { id: 'appearance', label: '外观' },
  { id: 'viewer', label: '查看器' },
] as const;

type SettingsSectionId = (typeof settingsSections)[number]['id'];
type ScanAction = 'count' | 'metadata' | 'thumbnails';

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
  const [stoppingScan, setStoppingScan] = useState(false);
  const [optimisticScanLibraryId, setOptimisticScanLibraryId] = useState<string | null>(null);
  const progressRefreshTimer = useRef<number | null>(null);
  const progressRefreshInFlight = useRef(false);
  const progressRefreshQueued = useRef(false);

  const refreshLibraries = useCallback(async () => {
    const librariesResult = await api.scanLibraries();
    setLibraries(librariesResult.items);
  }, []);

  const applyScanStatus = useCallback((scan: ScanStatus) => {
    setStatus(scan);
    if (!scan.running) {
      setOptimisticScanLibraryId(null);
    }
  }, []);

  const refreshScanStatus = useCallback(async () => {
    applyScanStatus(await api.scanStatus());
  }, [applyScanStatus]);

  const refreshActivity = useCallback(async () => {
    const activity = await api.settingsActivity();
    applyScanStatus(activity.scan);
    setProgress(activity.progress);
    setCleanup(activity.cleanup);
  }, [applyScanStatus]);

  const refreshActivityWithoutScan = useCallback(async () => {
    const activity = await api.settingsActivity();
    setProgress(activity.progress);
    setCleanup(activity.cleanup);
  }, []);

  const handleLiveScanStatus = useCallback((scan: ScanStatus) => {
    applyScanStatus(scan);
    if (!scan.running) {
      void Promise.all([refreshActivityWithoutScan(), refreshLibraries()]).catch((err) => {
        setError(err instanceof Error ? err.message : '刷新进度失败');
      });
    }
  }, [applyScanStatus, refreshActivityWithoutScan, refreshLibraries]);

  const runQueuedProgressRefresh = useCallback(() => {
    if (progressRefreshInFlight.current) {
      progressRefreshQueued.current = true;
      return;
    }
    progressRefreshInFlight.current = true;
    void Promise.all([refreshActivityWithoutScan(), refreshLibraries()])
      .catch((err) => {
        setError(err instanceof Error ? err.message : '刷新进度失败');
      })
      .finally(() => {
        progressRefreshInFlight.current = false;
        if (progressRefreshQueued.current && progressRefreshTimer.current === null) {
          progressRefreshTimer.current = window.setTimeout(() => {
            progressRefreshTimer.current = null;
            progressRefreshQueued.current = false;
            runQueuedProgressRefresh();
          }, 750);
        }
      });
  }, [refreshActivityWithoutScan, refreshLibraries]);

  const refreshLibraryProgress = useCallback(() => {
    progressRefreshQueued.current = true;
    if (progressRefreshTimer.current !== null) {
      return;
    }
    progressRefreshTimer.current = window.setTimeout(() => {
      progressRefreshTimer.current = null;
      progressRefreshQueued.current = false;
      runQueuedProgressRefresh();
    }, 750);
  }, [runQueuedProgressRefresh]);

  useEffect(() => () => {
    if (progressRefreshTimer.current !== null) {
      window.clearTimeout(progressRefreshTimer.current);
      progressRefreshTimer.current = null;
    }
  }, []);

  const eventsConnected = useScanStatusEvents(handleLiveScanStatus, [handleLiveScanStatus]);
  useAssetReadyEvents(refreshLibraryProgress, [refreshLibraryProgress], refreshLibraryProgress);

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
      if (!eventsConnected) {
        void refreshScanStatus().catch((err) => {
          setError(err instanceof Error ? err.message : '刷新扫描状态失败');
        });
      }
      const activityRefresh = eventsConnected ? refreshActivityWithoutScan() : refreshActivity();
      void Promise.all([activityRefresh, refreshLibraries()]).catch((err) => {
        setError(err instanceof Error ? err.message : '刷新进度失败');
      });
    }, 2500);
    return () => window.clearInterval(timer);
  }, [eventsConnected, refreshActivity, refreshActivityWithoutScan, refreshLibraries, refreshScanStatus]);

  async function createLibrary(name: string, relPaths: string[]) {
    const tempId = `pending-${Date.now()}`;
    const emptyLibraryProgress: ScanLibraryProgress = {
      active: false,
      assetTotal: 0,
      discoveredAt: null,
      discoveredFiles: 0,
      scannedFiles: 0,
      thumb: { error: 0, notRequired: 0, pending: 0, processing: 0, ready: 0, total: 0 },
      transcode: { error: 0, notRequired: 0, pending: 0, processing: 0, ready: 0, total: 0 },
      videoProxy: { error: 0, notRequired: 0, pending: 0, processing: 0, ready: 0, total: 0 },
      unscannedFiles: 0,
    };
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

  async function runGlobalScan(action: ScanAction) {
    if (status?.running || optimisticScanLibraryId) {
      setError('已有扫描正在运行');
      return;
    }
    setError(null);
    const request =
      action === 'count' ? api.countScan : action === 'thumbnails' ? api.rebuildThumbnails : api.metadataScan;
    try {
      const result = await request();
      if (!result.accepted) {
        setError('已有扫描正在运行');
        await refreshScanStatus();
        return;
      }
      await refreshScanStatus();
      void refreshLibraries().catch((err) => {
        setError(err instanceof Error ? err.message : '刷新图库失败');
      });
    } catch (err) {
      await refreshScanStatus().catch(() => undefined);
      setError(err instanceof Error ? err.message : '启动扫描失败');
    }
  }

  async function runLibraryScan(id: string, action: ScanAction) {
    if (status?.running || optimisticScanLibraryId) {
      setError('已有扫描正在运行');
      return;
    }
    const library = libraries.find((item) => item.id === id);
    if (!library) return;
    setError(null);
    const request =
      action === 'count'
        ? api.countScanLibrary
        : action === 'thumbnails'
          ? api.rebuildLibraryThumbnails
          : api.metadataScanLibrary;
    try {
      const result = await request(id);
      if (!result.accepted) {
        setError('已有扫描正在运行');
        setOptimisticScanLibraryId(null);
        await refreshScanStatus();
        return;
      }
      setOptimisticScanLibraryId(id);
      setStatus((current) => ({
        running: true,
        lastStart: current?.lastStart ?? Math.floor(Date.now() / 1000),
        lastRun: current?.lastRun ?? null,
        progress: {
          reason: `library:${library.name}`,
          state: 'running',
          requestedAction: 'start',
          task: action === 'count' ? 'count' : action === 'thumbnails' ? 'thumb_rebuild' : 'metadata',
          phase: 'queued',
          roots: library.folders.map((folder) => folder.relPath),
          currentRoot: library.folders[0]?.relPath ?? '',
          currentRelPath: '',
          discoveredFiles: library.progress.discoveredFiles,
          totalFiles: library.progress.discoveredFiles || library.progress.assetTotal,
          scannedFiles: library.progress.scannedFiles,
          totalSeen: library.progress.scannedFiles,
          assetsAdded: 0,
          assetsUpdated: 0,
          assetsDeleted: 0,
          errors: 0,
        },
      }));
      await refreshScanStatus();
      void refreshLibraries().catch((err) => {
        setError(err instanceof Error ? err.message : '刷新图库失败');
      });
    } catch (err) {
      setOptimisticScanLibraryId(null);
      await refreshScanStatus().catch(() => undefined);
      setError(err instanceof Error ? err.message : '扫描来源失败');
    }
  }

  async function stopScan() {
    if ((!status?.running && !optimisticScanLibraryId) || stoppingScan) return;
    setStoppingScan(true);
    setError(null);
    setStatus((current) =>
      current ? { ...current, progress: { ...current.progress, phase: 'stopping', requestedAction: 'stop' } } : current,
    );
    try {
      await api.pauseScan();
      setOptimisticScanLibraryId(null);
      await refreshScanStatus();
      void refreshLibraries().catch((err) => {
        setError(err instanceof Error ? err.message : '刷新图库失败');
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : '停止扫描失败');
    } finally {
      setStoppingScan(false);
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
  const totalMedia = progress?.thumb.ready ?? libraries.reduce((sum, library) => sum + library.progress.thumb.ready, 0);
  const proxiedVideos = progress?.videoProxy?.ready ?? libraries.reduce((sum, library) => sum + (library.progress.videoProxy?.ready ?? 0), 0);
  const scanRunning = Boolean(status?.running || optimisticScanLibraryId);
  const statusLabel = cleanup?.running ? '清理中' : scanRunning ? scanTaskLabel(liveProgress) : '空闲';

  useSidebarPanel(
    'settings',
    <div className="sidebar-control-stack">
      <div className="sidebar-list">
        {settingsSections.map((section) => (
          <button
            aria-current={activeSettingsSection === section.id ? 'page' : undefined}
            className={activeSettingsSection === section.id ? 'sidebar-list-row active' : 'sidebar-list-row'}
            key={section.id}
            type="button"
            onClick={() => setActiveSettingsSection(section.id)}
          >
            <span className="sidebar-list-marker" aria-hidden="true" />
            <span>{section.label}</span>
          </button>
        ))}
      </div>
    </div>,
    [activeSettingsSection],
  );

  return (
    <section className="page settings-page">
      <Toolbar title="设置" showScanAction={false} />
      <div className="settings-scroll">
        <div className="settings-layout">
          <div className="settings-content">
            {error && <div className="error-line">{error}</div>}

            {activeSettingsSection === 'libraries' && (
              <section className="settings-section library-scan-section">
                <CacheManager
                  librariesCount={libraries.length}
                  progress={progress}
                  proxiedVideos={proxiedVideos}
                  scanRunning={scanRunning}
                  status={status}
                  statusLabel={statusLabel}
                  stoppingScan={stoppingScan}
                  totalMedia={totalMedia}
                  onGlobalScan={(action) => void runGlobalScan(action)}
                  onStopScan={() => void stopScan()}
                />
                <ScanManager
                  addOpen={addOpen}
                  editingLibrary={editingLibrary}
                  libraries={libraries}
                  optimisticScanLibraryId={optimisticScanLibraryId}
                  scanRunning={scanRunning}
                  stoppingScan={stoppingScan}
                  onCreateLibrary={(name, relPaths) => void createLibrary(name, relPaths)}
                  onLibraryScan={(id, action) => void runLibraryScan(id, action)}
                  onRemoveLibrary={(id) => void removeLibrary(id)}
                  onSetAddOpen={setAddOpen}
                  onSetEditingLibrary={setEditingLibrary}
                  onStopScan={() => void stopScan()}
                  onUpdateLibrary={(id, name, relPaths) => void updateLibrary(id, name, relPaths)}
                />
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
    </section>
  );
}

const rowHeightOptions: Array<{ label: string; value: GridRowHeightLevel }> = [
  { label: '紧凑', value: 'compact' },
  { label: '小', value: 'small' },
  { label: '中', value: 'medium' },
  { label: '大', value: 'large' },
  { label: '超大', value: 'extra' },
];

function scanPhaseLabel(phase: string | undefined) {
  switch (phase) {
    case 'counting':
    case 'discovering':
      return '统计中';
    case 'scanning':
      return '扫描中';
    case 'thumb_rebuild':
      return '缩略图重建中';
    case 'stopping':
    case 'pausing':
      return '暂停中';
    case 'finished':
      return '完成';
    case 'paused':
      return '已暂停';
    case 'idle':
      return '空闲';
    default:
      return '处理中';
  }
}

function scanTaskLabel(progress: ScanStatus['progress'] | undefined) {
  switch (progress?.task) {
    case 'count':
      return '文件数扫描中';
    case 'metadata':
      return '媒体信息扫描中';
    case 'thumb_rebuild':
      return '缩略图重建中';
    default:
      return scanPhaseLabel(progress?.phase);
  }
}
