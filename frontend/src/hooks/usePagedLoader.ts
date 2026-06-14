import { useCallback, useEffect, useRef, useState, type DependencyList } from 'react';
import type { Page } from '../types/api';

export function usePagedLoader<T>(
  loadPage: (page: number) => Promise<Page<T>>,
  deps: DependencyList,
) {
  const [items, setItems] = useState<T[]>([]);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const requestId = useRef(0);

  const load = useCallback(async (pageToLoad: number, replace: boolean, currentRequest: number) => {
    setLoading(true);
    setError(null);
    try {
      const result = await loadPage(pageToLoad);
      if (requestId.current !== currentRequest) return;
      setItems((prev) => (replace ? result.items : [...prev, ...result.items]));
      setHasMore(result.hasMore);
      setPage(pageToLoad + 1);
    } catch (err) {
      if (requestId.current !== currentRequest) return;
      setError(err instanceof Error ? err.message : '加载失败');
    } finally {
      if (requestId.current === currentRequest) {
        setLoading(false);
      }
    }
  }, [loadPage]);

  const loadMore = useCallback(async () => {
    if (loading || !hasMore) return;
    const currentRequest = requestId.current;
    await load(page, false, currentRequest);
  }, [hasMore, load, loading, page]);

  const jumpToPage = useCallback(
    async (pageToLoad: number) => {
      const currentRequest = requestId.current + 1;
      requestId.current = currentRequest;
      setItems([]);
      setPage(pageToLoad);
      setHasMore(true);
      setError(null);
      await load(Math.max(1, pageToLoad), true, currentRequest);
    },
    [load],
  );

  const reset = useCallback(() => {
    const currentRequest = requestId.current + 1;
    requestId.current = currentRequest;
    setItems([]);
    setPage(1);
    setHasMore(true);
    setError(null);
    void load(1, true, currentRequest);
  }, [load]);

  useEffect(() => {
    reset();
  }, [reset, ...deps]);

  return { items, hasMore, loading, error, loadMore, reset, jumpToPage };
}
