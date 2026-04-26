import { useState, useEffect } from 'react';
import { KeyRound, Eye, EyeOff, CheckCircle2, XCircle } from 'lucide-react';
import { SetGeminiAPIKey, GetHasGeminiKey } from '../../../wailsjs/go/app/App';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';

interface ApiKeyTabProps {
  onSuccess?: () => void;
  onError?: (msg: string) => void;
}

export function ApiKeyTab({ onSuccess, onError }: ApiKeyTabProps) {
  const [key, setKey] = useState('');
  const [showKey, setShowKey] = useState(false);
  const [loading, setLoading] = useState(false);
  const [hasKey, setHasKey] = useState(false);
  const [status, setStatus] = useState<'idle' | 'connected' | 'error'>('idle');
  const [statusMsg, setStatusMsg] = useState('');

  useEffect(() => {
    GetHasGeminiKey()
      .then((has) => {
        setHasKey(has);
        if (has) setStatus('connected');
      })
      .catch(() => setHasKey(false));
  }, []);

  const handleSave = async () => {
    if (key.trim() === '') {
      setStatus('error');
      setStatusMsg('API key must not be empty');
      onError?.('API key must not be empty');
      return;
    }
    setLoading(true);
    setStatus('idle');
    try {
      await SetGeminiAPIKey(key);
      setHasKey(true);
      setStatus('connected');
      setStatusMsg('Last verified just now · 768-dim embeddings');
      onSuccess?.();
    } catch (err) {
      setStatus('error');
      setStatusMsg(String(err));
      onError?.(String(err));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div>
        <h2 style={styles.heading}>Gemini API Key</h2>
        <p style={styles.subtext}>
          Findo uses Gemini Embedding 2 to power semantic search. Your key is stored locally in the system keychain — never sent anywhere except Google.
        </p>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <label style={styles.label}>API key</label>
        <div style={styles.inputWrapper}>
          <KeyRound size={16} color="var(--text-tertiary)" style={{ flexShrink: 0 }} />
          <input
            type={showKey ? 'text' : 'password'}
            value={key}
            onChange={(e) => setKey(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSave()}
            placeholder={hasKey ? '••••••••••••••••••••••' : 'Enter Gemini API key'}
            style={styles.input}
            autoComplete="off"
            spellCheck={false}
          />
          <button
            onClick={() => setShowKey(!showKey)}
            style={styles.eyeBtn}
            title={showKey ? 'Hide key' : 'Show key'}
          >
            {showKey ? <EyeOff size={16} /> : <Eye size={16} />}
          </button>
        </div>
      </div>

      {status === 'connected' && (
        <div style={styles.statusConnected}>
          <CheckCircle2 size={18} color="var(--accent-green)" />
          <div>
            <div style={{ fontWeight: 600, color: 'var(--accent-green)', fontSize: 14 }}>Connected</div>
            <div style={{ fontSize: 13, color: 'var(--text-secondary)', marginTop: 2 }}>
              {statusMsg || 'Last verified · 768-dim embeddings'}
            </div>
          </div>
        </div>
      )}

      {status === 'error' && (
        <div style={styles.statusError}>
          <XCircle size={18} color="#e53935" />
          <div>
            <div style={{ fontWeight: 600, color: '#e53935', fontSize: 14 }}>Invalid key</div>
            <div style={{ fontSize: 13, color: 'var(--text-secondary)', marginTop: 2 }}>{statusMsg}</div>
          </div>
        </div>
      )}

      <div style={{ display: 'flex', gap: 10 }}>
        <button
          style={styles.primaryBtn}
          onClick={handleSave}
          disabled={loading}
        >
          {loading ? 'Verifying…' : 'Save & Verify'}
        </button>
        <button
          style={styles.secondaryBtn}
          onClick={() => BrowserOpenURL('https://aistudio.google.com/apikey')}
        >
          Get a key
        </button>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  heading: {
    fontSize: 22,
    fontWeight: 700,
    color: 'var(--text-primary)',
    margin: '0 0 8px',
    fontFamily: 'var(--font-sans)',
  },
  subtext: {
    fontSize: 14,
    color: 'var(--text-secondary)',
    margin: 0,
    lineHeight: 1.6,
    fontFamily: 'var(--font-sans)',
  },
  label: {
    fontSize: 13,
    color: 'var(--text-secondary)',
    fontWeight: 500,
    fontFamily: 'var(--font-sans)',
  },
  inputWrapper: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-lg, 12px)',
    padding: '10px 14px',
  },
  input: {
    flex: 1,
    background: 'transparent',
    border: 'none',
    outline: 'none',
    color: 'var(--text-primary)',
    fontSize: 14,
    fontFamily: 'var(--font-mono)',
  },
  eyeBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    padding: 0,
    flexShrink: 0,
  },
  statusConnected: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 12,
    background: 'rgba(16,185,129,0.08)',
    border: '1px solid rgba(16,185,129,0.2)',
    borderRadius: 'var(--radius-lg, 12px)',
    padding: '12px 16px',
  },
  statusError: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 12,
    background: 'rgba(229,57,53,0.08)',
    border: '1px solid rgba(229,57,53,0.2)',
    borderRadius: 'var(--radius-lg, 12px)',
    padding: '12px 16px',
  },
  primaryBtn: {
    background: 'var(--accent, #7c6fe0)',
    border: 'none',
    borderRadius: 'var(--radius-lg, 12px)',
    color: '#fff',
    fontSize: 14,
    fontWeight: 600,
    padding: '10px 20px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  secondaryBtn: {
    background: 'transparent',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-lg, 12px)',
    color: 'var(--text-secondary)',
    fontSize: 14,
    fontWeight: 500,
    padding: '10px 20px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
};
