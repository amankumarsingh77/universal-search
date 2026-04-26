import { useState } from 'react';
import { Settings } from 'lucide-react';

interface SettingsButtonProps {
  onClick: () => void;
}

export function SettingsButton({ onClick }: SettingsButtonProps) {
  const [hovered, setHovered] = useState(false);

  return (
    <button
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      aria-label="Open settings"
      style={{
        width: 32,
        height: 32,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: hovered ? 'var(--bg-hover)' : 'transparent',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-md, 8px)',
        cursor: 'pointer',
        flexShrink: 0,
        transition: 'background 0.1s',
        color: 'var(--text-secondary)',
      }}
      {...{ '--wails-draggable': 'no-drag' } as React.HTMLAttributes<HTMLButtonElement>}
    >
      <Settings size={16} strokeWidth={2} />
    </button>
  );
}
