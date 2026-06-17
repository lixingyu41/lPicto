import { useEffect, useState } from 'react';
import { Activity } from 'lucide-react';
import { api } from '../api/client';
import type { ScanStatus } from '../types/api';
import { formatDateTime } from '../utils/format';

export default function ScanStatusPanel() {
  const [status, setStatus] = useState<ScanStatus | null>(null);

  useEffect(() => {
    let live = true;
    async function load() {
      try {
        const result = await api.scanStatus();
        if (live) setStatus(result);
      } catch {
        if (live) setStatus(null);
      }
    }
    void load();
    const timer = window.setInterval(load, 5000);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, []);

  const lastRun = status?.lastRun;
  if (!status?.running) {
    return null;
  }
  return (
    <section className="scan-panel">
      <div className="scan-title">
        <Activity size={16} />
        扫描状态
      </div>
      <div className={status?.running ? 'status-dot running' : 'status-dot'} />
      <div className="scan-text">{status?.running ? '处理中' : '空闲'}</div>
      {lastRun && (
        <div className="scan-meta">
          <span>{formatDateTime(lastRun.startedAt)}</span>
          <span>
            新增 {lastRun.assetsAdded} / 更新 {lastRun.assetsUpdated} / 删除 {lastRun.assetsDeleted}
          </span>
        </div>
      )}
    </section>
  );
}
