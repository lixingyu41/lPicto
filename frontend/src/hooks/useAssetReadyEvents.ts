import { useEffect, useState, type DependencyList } from 'react';
import type { Asset, AssetDeletedEvent, ScanStatus } from '../types/api';

interface AssetEventHandlers {
  onAssetReady?: (asset: Asset) => void;
  onAssetDeleted?: (event: AssetDeletedEvent) => void;
}

export function useAssetReadyEvents(onAssetReady: (asset: Asset) => void, deps: DependencyList, onAssetDeleted?: (event: AssetDeletedEvent) => void) {
  return useAssetEvents({ onAssetReady, onAssetDeleted }, deps);
}

export function useAssetDeletedEvents(onAssetDeleted: (event: AssetDeletedEvent) => void, deps: DependencyList) {
  return useAssetEvents({ onAssetDeleted }, deps);
}

function useAssetEvents(handlers: AssetEventHandlers, deps: DependencyList) {
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
    source.addEventListener('scan_status', (event) => {
      if (closed) return;
      try {
        onScanStatus(JSON.parse((event as MessageEvent).data) as ScanStatus);
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
