import { useEffect } from 'react';
import { ArrowLeft, X, Folder, KeyRound, Keyboard, AlignJustify, Info } from 'lucide-react';
import { FoldersTab } from './settings/FoldersTab';
import { ApiKeyTab } from './settings/ApiKeyTab';
import { HotkeyTab } from './settings/HotkeyTab';
import { IndexingTab } from './settings/IndexingTab';
import { AboutTab } from './settings/AboutTab';

export type SettingsTab = 'folders' | 'api-key' | 'hotkey' | 'indexing' | 'about';

interface SettingsPanelProps {
  open: boolean;
  activeTab: SettingsTab;
  onTabChange: (tab: SettingsTab) => void;
  onClose: () => void;
  onSuccess?: (msg: string) => void;
  onError?: (msg: string) => void;
}

const NAV_ITEMS: { id: SettingsTab; label: string; icon: React.ReactNode }[] = [
  { id: 'folders', label: 'Folders', icon: <Folder size={17} /> },
  { id: 'api-key', label: 'API Key', icon: <KeyRound size={17} /> },
  { id: 'hotkey', label: 'Hotkey', icon: <Keyboard size={17} /> },
  { id: 'indexing', label: 'Indexing', icon: <AlignJustify size={17} /> },
  { id: 'about', label: 'About', icon: <Info size={17} /> },
];

const TAB_TITLES: Record<SettingsTab, string> = {
  folders: 'Folders',
  'api-key': 'API Key',
  hotkey: 'Hotkey',
  indexing: 'Indexing',
  about: 'About',
};

export function SettingsPanel({
  open,
  activeTab,
  onTabChange,
  onClose,
  onSuccess,
  onError,
}: SettingsPanelProps) {
  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown, true);
    return () => window.removeEventListener('keydown', handleKeyDown, true);
  }, [open, onClose]);

  useEffect(() => {
    if (!open) return;
    (window as unknown as Record<string, unknown>).__settingsPanelOpen = true;
    return () => {
      (window as unknown as Record<string, unknown>).__settingsPanelOpen = false;
    };
  }, [open]);

  if (!open) return null;

  return (
    <div style={styles.overlay}>
      {/* Header */}
      <div style={styles.header}>
        <button style={styles.backBtn} onClick={onClose} title="Back">
          <ArrowLeft size={18} />
        </button>
        <span style={styles.headerTitle}>{TAB_TITLES[activeTab]}</span>
        <button style={styles.closeBtn} onClick={onClose} title="Close settings">
          <X size={18} />
        </button>
      </div>

      <div style={styles.body}>
        {/* Left rail */}
        <nav style={styles.rail}>
          {NAV_ITEMS.map((item) => (
            <button
              key={item.id}
              onClick={() => onTabChange(item.id)}
              style={{
                ...styles.navItem,
                background: activeTab === item.id ? 'var(--bg-selected)' : 'transparent',
                color: activeTab === item.id ? 'var(--text-primary)' : 'var(--text-secondary)',
              }}
            >
              <span style={{ color: activeTab === item.id ? 'var(--text-primary)' : 'var(--text-tertiary)' }}>
                {item.icon}
              </span>
              {item.label}
            </button>
          ))}
        </nav>

        {/* Content pane */}
        <div style={styles.content}>
          {activeTab === 'folders' && <FoldersTab />}
          {activeTab === 'api-key' && (
            <ApiKeyTab
              onSuccess={() => onSuccess?.('Gemini API key saved successfully')}
              onError={(msg) => onError?.(msg)}
            />
          )}
          {activeTab === 'hotkey' && <HotkeyTab onError={(msg) => onError?.(msg)} />}
          {activeTab === 'indexing' && <IndexingTab />}
          {activeTab === 'about' && <AboutTab />}
        </div>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'absolute',
    inset: 0,
    background: 'var(--bg-surface-opaque)',
    display: 'flex',
    flexDirection: 'column',
    zIndex: 1500,
    borderRadius: 'var(--radius-window)',
    overflow: 'hidden',
  },
  rail: {
    width: 200,
    padding: '12px 8px',
    borderRight: '1px solid var(--border)',
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    flexShrink: 0,
    overflowY: 'auto',
    background: 'var(--bg-surface-opaque-2)',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    padding: '0 16px',
    height: 56,
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  backBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    padding: '6px',
    borderRadius: 'var(--radius-sm, 4px)',
    marginRight: 8,
  },
  headerTitle: {
    flex: 1,
    fontSize: 16,
    fontWeight: 700,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
  },
  closeBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    padding: '6px',
    borderRadius: 'var(--radius-sm, 4px)',
  },
  body: {
    display: 'flex',
    flex: 1,
    overflow: 'hidden',
  },
  navItem: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '9px 12px',
    borderRadius: 'var(--radius-md, 8px)',
    border: 'none',
    cursor: 'pointer',
    fontSize: 14,
    fontFamily: 'var(--font-sans)',
    textAlign: 'left',
    transition: 'background 0.1s, color 0.1s',
    width: '100%',
  },
  content: {
    flex: 1,
    padding: '28px 32px',
    overflowY: 'auto',
  },
};
