import type { Asset } from '../types/api';
import type { CSSProperties } from 'react';

export function normalizeRotation(value: number | null | undefined): 0 | 90 | 180 | 270 {
  const normalized = (((value ?? 0) % 360) + 360) % 360;
  if (normalized === 90 || normalized === 180 || normalized === 270) return normalized;
  return 0;
}

export function nextRotation(value: number | null | undefined): 0 | 90 | 180 | 270 {
  const current = normalizeRotation(value);
  if (current === 0) return 90;
  if (current === 90) return 180;
  if (current === 180) return 270;
  return 0;
}

export function rotationStyle(asset: Pick<Asset, 'rotation'>): CSSProperties | undefined {
  const rotation = normalizeRotation(asset.rotation);
  if (rotation === 0) return undefined;
  return { transform: `rotate(${rotation}deg)` };
}

export function isQuarterTurn(asset: Pick<Asset, 'rotation'>): boolean {
  const rotation = normalizeRotation(asset.rotation);
  return rotation === 90 || rotation === 270;
}

export function effectiveAspect(asset: Pick<Asset, 'width' | 'height' | 'mediaType' | 'rotation'>): number {
  const width = asset.width && asset.width > 0 ? asset.width : 0;
  const height = asset.height && asset.height > 0 ? asset.height : 0;
  if (width > 0 && height > 0) {
    return isQuarterTurn(asset) ? height / width : width / height;
  }
  return asset.mediaType === 'video' ? 16 / 9 : 1;
}

export interface BoxSize {
  height: number;
  width: number;
}

export function rotatedCoverStyle(asset: Pick<Asset, 'rotation'>, box: BoxSize): CSSProperties {
  const rotation = normalizeRotation(asset.rotation);
  const quarter = rotation === 90 || rotation === 270;
  const width = quarter ? box.height : box.width;
  const height = quarter ? box.width : box.height;
  return {
    height,
    left: (box.width - width) / 2,
    top: (box.height - height) / 2,
    transform: rotation === 0 ? undefined : `rotate(${rotation}deg)`,
    transformOrigin: 'center',
    width,
  };
}

export function rotatedContainStyle(
  asset: Pick<Asset, 'width' | 'height' | 'mediaType' | 'rotation'>,
  box: BoxSize,
): CSSProperties {
  const rotation = normalizeRotation(asset.rotation);
  if (box.width <= 0 || box.height <= 0) {
    return {
      height: '100%',
      left: 0,
      top: 0,
      transform: rotation === 0 ? undefined : `rotate(${rotation}deg)`,
      transformOrigin: 'center',
      width: '100%',
    };
  }

  const naturalWidth = Math.max(1, asset.width || box.width);
  const naturalHeight = Math.max(1, asset.height || box.height);
  const quarter = rotation === 90 || rotation === 270;
  const rotatedWidth = quarter ? naturalHeight : naturalWidth;
  const rotatedHeight = quarter ? naturalWidth : naturalHeight;
  const scale = Math.min(box.width / rotatedWidth, box.height / rotatedHeight);
  const visibleWidth = rotatedWidth * scale;
  const visibleHeight = rotatedHeight * scale;
  const width = quarter ? visibleHeight : visibleWidth;
  const height = quarter ? visibleWidth : visibleHeight;

  return {
    height,
    left: (box.width - width) / 2,
    top: (box.height - height) / 2,
    transform: rotation === 0 ? undefined : `rotate(${rotation}deg)`,
    transformOrigin: 'center',
    width,
  };
}
