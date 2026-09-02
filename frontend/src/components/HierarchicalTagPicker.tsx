import { useEffect, useMemo, useRef, useState } from 'react';
import { ListFilter, Search, X } from 'lucide-react';
import type { AITagTreeNode } from '../types/api';
import { aiTagTreeInvalidatedEvent, loadAITagTree, readAITagTreeFallback } from '../utils/aiTagTreeCache';

interface Props {
  selected: string[];
  onChange: (nodes: string[]) => void;
  compact?: boolean;
  inline?: boolean;
  searchVisible?: boolean;
}

const categoryOrder = ['people', 'action', 'shoes', 'socks', 'clothes', 'closeup'];
const subjectOrder = ['indoor', 'outdoor', 'count', 'posture', 'activity', 'shoes', 'socks', 'top', 'outerwear', 'dress', 'pants', 'sportswear', 'swimwear', 'hat', 'accessories', 'part', 'object', 'animal', 'nature', 'transport', 'food', 'weather', 'media', 'other'];
const dimensionOrder = ['type', 'color', 'style', 'state', 'place', 'count', 'part', 'activity', 'posture'];
const tagNameCollator = new Intl.Collator('zh-CN-u-co-pinyin', { numeric: true, sensitivity: 'base' });

function compareNodeLabels(a: AITagTreeNode, b: AITagTreeNode) {
  return tagNameCollator.compare(a.label, b.label) || a.id.localeCompare(b.id);
}

function stableNodeOrder(a: AITagTreeNode, b: AITagTreeNode) {
  const key = (node: AITagTreeNode) => {
    const parts = (node.id.split(':')[1] ?? '').split('.');
    return parts[parts.length - 1] ?? '';
  };
  const order = a.depth === 1 ? categoryOrder : a.depth === 2 ? subjectOrder : a.depth === 3 ? dimensionOrder : [];
  const ai = order.indexOf(key(a));
  const bi = order.indexOf(key(b));
  return (ai < 0 ? 999 : ai) - (bi < 0 ? 999 : bi) || compareNodeLabels(a, b);
}

export default function HierarchicalTagPicker({ selected, onChange, compact = false, inline = false, searchVisible = true }: Props) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(inline);
  const [query, setQuery] = useState('');
  const [nodes, setNodes] = useState<AITagTreeNode[]>(() => readAITagTreeFallback(selected) ?? []);
  const [draft, setDraft] = useState<string[]>(selected);
  const [activeRoot, setActiveRoot] = useState('');

  useEffect(() => {
    if (!open && !inline) return;
    let live = true;
    setDraft(selected);
    const cached = readAITagTreeFallback(selected);
    if (cached) setNodes(cached);
    const refresh = (force = false) => {
      void loadAITagTree(selected, force).then((tree) => { if (live) setNodes(tree); }).catch(() => undefined);
    };
    refresh();
    const handleInvalidated = () => refresh(true);
    window.addEventListener(aiTagTreeInvalidatedEvent, handleInvalidated);
    const close = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    if (!inline) document.addEventListener('pointerdown', close);
    return () => {
      live = false;
      window.removeEventListener(aiTagTreeInvalidatedEvent, handleInvalidated);
      if (!inline) document.removeEventListener('pointerdown', close);
    };
  }, [inline, open, selected]);

  const byId = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes]);
  const children = useMemo(() => {
    const map = new Map<string, AITagTreeNode[]>();
    for (const node of nodes) {
      const key = node.parentId ?? '';
      map.set(key, [...(map.get(key) ?? []), node]);
    }
    for (const values of map.values()) values.sort(stableNodeOrder);
    return map;
  }, [nodes]);
  const roots = useMemo(() => {
    const values = children.get('') ?? [];
    return [...values].sort((a, b) => {
      const ai = categoryOrder.indexOf(a.id.replace(/^ai:/, ''));
      const bi = categoryOrder.indexOf(b.id.replace(/^ai:/, ''));
      return (ai < 0 ? 999 : ai) - (bi < 0 ? 999 : bi) || b.count - a.count;
    });
  }, [children]);

  useEffect(() => {
    if (roots.length === 0) return;
    if (!roots.some((root) => root.id === activeRoot)) setActiveRoot(roots[0].id);
  }, [activeRoot, roots]);

  const isDescendant = (candidate: string, ancestor: string) => {
    let current = byId.get(candidate);
    while (current?.parentId) {
      if (current.parentId === ancestor) return true;
      current = byId.get(current.parentId);
    }
    return false;
  };
  const toggle = (node: AITagTreeNode) => {
    const next = draft.includes(node.id)
      ? draft.filter((id) => id !== node.id)
      : [...draft.filter((id) => !isDescendant(id, node.id) && !isDescendant(node.id, id)), node.id].slice(0, 32);
    setDraft(next);
    onChange(next);
  };
  const compactLabel = (id: string) => {
    const node = byId.get(id);
    if (!node) return id.replace(/^manual:/, '');
    const parent = node.parentId ? byId.get(node.parentId) : undefined;
    const subject = parent?.parentId ? byId.get(parent.parentId) : parent;
    if (!subject || subject.depth <= 1 || subject.label === node.label) return node.label;
    return `${subject.label} · ${node.label}`;
  };
  const selectedLabels = selected.map(compactLabel).filter(Boolean);
  const orderedDraft = useMemo(
    () => [...draft].sort((a, b) => tagNameCollator.compare(compactLabel(a), compactLabel(b)) || a.localeCompare(b)),
    [byId, draft],
  );

  const groups = useMemo(() => {
    const root = byId.get(activeRoot);
    if (!root) return [];
    const normalized = query.trim().toLocaleLowerCase();
    const subjects = children.get(root.id) ?? [];
    return subjects.map((subject) => {
      const dimensions = (children.get(subject.id) ?? []).map((dimension) => {
        const nested = children.get(dimension.id) ?? [];
        const values = (nested.length > 0 ? nested : [dimension])
          .filter((node) => !normalized || node.label.toLocaleLowerCase().includes(normalized))
          .sort(compareNodeLabels);
        return { dimension, values };
      }).filter((dimension) => dimension.values.length > 0 || !normalized);
      const values = Array.from(new Map(dimensions.flatMap((dimension) => dimension.values).map((node) => [node.id, node])).values())
        .sort(compareNodeLabels);
      return { subject, values };
    }).filter((group) => group.values.length > 0 || !normalized);
  }, [activeRoot, byId, children, query]);
  const manualValues = useMemo(() => {
    if (activeRoot !== 'manual') return [];
    const normalized = query.trim().toLocaleLowerCase();
    return [...(children.get('manual') ?? [])]
      .filter((node) => !normalized || node.label.toLocaleLowerCase().includes(normalized))
      .sort(compareNodeLabels);
  }, [activeRoot, children, query]);
  const nodeDisabled = (node: AITagTreeNode) => {
    if (draft.includes(node.id) || node.count > 0) return false;
    return !draft.some((id) => byId.get(id)?.facetKey === node.facetKey);
  };

  const picker = (
    <>
      {searchVisible && (
        <label className="compact-tag-search">
          <Search aria-hidden="true" size={14} />
          <input autoFocus value={query} placeholder="搜索当前分类的标签" onChange={(event) => setQuery(event.target.value)} />
        </label>
      )}
      <div className="compact-tag-selected">
        <span>已选</span>
        <div>
          {draft.length === 0 && <small>尚未选择</small>}
          {orderedDraft.map((id) => (
            <button key={id} type="button" onClick={() => {
              const next = draft.filter((value) => value !== id);
              setDraft(next);
              onChange(next);
            }}>
              {compactLabel(id)} <X size={10} />
            </button>
          ))}
        </div>
      </div>
      <div className="compact-tag-categories">
        {roots.map((root) => (
          <button className={activeRoot === root.id ? 'active' : ''} key={root.id} type="button" onClick={() => { setActiveRoot(root.id); setQuery(''); }}>
            {root.label}<small>{root.count}</small>
          </button>
        ))}
      </div>
      <div className="compact-tag-groups">
        {activeRoot === 'manual' ? (
          <section className="compact-tag-group compact-tag-manual-group">
            <div className="compact-tag-values">
              {manualValues.map((node) => (
                <button className={draft.includes(node.id) ? 'selected' : ''} disabled={nodeDisabled(node)} key={node.id} type="button" onClick={() => toggle(node)}>
                  {node.label}<small>{node.count}</small>
                </button>
              ))}
            </div>
            {manualValues.length === 0 && <span className="asset-tag-filter-empty">没有匹配自标</span>}
          </section>
        ) : groups.map(({ subject, values }) => (
          <section className="compact-tag-group" key={subject.id}>
            <div className="compact-tag-group-title">
              <strong>{subject.label}</strong>
              {children.get(subject.id)?.length ? (
                <button className={draft.includes(subject.id) ? 'selected' : ''} disabled={nodeDisabled(subject)} type="button" onClick={() => toggle(subject)}>
                  全部 <small>{subject.count}</small>
                </button>
              ) : null}
            </div>
            <div className="compact-tag-values">
              {values.map((node) => (
                <button className={draft.includes(node.id) ? 'selected' : ''} disabled={nodeDisabled(node)} key={node.id} type="button" onClick={() => toggle(node)}>
                  {node.label}<small>{node.count}</small>
                </button>
              ))}
            </div>
          </section>
        ))}
        {activeRoot !== 'manual' && groups.length === 0 && <span className="asset-tag-filter-empty">没有匹配标签</span>}
      </div>
      <div className="asset-tag-filter-actions">
        <button type="button" onClick={() => { setDraft([]); onChange([]); }}>清除全部</button>
      </div>
    </>
  );

  if (inline) return <div className="hierarchical-tag-picker inline" ref={rootRef}>{picker}</div>;
  return (
    <div className={compact ? 'hierarchical-tag-picker compact' : 'hierarchical-tag-picker'} ref={rootRef}>
      <button
        aria-expanded={open}
        className={selected.length > 0 ? 'asset-tag-filter-trigger active' : 'asset-tag-filter-trigger'}
        title={selectedLabels.length > 0 ? selectedLabels.join('、') : '筛选标签'}
        type="button"
        onClick={(event) => { event.stopPropagation(); setOpen((value) => !value); }}
      >
        <ListFilter size={13} />
        {selected.length > 0 && <span>{selected.length}</span>}
      </button>
      {open && <div className="asset-tag-filter-menu hierarchical" onClick={(event) => event.stopPropagation()}>{picker}</div>}
    </div>
  );
}

export function TagSelectionSummary({ nodes, onRemove }: { nodes: AITagTreeNode[]; onRemove?: (id: string) => void }) {
  return (
    <div className="hierarchical-tag-selection">
      {nodes.map((node) => (
        <span key={node.id}>{node.label}{onRemove && <button type="button" onClick={() => onRemove(node.id)}><X size={10} /></button>}</span>
      ))}
    </div>
  );
}
