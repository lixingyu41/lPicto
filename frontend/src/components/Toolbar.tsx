import type { ReactNode } from 'react';
import { RefreshCw } from 'lucide-react';
import { api } from '../api/client';

interface Props {
  title: string;
  children?: ReactNode;
  onScanStarted?: () => void;
  showScanAction?: boolean;
}

export default function Toolbar({ title, children, onScanStarted, showScanAction = true }: Props) {
  async function scan() {
    await api.triggerScan();
    onScanStarted?.();
  }

  return (
    <header className="toolbar">
      <h1>{title}</h1>
      <div className="toolbar-controls">{children}</div>
      {showScanAction && (
        <button className="icon-button command-button" type="button" onClick={() => void scan()} title="重新扫描">
          <RefreshCw size={17} />
          重新扫描
        </button>
      )}
    </header>
  );
}
