import { useState } from 'react';

type Props = {
  label: string;
  onDismiss?: () => void;
};

export default function WarningChip({ label, onDismiss }: Props) {
  const [dismissed, setDismissed] = useState(false);

  if (dismissed) return null;

  const handleDismiss = () => {
    setDismissed(true);
    onDismiss?.();
  };

  return (
    <span style={styles.chip}>
      <span style={styles.label}>{label}</span>
      <button
        type="button"
        style={styles.close}
        onClick={handleDismiss}
        aria-label="Dismiss warning"
      >
        ×
      </button>
    </span>
  );
}

const styles: Record<string, React.CSSProperties> = {
  chip: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '2px 8px 2px 10px',
    borderRadius: 999,
    background: 'var(--surface-warning, rgba(255, 176, 32, 0.18))',
    border: '1px solid var(--border-warning, rgba(255, 176, 32, 0.4))',
    color: 'var(--text-primary, #fff)',
    fontSize: 12,
    fontWeight: 500,
  },
  label: {
    lineHeight: '18px',
  },
  close: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'inherit',
    padding: 0,
    lineHeight: 1,
    fontSize: 14,
    opacity: 0.7,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 16,
    height: 16,
  },
};
