import { useEffect, useState, type DependencyList } from 'react';
import type { Asset } from '../types/api';

export function useAssetReadyEvents(onAssetReady: (asset: Asset) => void, deps: DependencyList) {
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
      if (closed) return;
      try {
        onAssetReady(JSON.parse((event as MessageEvent).data) as Asset);
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
