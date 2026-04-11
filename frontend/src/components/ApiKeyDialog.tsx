import { useState, useEffect, useCallback } from 'react';
import { SetGeminiAPIKey, GetHasGeminiKey } from '../../wailsjs/go/main/App';

interface ApiKeyDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  onError: (msg: string) => void;
}

export default function ApiKeyDialog({ onClose, onSuccess, onError }: ApiKeyDialogProps) {
  const [key, setKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [hasExistingKey, setHasExistingKey] = useState(false);

  useEffect(() => {
    GetHasGeminiKey()
      .then((has) => setHasExistingKey(has))
      .catch(() => setHasExistingKey(false));
  }, []);

  const handleSubmit = async () => {
    if (key.trim() === '') {
      onError('API key must not be empty');
      return;
    }
    setLoading(true);
    onClose();
    try {
      await SetGeminiAPIKey(key);
      onSuccess();
    } catch (err) {
      onError(String(err));
    }
  };

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    },
    [onClose]
  );

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  return (
    <div style={styles.backdrop} onClick={onClose}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>Set Gemini API Key</span>
          <button style={styles.closeBtn} onClick={onClose}>&times;</button>
        </div>

        <div style={styles.body}>
          <label style={styles.label}>API Key</label>
          <input
            style={styles.input}
            type="password"
            autoComplete="off"
            placeholder={hasExistingKey ? '••••••••' : 'Enter Gemini API key'}
            value={key}
            onChange={(e) => setKey(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSubmit()}
            autoFocus
          />
          <span style={styles.hint}>Your key will be validated before saving.</span>
        </div>

        <div style={styles.footer}>
          <button style={styles.cancelBtn} onClick={onClose}>
            Cancel
          </button>
          <button style={styles.saveBtn} onClick={handleSubmit} disabled={loading}>
            {loading ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  backdrop: {
    position: 'fixed',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    background: 'rgba(0, 0, 0, 0.6)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 1000,
  },
  modal: {
    background: 'var(--bg-surface)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    width: '420px',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '12px 16px',
    borderBottom: '1px solid var(--border)',
  },
  title: {
    fontSize: '14px',
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  closeBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-secondary)',
    fontSize: '18px',
    cursor: 'pointer',
    padding: '0 4px',
    lineHeight: 1,
  },
  body: {
    padding: '16px',
    display: 'flex',
    flexDirection: 'column',
    gap: '8px',
  },
  label: {
    fontSize: '13px',
    fontWeight: 500,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
  },
  input: {
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm, 4px)',
    color: 'var(--text-primary)',
    fontSize: '13px',
    padding: '7px 10px',
    fontFamily: 'var(--font-mono)',
    outline: 'none',
    width: '100%',
  },
  hint: {
    fontSize: '11px',
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-sans)',
  },
  footer: {
    padding: '10px 16px',
    borderTop: '1px solid var(--border)',
    display: 'flex',
    justifyContent: 'flex-end',
    gap: '8px',
  },
  cancelBtn: {
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm, 4px)',
    color: 'var(--text-secondary)',
    fontSize: '12px',
    padding: '5px 14px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  saveBtn: {
    background: 'var(--accent-green)',
    border: 'none',
    borderRadius: 'var(--radius-sm, 4px)',
    color: '#fff',
    fontSize: '12px',
    padding: '5px 14px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
    fontWeight: 600,
  },
};
