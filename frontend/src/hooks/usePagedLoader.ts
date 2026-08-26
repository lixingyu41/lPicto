import { useCallback, useEffect, useRef, useState, type DependencyList } from 'react';
import type { Page } from '../types/api';

export function usePagedLoader<T>(
  loadPage: (page: number) => Promise<Page<T>>,
  deps: DependencyList,
) {
  const [items, setItems] = useState<T[]>([]);
  const [page, setPage] = useState(1);
  const [startPage, setStartPage] = useState(1);
  const [hasMore, setHasMore] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const loadingRef = useRef(false);
  const requestId = useRef(0);
  const responsePageSize = useRef(0);
  const loadPageRef = useRef(loadPage);

  useEffect(() => {
    loadPageRef.current = loadPage;
  }, [loadPage]);

  const load = useCallback(async (pageToLoad: number, replace: boolean, currentRequest: number) => {
    loadingRef.current = true;
    setLoading(true);
    setError(null);
    try {
      const result = await loadPageWithTransientRetry(
        () => loadPageRef.current(pageToLoad),
        () => requestId.current === currentRequest,
      );
      if (requestId.current !== currentRequest) return;
      assertPageContract(result, pageToLoad, responsePageSize);
      setItems((prev) => (replace ? result.items : [...prev, ...result.items]));
      setHasMore(result.hasMore);
      setPage(pageToLoad + 1);
      if (replace) {
        setStartPage(pageToLoad);
      }
    } catch (err) {
      if (requestId.current !== currentRequest) return;
      setError(err instanceof Error ? err.message : '加载失败');
      setHasMore(false);
    } finally {
      if (requestId.current === currentRequest) {
        loadingRef.current = false;
        setLoading(false);
      }
    }
  }, []);

  const loadMore = useCallback(async () => {
    if (loadingRef.current || loading || !hasMore) return;
    const currentRequest = requestId.current;
    await load(page, false, currentRequest);
  }, [hasMore, load, loading, page]);

  const loadPrevious = useCallback(async (beforePrepend?: (result: Page<T>) => void): Promise<Page<T> | null> => {
    if (loadingRef.current || loading || startPage <= 1) return null;
    const pageToLoad = startPage - 1;
    const currentRequest = requestId.current;
    loadingRef.current = true;
    setLoading(true);
    setError(null);
    try {
      const result = await loadPageWithTransientRetry(
        () => loadPageRef.current(pageToLoad),
        () => requestId.current === currentRequest,
      );
      if (requestId.current !== currentRequest) return null;
      assertPageContract(result, pageToLoad, responsePageSize);
      beforePrepend?.(result);
      setItems((prev) => [...result.items, ...prev]);
      setStartPage(pageToLoad);
      return result;
    } catch (err) {
      if (requestId.current !== currentRequest) return null;
      setError(err instanceof Error ? err.message : '加载失败');
      return null;
    } finally {
      if (requestId.current === currentRequest) {
        loadingRef.current = false;
        setLoading(false);
      }
    }
  }, [loading, startPage]);

  const jumpToPage = useCallback(
    async (pageToLoad: number) => {
      const currentRequest = requestId.current + 1;
      requestId.current = currentRequest;
      responsePageSize.current = 0;
      loadingRef.current = false;
      setItems([]);
      setPage(pageToLoad);
      setStartPage(Math.max(1, pageToLoad));
      setHasMore(true);
      setError(null);
      await load(Math.max(1, pageToLoad), true, currentRequest);
    },
    [load],
  );

  const reset = useCallback(() => {
    const currentRequest = requestId.current + 1;
    requestId.current = currentRequest;
    responsePageSize.current = 0;
    loadingRef.current = false;
    setItems([]);
    setPage(1);
    setStartPage(1);
    setHasMore(true);
    setError(null);
    void load(1, true, currentRequest);
  }, [load]);

  useEffect(() => {
    reset();
  }, [reset, ...deps]);

  const mutateItems = useCallback((updater: (items: T[]) => T[], nextHasMore?: boolean) => {
    setItems((prev) => updater(prev));
    if (typeof nextHasMore === 'boolean') {
      setHasMore(nextHasMore);
    }
  }, []);

  return { items, hasMore, hasPrevious: startPage > 1, loading, error, loadMore, loadPrevious, reset, jumpToPage, mutateItems };
}

async function loadPageWithTransientRetry<T>(loadPage: () => Promise<Page<T>>, isCurrent: () => boolean) {
  try {
    return await loadPage();
  } catch (error) {
    if (!isCurrent() || !isTransientLoadError(error)) throw error;
    await new Promise((resolve) => window.setTimeout(resolve, 300));
    if (!isCurrent()) throw error;
    return loadPage();
  }
}

function isTransientLoadError(error: unknown) {
  if (!(error instanceof Error)) return false;
  return error.message === '请求超时'
    || error.name === 'AbortError'
    || /Failed to fetch|NetworkError|网络连接失败/i.test(error.message);
}

function assertPageContract<T>(result: Page<T>, requestedPage: number, pageSizeRef: { current: number }) {
  if (result.page !== requestedPage) {
    throw new Error(`分页响应错位：请求第 ${requestedPage} 页，收到第 ${result.page} 页`);
  }
  if (result.pageSize < 1) {
    throw new Error('分页响应缺少有效的 pageSize');
  }
  if (pageSizeRef.current > 0 && result.pageSize !== pageSizeRef.current) {
    throw new Error(`分页大小发生变化：${pageSizeRef.current} → ${result.pageSize}`);
  }
  pageSizeRef.current = result.pageSize;
}
