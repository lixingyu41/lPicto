import { Check, X } from 'lucide-react';
import type { Asset } from '../types/api';

interface AssetRecordDeleteDialogProps {
  asset: Asset;
  error: string | null;
  submitting: boolean;
  onClose: () => void;
  onConfirm: () => void;
}

export default function AssetRecordDeleteDialog({
  asset,
  error,
  submitting,
  onClose,
  onConfirm,
}: AssetRecordDeleteDialogProps) {
  return (
    <div className="modal-backdrop" role="presentation">
      <div className="asset-delete-dialog asset-record-delete-dialog" role="dialog" aria-modal="true" aria-label="确认删除媒体记录">
        <div className="modal-title">
          <span>确认删除记录</span>
          <button type="button" title="关闭" disabled={submitting} onClick={onClose}>
            <X size={17} />
          </button>
        </div>
        <div className="asset-delete-content">
          {error && <div className="sidebar-error">{error}</div>}
          <div className="asset-delete-summary">
            <strong>删除该媒体的全部应用数据</strong>
            <span>{asset.relPath}</span>
          </div>
          <div className="asset-delete-message-list danger">
            <span>数据库记录、AI 数据、缩略图、高清预览和视频转码缓存都会被永久清除。</span>
          </div>
          <div className="asset-delete-message-list">
            <span>源文件不会删除；以后重新扫描图库时，可以重新建立这条媒体记录。</span>
          </div>
        </div>
        <div className="modal-actions">
          <span />
          <button className="text-button" type="button" disabled={submitting} onClick={onClose}>取消</button>
          <button className="command-button danger" type="button" disabled={submitting} onClick={onConfirm}>
            <Check size={16} />
            {submitting ? '删除中' : '删除记录'}
          </button>
        </div>
      </div>
    </div>
  );
}
