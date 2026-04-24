import { useState, useEffect } from 'react';
import { labelForCode, descriptionForCode } from '../lib/errorLabels';

type Props = {
  code: string;
  retryAfterMs: number;
  onRetry: () => void;
};

export default function ErrorBanner({ code, retryAfterMs, onRetry }: Props) {
  const [secondsLeft, setSecondsLeft] = useState(() =>
    retryAfterMs > 0 ? Math.ceil(retryAfterMs / 1000) : 0
  );

  useEffect(() => {
    const initial = retryAfterMs > 0 ? Math.ceil(retryAfterMs / 1000) : 0;
    setSecondsLeft(initial);
    if (initial <= 0) return;

    const id = setInterval(() => {
      setSecondsLeft(prev => {
        if (prev <= 1) {
          clearInterval(id);
          return 0;
        }
        return prev - 1;
      });
    }, 1000);

    return () => clearInterval(id);
  }, [retryAfterMs]);

  const label = labelForCode(code, code);
  const description = descriptionForCode(code);
  const isDisabled = secondsLeft > 0;

  return (
    <div data-testid="error-banner" style={styles.banner}>
      <div style={styles.content}>
        <span style={styles.label}>{label}</span>
        {description && <span style={styles.description}>{description}</span>}
        {secondsLeft > 0 && (
          <span style={styles.countdown}>{secondsLeft}s</span>
        )}
      </div>
      <button
        type="button"
        style={{ ...styles.button, ...(isDisabled ? styles.buttonDisabled : {}) }}
        onClick={onRetry}
        disabled={isDisabled}
        aria-label="Retry"
      >
        Retry
      </button>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  banner: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
    padding: '10px 16px',
    margin: '8px 12px',
    borderRadius: 'var(--radius-md, 8px)',
    background: 'var(--surface-error, rgba(255, 80, 80, 0.12))',
    border: '1px solid var(--border-error, rgba(255, 80, 80, 0.4))',
    color: 'var(--text-primary, #fff)',
    fontSize: 13,
  },
  content: {
    flex: 1,
    display: 'flex',
    flexWrap: 'wrap',
    gap: '4px 8px',
    alignItems: 'center',
  },
  label: {
    fontWeight: 600,
  },
  description: {
    opacity: 0.8,
  },
  countdown: {
    fontVariantNumeric: 'tabular-nums',
    opacity: 0.7,
  },
  button: {
    padding: '6px 12px',
    borderRadius: 'var(--radius-sm, 6px)',
    background: 'var(--accent, #4f8ef7)',
    color: '#fff',
    border: 'none',
    cursor: 'pointer',
    fontSize: 12,
    fontWeight: 500,
    flexShrink: 0,
  },
  buttonDisabled: {
    opacity: 0.4,
    cursor: 'not-allowed',
  },
};
