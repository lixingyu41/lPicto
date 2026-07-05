import type { NFOFilterField } from '../types/api';

export const nfoFilterFields: Array<{ key: NFOFilterField; label: string; placeholder: string }> = [
  { key: 'actor', label: 'NFO 演员', placeholder: '选择或输入演员' },
  { key: 'id', label: 'NFO ID', placeholder: '选择或输入 ID' },
  { key: 'tag', label: 'NFO 标签/类型', placeholder: '选择或输入标签/类型' },
  { key: 'title', label: 'NFO 标题', placeholder: '选择或输入标题' },
  { key: 'year', label: 'NFO 年份', placeholder: '选择或输入年份' },
];

export const emptyNFOOptions: Record<NFOFilterField, string[]> = {
  actor: [],
  id: [],
  tag: [],
  title: [],
  year: [],
};

export interface NFOSearchFiltersProps {
  nfoQuery: string;
  nfoOptionQueries: Record<NFOFilterField, string>;
  nfoOptions: Record<NFOFilterField, string[]>;
  onNFOQueryChange: (value: string) => void;
  onNFOFieldQueryChange: (field: NFOFilterField, value: string) => void;
}

export default function NFOSearchFilters({
  nfoQuery,
  nfoOptionQueries,
  nfoOptions,
  onNFOQueryChange,
  onNFOFieldQueryChange,
}: NFOSearchFiltersProps) {
  return (
    <>
      <div className="sidebar-field-stack">
        {nfoFilterFields.map((field) => (
          <NFOFilterInput
            key={field.key}
            field={field.key}
            label={field.label}
            listID={`search-nfo-${field.key}`}
            onChange={onNFOFieldQueryChange}
            options={nfoOptions[field.key] ?? []}
            placeholder={field.placeholder}
            value={nfoOptionQueries[field.key]}
          />
        ))}
      </div>
      <label className="sidebar-field">
        <span>NFO 全文</span>
        <input value={nfoQuery} onChange={(event) => onNFOQueryChange(event.target.value)} placeholder="任意 NFO 文本" />
      </label>
    </>
  );
}

function NFOFilterInput({
  field,
  label,
  listID,
  onChange,
  options,
  placeholder,
  value,
}: {
  field: NFOFilterField;
  label: string;
  listID: string;
  onChange: (field: NFOFilterField, value: string) => void;
  options: string[];
  placeholder: string;
  value: string;
}) {
  return (
    <label className="sidebar-field">
      <span>{label}</span>
      <input list={listID} value={value} onChange={(event) => onChange(field, event.target.value)} placeholder={placeholder} />
      <datalist id={listID}>
        {(options ?? []).map((option) => (
          <option key={option} value={option} />
        ))}
      </datalist>
    </label>
  );
}
