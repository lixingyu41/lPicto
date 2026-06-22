import { useCallback, useEffect, useRef, useState } from 'react';
import type { LibraryAnchor } from '../types/api';
import {
  clearRestoreParamFromLocation,
  resetGridState,
  viewerOverlayAssetFocusChanged,
  viewerOverlayAssetFocusId,
  type GridReturnState,
} from '../utils/pageState';

interface UseWaterfallGridStateOptions {
  hasMore: boolean;
  initialState: GridReturnState;
  itemsLength: number;
  jumpToPage: (page: number) => Promise<void>;
  loading: boolean;
  loadMore: () => Promise<void>;
  pageSize: number;
  resetKey: string;
  restoreReady?: boolean;
  searchParams: URLSearchParams;
}

export function useWaterfallGridState({
  hasMore,
  initialState,
  itemsLength,
  jumpToPage,
  loading,
  loadMore,
  pageSize,
  resetKey,
  restoreReady = true,
  searchParams,
}: UseWaterfallGridStateOptions) {
  const initialStateRef = useRef(initialState);
  const [scrollTopTarget, setScrollTopTarget] = useState<{ scrollTop: number; signal: number } | undefined>(() =>
    initialStateRef.current.scrollTop > 0 && !initialStateRef.current.focusAssetId
      ? { scrollTop: initialStateRef.current.scrollTop, signal: 1 }
      : undefined,
  );
  const [scrollTarget, setScrollTarget] = useState<{ ratio: number; signal: number } | undefined>();
  const [scrollResetSignal, setScrollResetSignal] = useState(0);
  const [scrollRatio, setScrollRatio] = useState(0);
  const [loadedStartIndex, setLoadedStartIndex] = useState(0);
  const [focusAssetId, setFocusAssetId] = useState(initialStateRef.current.focusAssetId ?? null);
  const [, setGridUrlSignal] = useState(0);
  const gridStateRef = useRef<GridReturnState>(initialStateRef.current);
  const restoreRef = useRef({
    jumped: false,
    pending:
      initialStateRef.current.scrollTop > 0 ||
      initialStateRef.current.loadedItemCount > pageSize ||
      initialStateRef.current.loadedStartIndex > 0 ||
      Boolean(initialStateRef.current.focusAssetId),
    signal: 0,
  });
  const indexPageRef = useRef(1);
  const seekSignalRef = useRef(0);

  useEffect(() => {
    if (!searchParams.has('restore')) return;
    clearRestoreParamFromLocation();
  }, [searchParams]);

  useEffect(() => {
    const handleViewerFocus = (event: Event) => {
      const assetId = viewerOverlayAssetFocusId(event);
      if (assetId) {
        setFocusAssetId(assetId);
      }
    };
    window.addEventListener(viewerOverlayAssetFocusChanged, handleViewerFocus);
    return () => window.removeEventListener(viewerOverlayAssetFocusChanged, handleViewerFocus);
  }, []);

  useEffect(() => {
    indexPageRef.current = 1;
    setLoadedStartIndex(0);
    setScrollTarget(undefined);
    if (restoreRef.current.pending) return;
    gridStateRef.current = resetGridState();
    setFocusAssetId(null);
    setScrollResetSignal((value) => value + 1);
  }, [resetKey]);

  useEffect(() => {
    const restore = restoreRef.current;
    if (!restoreReady || !restore.pending || loading) return;
    const saved = initialStateRef.current;
    const startIndex = Math.max(0, saved.loadedStartIndex);
    if (startIndex > 0 && !restore.jumped) {
      restore.jumped = true;
      const page = Math.floor(startIndex / pageSize) + 1;
      indexPageRef.current = page;
      setLoadedStartIndex(startIndex);
      void jumpToPage(page);
      return;
    }
    const targetCount = Math.max(pageSize, saved.loadedItemCount);
    if (itemsLength < targetCount && hasMore) {
      void loadMore();
      return;
    }
    restore.pending = false;
    if (!saved.focusAssetId) {
      restore.signal += 1;
      setScrollTopTarget({ scrollTop: saved.scrollTop, signal: restore.signal });
    }
  }, [hasMore, itemsLength, jumpToPage, loadMore, loading, pageSize, restoreReady]);

  const getGridState = useCallback(
    (): GridReturnState => ({
      ...gridStateRef.current,
      focusAssetId: null,
      loadedItemCount: itemsLength,
      loadedStartIndex,
    }),
    [itemsLength, loadedStartIndex],
  );

  const handleGridScrollState = useCallback(
    (state: { ratio: number; scrollTop: number }) => {
      gridStateRef.current = {
        ...gridStateRef.current,
        focusAssetId: null,
        loadedItemCount: itemsLength,
        loadedStartIndex,
        scrollRatio: state.ratio,
        scrollTop: state.scrollTop,
      };
      setGridUrlSignal((value) => value + 1);
    },
    [itemsLength, loadedStartIndex],
  );

  const seekIndex = useCallback(
    (_anchor: LibraryAnchor, page: number, ratio: number) => {
      const signal = seekSignalRef.current + 1;
      seekSignalRef.current = signal;
      setScrollTarget({ ratio, signal });
      if (page === indexPageRef.current) return;
      indexPageRef.current = page;
      setLoadedStartIndex((Math.max(1, page) - 1) * pageSize);
      void jumpToPage(page).then(() => {
        if (seekSignalRef.current !== signal) return;
        const nextSignal = seekSignalRef.current + 1;
        seekSignalRef.current = nextSignal;
        setScrollTarget({ ratio, signal: nextSignal });
      });
    },
    [jumpToPage, pageSize],
  );

  return {
    focusAssetId,
    getGridState,
    handleGridScrollState,
    loadedStartIndex,
    scrollRatio,
    scrollResetSignal,
    scrollTarget,
    scrollTopTarget,
    seekIndex,
    setScrollRatio,
  };
}
