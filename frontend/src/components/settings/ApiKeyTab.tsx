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
          <KeyRound size={16} color="#6B6F76" style={{ flexShrink: 0 }} />
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
          <CheckCircle2 size={18} color="#4ADE80" />
          <div>
            <div style={{ fontWeight: 600, color: '#4ADE80', fontSize: 14 }}>Connected</div>
            <div style={{ fontSize: 13, color: '#8A8F98', marginTop: 2 }}>
              {statusMsg || 'Last verified · 768-dim embeddings'}
            </div>
          </div>
        </div>
      )}

      {status === 'error' && (
        <div style={styles.statusError}>
          <XCircle size={18} color="#F87171" />
          <div>
            <div style={{ fontWeight: 600, color: '#F87171', fontSize: 14 }}>Invalid key</div>
            <div style={{ fontSize: 13, color: '#8A8F98', marginTop: 2 }}>{statusMsg}</div>
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
    color: '#E6E8EC',
    margin: '0 0 8px',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  subtext: {
    fontSize: 14,
    color: '#8A8F98',
    margin: 0,
    lineHeight: 1.6,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  label: {
    fontSize: 13,
    color: '#C8CDD4',
    fontWeight: 500,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  inputWrapper: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    background: '#0F1014',
    border: '1px solid #23252B',
    borderRadius: 10,
    padding: '10px 14px',
  },
  input: {
    flex: 1,
    background: 'transparent',
    border: 'none',
    outline: 'none',
    color: '#E6E8EC',
    fontSize: 14,
    fontFamily: 'var(--font-mono, monospace)',
  },
  eyeBtn: {
    background: 'none',
    border: 'none',
    color: '#6B6F76',
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
    background: 'rgba(34,197,94,0.08)',
    border: '1px solid rgba(34,197,94,0.2)',
    borderRadius: 10,
    padding: '12px 16px',
  },
  statusError: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 12,
    background: 'rgba(248,113,113,0.08)',
    border: '1px solid rgba(248,113,113,0.2)',
    borderRadius: 10,
    padding: '12px 16px',
  },
  primaryBtn: {
    background: '#5865F2',
    border: 'none',
    borderRadius: 10,
    color: '#fff',
    fontSize: 14,
    fontWeight: 600,
    padding: '10px 20px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  secondaryBtn: {
    background: 'transparent',
    border: '1px solid #23252B',
    borderRadius: 10,
    color: '#C8CDD4',
    fontSize: 14,
    fontWeight: 500,
    padding: '10px 20px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans, system-ui)',
  },
};
