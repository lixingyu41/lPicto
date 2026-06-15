export type GridRowHeightLevel = 'compact' | 'small' | 'medium' | 'large' | 'extra';

export const gridRowHeightChanged = 'lpicto-grid-row-height-changed';

const rowHeightLevelKey = 'lpicto.gridRowHeightLevel';
const rowHeights: Record<GridRowHeightLevel, number> = {
  compact: 128,
  small: 152,
  medium: 176,
  large: 216,
  extra: 264,
};

export function loadGridRowHeightLevel(): GridRowHeightLevel {
  const raw = localStorage.getItem(rowHeightLevelKey);
  if (raw === 'compact' || raw === 'small' || raw === 'large' || raw === 'extra') {
    return raw;
  }
  return 'medium';
}

export function saveGridRowHeightLevel(level: GridRowHeightLevel) {
  localStorage.setItem(rowHeightLevelKey, level);
  window.dispatchEvent(new Event(gridRowHeightChanged));
}

export function gridRowHeightForLevel(level: GridRowHeightLevel) {
  return rowHeights[level] ?? rowHeights.medium;
}
