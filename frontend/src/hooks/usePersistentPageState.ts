import { useCallback, useEffect, useRef } from 'react';

export function usePersistentPageState(saveCurrentState: () => void, delay = 150) {
  const saveRef = useRef(saveCurrentState);
  const timerRef = useRef<number | null>(null);
  saveRef.current = saveCurrentState;

  const flushSave = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    saveRef.current();
  }, []);

  const scheduleSave = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
    }
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      saveRef.current();
    }, delay);
  }, [delay]);

  useEffect(() => {
    flushSave();
  }, [saveCurrentState, flushSave]);

  useEffect(() => {
    if (typeof window === 'undefined') return undefined;
    window.addEventListener('pagehide', flushSave);
    return () => window.removeEventListener('pagehide', flushSave);
  }, [flushSave]);

  useEffect(() => () => flushSave(), [flushSave]);

  return scheduleSave;
}
