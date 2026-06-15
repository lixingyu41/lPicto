import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { RefreshCw, Square } from 'lucide-react';
import { api } from '../api/client';
import type { ScanStatus } from '../types/api';

interface Props {
  title: string;
  children?: ReactNode;
  onScanStarted?: () => void;
  showScanAction?: boolean;
}

export default function Toolbar({ title, children, onScanStarted, showScanAction = true }: Props) {
  const [scanStatus, setScanStatus] = useState<ScanStatus | null>(null);
  const [scanBusy, setScanBusy] = useState(false);

  const refreshScanStatus = useCallback(async () => {
    if (!showScanAction) return;
    try {
      setScanStatus(await api.scanStatus());
    } catch {
      setScanStatus(null);
    }
  }, [showScanAction]);

  useEffect(() => {
    if (!showScanAction) return;
    void refreshScanStatus();
    const timer = window.setInterval(() => void refreshScanStatus(), 2500);
    return () => window.clearInterval(timer);
  }, [refreshScanStatus, showScanAction]);

  async function scan() {
    if (scanBusy) return;
    setScanBusy(true);
    try {
      if (scanStatus?.running) {
        await api.pauseScan();
      } else {
        const result = await api.triggerScan();
        if (result.started) onScanStarted?.();
      }
      await refreshScanStatus();
    } finally {
      setScanBusy(false);
    }
  }

  const scanRunning = Boolean(scanStatus?.running);
  const scanTitle = scanRunning ? '停止当前扫描' : '重新扫描';

  return (
    <header className="toolbar">
      <h1>{title}</h1>
      <div className="toolbar-controls">{children}</div>
      {showScanAction && (
        <button className="icon-button command-button" disabled={scanBusy} type="button" onClick={() => void scan()} title={scanTitle}>
          {scanRunning ? <Square size={17} /> : <RefreshCw size={17} />}
          {scanBusy ? (scanRunning ? '停止中' : '启动中') : scanRunning ? '停止' : '重新扫描'}
        </button>
      )}
    </header>
  );
}
