import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import Toolbar from '../components/Toolbar';
import { api } from '../api/client';
import type { AISettings, CleanupStatus, ProcessingProgress, ScanLibrary, ScanLibraryProgress, ScanStatus, StorageStatus, SystemTask, VideoProxySettings, WorkStatusCounts } from '../types/api';
import { useAssetReadyEvents, useScanStatusEvents } from '../hooks/useAssetReadyEvents';
import { loadGridRowHeightLevel, saveGridRowHeightLevel, type GridRowHeightLevel } from '../utils/gridPrefs';
import { loadThemeMode, saveThemeMode, type ThemeMode } from '../utils/themePrefs';
import {
  imageSlideshowSecondsRange,
  loadViewerPrefs,
  playbackModeOptions,
  playbackRates,
  saveViewerPrefs,
  videoPlaybackDelaySecondsRange,
  type ViewerPrefs,
} from '../utils/viewerPrefs';
import {
  mediaLayoutDefinition,
  mediaLayoutDefinitions,
  loadMediaViewPreferences,
  mediaColumnDefinitions,
  saveMediaViewPreferences,
  type MediaColumnId,
  type MediaViewPreferences,
} from '../utils/mediaViewPrefs';
import { CacheManager } from '../components/CacheManager';
import { ScanManager } from '../components/ScanManager';
import { loadSettingsSection, saveSettingsSection, settingsSectionFromSlug, settingsSectionPath, settingsSections, type SettingsSectionId } from '../utils/settingsRoute';
import { loadImmersiveChromeSize, saveImmersiveChromeSize, type ImmersiveChromeSize } from '../utils/immersiveChromePrefs';

export default function SettingsPage() {
  const navigate = useNavigate();
  const { section: sectionSlug } = useParams<{ section?: string }>();
  const routeSection = settingsSectionFromSlug(sectionSlug);
  const activeSettingsSection = routeSection ?? loadSettingsSection();
  const [status, setStatus] = useState<ScanStatus | null>(null);
  const [progress, setProgress] = useState<ProcessingProgress | null>(null);
  const [libraries, setLibraries] = useState<ScanLibrary[]>([]);
  const [cleanup, setCleanup] = useState<CleanupStatus | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [editingLibrary, setEditingLibrary] = useState<ScanLibrary | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [rowHeightLevel, setRowHeightLevel] = useState<GridRowHeightLevel>(() => loadGridRowHeightLevel());
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => loadThemeMode());
  const [immersiveChromeSize, setImmersiveChromeSize] = useState<ImmersiveChromeSize>(() => loadImmersiveChromeSize());
  const [viewerPrefs, setViewerPrefs] = useState<ViewerPrefs>(() => loadViewerPrefs());
  const [mediaViewPrefs, setMediaViewPrefs] = useState<MediaViewPreferences>(() => loadMediaViewPreferences());
  const [draggedMediaColumn, setDraggedMediaColumn] = useState<MediaColumnId | null>(null);
  const mediaViewPrefsRef = useRef(mediaViewPrefs);
  const mediaColumnElements = useRef(new Map<MediaColumnId, HTMLElement>());
  const mediaColumnRectsBeforeMove = useRef(new Map<MediaColumnId, DOMRect>());
  const [videoProxySettings, setVideoProxySettings] = useState<VideoProxySettings | null>(null);
  const [videoProxyTTLMinutes, setVideoProxyTTLMinutes] = useState('4320');
  const [videoProxyMaxCacheGB, setVideoProxyMaxCacheGB] = useState('0');
  const [videoProxySaving, setVideoProxySaving] = useState(false);
  const [aiSettings, setAISettings] = useState<AISettings | null>(null);
  const [storageStatus, setStorageStatus] = useState<StorageStatus | null>(null);
  const [systemTasks, setSystemTasks] = useState<SystemTask[]>([]);
  const progressRefreshTimer = useRef<number | null>(null);
  const progressRefreshInFlight = useRef(false);
  const progressRefreshQueued = useRef(false);

  mediaViewPrefsRef.current = mediaViewPrefs;

  useLayoutEffect(() => {
    const previousRects = mediaColumnRectsBeforeMove.current;
    if (previousRects.size === 0) return;
    mediaColumnRectsBeforeMove.current = new Map();
    mediaColumnElements.current.forEach((element, columnId) => {
      if (columnId === draggedMediaColumn) return;
      const previous = previousRects.get(columnId);
      if (!previous) return;
      const current = element.getBoundingClientRect();
      const x = previous.left - current.left;
      const y = previous.top - current.top;
      if (Math.abs(x) < 1 && Math.abs(y) < 1) return;
      element.getAnimations().forEach((animation) => animation.cancel());
      element.animate(
        [
          { transform: `translate3d(${x}px, ${y}px, 0)` },
          { transform: 'translate3d(0, 0, 0)' },
        ],
        { duration: 210, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' },
      );
    });
  }, [draggedMediaColumn, mediaViewPrefs.columnOrder]);

  useEffect(() => {
    const normalizedSection = sectionSlug?.trim().toLowerCase();
    if (normalizedSection === 'video-proxy' || normalizedSection === 'appearance') {
      navigate(settingsSectionPath(normalizedSection === 'appearance' ? 'viewer' : 'cache'), { replace: true });
      return;
    }
    if (!routeSection) {
      navigate(settingsSectionPath(activeSettingsSection), { replace: true });
      return;
    }
    saveSettingsSection(routeSection);
  }, [activeSettingsSection, navigate, routeSection, sectionSlug]);

  const selectSettingsSection = useCallback((section: SettingsSectionId) => {
    saveSettingsSection(section);
    navigate(settingsSectionPath(section));
  }, [navigate]);

  const refreshLibraries = useCallback(async () => {
    const librariesResult = await api.scanLibraries();
    setLibraries(librariesResult.items);
  }, []);

  const applyVideoProxySettings = useCallback((settings: VideoProxySettings) => {
    setVideoProxySettings(settings);
    setVideoProxyTTLMinutes(String(Math.round(settings.cacheTtlSeconds / 60)));
    setVideoProxyMaxCacheGB(formatCacheGB(settings.maxCacheBytes));
  }, []);

  const refreshVideoProxySettings = useCallback(async () => {
    applyVideoProxySettings(await api.videoProxySettings());
  }, [applyVideoProxySettings]);

  const applyScanStatus = useCallback((scan: ScanStatus) => {
    setStatus(scan);
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
      await Promise.all([refreshActivity(), refreshLibraries(), refreshVideoProxySettings()]);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取设置失败');
    }
  }, [refreshActivity, refreshLibraries, refreshVideoProxySettings]);

  const resetMediaLibrary = useCallback(async (confirmation: string) => {
    const result = await api.resetMediaLibrary(confirmation);
    const [activity, libraryResult] = await Promise.all([api.settingsActivity(), api.scanLibraries()]);
    applyScanStatus(activity.scan);
    setProgress(activity.progress);
    setCleanup(activity.cleanup);
    setLibraries(libraryResult.items);
    setSystemTasks([]);
    return result;
  }, [applyScanStatus]);

  useEffect(() => {
    void refreshInitial();
  }, [refreshInitial]);

  useEffect(() => {
    if (activeSettingsSection !== 'ai') return;
    let live = true;
    const refresh = () => void Promise.all([api.aiSettings(), api.storageStatus()])
      .then(([settingsResult, storageResult]) => { if (live) { setAISettings(settingsResult); setStorageStatus(storageResult); } })
      .catch((err) => { if (live) setError(err instanceof Error ? err.message : '读取 AI 状态失败'); });
    refresh();
    const timer = window.setInterval(refresh, 15_000);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [activeSettingsSection]);

  useEffect(() => {
    if (activeSettingsSection !== 'tasks') return;
    let live = true;
    let timer: number | null = null;
    const schedule = (delay: number) => {
      if (!live) return;
      timer = window.setTimeout(refresh, delay);
    };
    const refresh = async () => {
      try {
        const result = await api.systemTasks();
        if (!live) return;
        setSystemTasks(result.items);
        const activelyChanging = result.items.some((task) =>
          task.status === 'running' ||
          (task.progress?.queued ?? 0) > 0 ||
          (task.progress?.processing ?? 0) > 0
        );
        schedule(document.visibilityState === 'visible' ? (activelyChanging ? 1_000 : 5_000) : 10_000);
      } catch (err) {
        if (!live) return;
        setError(err instanceof Error ? err.message : '读取任务状态失败');
        schedule(document.visibilityState === 'visible' ? 5_000 : 10_000);
      }
    };
    const handleVisibilityChange = () => {
      if (!live || document.visibilityState !== 'visible') return;
      if (timer !== null) window.clearTimeout(timer);
      timer = null;
      void refresh();
    };
    document.addEventListener('visibilitychange', handleVisibilityChange);
    void refresh();
    return () => {
      live = false;
      if (timer !== null) window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [activeSettingsSection]);

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
      aiFocus: '',
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

  function updateViewerPrefs(next: ViewerPrefs) {
    saveViewerPrefs(next);
    setViewerPrefs(loadViewerPrefs());
  }

  function updateMediaViewPrefs(next: MediaViewPreferences) {
    setMediaViewPrefs(next);
    saveMediaViewPreferences(next);
  }

  function toggleMediaColumn(column: MediaColumnId, checked: boolean) {
    const visibleColumns = checked
      ? Array.from(new Set([...mediaViewPrefs.visibleColumns, column]))
      : mediaViewPrefs.visibleColumns.filter((id) => id !== column);
    updateMediaViewPrefs({ ...mediaViewPrefs, visibleColumns });
  }

  function moveMediaColumnDuringDrag(target: MediaColumnId) {
    if (!draggedMediaColumn || draggedMediaColumn === target) return;
    mediaColumnRectsBeforeMove.current = new Map(
      Array.from(mediaColumnElements.current.entries()).map(([id, element]) => [id, element.getBoundingClientRect()]),
    );
    setMediaViewPrefs((current) => {
      const sourceIndex = current.columnOrder.indexOf(draggedMediaColumn);
      if (sourceIndex < 0 || sourceIndex === current.columnOrder.indexOf(target)) return current;
      const next = current.columnOrder.filter((id) => id !== draggedMediaColumn);
      const targetIndex = next.indexOf(target);
      if (targetIndex < 0) return current;
      next.splice(targetIndex, 0, draggedMediaColumn);
      return { ...current, columnOrder: next };
    });
  }

  function finishMediaColumnDrag() {
    setDraggedMediaColumn(null);
    saveMediaViewPreferences(mediaViewPrefsRef.current);
  }

  function updateThemeMode(next: ThemeMode) {
    setThemeMode(next);
    saveThemeMode(next);
  }

  function updateImmersiveChromeSize(next: ImmersiveChromeSize) {
    setImmersiveChromeSize(next);
    saveImmersiveChromeSize(next);
  }

  function updateRowHeightLevel(next: GridRowHeightLevel) {
    setRowHeightLevel(next);
    saveGridRowHeightLevel(next);
  }

  async function saveVideoProxySettings() {
    const ttlMinutes = Number(videoProxyTTLMinutes);
    const maxCacheGB = Number(videoProxyMaxCacheGB);
    if (!Number.isFinite(ttlMinutes) || ttlMinutes < 1 || ttlMinutes > videoProxyMaxTTLMinutes) {
      setError(`保留时长范围是 1-${videoProxyMaxTTLMinutes} 分钟`);
      return;
    }
    if (!Number.isFinite(maxCacheGB) || maxCacheGB < 0) {
      setError('最大缓存容量不能小于 0');
      return;
    }
    setVideoProxySaving(true);
    setError(null);
    try {
      const saved = await api.updateVideoProxySettings({
        cacheTtlSeconds: Math.round(ttlMinutes) * 60,
        maxCacheBytes: Math.round(maxCacheGB * bytesPerGB),
      });
      applyVideoProxySettings(saved);
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存转码缓存设置失败');
    } finally {
      setVideoProxySaving(false);
    }
  }

  async function saveLibraryAIFocus(id: string, focus: string) {
    const updated = await api.updateScanLibraryAIFocus(id, focus);
    setLibraries((current) => current.map((library) => library.id === id ? updated : library));
    return updated;
  }

  async function reanalyzeLibraryAI(id: string) {
    return api.reindexScanLibraryAI(id);
  }

  return (
    <section className="page settings-page">
      <Toolbar title="设置" showScanAction={false} />
      <div className="settings-scroll">
        <div className="settings-layout">
          <nav className="settings-section-nav" aria-label="设置菜单">
            {settingsSections.map((section) => (
              <button
                aria-current={activeSettingsSection === section.id ? 'page' : undefined}
                className={activeSettingsSection === section.id ? 'active' : ''}
                key={section.id}
                type="button"
                onClick={() => selectSettingsSection(section.id)}
              >
                {section.label}
              </button>
            ))}
          </nav>
          <div className="settings-content">
            {error && <div className="error-line">{error}</div>}

            {activeSettingsSection === 'libraries' && (
              <section className="settings-section library-scan-section">
                <ScanManager
                  addOpen={addOpen}
                  editingLibrary={editingLibrary}
                  libraries={libraries}
                  onCreateLibrary={(name, relPaths) => void createLibrary(name, relPaths)}
                  onRemoveLibrary={(id) => void removeLibrary(id)}
                  onSetAddOpen={setAddOpen}
                  onSetEditingLibrary={setEditingLibrary}
                  onUpdateLibrary={(id, name, relPaths) => void updateLibrary(id, name, relPaths)}
                />
              </section>
            )}

            {activeSettingsSection === 'cache' && (
              <section className="settings-section cache-settings-section">
                <CacheManager
                  cleanup={cleanup}
                  progress={progress}
                  onReset={resetMediaLibrary}
                  onCleanup={async () => {
                    await api.runSystemTask('cache_cleanup', 'cleanup', null);
                    await refreshActivity();
                  }}
                />
                <section className="settings-panel settings-section">
                  <div className="settings-panel-title">视频播放处理</div>
                  <div className="muted-line cache-policy-intro">此设置仅保存在当前浏览器。浏览器优先会直接读取原文件并使用本机硬件解码；格式不受支持时自动回退服务器转码。</div>
                  <div className="settings-field settings-field-wide">
                    <span>处理方式</span>
                    <div className="settings-segmented two-options">
                      <button
                        className={viewerPrefs.videoProcessingMode === 'browser' ? 'active' : ''}
                        type="button"
                        onClick={() => updateViewerPrefs({ ...viewerPrefs, videoProcessingMode: 'browser' })}
                      >
                        浏览器优先
                      </button>
                      <button
                        className={viewerPrefs.videoProcessingMode === 'server' ? 'active' : ''}
                        type="button"
                        onClick={() => updateViewerPrefs({ ...viewerPrefs, videoProcessingMode: 'server' })}
                      >
                        服务器转码
                      </button>
                    </div>
                  </div>
                </section>
                <section className="settings-panel settings-section">
                  <div className="settings-panel-title">视频转码缓存</div>
                  <div className="muted-line cache-policy-intro">只控制视频播放时生成的代理文件；缩略图、高清预览和视频封面由图库扫描自动维护。</div>
                  <div className="viewer-settings-grid">
                    <label className="settings-field">
                      <span>保留时长（分钟）</span>
                      <input
                        min={1}
                        max={videoProxyMaxTTLMinutes}
                        step={1}
                        type="number"
                        value={videoProxyTTLMinutes}
                        onChange={(event) => setVideoProxyTTLMinutes(event.target.value)}
                      />
                    </label>
                    <label className="settings-field">
                      <span>最大容量（GB）</span>
                      <input
                        min={0}
                        step={1}
                        type="number"
                        value={videoProxyMaxCacheGB}
                        onChange={(event) => setVideoProxyMaxCacheGB(event.target.value)}
                      />
                    </label>
                    <div className="settings-field settings-field-wide settings-help-line">
                      <span>{videoProxySettings ? videoProxySettingsSummary(videoProxySettings) : '读取中'}</span>
                      <button className="settings-save-button" type="button" disabled={videoProxySaving} onClick={() => void saveVideoProxySettings()}>
                        {videoProxySaving ? '保存中' : '保存'}
                      </button>
                    </div>
                  </div>
                </section>
              </section>
            )}

            {activeSettingsSection === 'viewer' && (
              <section className="settings-section viewer-settings-section">
                <section className="settings-panel">
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
                    <span>悬浮菜单尺寸</span>
                    <div className="settings-segmented five-options" aria-label="悬浮菜单尺寸">
                      {([1, 2, 3, 4, 5] as const).map((size) => (
                        <button
                          className={immersiveChromeSize === size ? 'active' : ''}
                          key={size}
                          type="button"
                          title={size === 1 ? '最小' : size === 5 ? '最大' : `尺寸 ${size}`}
                          onClick={() => updateImmersiveChromeSize(size)}
                        >
                          {size}
                        </button>
                      ))}
                    </div>
                  </div>
                  <div className="settings-field settings-field-wide settings-field-spaced">
                    <span>媒体显示尺寸</span>
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
                <section className="settings-panel">
                  <div className="settings-panel-title">视频播放</div>
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
                    <span>字幕默认开启</span>
                  </label>
                  <div className="settings-field settings-field-wide">
                    <span>播放完毕后</span>
                    <div className="settings-segmented">
                      {playbackModeOptions.map((option) => (
                        <button
                          className={viewerPrefs.playbackMode === option.value ? 'active' : ''}
                          key={option.value}
                          type="button"
                          onClick={() => updateViewerPrefs({ ...viewerPrefs, playbackMode: option.value })}
                        >
                          {option.label}
                        </button>
                      ))}
                    </div>
                  </div>
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
                  <label className="settings-field settings-field-wide">
                    <span>视频播放延迟（秒）</span>
                    <input
                      max={videoPlaybackDelaySecondsRange.max}
                      min={videoPlaybackDelaySecondsRange.min}
                      step={0.1}
                      type="number"
                      value={viewerPrefs.videoPlaybackDelaySeconds}
                      onChange={(event) => updateViewerPrefs({
                        ...viewerPrefs,
                        videoPlaybackDelaySeconds: Number(event.target.value),
                      })}
                    />
                  </label>
                  </div>
                </section>
                <section className="settings-panel">
                  <div className="settings-panel-title">图片查看</div>
                  <div className="viewer-settings-grid">
                  <label className="settings-field settings-field-wide">
                    <span>连续播放间隔（秒）</span>
                    <input
                      max={imageSlideshowSecondsRange.max}
                      min={imageSlideshowSecondsRange.min}
                      step={1}
                      type="number"
                      value={viewerPrefs.imageSlideshowSeconds}
                      onChange={(event) => updateViewerPrefs({ ...viewerPrefs, imageSlideshowSeconds: Number(event.target.value) })}
                    />
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
                <section className="settings-panel media-layout-settings">
                  <div className="settings-panel-title">媒体布局</div>
                  <div className="settings-field settings-field-wide">
                    <span>全局布局</span>
                    <div className="settings-segmented three-options">
                      {mediaLayoutDefinitions.map((layout) => (
                        <button
                          className={mediaViewPrefs.mode === layout.id ? 'active' : ''}
                          key={layout.id}
                          title={layout.description}
                          type="button"
                          onClick={() => updateMediaViewPrefs({ ...mediaViewPrefs, mode: layout.id })}
                        >
                          {layout.label}
                        </button>
                      ))}
                    </div>
                  </div>
                  <label className="settings-check-row settings-field-wide">
                    <input
                      type="checkbox"
                      checked={mediaViewPrefs.videoHoverPreview}
                      onChange={(event) => updateMediaViewPrefs({
                        ...mediaViewPrefs,
                        videoHoverPreview: event.target.checked,
                      })}
                    />
                    <span>视频缩略图悬停预览</span>
                  </label>
                  {mediaLayoutDefinition(mediaViewPrefs.mode).configurableColumns && (
                  <div className="media-column-settings" aria-label="列表显示列">
                    <div className="muted-line">勾选显示字段，拖动调整顺序；列宽在列表表头中调节。</div>
                    {mediaViewPrefs.columnOrder.map((columnId) => {
                      const definition = mediaColumnDefinitions.find((column) => column.id === columnId);
                      if (!definition) return null;
                      const selected = mediaViewPrefs.visibleColumns.includes(columnId);
                      return (
                        <div
                          aria-checked={selected}
                          className={[
                            'media-column-setting',
                            selected ? 'selected' : '',
                            draggedMediaColumn === columnId ? 'dragging' : '',
                          ].filter(Boolean).join(' ')}
                          key={columnId}
                          ref={(element) => {
                            if (element) mediaColumnElements.current.set(columnId, element);
                            else mediaColumnElements.current.delete(columnId);
                          }}
                          role="checkbox"
                          tabIndex={0}
                          onDragEnter={() => moveMediaColumnDuringDrag(columnId)}
                          onDragOver={(event) => event.preventDefault()}
                          onDrop={(event) => {
                            event.preventDefault();
                            finishMediaColumnDrag();
                          }}
                          onClick={() => toggleMediaColumn(columnId, !selected)}
                          onKeyDown={(event) => {
                            if (event.key !== 'Enter' && event.key !== ' ') return;
                            event.preventDefault();
                            toggleMediaColumn(columnId, !selected);
                          }}
                        >
                          <span
                            className="media-column-drag"
                            draggable
                            role="button"
                            tabIndex={0}
                            title={`拖动“${definition.label}”调整顺序`}
                            onClick={(event) => event.stopPropagation()}
                            onDragEnd={finishMediaColumnDrag}
                            onDragStart={(event) => {
                              setDraggedMediaColumn(columnId);
                              event.dataTransfer.effectAllowed = 'move';
                              event.dataTransfer.setData('text/plain', columnId);
                              const card = event.currentTarget.closest('.media-column-setting');
                              if (card instanceof HTMLElement) event.dataTransfer.setDragImage(card, 18, 18);
                            }}
                          >
                            ⋮⋮
                          </span>
                          <span>{definition.label}</span>
                        </div>
                      );
                    })}
                  </div>
                  )}
                </section>
              </section>
            )}

            {activeSettingsSection === 'ai' && (
              <AISettingsPanel
                libraries={libraries}
                settings={aiSettings}
                onReanalyzeLibrary={reanalyzeLibraryAI}
                onSaveLibraryFocus={saveLibraryAIFocus}
              />
            )}

            {activeSettingsSection === 'tasks' && (
              <TaskSettingsPanel tasks={systemTasks} />
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

const bytesPerGB = 1024 ** 3;
const videoProxyMaxTTLMinutes = 30 * 24 * 60;
function AISettingsPanel({
  libraries,
  settings,
  onReanalyzeLibrary,
  onSaveLibraryFocus,
}: {
  libraries: ScanLibrary[];
  settings: AISettings | null;
  onReanalyzeLibrary: (id: string) => Promise<{ accepted: boolean; count: number; libraryId: string }>;
  onSaveLibraryFocus: (id: string, focus: string) => Promise<ScanLibrary>;
}) {
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState<Set<string>>(new Set());
  const [messages, setMessages] = useState<Record<string, string>>({});
  const [libraryBusy, setLibraryBusy] = useState<string | null>(null);
  useEffect(() => {
    setDrafts((current) => Object.fromEntries(libraries.map((library) => [library.id, current[library.id] ?? library.aiFocus ?? ''])));
  }, [libraries]);
  if (!settings) {
    return (
      <section className="settings-panel settings-section">
        <div className="settings-panel-title">AI 分析</div>
        <div className="muted-line">读取中</div>
      </section>
    );
  }
  return (
    <section className="settings-panel settings-section ai-settings-panel">
      <div className="settings-panel-title">AI 分析</div>
      <div className="ai-settings-controls">
        <div className="settings-help-line">新增媒体会在基础缓存完成后自动进入 AI 分析，无需手动启动或停止。</div>
      </div>
      <div className="ai-focus-settings">
        <div className="settings-panel-title">按图库重点识别</div>
        <div className="settings-help-line">仅填写希望 AI 优先描述和标注的内容，最多 500 字。</div>
        {libraries.map((library) => {
          const draft = drafts[library.id] ?? '';
          const changed = draft.trim() !== (library.aiFocus ?? '').trim();
          return (
            <div className="ai-focus-library" key={library.id}>
              <div className="ai-focus-library-title">
                <strong>{library.name}</strong>
                <small>{library.progress.assetTotal.toLocaleString()} 个媒体</small>
              </div>
              <textarea
                maxLength={500}
                placeholder="例如：车型、服装款式、拍摄器材、室内陈设"
                value={draft}
                onChange={(event) => setDrafts((current) => ({ ...current, [library.id]: event.target.value }))}
              />
              <div className="ai-focus-library-actions">
                <small>{draft.length}/500</small>
                <button
                  className="settings-save-button"
                  disabled={!changed || libraryBusy !== null}
                  type="button"
                  onClick={() => {
                    setLibraryBusy(library.id);
                    setMessages((current) => ({ ...current, [library.id]: '' }));
                    void onSaveLibraryFocus(library.id, draft)
                      .then(() => {
                        setSaved((current) => new Set(current).add(library.id));
                        setMessages((current) => ({ ...current, [library.id]: '重点已保存，现有结果尚未更新。' }));
                      })
                      .catch((err) => setMessages((current) => ({ ...current, [library.id]: err instanceof Error ? err.message : '保存失败' })))
                      .finally(() => setLibraryBusy(null));
                  }}
                >
                  {libraryBusy === library.id ? '保存中' : '保存重点'}
                </button>
              </div>
              {saved.has(library.id) && (
                <div className="ai-focus-reanalyze">
                  <span>{messages[library.id]}</span>
                  <button
                    disabled={libraryBusy !== null}
                    type="button"
                    onClick={() => {
                      setLibraryBusy(library.id);
                      void onReanalyzeLibrary(library.id)
                        .then((result) => {
                          setMessages((current) => ({ ...current, [library.id]: `已将 ${result.count.toLocaleString()} 个媒体加入重新分析队列。` }));
                          setSaved((current) => { const next = new Set(current); next.delete(library.id); return next; });
                        })
                        .catch((err) => setMessages((current) => ({ ...current, [library.id]: err instanceof Error ? err.message : '重新分析失败' })))
                        .finally(() => setLibraryBusy(null));
                    }}
                  >
                    重新分析此图库
                  </button>
                </div>
              )}
              {!saved.has(library.id) && messages[library.id] && <div className="settings-help-line">{messages[library.id]}</div>}
            </div>
          );
        })}
      </div>
    </section>
  );
}

const automaticTaskPipelines = [
  {
    id: 'media_pipeline',
    name: '媒体自动处理',
    description: '发现新媒体后自动完成入库、缩略图、封面、高清预览和进度预览图。',
    taskIds: ['media_scan', 'thumbnail_creation', 'video_poster_creation', 'preview_creation', 'storyboard_creation'],
  },
  {
    id: 'intelligence_pipeline',
    name: '智能分析',
    description: '媒体基础缓存就绪后，自动执行 AI 分析和重复文件索引。',
    taskIds: ['ai_analysis', 'duplicate_scan'],
  },
  {
    id: 'maintenance_pipeline',
    name: '后台维护',
    description: '自动检查任务执行器、存储、NAS 监听、AI 服务和缓存空间。',
    taskIds: ['task_executor_health', 'storage_health_check', 'nas_realtime_watcher', 'ai_health_check', 'cache_cleanup', 'library_scan', 'source_io_scheduler'],
  },
] as const;

function TaskSettingsPanel({ tasks }: { tasks: SystemTask[] }) {
  const navigate = useNavigate();
  const [nowSeconds, setNowSeconds] = useState(() => Math.floor(Date.now() / 1000));
  const [globalScanStarting, setGlobalScanStarting] = useState(false);
  const [globalScanMessage, setGlobalScanMessage] = useState('');
  useEffect(() => {
    if (!tasks.some((task) => task.status === 'running')) return;
    setNowSeconds(Math.floor(Date.now() / 1000));
    const timer = window.setInterval(() => setNowSeconds(Math.floor(Date.now() / 1000)), 1000);
    return () => window.clearInterval(timer);
  }, [tasks]);
  const openFailureInLibrary = useCallback((path: string) => {
    const normalizedPath = path.replace(/\\/g, '/').replace(/^\/+|\/+$/g, '');
    const parts = normalizedPath.split('/').filter(Boolean);
    const filename = parts.pop()?.trim() || path.trim();
    if (!filename) return;
    const folder = parts.join('/');
    navigate({
      pathname: '/folders',
      search: new URLSearchParams({
        folder,
        group: 'none',
        orientation: 'all',
        q: filename,
        rating: 'all',
        recursive: '0',
        type: 'all',
      }).toString(),
    });
  }, [navigate]);
  const byID = new Map(tasks.map((task) => [task.id, task]));
  const assigned = new Set<string>(automaticTaskPipelines.flatMap((pipeline) => [...pipeline.taskIds]));
  const pipelines = automaticTaskPipelines.map((pipeline) => {
    const members = pipeline.taskIds.map((id) => byID.get(id)).filter((task): task is SystemTask => Boolean(task));
    if (pipeline.id === 'maintenance_pipeline') {
      members.push(...tasks.filter((task) => !assigned.has(task.id)));
    }
    return { ...pipeline, members, task: mergeAutomaticTasks(pipeline.id, pipeline.name, pipeline.description, members) };
  });
  const countedTaskIDs = new Set(['media_scan', 'thumbnail_creation', 'video_poster_creation', 'preview_creation', 'storyboard_creation', 'ai_analysis', 'duplicate_scan']);
  const countedTasks = tasks.filter((task) => countedTaskIDs.has(task.id));
  const active = countedTasks.reduce((sum, task) => sum + (task.progress?.processing ?? 0), 0);
  const queued = countedTasks.reduce((sum, task) => sum + Math.max(task.progress?.queued ?? 0, task.progress?.pending ?? 0), 0);
  const failed = countedTasks.reduce((sum, task) => sum + (task.progress?.failed ?? 0), 0);
  const executor = byID.get('task_executor_health');
  const mediaScan = byID.get('media_scan');
  const globalScanRunning = globalScanStarting || mediaScan?.status === 'running';
  const globalStatus = executor?.status === 'failed' ? 'failed' : active > 0 ? 'running' : queued > 0 ? 'pending' : failed > 0 ? 'warning' : 'success';
  const renderPipeline = (task: SystemTask, members: SystemTask[]) => {
    const finishedAt = task.status === 'running' ? null : task.lastFinishedAt;
    const durationSeconds = task.status === 'running' && task.lastStartedAt != null
      ? Math.max(0, nowSeconds - task.lastStartedAt)
      : task.durationSeconds;
    const hasTimeline = task.lastStartedAt != null || finishedAt != null || durationSeconds != null || task.nextRunAt != null;
    const hasFailures = (task.failures?.length ?? 0) > 0;
    const hasDetails = hasTimeline || hasFailures;
    return (
      <article
        aria-label={`${task.name}，${systemTaskStatusLabel(task.status)}`}
        className={`system-task-card automatic-task-card status-${task.status}`}
        key={task.id}
      >
        <div className="system-task-heading">
          <div className="system-task-title">
            <span
              aria-describedby={`task-description-${task.id}`}
              className="system-task-name-wrap"
              tabIndex={0}
            >
              <span className="system-task-name">{task.name}</span>
              <span className="system-task-description-tooltip" id={`task-description-${task.id}`} role="tooltip">
                {task.description}
              </span>
            </span>
          </div>
          <span className={`automatic-task-state status-${task.status}`}>{systemTaskStatusLabel(task.status)}</span>
        </div>
        {task.progress && (
          <SystemTaskProgress
            averageSecondsPerItem={task.averageSecondsPerItem}
            task={task}
          />
        )}
        {task.blockedReason && (
          <div className="system-task-blocked-reason" role="status">
            <span aria-hidden="true" className="system-task-blocked-indicator" />
            {task.blockedReason}
          </div>
        )}
        <div className="automatic-task-stages">
          {members.map((member) => {
            const memberPending = Math.max(member.progress?.queued ?? 0, member.progress?.pending ?? 0);
            return (
              <div className={`automatic-task-stage status-${member.status}`} key={member.id}>
                <strong>{automaticTaskStageName(member)}</strong>
                <span>{automaticTaskStageSummary(member, memberPending)}</span>
              </div>
            );
          })}
        </div>
        {hasDetails && <div className="system-task-details">
          {hasTimeline && (
            <div className="system-task-timeline">
              {task.lastStartedAt != null && <span>开始 {formatSystemTaskTime(task.lastStartedAt)}</span>}
              {finishedAt != null && <span>完成 {formatSystemTaskTime(finishedAt)}</span>}
              {durationSeconds != null && <span>{task.status === 'running' ? '运行时长' : '耗时'} {formatTaskDuration(durationSeconds)}</span>}
              {task.nextRunAt != null && <span>下次 {formatSystemTaskTime(task.nextRunAt)}</span>}
            </div>
          )}
          {!task.progress && <div className="system-task-message">{task.message || '尚未运行'}</div>}
          {hasFailures && <SystemTaskFailures task={task} onOpen={openFailureInLibrary} />}
        </div>}
      </article>
    );
  };
  return (
    <section className="settings-panel settings-section system-tasks-panel">
      <div className="settings-panel-title">自动任务</div>
      <div className={`automatic-task-overview status-${globalStatus}`}>
        <div><strong>{automaticTaskOverviewTitle(globalStatus)}</strong><span>{executor?.message || '系统会自动安排全部处理任务'}</span></div>
        <div className="automatic-task-overview-actions">
          <div className="automatic-task-overview-stats"><span>处理中 <b>{active.toLocaleString()}</b></span><span>待处理 <b>{queued.toLocaleString()}</b></span><span>失败 <b>{failed.toLocaleString()}</b></span></div>
          <button
            aria-busy={globalScanStarting}
            className="automatic-task-global-scan"
            disabled={globalScanRunning}
            type="button"
            onClick={() => {
              setGlobalScanStarting(true);
              setGlobalScanMessage('');
              void api.runSystemTask('media_scan', 'scan', null)
                .then(() => setGlobalScanMessage('全局扫描已加入队列'))
                .catch((err) => setGlobalScanMessage(err instanceof Error ? err.message : '全局扫描启动失败'))
                .finally(() => setGlobalScanStarting(false));
            }}
          >
            {globalScanRunning && <span aria-hidden="true" className="button-progress-spinner" />}
            {globalScanRunning ? '扫描中' : '全局扫描'}
          </button>
        </div>
      </div>
      {globalScanMessage && <div className="automatic-task-global-scan-message" role="status">{globalScanMessage}</div>}
      <div className="system-task-groups automatic-task-groups">
        {tasks.length === 0 && <div className="muted-line">读取中</div>}
        {pipelines.map((pipeline) => renderPipeline(pipeline.task, pipeline.members))}
      </div>
    </section>
  );
}

function mergeAutomaticTasks(id: string, name: string, description: string, members: SystemTask[]): SystemTask {
  const progress = members.reduce((sum, task) => ({
    total: sum.total + (task.progress?.total ?? 0),
    completed: sum.completed + (task.progress?.completed ?? 0),
    queued: 0,
    pending: sum.pending + Math.max(task.progress?.queued ?? 0, task.progress?.pending ?? 0),
    processing: sum.processing + (task.progress?.processing ?? 0),
    failed: sum.failed + (task.progress?.failed ?? 0),
  }), { total: 0, completed: 0, queued: 0, pending: 0, processing: 0, failed: 0 });
  const status: SystemTask['status'] = members.some((task) => task.status === 'failed') ? 'failed'
    : members.some((task) => task.status === 'running') ? 'running'
      : members.some((task) => task.status === 'warning') ? 'warning'
        : members.some((task) => task.status === 'pending') ? 'pending'
          : members.length > 0 && members.every((task) => task.status === 'success' || task.status === 'skipped') ? 'success' : 'never';
  const starts = members.map((task) => task.lastStartedAt).filter((value): value is number => value != null);
  const finishes = members.map((task) => task.lastFinishedAt).filter((value): value is number => value != null);
  const failures = members.flatMap((task) => task.failures ?? []).slice(0, 100);
  return {
    id, name, description, schedule: '全自动', status, succeeded: status === 'success' ? true : status === 'failed' ? false : null,
    lastStartedAt: starts.length > 0 ? Math.min(...starts) : null,
    lastFinishedAt: finishes.length > 0 ? Math.max(...finishes) : null,
    nextRunAt: null, durationSeconds: null, averageSecondsPerItem: null,
    message: '', blockedReason: members.find((task) => task.blockedReason)?.blockedReason,
    lastError: members.find((task) => task.lastError)?.lastError ?? '', processed: progress.completed,
    failedCount: members.reduce((sum, task) => sum + Number(task.failedCount || 0), 0), canRetry: false,
    supportsScope: false, progress, actions: [], failures,
  };
}

function automaticTaskStageName(task: SystemTask) {
  const names: Record<string, string> = {
    media_scan: '发现与入库', thumbnail_creation: '缩略图', video_poster_creation: '视频封面', preview_creation: '高清预览', storyboard_creation: '进度预览',
    ai_analysis: 'AI 分析', duplicate_scan: '重复索引', task_executor_health: '执行器自检', storage_health_check: '存储连接', nas_realtime_watcher: 'NAS 监听',
    ai_health_check: 'AI 服务', cache_cleanup: '缓存空间', library_scan: '媒体库检查', source_io_scheduler: '读取调度',
  };
  return names[task.id] ?? task.name;
}

function automaticTaskStageSummary(task: SystemTask, pending: number) {
  if (task.status === 'running') return `处理中 ${Math.max(1, task.progress?.processing ?? 0).toLocaleString()}`;
  if (task.status === 'failed') return `失败 ${(task.progress?.failed ?? task.failedCount ?? 0).toLocaleString()}`;
  if (pending > 0) return `待处理 ${pending.toLocaleString()}`;
  if (task.status === 'success') return '正常';
  if (task.status === 'warning') return '自动等待';
  return systemTaskStatusLabel(task.status);
}

function automaticTaskOverviewTitle(status: SystemTask['status']) {
  if (status === 'failed') return '自动处理异常';
  if (status === 'running') return '正在自动处理';
  if (status === 'pending') return '任务正在等待调度';
  if (status === 'warning') return '部分任务需要关注';
  return '自动处理正常';
}

function SystemTaskFailures({ task, onOpen }: { task: SystemTask; onOpen: (path: string) => void }) {
  const failures = task.failures ?? [];
  const shown = failures.length;
  const total = Math.max(shown, Number(task.failedCount));
  return (
    <details className="system-task-failures">
      <summary>
        <span>失败明细</span>
        <span className="system-task-failure-count">显示 {shown.toLocaleString()} / {total.toLocaleString()}</span>
      </summary>
      <div className="system-task-failure-list">
        {failures.map((failure, index) => (
          <div className="system-task-failure-item" key={`${failure.assetId ?? 'summary'}:${failure.path}:${index}`}>
            <div className="system-task-failure-content">
              {failure.path && <div className="system-task-failure-path">{failure.path}</div>}
              <div className="system-task-error">{failure.reason}</div>
            </div>
            {failure.path && (
              <button className="system-task-failure-open" type="button" onClick={() => onOpen(failure.path)}>
                在图库中查看
              </button>
            )}
          </div>
        ))}
      </div>
    </details>
  );
}

function SystemTaskProgress({ averageSecondsPerItem, task }: { averageSecondsPerItem: number | null; task: SystemTask }) {
  const progress = task.progress!;
  const percent = progress.total > 0 ? Math.min(100, progress.completed / progress.total * 100) : 0;
  const stats = [
    { label: '已处理', value: progress.completed, tone: 'success' },
    { label: '待处理', value: Math.max(progress.queued ?? 0, progress.pending), tone: 'queued' },
    { label: '处理中', value: progress.processing, tone: 'running' },
    { label: '失败', value: progress.failed, tone: 'failed' },
  ];
  return (
    <div className="system-task-progress">
      <div className="system-task-progress-stats">
        {stats.map((stat) => (
          <div className={`system-task-stat ${stat.tone}`} key={stat.label}>
            <strong>{stat.value.toLocaleString()}</strong>
            <span>{stat.label}</span>
            {stat.label === '处理中' && task.status === 'running' && averageSecondsPerItem != null && (
              <small>{formatAverageTaskRate(averageSecondsPerItem)}</small>
            )}
          </div>
        ))}
      </div>
      <div className="progress-bar"><div className="progress-fill" style={{ width: `${percent}%` }} /></div>
    </div>
  );
}

function formatAverageTaskRate(secondsPerItem: number) {
  if (!Number.isFinite(secondsPerItem) || secondsPerItem <= 0) return '平均 0 秒/项';
  const rate = 1 / secondsPerItem;
  if (rate >= 10) return `平均 ${rate.toFixed(1)} 项/秒`;
  if (rate >= 1) return `平均 ${rate.toFixed(2)} 项/秒`;
  if (secondsPerItem >= 60) return `平均 ${(secondsPerItem / 60).toFixed(1)} 分钟/项`;
  return `平均 ${secondsPerItem.toFixed(1)} 秒/项`;
}

function systemTaskStatusLabel(status: SystemTask['status']) {
  switch (status) {
    case 'running': return '运行中';
    case 'pending': return '等待中';
    case 'success': return '成功';
    case 'warning': return '部分失败';
    case 'failed': return '失败';
    case 'stopped': return '已停止';
    case 'interrupted': return '当前空闲';
    case 'skipped': return '无需运行';
    default: return '尚未运行';
  }
}

function formatSystemTaskTime(value: number | null) {
  if (value == null || value <= 0) return '尚未运行';
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  }).format(new Date(value * 1000));
}

function formatTaskDuration(seconds: number) {
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  if (minutes < 60) return rest > 0 ? `${minutes} 分 ${rest} 秒` : `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  return `${hours} 小时 ${minutes % 60} 分钟`;
}

function formatCacheGB(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0';
  const value = bytes / bytesPerGB;
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

function videoProxySettingsSummary(settings: VideoProxySettings) {
  const minutes = Math.round(settings.cacheTtlSeconds / 60);
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  const restMinutes = minutes % 60;
  const ttlParts = [
    days > 0 ? `${days} 天` : '',
    hours > 0 ? `${hours} 小时` : '',
    restMinutes > 0 || (days === 0 && hours === 0) ? `${restMinutes} 分钟` : '',
  ].filter(Boolean);
  const cap = settings.maxCacheBytes > 0 ? `${formatCacheGB(settings.maxCacheBytes)} GB` : '不限制容量';
  return `${ttlParts.join(' ')}，${cap}`;
}
