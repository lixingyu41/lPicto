import { useEffect, useState } from 'react';
import type { CleanupStatus, MediaLibraryResetResult, ProcessingProgress, WorkStatusCounts } from '../types/api';
import { formatBytes } from '../utils/format';

const resetConfirmation = '彻底重置';

export function CacheManager({ cleanup, progress, onReset, onCleanup }: {
  cleanup: CleanupStatus | null;
  progress: ProcessingProgress | null;
  onReset: (confirmation: string) => Promise<MediaLibraryResetResult>;
  onCleanup?: () => Promise<void>;
}) {
  const [resetOpen, setResetOpen] = useState(false);
  const [confirmation, setConfirmation] = useState('');
  const [resetting, setResetting] = useState(false);
  const [resetError, setResetError] = useState<string | null>(null);
  const [resetResult, setResetResult] = useState<MediaLibraryResetResult | null>(null);
  const queue = progress?.queue;
  const waiting = (queue?.thumbQueued ?? 0) + (queue?.previewQueued ?? 0) +
    (queue?.videoPosterQueued ?? 0) + (queue?.videoProxyQueued ?? 0) + (queue?.videoQueued ?? 0);
  const working = (queue?.activeThumb ?? 0) + (queue?.activeTranscode ?? 0);
  const state = cleanup?.running ? '正在清理' : progress?.active ? '正在生成' : '空闲';
  const cachePending = !progress?.cache || (progress.cache.refreshing && progress.cache.updatedAt === 0);

  useEffect(() => {
    if (!resetOpen) return undefined;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !resetting) setResetOpen(false);
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [resetOpen, resetting]);

  const openReset = () => {
    setConfirmation('');
    setResetError(null);
    setResetOpen(true);
  };

  const confirmReset = async () => {
    if (confirmation.trim() !== resetConfirmation || resetting) return;
    setResetting(true);
    setResetError(null);
    try {
      const result = await onReset(confirmation.trim());
      setResetResult(result);
      setResetOpen(false);
      setConfirmation('');
    } catch (err) {
      setResetError(err instanceof Error ? err.message : '彻底重置失败');
    } finally {
      setResetting(false);
    }
  };

  return (
    <>
      <section className="settings-panel cache-overview-panel">
        <div className="settings-panel-title">缓存概览</div>
        <div className="muted-line cache-overview-intro">缩略图、高清预览、视频封面和播放代理均保存在 Ubuntu 本地缓存中。</div>
        <div className="metric-grid cache-summary-grid">
          <Metric label="总占用" value={cachePending ? '统计中' : formatBytes(progress.cache.sizeBytes)} />
          <Metric label="媒体缓存" value={cachePending ? '统计中' : formatBytes(progress.cache.cacheBytes)} />
          <Metric label="数据库" value={cachePending ? '统计中' : formatBytes(progress.cache.databaseBytes)} />
          <Metric label="缓存文件" value={cachePending ? '统计中' : progress.cache.fileCount.toLocaleString()} />
          <Metric label="缓存上限" value={cachePending ? '统计中' : formatBytes(progress.cache.maxBytes)} />
          <Metric label="磁盘保留" value={cachePending ? '统计中' : formatBytes(progress.cache.minFreeBytes)} />
          <Metric label="磁盘可用" value={cachePending ? '统计中' : formatBytes(progress.cache.freeBytes)} />
          <Metric label="可回收" value={cachePending ? '统计中' : formatBytes(progress.cache.reclaimableBytes)} />
          <Metric label="后台状态" value={state} />
          <Metric label="队列" value={`${waiting.toLocaleString()} 等待 · ${working.toLocaleString()} 处理中`} />
        </div>
        {!cachePending && (
          <div className="cache-kind-usage" aria-label="缓存分类占用">
            <Metric label="缩略图与封面" value={formatBytes((progress.cache.byKind.thumbs ?? 0) + (progress.cache.byKind['video-posters'] ?? 0))} />
            <Metric label="图片与预览" value={formatBytes((progress.cache.byKind.originals ?? 0) + (progress.cache.byKind.previews ?? 0))} />
            <Metric label="视频播放" value={formatBytes((progress.cache.byKind['video-chunks'] ?? 0) + (progress.cache.byKind['video-proxies'] ?? 0))} />
            <Metric label="音频播放" value={formatBytes((progress.cache.byKind['audio-chunks'] ?? 0) + (progress.cache.byKind['audio-proxies'] ?? 0))} />
            <Metric label="AI 暂存" value={formatBytes(progress.cache.byKind['ai-staging'] ?? 0)} />
          </div>
        )}
        {onCleanup && (
          <div className="cache-overview-actions">
            <button type="button" disabled={cleanup?.running} onClick={() => void onCleanup()}>
              {cleanup?.running ? '正在清理' : '清理无效缓存'}
            </button>
          </div>
        )}
        <div className="cache-work-list">
          <CacheWorkRow counts={progress?.thumb} label="缩略图" note="用于瀑布流和媒体列表" />
          <CacheWorkRow counts={progress?.preview} label="高清预览" note="用于浏览器无法直接显示的图片" />
          <CacheWorkRow counts={progress?.videoPoster} label="视频封面" note="用于视频未播放时的首帧画面" />
          <CacheWorkRow counts={progress?.videoProxy} label="视频代理" note="用于浏览器无法直接播放的视频" />
        </div>
      </section>
      <section className="settings-panel media-library-reset-panel">
        <div className="media-library-reset-heading">
          <div>
            <div className="settings-panel-title">彻底重置媒体库</div>
            <div className="muted-line">清空媒体数据库和全部派生缓存，保留图库路径、AI 模型与应用设置。</div>
          </div>
          <button className="command-button danger" type="button" onClick={openReset}>彻底重置媒体库</button>
        </div>
        {resetResult && (
          <div className="media-library-reset-result" role="status">
            已删除 {resetResult.deletedAssets.toLocaleString()} 项媒体记录、{resetResult.deletedFiles.toLocaleString()} 个缓存文件，释放 {formatBytes(resetResult.releasedBytes)}。
          </div>
        )}
      </section>
      {resetOpen && (
        <div className="modal-backdrop task-confirmation-backdrop" role="presentation" onMouseDown={(event) => {
          if (event.target === event.currentTarget && !resetting) setResetOpen(false);
        }}>
          <div aria-describedby="media-library-reset-description" aria-labelledby="media-library-reset-title" aria-modal="true" className="task-confirmation-dialog media-library-reset-dialog" role="dialog">
            <div className="modal-title" id="media-library-reset-title">确认彻底重置媒体库</div>
            <div className="task-confirmation-content" id="media-library-reset-description">
              <p>系统会先停止扫描、媒体处理、AI 分析和视频转码，再永久删除媒体索引、相册、智能集合、标签、任务记录及全部缓存文件。图库路径、AI 模型和应用设置会保留。</p>
              <label className="media-library-reset-confirmation">
                <span>输入“{resetConfirmation}”后继续</span>
                <input
                  autoFocus
                  disabled={resetting}
                  value={confirmation}
                  onChange={(event) => setConfirmation(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') void confirmReset();
                  }}
                />
              </label>
              {resetError && <div className="error-line">{resetError}</div>}
            </div>
            <div className="modal-actions">
              <button disabled={resetting} type="button" onClick={() => setResetOpen(false)}>取消</button>
              <button
                className="command-button danger"
                disabled={confirmation.trim() !== resetConfirmation || resetting}
                type="button"
                onClick={() => void confirmReset()}
              >
                {resetting ? '正在重置' : '彻底重置'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

function CacheWorkRow({ counts, label, note }: { counts?: WorkStatusCounts; label: string; note: string }) {
  const total = Math.max(0, (counts?.total ?? 0) - (counts?.notRequired ?? 0));
  const ready = Math.min(counts?.ready ?? 0, total);
  const pending = (counts?.pending ?? 0) + (counts?.processing ?? 0);
  const errors = counts?.error ?? 0;
  const percent = total > 0 ? Math.min(100, (ready / total) * 100) : 0;
  return (
    <div className="cache-work-row">
      <div className="cache-work-heading">
        <div><strong>{label}</strong><span>{note}</span></div>
        <span>{ready.toLocaleString()}/{total.toLocaleString()}</span>
      </div>
      <div className="progress-bar"><div className="progress-fill" style={{ width: `${percent}%` }} /></div>
      <div className="cache-work-meta">
        <span>{pending > 0 ? `待处理 ${pending.toLocaleString()}` : '没有待处理任务'}</span>
        {errors > 0 && <span className="cache-work-error">失败 {errors.toLocaleString()}</span>}
      </div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div className="metric"><span>{label}</span><strong>{value}</strong></div>;
}
