import type { ProcessingProgress, ScanLibraryProgress, ScanStatus, WorkStatusCounts } from '../types/api';

export type ScanAction = 'count' | 'metadata' | 'thumbnails';

export function scanActionFromStatus(status: ScanStatus | null): ScanAction | null {
  if (!status?.running) return null;
  switch (status?.progress.task) {
    case 'count': return 'count';
    case 'metadata': return 'metadata';
    case 'thumb_continue':
    case 'thumb_rebuild': return 'thumbnails';
    default: return null;
  }
}

export function ScanTaskProgress({
  action,
  status,
  libraryProgress,
  processing,
  queued = false,
}: {
  action: ScanAction;
  status: ScanStatus | null;
  libraryProgress?: ScanLibraryProgress;
  processing?: ProcessingProgress | null;
  queued?: boolean;
}) {
  const live = status?.progress;
  const stopping = live?.phase === 'stopping' || live?.phase === 'pausing';
  const location = libraryProgress ? '' : currentLocation(live?.currentRoot, live?.currentRelPath);

  if (action === 'count') {
    return (
      <ProgressShell
        detail={stopping ? '正在结束文件清点' : location || '正在遍历图库目录，完成后更新文件总数'}
        indeterminate
        meta={live?.totalFiles ? `已统计 ${live.totalFiles.toLocaleString()} 个媒体文件` : '文件总数未知，完成前不显示百分比'}
        title={queued ? '文件清点等待启动' : stopping ? '文件清点正在停止' : '正在清点文件'}
      />
    );
  }

  if (action === 'metadata') {
    const discovered = libraryProgress?.discoveredFiles ?? Math.max(live?.discoveredFiles ?? 0, live?.totalFiles ?? 0);
    const scanned = libraryProgress?.scannedFiles ?? Math.max(live?.scannedFiles ?? 0, live?.totalSeen ?? 0);
    const discovering = queued || live?.phase === 'queued' || live?.phase === 'counting' || live?.phase === 'discovering';
    const total = Math.max(discovered, scanned);
    const percent = total > 0 ? Math.min(100, (scanned / total) * 100) : 0;
    return (
      <ProgressShell
        detail={location || (discovering ? '正在发现媒体文件' : '正在写入媒体信息')}
        indeterminate={discovering || stopping}
        meta={`已发现 ${discovered.toLocaleString()} · 已处理 ${scanned.toLocaleString()}${live?.errors ? ` · 失败 ${live.errors.toLocaleString()}` : ''}`}
        percent={percent}
        title={queued ? '媒体信息扫描等待启动' : stopping ? '媒体信息扫描正在停止' : discovering ? '正在发现媒体' : '正在提取媒体信息'}
      />
    );
  }

  const counts = libraryProgress?.thumb ?? processing?.thumb ?? emptyCounts;
  const queuedCount = processing?.queue.thumbQueued ?? 0;
  const activeCount = processing?.queue.activeThumb ?? 0;
  const required = Math.max(0, counts.total - counts.notRequired);
  const complete = Math.min(counts.ready, required);
  const percent = required > 0 ? Math.min(100, (complete / required) * 100) : 0;
  const preparing = queued || status?.running;
  const continuing = status?.progress.task === 'thumb_continue';
  return (
    <ProgressShell
      detail={preparing ? (continuing ? '正在补排尚未完成的缩略图任务' : '正在重置缩略图状态并创建后台任务') : `等待 ${counts.pending.toLocaleString()} · 处理中 ${counts.processing.toLocaleString()} · 队列 ${queuedCount.toLocaleString()} · 工作线程 ${activeCount.toLocaleString()}`}
      indeterminate={preparing && required === 0}
      meta={`${complete.toLocaleString()}/${required.toLocaleString()}${counts.error ? ` · 失败 ${counts.error.toLocaleString()}` : ''}`}
      percent={percent}
      title={preparing ? (continuing ? '正在继续缩略图任务' : '正在准备缩略图重建') : counts.pending + counts.processing > 0 ? '后台正在生成缩略图' : '缩略图任务完成'}
    />
  );
}

function ProgressShell({ detail, indeterminate = false, meta, percent = 0, title }: {
  detail: string;
  indeterminate?: boolean;
  meta: string;
  percent?: number;
  title: string;
}) {
  return (
    <div className="scan-task-progress" aria-live="polite">
      <div className="scan-task-progress-heading">
        <strong>{title}</strong>
        <span>{meta}</span>
      </div>
      <div className={indeterminate ? 'progress-bar indeterminate' : 'progress-bar'}>
        <div className="progress-fill" style={indeterminate ? undefined : { width: `${percent}%` }} />
      </div>
      <div className="scan-task-progress-detail" title={detail}>{detail}</div>
    </div>
  );
}

function currentLocation(root = '', relPath = '') {
  const value = relPath || root;
  return value ? `当前：/${value}` : '';
}

const emptyCounts: WorkStatusCounts = {
  error: 0,
  notRequired: 0,
  pending: 0,
  processing: 0,
  ready: 0,
  total: 0,
};
