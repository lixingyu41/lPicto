export const waterfallPageSize = 100;
export const waterfallBasePrefetchScreens = 3;
export const waterfallFastPrefetchScreens = 5;
export const waterfallPreviousPrefetchScreens = 2;
export const waterfallThumbnailOverscanScreens = 2.5;
export const waterfallFastScrollPixelsPerMs = 1.2;
export const waterfallResizeSettleMs = 180;

export function waterfallOverscanRows(viewportHeight: number, rowHeight: number, gap: number) {
  const rowExtent = Math.max(1, rowHeight + gap);
  return Math.min(48, Math.max(8, Math.ceil((Math.max(0, viewportHeight) / rowExtent) * waterfallThumbnailOverscanScreens)));
}

export function waterfallPrefetchScreens(velocityPixelsPerMs: number) {
  return velocityPixelsPerMs >= waterfallFastScrollPixelsPerMs
    ? waterfallFastPrefetchScreens
    : waterfallBasePrefetchScreens;
}
