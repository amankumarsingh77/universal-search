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
        background: hovered ? '#1E2028' : '#16181D',
        border: '1px solid #23252B',
        borderRadius: 8,
        cursor: 'pointer',
        flexShrink: 0,
        transition: 'background 0.1s',
      }}
      {...{ '--wails-draggable': 'no-drag' } as React.HTMLAttributes<HTMLButtonElement>}
    >
      <Settings size={16} color="#B4B8C0" strokeWidth={2} />
    </button>
  );
}
