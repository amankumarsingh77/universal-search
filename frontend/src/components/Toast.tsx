import { useEffect } from 'react';

interface ToastProps {
  message: string;
  type: 'success' | 'error';
  onDismiss: () => void;
}

export default function Toast({ message, type, onDismiss }: ToastProps) {
  useEffect(() => {
    const timer = setTimeout(onDismiss, 4000);
    return () => clearTimeout(timer);
  }, [onDismiss]);

  return (
    <div style={type === 'success' ? styles.successToast : styles.errorToast}>
      <span style={styles.indicator}>
        {type === 'success' ? '✓' : '✗'}
      </span>
      <span style={styles.message}>{message}</span>
    </div>
  );
}

const baseToast: React.CSSProperties = {
  position: 'fixed',
  bottom: 24,
  right: 24,
  zIndex: 2000,
  minWidth: 280,
  padding: '12px 16px',
  borderRadius: 'var(--radius-md)',
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  color: 'var(--text-primary)',
  fontSize: 13,
  fontFamily: 'var(--font-sans)',
};

const styles: Record<string, React.CSSProperties> = {
  successToast: {
    ...baseToast,
    background: 'rgba(16, 185, 129, 0.12)',
    border: '1px solid var(--accent-green)',
  },
  errorToast: {
    ...baseToast,
    background: 'rgba(229, 57, 53, 0.12)',
    border: '1px solid #e53935',
  },
  indicator: {
    fontWeight: 600,
    flexShrink: 0,
  },
  message: {
    flex: 1,
  },
};
