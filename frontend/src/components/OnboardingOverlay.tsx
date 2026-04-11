import { useState, useEffect } from 'react';
import { GetHotkeyString } from '../../wailsjs/go/main/App';

interface Props {
  onDismiss: () => void;
}

export function OnboardingOverlay({ onDismiss }: Props) {
  const [hotkey, setHotkey] = useState('⌘⇧Space');

  useEffect(() => {
    GetHotkeyString().then(s => { if (s) setHotkey(s); }).catch(() => {});
  }, []);

  return (
    <div style={overlayStyle}>
      <div style={cardStyle}>
        <h2 style={{ margin: '0 0 12px', fontSize: '18px', fontWeight: 600 }}>
          Welcome to Universal Search
        </h2>
        <p style={{ margin: '0 0 8px', color: 'var(--text-secondary, #aaa)', fontSize: '14px' }}>
          Search your files instantly from anywhere.
        </p>
        <p style={{ margin: '0 0 24px', fontSize: '14px' }}>
          Press{' '}
          <kbd style={kbdStyle}>{hotkey}</kbd>
          {' '}anywhere to open or close this window.
        </p>
        <button onClick={onDismiss} style={btnStyle}>
          Got it
        </button>
      </div>
    </div>
  );
}

const overlayStyle: React.CSSProperties = {
  position: 'fixed',
  inset: 0,
  background: 'rgba(0,0,0,0.75)',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  zIndex: 1000,
};

const cardStyle: React.CSSProperties = {
  background: 'var(--bg-elevated, #1a1a1a)',
  border: '1px solid var(--border, #333)',
  borderRadius: '12px',
  padding: '32px',
  maxWidth: '360px',
  textAlign: 'center',
  color: 'var(--text-primary, #fff)',
};

const kbdStyle: React.CSSProperties = {
  display: 'inline-block',
  padding: '2px 8px',
  borderRadius: '6px',
  background: 'var(--bg-base, #0a0a0a)',
  border: '1px solid var(--border, #444)',
  fontFamily: 'monospace',
  fontSize: '14px',
};

const btnStyle: React.CSSProperties = {
  padding: '10px 28px',
  borderRadius: '8px',
  background: 'var(--accent, #6366f1)',
  color: '#fff',
  border: 'none',
  cursor: 'pointer',
  fontSize: '14px',
  fontWeight: 600,
};
