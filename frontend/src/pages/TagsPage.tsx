import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowLeft, GitMerge, Plus, RefreshCw, Trash2 } from 'lucide-react';
import { api } from '../api/client';
import { useSidebarPanel } from '../components/SidebarContext';
import type { TagSummary } from '../types/api';

export default function TagsPage() {
  const navigate = useNavigate();
  const [tags, setTags] = useState<TagSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createName, setCreateName] = useState('');
  const [mergeSources, setMergeSources] = useState('');
  const [mergeTarget, setMergeTarget] = useState('');
  const sortedTags = useMemo(() => [...tags].sort((a, b) => a.name.localeCompare(b.name, 'zh-Hans-CN')), [tags]);

  const loadTags = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await api.tags();
      setTags(result.items ?? []);
    } catch (err) {
      setTags([]);
      setError(err instanceof Error ? err.message : '标签加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadTags();
  }, [loadTags]);

  const createTag = useCallback(async () => {
    const name = createName.trim();
    if (!name) return;
    setError(null);
    try {
      const created = await api.createTag(name);
      setTags((current) => upsertTag(current, created));
      setCreateName('');
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建标签失败');
    }
  }, [createName]);

  const renameTag = useCallback(async (tag: TagSummary, nextTag: string) => {
    const clean = nextTag.trim();
    if (!clean || clean === tag.name) return;
    setError(null);
    try {
      const updated = await api.updateTag(tag.id, clean);
      setTags((current) => upsertTag(current.filter((item) => item.id !== tag.id), updated));
    } catch (err) {
      setError(err instanceof Error ? err.message : '重命名标签失败');
    }
  }, []);

  const deleteTag = useCallback(async (tag: TagSummary) => {
    if (!window.confirm(`删除标签「${tag.name}」？媒体文件不会被删除。`)) return;
    setError(null);
    try {
      await api.deleteTag(tag.id);
      setTags((current) => current.filter((item) => item.id !== tag.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除标签失败');
    }
  }, []);

  const mergeTags = useCallback(async () => {
    const sourceNames = mergeSources
      .split(',')
      .map((tag) => tag.trim())
      .filter(Boolean);
    const targetName = mergeTarget.trim();
    const target = tags.find((tag) => tag.name === targetName);
    const sourceIds = sourceNames
      .map((name) => tags.find((tag) => tag.name === name)?.id ?? 0)
      .filter((id) => id > 0);
    if (sourceIds.length === 0 || !target) return;
    setError(null);
    try {
      const result = await api.mergeTags(sourceIds, target.id, targetName);
      setTags((current) => upsertTag(current.filter((item) => !sourceIds.includes(item.id)), result));
      setMergeSources('');
      setMergeTarget('');
    } catch (err) {
      setError(err instanceof Error ? err.message : '合并标签失败');
    }
  }, [mergeSources, mergeTarget]);

  useSidebarPanel(
    'collections',
    <div className="sidebar-control-stack sidebar-tags-panel">
      <button className="sidebar-command" type="button" onClick={() => navigate('/collections')}>
        <ArrowLeft size={15} />
        <span>返回集合</span>
      </button>
      <button className="sidebar-command" disabled={loading} type="button" onClick={() => void loadTags()}>
        <RefreshCw size={15} />
        <span>{loading ? '刷新中' : '刷新标签'}</span>
      </button>
      <div className="sidebar-group-section">
        <div className="sidebar-control-subtitle">统计</div>
        <div className="sidebar-list-row">
          <span className="sidebar-list-marker" aria-hidden="true" />
          <span>标签数量</span>
          <small>{tags.length}</small>
        </div>
      </div>
    </div>,
    [loading, loadTags, navigate, tags.length],
  );

  return (
    <section className="page settings-page tags-page">
      <div className="settings-scroll">
        <div className="settings-layout">
          <div className="settings-content">
            {error && <div className="error-line">{error}</div>}
            <section className="settings-panel settings-section">
              <div className="settings-panel-title">标签管理</div>
              <div className="settings-grid">
                <label className="settings-field">
                  <span>新标签</span>
                  <input value={createName} onChange={(event) => setCreateName(event.target.value)} placeholder="标签名" />
                </label>
                <div className="settings-action-row">
                  <button className="settings-action" disabled={!createName.trim()} type="button" onClick={() => void createTag()}>
                    <Plus size={15} />
                    <span>创建</span>
                  </button>
                </div>
                <label className="settings-field">
                  <span>合并来源</span>
                  <input value={mergeSources} onChange={(event) => setMergeSources(event.target.value)} placeholder="用英文逗号分隔" />
                </label>
                <label className="settings-field">
                  <span>合并到</span>
                  <input value={mergeTarget} onChange={(event) => setMergeTarget(event.target.value)} placeholder="目标标签" />
                </label>
                <div className="settings-action-row">
                  <button className="settings-action" disabled={!mergeSources.trim() || !mergeTarget.trim()} type="button" onClick={() => void mergeTags()}>
                    <GitMerge size={15} />
                    <span>合并</span>
                  </button>
                </div>
              </div>
            </section>
            <section className="settings-panel settings-section">
              <div className="settings-panel-heading">
                <div className="settings-panel-title">全部标签</div>
                <button className="settings-action" disabled={loading} type="button" onClick={() => void loadTags()}>
                  <RefreshCw size={15} />
                  <span>{loading ? '刷新中' : '刷新'}</span>
                </button>
              </div>
              {sortedTags.length === 0 && !loading ? (
                <div className="empty-state">暂无标签</div>
              ) : (
                <div className="tag-management-list">
                  {sortedTags.map((tag) => (
                    <TagRow key={tag.id} tag={tag} onDelete={deleteTag} onRename={renameTag} />
                  ))}
                </div>
              )}
            </section>
          </div>
        </div>
      </div>
    </section>
  );
}

function TagRow({
  onDelete,
  onRename,
  tag,
}: {
  onDelete: (tag: TagSummary) => void;
  onRename: (tag: TagSummary, nextTag: string) => void;
  tag: TagSummary;
}) {
  const [name, setName] = useState(tag.name);

  useEffect(() => {
    setName(tag.name);
  }, [tag.name]);

  return (
    <div className="settings-check-row tag-management-row">
      <span className="tag-management-name">{tag.name}</span>
      <small>{tag.assetCount} 个媒体</small>
      <input value={name} onChange={(event) => setName(event.target.value)} aria-label={`重命名 ${tag.name}`} />
      <button className="settings-action" disabled={!name.trim() || name.trim() === tag.name} type="button" onClick={() => void onRename(tag, name)}>
        保存
      </button>
      <button className="settings-action danger" type="button" title="删除标签" aria-label={`删除 ${tag.name}`} onClick={() => void onDelete(tag)}>
        <Trash2 size={15} />
      </button>
    </div>
  );
}

function upsertTag(items: TagSummary[], tag: TagSummary) {
  const next = items.filter((item) => item.id !== tag.id);
  next.push(tag);
  return next;
}
