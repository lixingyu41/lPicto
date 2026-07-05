import { Check, X } from 'lucide-react';
import type { AssetDeleteEntry, AssetDeletePlan } from '../types/api';
import { formatBytes } from '../utils/format';

interface AssetDeleteDialogProps {
  error: string | null;
  loading: boolean;
  plan: AssetDeletePlan | null;
  submitting: boolean;
  onClose: () => void;
  onConfirm: () => void;
}

export default function AssetDeleteDialog({
  error,
  loading,
  plan,
  submitting,
  onClose,
  onConfirm,
}: AssetDeleteDialogProps) {
  const canConfirm = Boolean(plan?.canDelete) && !loading && !submitting;
  const files = plan?.mode === 'folder' ? plan.folderContents.filter((item) => item.kind !== 'folder') : plan?.files ?? [];
  const folders = plan?.mode === 'folder' ? [plan.folder, ...plan.folderContents.filter((item) => item.kind === 'folder')].filter((item): item is AssetDeleteEntry => Boolean(item)) : [];
  return (
    <div className="modal-backdrop" role="presentation">
      <div className="asset-delete-dialog" role="dialog" aria-modal="true" aria-label="确认删除媒体">
        <div className="modal-title">
          <span>确认删除媒体</span>
          <button type="button" title="关闭" disabled={submitting} onClick={onClose}>
            <X size={17} />
          </button>
        </div>
        <div className="asset-delete-content">
          {loading && <div className="muted-line">计算删除范围</div>}
          {error && <div className="sidebar-error">{error}</div>}
          {plan && (
            <>
              <div className="asset-delete-summary">
                <strong>{plan.mode === 'folder' ? '将删除媒体所在文件夹' : '将删除同名文件'}</strong>
                <span>{plan.asset.relPath}</span>
              </div>
              {plan.warnings.length > 0 && (
                <div className="asset-delete-message-list">
                  {plan.warnings.map((warning) => (
                    <span key={warning}>{warning}</span>
                  ))}
                </div>
              )}
              {plan.blockers.length > 0 && (
                <div className="asset-delete-message-list danger">
                  {plan.blockers.map((blocker) => (
                    <span key={blocker}>{blocker}</span>
                  ))}
                </div>
              )}
              {folders.length > 0 && (
                <DeleteSection title="将删除文件夹" items={folders} />
              )}
              <DeleteSection title="将删除文件" items={files} />
              {plan.mode === 'folder' && plan.folderContents.length > 0 && (
                <DeleteSection title="文件夹内全部内容" items={plan.folderContents} compact />
              )}
            </>
          )}
        </div>
        <div className="modal-actions">
          <span>{plan ? `${files.length} 个文件 / ${folders.length} 个文件夹` : ''}</span>
          <button className="text-button" type="button" disabled={submitting} onClick={onClose}>
            取消
          </button>
          <button className="command-button danger" type="button" disabled={!canConfirm} onClick={onConfirm}>
            <Check size={16} />
            {submitting ? '删除中' : '确认删除'}
          </button>
        </div>
      </div>
    </div>
  );
}

function DeleteSection({ title, items, compact = false }: { title: string; items: AssetDeleteEntry[]; compact?: boolean }) {
  if (items.length === 0) {
    return (
      <section className="asset-delete-section">
        <div className="asset-delete-section-title">{title}</div>
        <div className="muted-line">无</div>
      </section>
    );
  }
  return (
    <section className={compact ? 'asset-delete-section compact' : 'asset-delete-section'}>
      <div className="asset-delete-section-title">{title}</div>
      <div className="asset-delete-list">
        {items.map((item) => (
          <div className="asset-delete-row" key={`${item.kind}-${item.relPath}`}>
            <span>{item.relPath}</span>
            <small>{deleteKindLabel(item)} · {item.kind === 'folder' ? '文件夹' : formatBytes(item.size)} · {item.reason}</small>
          </div>
        ))}
      </div>
    </section>
  );
}

function deleteKindLabel(item: AssetDeleteEntry) {
  if (item.kind === 'folder') return '文件夹';
  if (item.kind === 'symlink') return '链接';
  return item.isMedia ? '媒体' : '文件';
}
