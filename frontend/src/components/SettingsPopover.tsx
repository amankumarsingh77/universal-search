import { useEffect, useRef, useState } from 'react';
import { FolderOpen, KeyRound, Keyboard, RefreshCw, PauseCircle, Info } from 'lucide-react';
import { ReindexNow, PauseIndexing } from '../../wailsjs/go/app/App';
import type { SettingsTab } from './SettingsPanel';

interface SettingsPopoverProps {
  open: boolean;
  onClose: () => void;
  onOpenSettings: (tab: SettingsTab) => void;
}

interface MenuItemProps {
  icon: React.ReactNode;
  label: string;
  shortcut?: string;
  onClick: () => void;
  dividerBefore?: boolean;
}

function MenuItem({ icon, label, shortcut, onClick }: MenuItemProps) {
  const [hovered, setHovered] = useState(false);
  return (
    <button
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        width: '100%',
        padding: '8px 14px',
        background: hovered ? 'rgba(255,255,255,0.05)' : 'transparent',
        border: 'none',
        cursor: 'pointer',
        textAlign: 'left',
        color: 'var(--text-primary)',
        fontSize: 14,
        fontFamily: 'var(--font-sans, system-ui)',
        borderRadius: 4,
      }}
    >
      <span style={{ color: 'var(--text-secondary)', flexShrink: 0 }}>{icon}</span>
      <span style={{ flex: 1 }}>{label}</span>
      {shortcut && (
        <span style={{ color: 'var(--text-tertiary)', fontSize: 12 }}>{shortcut}</span>
      )}
    </button>
  );
}

export function SettingsPopover({ open, onClose, onOpenSettings }: SettingsPopoverProps) {
  const popoverRef = useRef<HTMLDivElement>(null);

  // Suppress hide while popover is open via a global flag the App blur handler checks
  useEffect(() => {
    if (!open) return;
    (window as unknown as Record<string, unknown>).__settingsPopoverOpen = true;
    return () => {
      (window as unknown as Record<string, unknown>).__settingsPopoverOpen = false;
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    const handleMouseDown = (e: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown, true);
    document.addEventListener('mousedown', handleMouseDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown, true);
      document.removeEventListener('mousedown', handleMouseDown);
    };
  }, [open, onClose]);

  if (!open) return null;

  const handleReindex = () => {
    ReindexNow();
    onClose();
  };

  const handlePause = () => {
    PauseIndexing();
    onClose();
  };

  const handleAbout = () => {
    onOpenSettings('about');
  };

  const handleOpenTab = (tab: SettingsTab) => {
    onOpenSettings(tab);
  };

  return (
    <div
      style={{
        position: 'absolute',
        top: 56,
        right: 12,
        width: 280,
        background: 'var(--bg-surface)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-lg, 12px)',
        boxShadow: '0 8px 32px rgba(0,0,0,0.6)',
        zIndex: 2000,
        padding: '6px 0',
        userSelect: 'none',
      }}
      ref={popoverRef}
      onClick={(e) => e.stopPropagation()}
      {...{ '--wails-draggable': 'no-drag' } as React.HTMLAttributes<HTMLDivElement>}
    >
      <div style={{
        padding: '4px 14px 6px',
        fontSize: 10,
        fontWeight: 600,
        letterSpacing: '0.08em',
        textTransform: 'uppercase' as const,
        color: 'var(--text-tertiary)',
        fontFamily: 'var(--font-sans, system-ui)',
      }}>
        Settings
      </div>

      <MenuItem
        icon={<FolderOpen size={16} />}
        label="Manage Folders…"
        shortcut="⌘O"
        onClick={() => handleOpenTab('folders')}
      />
      <MenuItem
        icon={<KeyRound size={16} />}
        label="API Key"
        onClick={() => handleOpenTab('api-key')}
      />
      <MenuItem
        icon={<Keyboard size={16} />}
        label="Hotkey"
        onClick={() => handleOpenTab('hotkey')}
      />

      <div style={{ height: 1, background: 'var(--border)', margin: '4px 0' }} />

      <MenuItem
        icon={<RefreshCw size={16} />}
        label="Re-index Now"
        shortcut="⌘R"
        onClick={handleReindex}
      />
      <MenuItem
        icon={<PauseCircle size={16} />}
        label="Pause Indexing"
        onClick={handlePause}
      />

      <div style={{ height: 1, background: 'var(--border)', margin: '4px 0' }} />

      <MenuItem
        icon={<Info size={16} />}
        label="About"
        onClick={handleAbout}
      />
    </div>
  );
}
