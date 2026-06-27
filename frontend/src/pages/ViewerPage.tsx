import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useParams, useSearchParams, type Location } from 'react-router-dom';
import { Check, LogOut, Trash2, X } from 'lucide-react';
import { api } from '../api/client';
import type { Asset, AssetDeleteEntry, AssetDeletePlan, AssetDeleteResult, AssetRating, AssetSidecars, Neighbors, NFOField, NFOFilterField, VideoProxyRuntime } from '../types/api';
import RatingStars, { normalizeAssetRating } from '../components/RatingStars';
import { formatBytes, formatDateTime, formatDuration } from '../utils/format';
import ImageViewer from '../viewer/ImageViewer';
import VideoViewer from '../viewer/VideoViewer';
import { useKeyboard } from '../hooks/useKeyboard';
import { useRestoreSidebarState, useSidebarPanel, type SidebarReturnState } from '../components/SidebarContext';
import { nextRotation } from '../utils/rotation';
import {
  decodeReturnState,
  emitAssetRatingChanged,
  emitViewerOverlayAssetFocus,
  encodeReturnState,
  loadViewerReturnPath,
  type GridReturnState,
} from '../utils/pageState';
import { loadViewerPrefs, saveViewerPrefs, viewerPrefsChanged } from '../utils/viewerPrefs';
import { preloadViewerAsset, preloadViewerAssets } from '../utils/imagePreload';

interface WheelBase {
  next: Asset[];
  offset: number;
  previous: Asset[];
}

interface ViewerPageProps {
  overlay?: boolean;
}

interface ViewerLocationState {
  backgroundLocation?: Location;
  initialAsset?: Asset;
}

const wheelStepCooldownMs = 220;
const wheelStepThreshold = 60;
const viewerReturnPageSize = 100;

export default function ViewerPage({ overlay = false }: ViewerPageProps) {
  const params = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [neighbors, setNeighbors] = useState<Neighbors | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sidecars, setSidecars] = useState<AssetSidecars | null>(null);
  const [sidecarError, setSidecarError] = useState<string | null>(null);
  const [subtitlesEnabled, setSubtitlesEnabled] = useState(false);
  const [selectedSubtitleId, setSelectedSubtitleId] = useState('');
  const [playbackRate, setPlaybackRate] = useState(() => loadViewerPrefs().playbackRate);
  const [videoProxyRuntime, setVideoProxyRuntime] = useState<VideoProxyRuntime | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletePlan, setDeletePlan] = useState<AssetDeletePlan | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteSubmitting, setDeleteSubmitting] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const wheelBase = useRef<WheelBase | null>(null);
  const wheelDelta = useRef(0);
  const wheelLastStepAt = useRef(0);
  const wheelResetTimer = useRef<number | null>(null);
  const viewerRef = useRef<HTMLElement | null>(null);
  const viewerReturnStateRef = useRef(decodeReturnState<Partial<SidebarReturnState>>(searchParams.get('returnState'), {}));
  const restoreSidebarState = useRestoreSidebarState();
  const viewerLocationState = location.state as ViewerLocationState | null;
  const backgroundLocation = viewerLocationState?.backgroundLocation;
  const assetId = Number(params.assetId || assetIdFromPath(location.pathname) || 0);
  const initialAsset = viewerLocationState?.initialAsset?.id === assetId ? viewerLocationState.initialAsset : undefined;

  const query = useMemo(() => {
    const result: Record<string, string> = {};
    searchParams.forEach((value, key) => {
      result[key] = value;
    });
    return result;
  }, [searchParams]);

  useEffect(() => {
    let live = true;
    const controller = new AbortController();
    async function load() {
      try {
        const result = await api.neighbors(assetId, query, controller.signal);
        if (!live) return;
        setNeighbors(result);
        setError(null);
      } catch (err) {
        if (isAbortError(err)) return;
        if (!live) return;
        setError(err instanceof Error ? err.message : '读取资源失败');
      }
    }
    if (assetId > 0) void load();
    return () => {
      live = false;
      controller.abort();
    };
  }, [assetId, query]);

  const activeNeighbors = neighbors?.current.id === assetId ? neighbors : null;
  const current = activeNeighbors?.current ?? initialAsset;

  useEffect(() => {
    if (!overlay || !current) return;
    emitViewerOverlayAssetFocus(current.id);
  }, [current?.id, overlay]);

  useEffect(() => {
    if (current?.mediaType === 'image') {
      preloadViewerAsset(current, 'high');
    }
    if (activeNeighbors) {
      preloadViewerAssets(preloadOrder(activeNeighbors).slice(1));
    }
  }, [activeNeighbors, current]);

  useEffect(() => {
    setVideoProxyRuntime(null);
    setDeleteDialogOpen(false);
    setDeletePlan(null);
    setDeleteError(null);
  }, [current?.id]);

  useEffect(() => {
    let live = true;
    async function loadSidecars(asset: Asset) {
      try {
        const result = await api.assetSidecars(asset.id);
        if (!live) return;
        setSidecars(result);
        setSidecarError(null);
        const defaultID = result.defaultSubtitleId ?? result.subtitles[0]?.id ?? '';
        setSelectedSubtitleId(defaultID);
        setSubtitlesEnabled(Boolean(defaultID) && loadViewerPrefs().subtitlesEnabled);
      } catch (err) {
        if (!live) return;
        setSidecars(null);
        setSidecarError(err instanceof Error ? err.message : '读取附加信息失败');
        setSelectedSubtitleId('');
        setSubtitlesEnabled(false);
      }
    }
    if (current) {
      void loadSidecars(current);
    } else {
      setSidecars(null);
      setSidecarError(null);
      setSelectedSubtitleId('');
      setSubtitlesEnabled(false);
    }
    return () => {
      live = false;
    };
  }, [current?.id]);

  useEffect(() => {
    function onPrefsChanged() {
      const prefs = loadViewerPrefs();
      setPlaybackRate(prefs.playbackRate);
      setSubtitlesEnabled(Boolean(selectedSubtitleId) && prefs.subtitlesEnabled);
    }
    window.addEventListener(viewerPrefsChanged, onPrefsChanged);
    window.addEventListener('storage', onPrefsChanged);
    return () => {
      window.removeEventListener(viewerPrefsChanged, onPrefsChanged);
      window.removeEventListener('storage', onPrefsChanged);
    };
  }, [selectedSubtitleId]);

  const updatePlaybackRate = useCallback((value: number) => {
    const prefs = { ...loadViewerPrefs(), playbackRate: value };
    saveViewerPrefs(prefs);
    setPlaybackRate(prefs.playbackRate);
  }, []);

  const updateSubtitlesEnabled = useCallback((value: boolean) => {
    saveViewerPrefs({ ...loadViewerPrefs(), subtitlesEnabled: value });
    setSubtitlesEnabled(Boolean(selectedSubtitleId) && value);
  }, [selectedSubtitleId]);

  const updateSelectedSubtitle = useCallback((value: string) => {
    setSelectedSubtitleId(value);
    if (value) {
      saveViewerPrefs({ ...loadViewerPrefs(), subtitlesEnabled: true });
      setSubtitlesEnabled(true);
    } else {
      setSubtitlesEnabled(false);
    }
  }, []);

  useEffect(() => {
    return () => {
      if (wheelResetTimer.current !== null) {
        window.clearTimeout(wheelResetTimer.current);
      }
    };
  }, []);

  const goAsset = useCallback(
    (asset: Asset | undefined) => {
      if (!asset) return;
      preloadViewerAsset(asset, 'high');
      navigate(
        { pathname: `/viewer/${asset.id}`, search: searchParams.toString() },
        overlay && backgroundLocation
          ? { replace: true, state: { backgroundLocation, initialAsset: asset } }
          : { state: { initialAsset: asset } },
      );
    },
    [backgroundLocation, navigate, overlay, searchParams],
  );

  const goWheelStep = useCallback(
    (direction: 1 | -1, now = Date.now()) => {
      if (now - wheelLastStepAt.current < wheelStepCooldownMs) return;
      const base =
        wheelBase.current ??
        (activeNeighbors
          ? { next: activeNeighbors.next, offset: 0, previous: activeNeighbors.previous }
          : null);
      if (!base) return;

      const nextOffset = base.offset + direction;
      const target = nextOffset > 0 ? base.next[nextOffset - 1] : base.previous[Math.abs(nextOffset) - 1];
      if (!target) return;

      wheelLastStepAt.current = now;
      base.offset = nextOffset;
      wheelBase.current = base;
      goAsset(target);
      if (wheelResetTimer.current !== null) {
        window.clearTimeout(wheelResetTimer.current);
      }
      wheelResetTimer.current = window.setTimeout(() => {
        wheelBase.current = null;
        wheelResetTimer.current = null;
      }, 320);
    },
    [activeNeighbors, goAsset],
  );

  useEffect(() => {
    const element = viewerRef.current;
    if (!element) return;
    const handleWheel = (event: WheelEvent) => {
      if (event.cancelable) event.preventDefault();
      wheelDelta.current += event.deltaY;
      if (Math.abs(wheelDelta.current) < wheelStepThreshold) return;
      const direction = wheelDelta.current > 0 ? 1 : -1;
      wheelDelta.current = 0;
      goWheelStep(direction, Date.now());
    };
    element.addEventListener('wheel', handleWheel, { passive: false });
    return () => {
      element.removeEventListener('wheel', handleWheel);
    };
  }, [goWheelStep]);

  const leave = useCallback(() => {
    if (overlay) {
      navigate(-1);
      return;
    }
    void (async () => {
      const context = searchParams.get('context');
      const fallback =
        context === 'folder' ? '/folders' : context === 'album' ? '/albums' : context === 'search' ? '/search' : context === 'rating' ? '/ratings' : '/library';
      const returnPath = searchParams.get('returnPath');
      const targetPath = returnPathMatchesFallback(returnPath, fallback) ? returnPath! : fallback;
      const restoreState = await returnStateForCurrentAsset(searchParams, current?.id);
      if (restoreState) {
        navigate(appendRestoreParam(targetPath, restoreState), overlay ? { replace: true } : undefined);
        return;
      }
      const storageReturnPath = loadViewerReturnPath();
      navigate(returnPathMatchesFallback(storageReturnPath, fallback) ? storageReturnPath : fallback);
    })();
  }, [current?.id, navigate, overlay, searchParams]);

  const closeDeleteDialog = useCallback(() => {
    if (deleteSubmitting) return;
    setDeleteDialogOpen(false);
    setDeletePlan(null);
    setDeleteError(null);
  }, [deleteSubmitting]);

  const openDeleteDialog = useCallback(async () => {
    if (!current) return;
    setDeleteDialogOpen(true);
    setDeleteLoading(true);
    setDeleteSubmitting(false);
    setDeletePlan(null);
    setDeleteError(null);
    try {
      const plan = await api.assetDeletePlan(current.id);
      setDeletePlan(plan);
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : '读取删除范围失败');
    } finally {
      setDeleteLoading(false);
    }
  }, [current?.id]);

  const goAfterDelete = useCallback(
    (result: AssetDeleteResult) => {
      const deleted = new Set(result.deletedAssetIds);
      const target = activeNeighbors ? [...activeNeighbors.next, ...activeNeighbors.previous].find((asset) => !deleted.has(asset.id)) : undefined;
      if (target) {
        goAsset(target);
        return;
      }
      leave();
    },
    [activeNeighbors, goAsset, leave],
  );

  const confirmDeleteAsset = useCallback(async () => {
    if (!current || !deletePlan || deleteSubmitting) return;
    setDeleteSubmitting(true);
    setDeleteError(null);
    try {
      const result = await api.deleteAsset(current.id, deletePlan.token);
      if (result.stale && result.plan) {
        setDeletePlan(result.plan);
        setDeleteError('删除范围已变化，请重新确认');
        return;
      }
      if (result.failures.length > 0) {
        setDeleteError(`删除失败：${result.failures.map((failure) => failure.relPath).join('、')}`);
      }
      if (result.deletedAssetIds.includes(current.id)) {
        setDeleteDialogOpen(false);
        goAfterDelete(result);
      }
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : '删除失败');
    } finally {
      setDeleteSubmitting(false);
    }
  }, [current, deletePlan, deleteSubmitting, goAfterDelete]);

  const rotateCurrentAsset = useCallback(async () => {
    if (!current) return;
    const pref = await api.updateAssetPreferences(current.id, nextRotation(current.rotation));
    setNeighbors((value) => (value ? updateNeighborRotation(value, pref.assetId, pref.rotation) : value));
  }, [current]);

  const rateCurrentAsset = useCallback(async (rating: AssetRating) => {
    if (!current) return;
    const pref = await api.updateAssetRating(current.id, rating);
    const nextRating = normalizeAssetRating(pref.rating);
    emitAssetRatingChanged(pref.assetId, nextRating);
    setNeighbors((value) => {
      if (value) return updateNeighborRating(value, pref.assetId, nextRating);
      if (current.id === pref.assetId) {
        return { current: { ...current, rating: nextRating }, previous: [], next: [] };
      }
      return value;
    });
  }, [current]);

  const searchByNFOValue = useCallback(
    (field: NFOFilterField | 'nfo', value: string) => {
      const query = new URLSearchParams();
      query.set(searchParamForNFOField(field), value);
      navigate({ pathname: '/search', search: query.toString() });
    },
    [navigate],
  );

  const toggleFullscreen = useCallback(() => {
    const target = fullscreenTarget(viewerRef.current);
    if (document.fullscreenElement) {
      void document.exitFullscreen();
      return;
    }
    if (target) {
      void target.requestFullscreen();
    }
  }, []);

  useEffect(() => {
    function onFullscreenChange() {
      const target = fullscreenTarget(viewerRef.current);
      const fullscreenElement = document.fullscreenElement;
      setFullscreen(Boolean(fullscreenElement && target && (fullscreenElement === target || fullscreenElement.contains(target))));
    }
    document.addEventListener('fullscreenchange', onFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', onFullscreenChange);
  }, []);

  useKeyboard(
    useCallback(
      (event: KeyboardEvent) => {
        if (deleteDialogOpen) {
          if (event.key === 'Escape') closeDeleteDialog();
          return;
        }
        if (event.key === 'Escape') leave();
        if (event.key.toLowerCase() === 'f') toggleFullscreen();
        if (event.key === 'ArrowLeft' || event.key.toLowerCase() === 'a') goAsset(activeNeighbors?.previous[0]);
        if (event.key === 'ArrowRight' || event.key.toLowerCase() === 'd') goAsset(activeNeighbors?.next[0]);
      },
      [activeNeighbors, closeDeleteDialog, deleteDialogOpen, goAsset, leave, toggleFullscreen],
    ),
  );

  useSidebarPanel(
    'viewer',
    <ViewerSidebarPanel
      asset={current}
      error={error}
      sidecarError={sidecarError}
      sidecars={sidecars}
      videoProxyRuntime={videoProxyRuntime}
      onLeave={leave}
      onDelete={openDeleteDialog}
      onNFOSearch={searchByNFOValue}
      onRatingChange={(rating) => void rateCurrentAsset(rating)}
      deleting={deleteLoading || deleteSubmitting}
    />,
    [
      current?.id,
      current?.rating,
      current?.rotation,
      error,
      sidecarError,
      sidecars,
      videoProxyRuntime,
      leave,
      openDeleteDialog,
      searchByNFOValue,
      rateCurrentAsset,
      deleteLoading,
      deleteSubmitting,
    ],
  );

  useEffect(() => {
    if (overlay) return;
    restoreSidebarState(viewerReturnStateRef.current);
  }, [overlay, restoreSidebarState]);

  return (
    <>
      <section
        ref={viewerRef}
        className={overlay ? 'viewer-page viewer-overlay' : 'viewer-page'}
      >
        <div className="viewer-body">
          {current &&
            (current.mediaType === 'image' ? (
              <ImageViewer
                asset={current}
                fullscreen={fullscreen}
                onRotate={() => void rotateCurrentAsset()}
                onToggleFullscreen={toggleFullscreen}
              />
            ) : (
              <VideoViewer
                asset={current}
                fullscreen={fullscreen}
                playbackRate={playbackRate}
                selectedSubtitleId={selectedSubtitleId}
                subtitles={sidecars?.subtitles ?? []}
                subtitlesEnabled={subtitlesEnabled}
                onPlaybackRateChange={updatePlaybackRate}
                onRotate={() => void rotateCurrentAsset()}
                onSelectedSubtitleChange={updateSelectedSubtitle}
                onSubtitlesEnabledChange={updateSubtitlesEnabled}
                onToggleFullscreen={toggleFullscreen}
                onProxyRuntimeChange={setVideoProxyRuntime}
              />
            ))}
        </div>
      </section>
      {deleteDialogOpen && (
        <AssetDeleteDialog
          error={deleteError}
          loading={deleteLoading}
          plan={deletePlan}
          submitting={deleteSubmitting}
          onClose={closeDeleteDialog}
          onConfirm={() => void confirmDeleteAsset()}
        />
      )}
    </>
  );
}

function isAbortError(err: unknown) {
  return err instanceof DOMException && err.name === 'AbortError';
}

function fullscreenTarget(viewer: HTMLElement | null) {
  return viewer?.closest<HTMLElement>('.viewer-shell-overlay') ?? viewer;
}

function assetIdFromPath(pathname: string) {
  const match = pathname.match(/^\/viewer\/(\d+)/);
  return match?.[1] ?? '';
}

async function returnStateForCurrentAsset(searchParams: URLSearchParams, assetId: number | undefined) {
  const rawReturnState = searchParams.get('returnState');
  const baseState = decodeReturnState<Record<string, unknown> & Partial<GridReturnState>>(rawReturnState, {});
  if (!assetId) {
    return rawReturnState ? baseState : null;
  }
  try {
    const position = await api.assetPosition(assetId, assetPositionParams(searchParams));
    return {
      ...baseState,
      focusAssetId: assetId,
      loadedItemCount: viewerReturnPageSize,
      loadedStartIndex: Math.max(0, (position.page - 1) * viewerReturnPageSize),
      scrollRatio: position.position,
      scrollTop: 0,
    };
  } catch {
    return rawReturnState ? baseState : null;
  }
}

function assetPositionParams(searchParams: URLSearchParams) {
  const params: Record<string, string | number> = { pageSize: viewerReturnPageSize };
  searchParams.forEach((value, key) => {
    if (key === 'returnPath' || key === 'returnState') return;
    params[key] = value;
  });
  return params;
}

function returnPathMatchesFallback(returnPath: string | null, fallback: string) {
  if (!returnPath) return false;
  try {
    return new URL(returnPath, window.location.origin).pathname === fallback;
  } catch {
    return returnPath === fallback || returnPath.startsWith(`${fallback}?`);
  }
}

function appendRestoreParam(path: string, state: object) {
  const separator = path.includes('?') ? '&' : '?';
  return `${path}${separator}restore=${encodeReturnState(state)}`;
}

function ViewerSidebarPanel({
  asset,
  error,
  sidecarError,
  sidecars,
  videoProxyRuntime,
  onLeave,
  onDelete,
  onNFOSearch,
  onRatingChange,
  deleting,
}: {
  asset: Asset | undefined;
  error: string | null;
  sidecarError: string | null;
  sidecars: AssetSidecars | null;
  videoProxyRuntime: VideoProxyRuntime | null;
  onLeave: () => void;
  onDelete: () => void;
  onNFOSearch: (field: NFOFilterField | 'nfo', value: string) => void;
  onRatingChange: (rating: AssetRating) => void;
  deleting: boolean;
}) {
  const nfoFields = sidecars?.nfo?.fields ?? {};
  const nfoGroups = sidecars?.nfo?.groups?.filter((group) => group.items.length > 0) ?? [];
  const nfoFieldEntries = Object.entries(nfoFields).filter(([, value]) => value.trim() !== '');
  const showVideoProxyRuntime = asset?.mediaType === 'video' && asset.browserPlayable === false;
  return (
    <div className="sidebar-control-stack">
      <div className="sidebar-control-title">查看器</div>
      <div className="sidebar-viewer-actions">
        <button className="sidebar-square-button sidebar-viewer-leave-button" type="button" title="退出查看" onClick={onLeave}>
          <LogOut size={16} />
          <span>退出查看</span>
        </button>
        {asset && (
          <button
            className="sidebar-square-button sidebar-viewer-delete-button"
            type="button"
            title="删除媒体"
            disabled={deleting}
            onClick={onDelete}
          >
            <Trash2 size={16} />
          </button>
        )}
      </div>
      {error && <div className="sidebar-error">{error}</div>}
      {sidecarError && <div className="sidebar-error">{sidecarError}</div>}
      {asset && (
        <>
          <div className="sidebar-asset-info">
            <strong>{asset.filename}</strong>
            <span>{asset.relPath}</span>
            <div className="sidebar-info-chips">
              {assetInfoChips(asset).map((value) => (
                <span className="sidebar-info-chip" key={value}>
                  {value}
                </span>
              ))}
            </div>
          </div>
          {showVideoProxyRuntime && <SidebarVideoProxyRuntimePanel runtime={videoProxyRuntime} />}
          <div className="sidebar-control-title">星级</div>
          <RatingStars value={normalizeAssetRating(asset.rating)} onChange={onRatingChange} />
        </>
      )}
      {sidecars?.nfo && (
        <div className="sidebar-nfo">
          <div className="sidebar-nfo-header">
            <div className="sidebar-control-title">NFO</div>
            <small>{sidecars.nfo.filename}</small>
          </div>
          {nfoGroups.length > 0
            ? nfoGroups.map((group) => (
                <section className="sidebar-nfo-group" key={group.title}>
                  <div className="sidebar-nfo-group-title">{group.title}</div>
                  <div className="sidebar-nfo-items">
                    {group.items.map((item, index) => (
                      <NFOValue groupTitle={group.title} key={`${item.key}-${item.value}-${index}`} item={item} onSearch={onNFOSearch} />
                    ))}
                  </div>
                </section>
              ))
            : nfoFieldEntries.length > 0 && (
                <section className="sidebar-nfo-group">
                  <div className="sidebar-nfo-group-title">字段</div>
                  <div className="sidebar-nfo-items">
                    {nfoFieldEntries.map(([key, value]) => (
                      <NFOValue
                        key={key}
                        item={{ key, label: key, value, copyable: false }}
                        onSearch={onNFOSearch}
                      />
                    ))}
                  </div>
                </section>
              )}
          {nfoGroups.length === 0 && nfoFieldEntries.length === 0 && sidecars.nfo.text && <pre>{sidecars.nfo.text}</pre>}
        </div>
      )}
    </div>
  );
}

function AssetDeleteDialog({
  error,
  loading,
  plan,
  submitting,
  onClose,
  onConfirm,
}: {
  error: string | null;
  loading: boolean;
  plan: AssetDeletePlan | null;
  submitting: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const canConfirm = Boolean(plan?.canDelete) && !loading && !submitting;
  const files = plan?.mode === 'folder' ? plan.folderContents.filter((item) => item.kind !== 'folder') : plan?.files ?? [];
  const folders = plan?.mode === 'folder' ? [plan.folder, ...plan.folderContents.filter((item) => item.kind === 'folder')].filter((item): item is AssetDeleteEntry => Boolean(item)) : [];
  return (
    <div className="modal-backdrop" role="presentation">
      <div className="asset-delete-dialog" role="dialog" aria-modal="true" aria-label="确认删除媒体">
        <div className="modal-title">
          <span>确认删除媒体</span>
          <button type="button" title="关闭" disabled={submitting} onClick={onClose}>
            <X size={17} />
          </button>
        </div>
        <div className="asset-delete-content">
          {loading && <div className="muted-line">计算删除范围</div>}
          {error && <div className="sidebar-error">{error}</div>}
          {plan && (
            <>
              <div className="asset-delete-summary">
                <strong>{plan.mode === 'folder' ? '将删除媒体所在文件夹' : '将删除同名文件'}</strong>
                <span>{plan.asset.relPath}</span>
              </div>
              {plan.warnings.length > 0 && (
                <div className="asset-delete-message-list">
                  {plan.warnings.map((warning) => (
                    <span key={warning}>{warning}</span>
                  ))}
                </div>
              )}
              {plan.blockers.length > 0 && (
                <div className="asset-delete-message-list danger">
                  {plan.blockers.map((blocker) => (
                    <span key={blocker}>{blocker}</span>
                  ))}
                </div>
              )}
              {folders.length > 0 && (
                <DeleteSection title="将删除文件夹" items={folders} />
              )}
              <DeleteSection title="将删除文件" items={files} />
              {plan.mode === 'folder' && plan.folderContents.length > 0 && (
                <DeleteSection title="文件夹内全部内容" items={plan.folderContents} compact />
              )}
            </>
          )}
        </div>
        <div className="modal-actions">
          <span>{plan ? `${files.length} 个文件 / ${folders.length} 个文件夹` : ''}</span>
          <button className="text-button" type="button" disabled={submitting} onClick={onClose}>
            取消
          </button>
          <button className="command-button danger" type="button" disabled={!canConfirm} onClick={onConfirm}>
            <Check size={16} />
            {submitting ? '删除中' : '确认删除'}
          </button>
        </div>
      </div>
    </div>
  );
}

function DeleteSection({ title, items, compact = false }: { title: string; items: AssetDeleteEntry[]; compact?: boolean }) {
  if (items.length === 0) {
    return (
      <section className="asset-delete-section">
        <div className="asset-delete-section-title">{title}</div>
        <div className="muted-line">无</div>
      </section>
    );
  }
  return (
    <section className={compact ? 'asset-delete-section compact' : 'asset-delete-section'}>
      <div className="asset-delete-section-title">{title}</div>
      <div className="asset-delete-list">
        {items.map((item) => (
          <div className="asset-delete-row" key={`${item.kind}-${item.relPath}`}>
            <span>{item.relPath}</span>
            <small>{deleteKindLabel(item)} · {item.kind === 'folder' ? '文件夹' : formatBytes(item.size)} · {item.reason}</small>
          </div>
        ))}
      </div>
    </section>
  );
}

function deleteKindLabel(item: AssetDeleteEntry) {
  if (item.kind === 'folder') return '文件夹';
  if (item.kind === 'symlink') return '链接';
  return item.isMedia ? '媒体' : '文件';
}

function SidebarVideoProxyRuntimePanel({ runtime }: { runtime: VideoProxyRuntime | null }) {
  const progress = runtime ? Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100) : 0;
  const status = runtime ? videoProxyRuntimeLabel(runtime) : '准备转码';
  return (
    <div className="sidebar-video-proxy-runtime" aria-label="转码状态">
      <span>实时转码</span>
      <strong>{status}</strong>
      <div className="sidebar-video-proxy-runtime-bar" aria-label={`转码进度 ${progress}%`}>
        <div className="sidebar-video-proxy-runtime-fill" style={{ width: `${progress}%` }} />
      </div>
      <small>{runtime ? `${formatDuration(runtime.secondsDone || 0)} / ${formatDuration(runtime.duration || 0)}` : '0:00 / 0:00'}</small>
    </div>
  );
}

function videoProxyRuntimeLabel(runtime: VideoProxyRuntime) {
  if (runtime.status === 'cached' || runtime.cached) return '已缓存';
  if (runtime.status === 'error') return '转码失败';
  if (runtime.status === 'queued' || runtime.queued) return '等待转码槽位';
  if (runtime.transcoding) return `实时转码 ${Math.round(Math.min(1, Math.max(0, runtime.progress || 0)) * 100)}%`;
  return '准备转码';
}

function assetInfoChips(asset: Asset) {
  const chips = [asset.mediaType === 'image' ? '照片' : '视频', formatBytes(asset.size), formatDateTime(asset.timelineAt)];
  if (asset.width && asset.height) chips.push(`${asset.width} x ${asset.height}`);
  if (asset.duration !== null) chips.push(formatDuration(asset.duration));
  if (asset.mediaType === 'video') chips.push(`${asset.rotation || 0}°`);
  return chips;
}

function NFOValue({
  groupTitle,
  item,
  onSearch,
}: {
  groupTitle?: string;
  item: NFOField;
  onSearch: (field: NFOFilterField | 'nfo', value: string) => void;
}) {
  const content = (
    <>
      <span>{item.label}</span>
      {item.value}
    </>
  );
  const field = nfoSearchFieldForItem(item, groupTitle);
  const value = nfoSearchValue(field, item.value);
  return (
    <button className="sidebar-nfo-item searchable" type="button" title="搜索此项" onClick={() => onSearch(field, value)}>
      {content}
    </button>
  );
}

function searchParamForNFOField(field: NFOFilterField | 'nfo') {
  switch (field) {
    case 'actor':
      return 'nfoActor';
    case 'id':
      return 'nfoId';
    case 'tag':
      return 'nfoTag';
    case 'title':
      return 'nfoTitle';
    case 'year':
      return 'nfoYear';
    default:
      return 'nfo';
  }
}

function nfoSearchFieldForItem(item: NFOField, groupTitle = ''): NFOFilterField | 'nfo' {
  const key = item.key.trim().toLowerCase();
  const label = item.label.trim().toLowerCase();
  const group = groupTitle.trim().toLowerCase();
  if (group === '演员' || key === 'actor' || label === '演员') return 'actor';
  if (group === 'id' || key === 'uniqueid' || key.startsWith('uniqueid:') || label === 'id' || label === 'imdb' || label === 'tmdb') return 'id';
  if (group === '标记' || group === '类型' || key === 'tag' || key === 'genre' || label === '标签' || label === '类型') return 'tag';
  if (key === 'title' || key === 'originaltitle' || key === 'sorttitle' || label === '标题' || label === '原名' || label === '排序') return 'title';
  if (key === 'year' || label === '年份') return 'year';
  return 'nfo';
}

function nfoSearchValue(field: NFOFilterField | 'nfo', value: string) {
  const clean = value.trim();
  if (field === 'actor') {
    return clean.split(/\s+\/\s+/, 1)[0] || clean;
  }
  return clean;
}

function updateNeighborRotation(neighbors: Neighbors, assetId: number, rotation: number): Neighbors {
  const update = (asset: Asset) => (asset.id === assetId ? { ...asset, rotation } : asset);
  return {
    current: update(neighbors.current),
    previous: neighbors.previous.map(update),
    next: neighbors.next.map(update),
  };
}

function updateNeighborRating(neighbors: Neighbors, assetId: number, rating: AssetRating): Neighbors {
  const update = (asset: Asset) => (asset.id === assetId ? { ...asset, rating } : asset);
  return {
    current: update(neighbors.current),
    previous: neighbors.previous.map(update),
    next: neighbors.next.map(update),
  };
}

function preloadOrder(neighbors: Neighbors) {
  const order = [
    neighbors.current,
    neighbors.next[0],
    neighbors.next[1],
    neighbors.previous[0],
    neighbors.next[2],
    neighbors.previous[1],
    neighbors.previous[2],
  ];
  const seen = new Set<number>();
  return order.filter((asset): asset is Asset => {
    if (!asset || seen.has(asset.id)) return false;
    seen.add(asset.id);
    return true;
  });
}
