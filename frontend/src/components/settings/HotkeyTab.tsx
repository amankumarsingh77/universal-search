import { useState, useEffect, useCallback } from 'react';
import { Circle, AlertTriangle } from 'lucide-react';
import { GetHotkeyString, SetSetting } from '../../../wailsjs/go/app/App';

const SUGGESTED = [
  { label: '⌘ + Shift + Space', combo: 'cmd+shift+space', hint: 'Default' },
  { label: '⌥ + Space', combo: 'alt+space', hint: 'Raycast-style' },
  { label: '⌃ + Space', combo: 'ctrl+space', hint: 'Alfred-style' },
];

const DEFAULT_HOTKEY = 'cmd+shift+space';

function parseHotkeyDisplay(raw: string): string[] {
  // raw is already human-readable from GetHotkeyString e.g. "⌘⇧Space"
  // We'll just return it as a single chip for simplicity, or try to split
  if (!raw) return ['⌘', '⇧', 'Space'];
  // Split on known modifier symbols
  const chars: string[] = [];
  let remaining = raw;
  const modifiers = ['⌘', '⌃', '⌥', '⇧'];
  for (const m of modifiers) {
    if (remaining.includes(m)) {
      chars.push(m);
      remaining = remaining.replace(m, '');
    }
  }
  const key = remaining.trim();
  if (key) chars.push(key);
  return chars.length > 0 ? chars : [raw];
}

function buildComboString(e: KeyboardEvent): string {
  const parts: string[] = [];
  if (e.metaKey) parts.push('cmd');
  if (e.ctrlKey) parts.push('ctrl');
  if (e.shiftKey) parts.push('shift');
  if (e.altKey) parts.push('alt');

  const key = e.key.toLowerCase();
  if (!['meta', 'control', 'shift', 'alt'].includes(key)) {
    parts.push(key === ' ' ? 'space' : key);
  }
  return parts.join('+');
}

function buildDisplayFromCombo(combo: string): string[] {
  return combo.split('+').map((p) => {
    switch (p) {
      case 'cmd': return '⌘';
      case 'ctrl': return '⌃';
      case 'shift': return '⇧';
      case 'alt': return '⌥';
      case 'space': return 'Space';
      default: return p.toUpperCase();
    }
  });
}

interface HotkeyTabProps {
  onError?: (msg: string) => void;
}

export function HotkeyTab({ onError }: HotkeyTabProps = {}) {
  const [currentHotkey, setCurrentHotkey] = useState('');
  const [recording, setRecording] = useState(false);
  const [liveCombo, setLiveCombo] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    GetHotkeyString().then(setCurrentHotkey).catch(() => {});
  }, []);

  const isSpotlightConflict = currentHotkey.includes('⌘') && currentHotkey.includes('Space') && !currentHotkey.includes('⇧') && !currentHotkey.includes('⌥') && !currentHotkey.includes('⌃');

  const applyCombo = useCallback(async (combo: string) => {
    setSaving(true);
    try {
      await SetSetting('global_hotkey', combo);
      const updated = await GetHotkeyString();
      setCurrentHotkey(updated);
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : typeof err === 'string' ? err : 'Failed to update hotkey';
      console.error('Failed to set hotkey:', err);
      onError?.(msg);
    } finally {
      setSaving(false);
      setRecording(false);
      setLiveCombo('');
    }
  }, [onError]);

  useEffect(() => {
    if (!recording) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();

      if (e.key === 'Escape') {
        setRecording(false);
        setLiveCombo('');
        return;
      }

      const combo = buildComboString(e);
      // Only update if we have a non-modifier key
      const hasNonMod = !['meta', 'control', 'shift', 'alt'].includes(e.key.toLowerCase());
      if (hasNonMod && combo.includes('+')) {
        setLiveCombo(combo);
        // Auto-save after brief delay
        setTimeout(() => applyCombo(combo), 600);
      } else if (hasNonMod) {
        setLiveCombo(combo);
      }
    };

    window.addEventListener('keydown', handleKeyDown, true);
    return () => window.removeEventListener('keydown', handleKeyDown, true);
  }, [recording, applyCombo]);

  const displayChips = liveCombo
    ? buildDisplayFromCombo(liveCombo)
    : parseHotkeyDisplay(currentHotkey);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div>
        <h2 style={styles.heading}>Global Hotkey</h2>
        <p style={styles.subtext}>
          Press the combination of keys you want to use to open Findo from anywhere on your system.
        </p>
      </div>

      <div style={styles.captureCard}>
        <div style={styles.captureLabel}>Current shortcut</div>
        <div style={styles.keycapRow}>
          {displayChips.map((chip, i) => (
            <span key={i}>
              <span style={styles.keycap}>{chip}</span>
              {i < displayChips.length - 1 && (
                <span style={styles.plus}>+</span>
              )}
            </span>
          ))}
        </div>
        <div style={{ display: 'flex', gap: 10, marginTop: 16, justifyContent: 'center' }}>
          <button
            style={recording ? styles.recordingBtn : styles.primaryBtn}
            onClick={() => {
              setRecording(!recording);
              setLiveCombo('');
            }}
            disabled={saving}
          >
            <Circle size={14} fill={recording ? '#fff' : 'none'} />
            {recording ? 'Recording… (press keys)' : 'Record new shortcut'}
          </button>
          <button
            style={styles.secondaryBtn}
            onClick={() => applyCombo(DEFAULT_HOTKEY)}
            disabled={saving}
          >
            Reset to default
          </button>
        </div>
        {saved && <div style={styles.savedMsg}>Shortcut saved!</div>}
      </div>

      {isSpotlightConflict && (
        <div style={styles.conflictCard}>
          <AlertTriangle size={18} color="#F59E0B" style={{ flexShrink: 0 }} />
          <div>
            <div style={{ fontWeight: 600, color: '#F59E0B', fontSize: 14 }}>Possible conflict with Spotlight</div>
            <div style={{ fontSize: 13, color: '#A8865A', marginTop: 4 }}>
              macOS uses ⌘+Space for Spotlight by default. Findo will still register, but the system shortcut may take priority.
            </div>
          </div>
        </div>
      )}

      <div>
        <h3 style={styles.sectionLabel}>Suggested shortcuts</h3>
        <div style={styles.suggestedList}>
          {SUGGESTED.map((s) => (
            <button
              key={s.combo}
              style={styles.suggestedRow}
              onClick={() => applyCombo(s.combo)}
              disabled={saving}
            >
              <span style={styles.suggestedLabel}>{s.label}</span>
              <span style={styles.suggestedHint}>{s.hint}</span>
            </button>
          ))}
        </div>
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
  captureCard: {
    background: '#0F1014',
    border: '1px solid #1B1D22',
    borderRadius: 12,
    padding: '24px 20px',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
  },
  captureLabel: {
    fontSize: 13,
    color: '#6B6F76',
    marginBottom: 16,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  keycapRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    flexWrap: 'wrap' as const,
    justifyContent: 'center',
  },
  keycap: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: 44,
    height: 44,
    padding: '0 10px',
    background: '#1B1D22',
    border: '1px solid #2A2D35',
    borderRadius: 8,
    fontSize: 18,
    color: '#E6E8EC',
    fontFamily: 'var(--font-sans, system-ui)',
    fontWeight: 600,
    boxShadow: '0 2px 4px rgba(0,0,0,0.4)',
  },
  plus: {
    color: '#6B6F76',
    fontSize: 16,
    margin: '0 4px',
  },
  primaryBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    background: '#5865F2',
    border: 'none',
    borderRadius: 10,
    color: '#fff',
    fontSize: 14,
    fontWeight: 600,
    padding: '10px 18px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  recordingBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    background: '#E53935',
    border: 'none',
    borderRadius: 10,
    color: '#fff',
    fontSize: 14,
    fontWeight: 600,
    padding: '10px 18px',
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
    padding: '10px 18px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  savedMsg: {
    marginTop: 10,
    fontSize: 13,
    color: '#4ADE80',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  conflictCard: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 12,
    background: 'rgba(245,158,11,0.08)',
    border: '1px solid rgba(245,158,11,0.25)',
    borderRadius: 10,
    padding: '12px 16px',
  },
  sectionLabel: {
    fontSize: 14,
    fontWeight: 600,
    color: '#C8CDD4',
    margin: '0 0 10px',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  suggestedList: {
    border: '1px solid #1B1D22',
    borderRadius: 10,
    overflow: 'hidden',
  },
  suggestedRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    width: '100%',
    padding: '12px 16px',
    background: '#0F1014',
    border: 'none',
    borderBottom: '1px solid #1B1D22',
    cursor: 'pointer',
    textAlign: 'left' as const,
  },
  suggestedLabel: {
    fontSize: 14,
    color: '#E6E8EC',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  suggestedHint: {
    fontSize: 13,
    color: '#6B6F76',
    fontFamily: 'var(--font-sans, system-ui)',
  },
};
