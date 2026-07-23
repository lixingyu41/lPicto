import type { CleanupStatus, ProcessingProgress, WorkStatusCounts } from '../types/api';
import { formatBytes } from '../utils/format';

export function CacheManager({ cleanup, progress }: {
  cleanup: CleanupStatus | null;
  progress: ProcessingProgress | null;
}) {
  const queue = progress?.queue;
  const waiting = (queue?.thumbQueued ?? 0) + (queue?.previewQueued ?? 0) +
    (queue?.videoPosterQueued ?? 0) + (queue?.videoProxyQueued ?? 0) + (queue?.videoQueued ?? 0);
  const working = (queue?.activeThumb ?? 0) + (queue?.activeTranscode ?? 0);
  const state = cleanup?.running ? '正在清理' : progress?.active ? '正在生成' : '空闲';
  const cachePending = !progress?.cache || (progress.cache.refreshing && progress.cache.updatedAt === 0);

  return (
    <section className="settings-panel cache-overview-panel">
      <div className="settings-panel-title">缓存概览</div>
      <div className="muted-line cache-overview-intro">缩略图、高清预览、视频封面和播放代理均保存在 Ubuntu 本地缓存中。</div>
      <div className="metric-grid cache-summary-grid">
        <Metric label="总占用" value={cachePending ? '统计中' : formatBytes(progress.cache.sizeBytes)} />
        <Metric label="媒体缓存" value={cachePending ? '统计中' : formatBytes(progress.cache.cacheBytes)} />
        <Metric label="数据库" value={cachePending ? '统计中' : formatBytes(progress.cache.databaseBytes)} />
        <Metric label="缓存文件" value={cachePending ? '统计中' : progress.cache.fileCount.toLocaleString()} />
        <Metric label="后台状态" value={state} />
        <Metric label="队列" value={`${waiting.toLocaleString()} 等待 · ${working.toLocaleString()} 处理中`} />
      </div>
      <div className="cache-work-list">
        <CacheWorkRow counts={progress?.thumb} label="缩略图" note="用于瀑布流和媒体列表" />
        <CacheWorkRow counts={progress?.preview} label="高清预览" note="用于浏览器无法直接显示的图片" />
        <CacheWorkRow counts={progress?.videoPoster} label="视频封面" note="用于视频未播放时的首帧画面" />
        <CacheWorkRow counts={progress?.videoProxy} label="视频代理" note="用于浏览器无法直接播放的视频" />
      </div>
    </section>
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
