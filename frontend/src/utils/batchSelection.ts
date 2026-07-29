export type AssetGridBatchCommand =
  | 'toggle-selection'
  | 'auto-select'
  | 'select-all'
  | 'clear'
  | 'add-tag'
  | 'set-rating'
  | 'add-album'
  | 'rotate'
  | 'hide'
  | 'delete'
  | 'delete-records';

export interface AssetGridBatchState {
  available: boolean;
  busy: boolean;
  canAutoSelect: boolean;
  message: string;
  progress: { current: number; total: number } | null;
  selectedCount: number;
  selectionMode: boolean;
}

export const assetGridBatchCommandEvent = 'lpicto:asset-grid-batch-command';
export const assetGridBatchStateEvent = 'lpicto:asset-grid-batch-state';
export const assetGridBatchStateRequestEvent = 'lpicto:asset-grid-batch-state-request';

export function dispatchAssetGridBatchCommand(command: AssetGridBatchCommand) {
  window.dispatchEvent(new CustomEvent(assetGridBatchCommandEvent, { detail: { command } }));
}
