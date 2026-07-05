import { Square } from 'lucide-react';
import type { ProcessingProgress, ScanStatus } from '../types/api';
import { formatBytes } from '../utils/format';

export interface CacheManagerProps {
  status: ScanStatus | null;
  statusLabel: string;
  scanRunning: boolean;
  stoppingScan: boolean;
  totalMedia: number;
  proxiedVideos: number;
  progress: ProcessingProgress | null;
  librariesCount: number;
  onStopScan: () => void;
  onGlobalScan: (action: 'count' | 'metadata' | 'thumbnails') => void;
}

export function CacheManager({
  status,
  statusLabel,
  scanRunning,
  stoppingScan,
  totalMedia,
  proxiedVideos,
  progress,
  librariesCount,
  onStopScan,
  onGlobalScan,
}: CacheManagerProps) {
  return (
    <div className="settings-panel">
      <div className="settings-panel-heading">
        <div className="settings-panel-title">总扫描</div>
        <button
          aria-label="停止当前扫描"
          className="command-button scan-stop-button"
          disabled={!scanRunning || stoppingScan}
          title={scanRunning ? '停止当前扫描' : '当前没有运行中的扫描'}
          type="button"
          onClick={onStopScan}
        >
          <Square size={14} />
          {stoppingScan ? '停止中' : '停止'}
        </button>
      </div>
      <div className="metric-grid scan-summary-grid">
        <Metric label="状态" value={statusLabel} />
        <Metric label="已建缩略图" value={String(totalMedia)} />
        <Metric label="已代理视频" value={String(proxiedVideos)} />
        <Metric label="缓存" value={cacheSizeLabel(progress)} />
        <Metric label="图库个数" value={String(librariesCount)} />
      </div>
      <div className="selected-folder-actions scan-action-row">
        <button className="command-button" disabled={scanRunning || stoppingScan} type="button" onClick={() => onGlobalScan('count')}>
          文件数
        </button>
        <button className="command-button" disabled={scanRunning || stoppingScan} type="button" onClick={() => onGlobalScan('metadata')}>
          媒体信息
        </button>
        <button className="command-button" disabled={scanRunning || stoppingScan} type="button" onClick={() => onGlobalScan('thumbnails')}>
          缩略图重建
        </button>
      </div>
    </div>
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

function cacheSizeLabel(progress: ProcessingProgress | null) {
  if (!progress?.cache) return '0 B';
  if (progress.cache.refreshing && progress.cache.updatedAt === 0) return '统计中';
  return formatBytes(progress.cache.sizeBytes);
}
