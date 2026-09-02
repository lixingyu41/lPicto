import { useEffect, useState, type DependencyList } from 'react';
import type { Asset, AssetDeletedEvent, ScanStatus } from '../types/api';

export interface LibraryEventHandlers {
  onAssetReady?: (asset: Asset) => void;
  onAssetDeleted?: (event: AssetDeletedEvent) => void;
  onFolderTreeChanged?: () => void;
  onScanStatus?: (status: ScanStatus) => void;
}

export function useAssetReadyEvents(onAssetReady: (asset: Asset) => void, deps: DependencyList, onAssetDeleted?: (event: AssetDeletedEvent) => void) {
  return useLibraryEvents({ onAssetReady, onAssetDeleted }, deps);
}

export function useAssetDeletedEvents(onAssetDeleted: (event: AssetDeletedEvent) => void, deps: DependencyList) {
  return useLibraryEvents({ onAssetDeleted }, deps);
}

export function useLibraryEvents(handlers: LibraryEventHandlers, deps: DependencyList) {
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (typeof EventSource === 'undefined') {
      setConnected(false);
      return undefined;
    }
    let closed = false;
    const source = new EventSource('/api/events');
    source.addEventListener('open', () => {
      if (!closed) setConnected(true);
    });
    source.addEventListener('asset_ready', (event) => {
      if (closed || !handlers.onAssetReady) return;
      try {
        handlers.onAssetReady(JSON.parse((event as MessageEvent).data) as Asset);
      } catch {
        // Ignore malformed events from a stale connection.
      }
    });
    source.addEventListener('asset_deleted', (event) => {
      if (closed || !handlers.onAssetDeleted) return;
      try {
        handlers.onAssetDeleted(JSON.parse((event as MessageEvent).data) as AssetDeletedEvent);
      } catch {
        // Ignore malformed events from a stale connection.
      }
    });
    source.addEventListener('folder_tree_changed', () => {
      if (!closed) handlers.onFolderTreeChanged?.();
    });
    source.addEventListener('scan_status', (event) => {
      if (closed || !handlers.onScanStatus) return;
      try {
        handlers.onScanStatus(JSON.parse((event as MessageEvent).data) as ScanStatus);
      } catch {
        // Ignore malformed events from a stale connection.
      }
    });
    source.addEventListener('error', () => {
      if (!closed) setConnected(false);
    });
    return () => {
      closed = true;
      setConnected(false);
      source.close();
    };
  }, deps);

  return connected;
}

export function useScanStatusEvents(onScanStatus: (status: ScanStatus) => void, deps: DependencyList) {
  return useLibraryEvents({ onScanStatus }, deps);
}
