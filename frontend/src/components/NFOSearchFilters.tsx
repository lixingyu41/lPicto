import type { NFOFilterField } from '../types/api';

export const nfoFilterFields: Array<{ key: NFOFilterField; label: string; placeholder: string }> = [
  { key: 'actor', label: '元数据 演员', placeholder: '输入演员' },
  { key: 'id', label: '元数据 ID', placeholder: '输入 ID' },
  { key: 'tag', label: '元数据 标签/类型', placeholder: '输入标签/类型' },
  { key: 'title', label: '元数据 标题', placeholder: '输入标题' },
  { key: 'year', label: '元数据 年份', placeholder: '输入年份' },
];

export interface NFOSearchFiltersProps {
  nfoQuery: string;
  nfoOptionQueries: Record<NFOFilterField, string>;
  onNFOQueryChange: (value: string) => void;
  onNFOFieldQueryChange: (field: NFOFilterField, value: string) => void;
}

export default function NFOSearchFilters({
  nfoQuery,
  nfoOptionQueries,
  onNFOQueryChange,
  onNFOFieldQueryChange,
}: NFOSearchFiltersProps) {
  return (
    <>
      {nfoFilterFields.map((field) => (
        <NFOFilterInput
          key={field.key}
          field={field.key}
          label={field.label}
          onChange={onNFOFieldQueryChange}
          placeholder={field.placeholder}
          value={nfoOptionQueries[field.key]}
        />
      ))}
      <label className="sidebar-field">
        <span>元数据全文</span>
        <input value={nfoQuery} onChange={(event) => onNFOQueryChange(event.target.value)} placeholder="任意元数据文本" />
      </label>
    </>
  );
}

function NFOFilterInput({
  field,
  label,
  onChange,
  placeholder,
  value,
}: {
  field: NFOFilterField;
  label: string;
  onChange: (field: NFOFilterField, value: string) => void;
  placeholder: string;
  value: string;
}) {
  return (
    <label className="sidebar-field">
      <span>{label}</span>
      <input value={value} onChange={(event) => onChange(field, event.target.value)} placeholder={placeholder} />
    </label>
  );
}
