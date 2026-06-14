import { useCallback, useState } from 'react';

interface Point {
  x: number;
  y: number;
}

export function useImageTransform() {
  const [scale, setScale] = useState(1);
  const [offset, setOffset] = useState<Point>({ x: 0, y: 0 });
  const [dragStart, setDragStart] = useState<Point | null>(null);

  const reset = useCallback(() => {
    setScale(1);
    setOffset({ x: 0, y: 0 });
    setDragStart(null);
  }, []);

  const zoomBy = useCallback((delta: number) => {
    setScale((prev) => {
      const next = Math.min(6, Math.max(1, Number((prev + delta).toFixed(2))));
      if (next === 1) setOffset({ x: 0, y: 0 });
      return next;
    });
  }, []);

  const toggleZoom = useCallback(() => {
    setScale((prev) => {
      if (prev > 1) {
        setOffset({ x: 0, y: 0 });
        return 1;
      }
      return 2.5;
    });
  }, []);

  const startDrag = useCallback((point: Point) => {
    setDragStart(point);
  }, []);

  const dragTo = useCallback(
    (point: Point) => {
      if (!dragStart || scale <= 1) return;
      setOffset((prev) => ({
        x: prev.x + point.x - dragStart.x,
        y: prev.y + point.y - dragStart.y,
      }));
      setDragStart(point);
    },
    [dragStart, scale],
  );

  const endDrag = useCallback(() => {
    setDragStart(null);
  }, []);

  return { scale, offset, reset, zoomBy, toggleZoom, startDrag, dragTo, endDrag, dragging: dragStart !== null };
}
