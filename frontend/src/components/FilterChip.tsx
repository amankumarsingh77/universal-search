interface FilterChipProps {
  label: string
  clauseKey: string
  onRemove: (clauseKey: string) => void
}

export function FilterChip({ label, clauseKey, onRemove }: FilterChipProps) {
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: '4px',
      padding: '2px 8px', borderRadius: '12px',
      background: 'rgba(255,255,255,0.15)', fontSize: '12px',
      color: 'rgba(255,255,255,0.9)', margin: '2px'
    }}>
      {label}
      <button
        onClick={() => onRemove(clauseKey)}
        style={{ background: 'none', border: 'none', cursor: 'pointer', padding: '0 2px', color: 'inherit', fontSize: '14px' }}
        aria-label={`Remove ${label} filter`}
      >×</button>
    </span>
  )
}
