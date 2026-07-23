import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import Toolbar from '../components/Toolbar';
import { useSidebarPanel } from '../components/SidebarContext';
import { api } from '../api/client';
import type { AISettings, CleanupStatus, ProcessingProgress, ScanLibrary, ScanLibraryProgress, ScanStatus, StorageStatus, SystemTask, VideoProxySettings, WorkStatusCounts } from '../types/api';
import { useAssetReadyEvents, useScanStatusEvents } from '../hooks/useAssetReadyEvents';
import { loadGridRowHeightLevel, saveGridRowHeightLevel, type GridRowHeightLevel } from '../utils/gridPrefs';
import { loadThemeMode, saveThemeMode, type ThemeMode } from '../utils/themePrefs';
import { imageSlideshowSecondsRange, loadViewerPrefs, playbackModeOptions, playbackRates, saveViewerPrefs, type ViewerPrefs } from '../utils/viewerPrefs';
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
  const [aiSettingsSaving, setAISettingsSaving] = useState(false);
  const [systemTasks, setSystemTasks] = useState<SystemTask[]>([]);
  const [taskActionBusy, setTaskActionBusy] = useState<string | null>(null);
  const [taskScopes, setTaskScopes] = useState<Record<string, string>>({});
  const [taskConfirmation, setTaskConfirmation] = useState<{
    task: SystemTask;
    action: SystemTask['actions'][number];
    scope: string;
  } | null>(null);
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
    if (sectionSlug?.trim().toLowerCase() === 'video-proxy') {
      navigate(settingsSectionPath('cache'), { replace: true });
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

  useEffect(() => {
    void refreshInitial();
  }, [refreshInitial]);

  useEffect(() => {
    setTaskScopes((current) => Object.fromEntries(Object.entries(current).filter(([, id]) => id === 'all' || libraries.some((library) => library.id === id))));
  }, [libraries]);

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
    const refresh = () => void api.systemTasks()
      .then((result) => { if (live) setSystemTasks(result.items); })
      .catch((err) => { if (live) setError(err instanceof Error ? err.message : '读取任务状态失败'); });
    refresh();
    const timer = window.setInterval(refresh, 10_000);
    return () => {
      live = false;
      window.clearInterval(timer);
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

  async function executeSystemTask(task: SystemTask, action: SystemTask['actions'][number], selectedScope: string) {
    if (taskActionBusy) return;
    const key = `${task.id}:${action.id}`;
    setTaskActionBusy(key);
    setError(null);
    try {
      if (action.id === 'stop') {
        await api.stopSystemTask(task.id);
      } else {
        await api.runSystemTask(task.id, action.id, selectedScope === 'all' ? null : selectedScope);
      }
      const [tasksResult] = await Promise.all([
        api.systemTasks(),
        refreshActivity(),
        refreshLibraries(),
      ]);
      setSystemTasks(tasksResult.items);
    } catch (err) {
      setError(err instanceof Error ? err.message : '执行任务失败');
    } finally {
      setTaskActionBusy(null);
    }
  }

  function runSystemTask(task: SystemTask, action: SystemTask['actions'][number]) {
    if (taskActionBusy || !action.enabled) return;
    const selectedScope = task.supportsScope ? (taskScopes[task.id] ?? 'all') : 'all';
    if (action.requiresConfirmation) {
      setTaskConfirmation({ task, action, scope: selectedScope });
      return;
    }
    void executeSystemTask(task, action, selectedScope);
  }

  function updateViewerPrefs(next: ViewerPrefs) {
    setViewerPrefs(next);
    saveViewerPrefs(next);
  }

  function updateMediaViewPrefs(next: MediaViewPreferences) {
    setMediaViewPrefs(next);
    saveMediaViewPreferences(next);
  }

  function toggleMediaColumn(column: MediaColumnId, checked: boolean) {
    if (column === 'media') return;
    const visibleColumns = checked
      ? Array.from(new Set([...mediaViewPrefs.visibleColumns, column]))
      : mediaViewPrefs.visibleColumns.filter((id) => id !== column);
    updateMediaViewPrefs({ ...mediaViewPrefs, visibleColumns });
  }

  function moveMediaColumnDuringDrag(target: MediaColumnId) {
    if (!draggedMediaColumn || draggedMediaColumn === target || draggedMediaColumn === 'media' || target === 'media') return;
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

  async function updateAIAutomatic(enabled: boolean) {
    if (aiSettingsSaving) return;
    setAISettingsSaving(true);
    setError(null);
    try {
      const saved = await api.updateAISettings(enabled);
      setAISettings(saved);
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存 AI 设置失败');
    } finally {
      setAISettingsSaving(false);
    }
  }

  async function toggleAIManualRun() {
    if (!aiSettings || aiSettings.autoAnalyze || aiSettingsSaving) return;
    setAISettingsSaving(true);
    setError(null);
    try {
      const saved = aiSettings.manualRun
        ? await api.stopAIManually()
        : (await api.runAIManually()).settings;
      setAISettings(saved);
    } catch (err) {
      setError(err instanceof Error ? err.message : '切换手动 AI 分析失败');
    } finally {
      setAISettingsSaving(false);
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
            onClick={() => selectSettingsSection(section.id)}
          >
            <span className="sidebar-list-marker" aria-hidden="true" />
            <span>{section.label}</span>
          </button>
        ))}
      </div>
    </div>,
    [activeSettingsSection, selectSettingsSection],
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
                <CacheManager cleanup={cleanup} progress={progress} />
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
                  <span>图库单行高度</span>
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
              <section className="settings-section viewer-settings-section">
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
                    <div className="settings-segmented three-options">
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
                    <div className="settings-segmented">
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
                  {mediaLayoutDefinition(mediaViewPrefs.mode).configurableColumns && (
                  <div className="media-column-settings" aria-label="列表显示列">
                    <div className="muted-line">勾选显示字段，拖动调整顺序；列宽在列表表头中调节。</div>
                    {mediaViewPrefs.columnOrder.map((columnId) => {
                      const definition = mediaColumnDefinitions.find((column) => column.id === columnId);
                      if (!definition) return null;
                      const locked = columnId === 'media';
                      const selected = locked || mediaViewPrefs.visibleColumns.includes(columnId);
                      return (
                        <div
                          aria-checked={selected}
                          aria-disabled={locked}
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
                          tabIndex={locked ? -1 : 0}
                          onDragEnter={() => moveMediaColumnDuringDrag(columnId)}
                          onDragOver={(event) => event.preventDefault()}
                          onDrop={(event) => {
                            event.preventDefault();
                            finishMediaColumnDrag();
                          }}
                          onClick={() => {
                            if (!locked) toggleMediaColumn(columnId, !selected);
                          }}
                          onKeyDown={(event) => {
                            if (locked || (event.key !== 'Enter' && event.key !== ' ')) return;
                            event.preventDefault();
                            toggleMediaColumn(columnId, !selected);
                          }}
                        >
                          <span
                            aria-hidden={locked}
                            className={locked ? 'media-column-drag locked' : 'media-column-drag'}
                            draggable={!locked}
                            role={locked ? undefined : 'button'}
                            tabIndex={locked ? undefined : 0}
                            title={locked ? undefined : `拖动“${definition.label}”调整顺序`}
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
                busy={aiSettingsSaving}
                libraries={libraries}
                settings={aiSettings}
				storageAvailable={storageStatus?.available !== false}
                onAutomaticChange={(enabled) => void updateAIAutomatic(enabled)}
                onReanalyzeLibrary={reanalyzeLibraryAI}
                onSaveLibraryFocus={saveLibraryAIFocus}
                onToggleManual={() => void toggleAIManualRun()}
              />
            )}

            {activeSettingsSection === 'tasks' && (
              <TaskSettingsPanel
                actionBusy={taskActionBusy}
                libraries={libraries}
                scopes={taskScopes}
                tasks={systemTasks}
                onAction={runSystemTask}
                onScopeChange={(taskId, libraryId) => setTaskScopes((current) => ({ ...current, [taskId]: libraryId }))}
              />
            )}
          </div>
        </div>
      </div>
      {taskConfirmation && (
        <TaskConfirmationDialog
          action={taskConfirmation.action}
          busy={taskActionBusy !== null}
          scopeLabel={taskScopeLabel(taskConfirmation.scope, libraries)}
          task={taskConfirmation.task}
          onCancel={() => setTaskConfirmation(null)}
          onConfirm={() => {
            const pending = taskConfirmation;
            setTaskConfirmation(null);
            void executeSystemTask(pending.task, pending.action, pending.scope);
          }}
        />
      )}
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
  busy,
  libraries,
  settings,
	storageAvailable,
  onAutomaticChange,
  onReanalyzeLibrary,
  onSaveLibraryFocus,
  onToggleManual,
}: {
  busy: boolean;
  libraries: ScanLibrary[];
  settings: AISettings | null;
	storageAvailable: boolean;
  onAutomaticChange: (enabled: boolean) => void;
  onReanalyzeLibrary: (id: string) => Promise<{ accepted: boolean; count: number; libraryId: string }>;
  onSaveLibraryFocus: (id: string, focus: string) => Promise<ScanLibrary>;
  onToggleManual: () => void;
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
        <label className="settings-check-row settings-field-wide">
          <input
            checked={settings.autoAnalyze}
            disabled={busy}
            type="checkbox"
            onChange={(event) => onAutomaticChange(event.target.checked)}
          />
          <span>自动分析新增媒体并持续补齐图库</span>
        </label>
        <div className="settings-help-line">
			<span>{!storageAvailable ? '停' : settings.autoAnalyze ? '自动模式运行中' : settings.manualRun ? '手动全库分析运行中' : '自动分析已关闭'}</span>
          <button
            className="settings-save-button"
            disabled={busy || settings.autoAnalyze}
            type="button"
            onClick={onToggleManual}
          >
            {busy ? '处理中' : settings.manualRun ? '停止手动分析' : '手动开始'}
          </button>
        </div>
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

function TaskConfirmationDialog({ action, busy, scopeLabel, task, onCancel, onConfirm }: {
  action: SystemTask['actions'][number];
  busy: boolean;
  scopeLabel: string;
  task: SystemTask;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busy) onCancel();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [busy, onCancel]);
  const cleanup = action.id === 'cleanup';
  const restart = action.id === 'restart';
  const title = cleanup ? '确认清理缓存' : restart ? '确认重启服务' : '确认全部重建';
  const detail = cleanup
    ? '只会删除没有数据库引用的缓存和超过 24 小时的临时文件。'
    : restart
      ? 'AI 服务会立即重启，当前分析会中断并在服务恢复后重新排队。'
      : '该范围内已经完成的结果会被重置并重新处理，任务启动后可停止，但已完成的处理不会回滚。';
  const confirmLabel = cleanup ? '立即清理' : restart ? '重启服务' : '全部重建';
  return (
    <div className="modal-backdrop task-confirmation-backdrop" role="presentation" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !busy) onCancel();
    }}>
      <div aria-describedby="task-confirmation-description" aria-labelledby="task-confirmation-title" aria-modal="true" className="task-confirmation-dialog" role="dialog">
        <div className="modal-title" id="task-confirmation-title">{title}</div>
        <div className="task-confirmation-content" id="task-confirmation-description">
          <div><span>任务</span><strong>{task.name}</strong></div>
          <div><span>执行范围</span><strong>{scopeLabel}</strong></div>
          <p>{detail}</p>
        </div>
        <div className="modal-actions">
          <button className="command-button" disabled={busy} type="button" onClick={onCancel}>取消</button>
          <button className="command-button danger" disabled={busy} type="button" onClick={onConfirm}>{confirmLabel}</button>
        </div>
      </div>
    </div>
  );
}

function taskScopeLabel(scope: string, libraries: ScanLibrary[]) {
  if (scope === 'all') return '全部图库';
  return libraries.find((library) => library.id === scope)?.name ?? '所选图库';
}

function TaskSettingsPanel({ actionBusy, libraries, scopes, tasks, onAction, onScopeChange }: {
  actionBusy: string | null;
  libraries: ScanLibrary[];
  scopes: Record<string, string>;
  tasks: SystemTask[];
  onAction: (task: SystemTask, action: SystemTask['actions'][number]) => void;
  onScopeChange: (taskId: string, libraryId: string) => void;
}) {
  const navigate = useNavigate();
  const [nowSeconds, setNowSeconds] = useState(() => Math.floor(Date.now() / 1000));
  useEffect(() => {
    if (!tasks.some((task) => task.status === 'running')) return;
    setNowSeconds(Math.floor(Date.now() / 1000));
    const timer = window.setInterval(() => setNowSeconds(Math.floor(Date.now() / 1000)), 1000);
    return () => window.clearInterval(timer);
  }, [tasks]);
  const openFailureInLibrary = useCallback((path: string) => {
    const filename = path.replace(/\\/g, '/').split('/').pop()?.trim() || path.trim();
    if (!filename) return;
    navigate({ pathname: '/library', search: new URLSearchParams({ q: filename, visible: 'all' }).toString() });
  }, [navigate]);
  return (
    <section className="settings-panel settings-section system-tasks-panel">
      <div className="settings-panel-title">系统任务</div>
      <div className="system-task-list">
        {tasks.length === 0 && <div className="muted-line">读取中</div>}
        {tasks.map((task) => {
          const finishedAt = task.status === 'running' ? null : task.lastFinishedAt;
          const durationSeconds = task.status === 'running' && task.lastStartedAt != null
            ? Math.max(0, nowSeconds - task.lastStartedAt)
            : task.durationSeconds;
          const hasTimeline = task.lastStartedAt != null || finishedAt != null || durationSeconds != null || task.nextRunAt != null;
          const hasFailures = (task.failures?.length ?? 0) > 0;
          const hasDetails = hasTimeline || !task.progress || hasFailures;
          return (
            <article
              aria-label={`${task.name}，${systemTaskStatusLabel(task.status)}`}
              className={`system-task-card status-${task.status}`}
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
                <div className="system-task-actions">
                  {task.supportsScope && (
                    <select
                      aria-label={`${task.name}执行范围`}
                      disabled={task.status === 'running' || actionBusy !== null}
                      value={scopes[task.id] ?? 'all'}
                      onChange={(event) => onScopeChange(task.id, event.target.value)}
                    >
                      <option value="all">全部图库</option>
                      {libraries.map((library) => <option key={library.id} value={library.id}>{library.name}</option>)}
                    </select>
                  )}
                  {task.actions.map((action) => {
                    const key = `${task.id}:${action.id}`;
                    return (
                      <button
                        className={`settings-save-button system-task-run-button ${action.kind}`}
                        disabled={!action.enabled || actionBusy !== null}
                        key={action.id}
                        type="button"
                        onClick={() => onAction(task, action)}
                      >
                        {actionBusy === key ? taskActionBusyLabel(action.id) : action.label}
                      </button>
                    );
                  })}
                </div>
              </div>
              {task.progress && <SystemTaskProgress task={task} />}
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
        })}
      </div>
    </section>
  );
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

function SystemTaskProgress({ task }: { task: SystemTask }) {
  const progress = task.progress!;
  const percent = progress.total > 0 ? Math.min(100, progress.completed / progress.total * 100) : 0;
  const stats = [
    { label: '总计', value: progress.total, tone: 'neutral' },
    { label: '已完成', value: progress.completed, tone: 'success' },
    { label: '队列', value: progress.queued ?? 0, tone: 'queued' },
    { label: '处理中', value: progress.processing, tone: 'running' },
    { label: '等待', value: progress.pending, tone: 'pending' },
    { label: '失败', value: progress.failed, tone: 'failed' },
  ];
  return (
    <div className="system-task-progress">
      <div className="system-task-progress-stats">
        {stats.map((stat) => (
          <div className={`system-task-stat ${stat.tone}`} key={stat.label}>
            <strong>{stat.value.toLocaleString()}</strong>
            <span>{stat.label}</span>
          </div>
        ))}
      </div>
      <div className="progress-bar"><div className="progress-fill" style={{ width: `${percent}%` }} /></div>
    </div>
  );
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

function taskActionBusyLabel(action: string) {
  if (action === 'stop') return '停止中';
  if (action === 'cleanup') return '清理中';
  if (action === 'check') return '检查中';
  if (action === 'restart') return '重启中';
  return '启动中';
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
