import type { SearchResultDTO } from '../hooks/useSearch';
import { formatSize } from '../utils/format';

interface ResultItemProps {
  result: SearchResultDTO;
  isSelected: boolean;
  onClick: () => void;
  onDoubleClick: () => void;
}

function formatTimestamp(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, '0')}`;
}

function getTypeColor(fileType: string): string {
  switch (fileType) {
    case 'video': return 'var(--accent-video)';
    case 'image': return 'var(--accent-image)';
    case 'audio': return 'var(--accent-audio)';
    case 'code': return 'var(--accent-code)';
    default: return 'var(--text-secondary)';
  }
}

function getTypeIcon(fileType: string): string {
  switch (fileType) {
    case 'video': return '🎬';
    case 'image': return '🖼';
    case 'audio': return '🎵';
    case 'code': return '💻';
    case 'text': return '📄';
    default: return '📁';
  }
}

function getShortDir(filePath: string): string {
  const parts = filePath.split('/');
  parts.pop(); // remove filename
  if (parts.length === 0) return '';
  return parts.slice(-2).join('/');
}

function getSecondaryText(result: SearchResultDTO): string {
  const parts: string[] = [];
  const typeLabel = result.fileType.charAt(0).toUpperCase() + result.fileType.slice(1);
  parts.push(typeLabel);

  if (result.fileType === 'video' && result.startTime > 0) {
    parts.push(`@ ${formatTimestamp(result.startTime)}`);
  }

  parts.push(formatSize(result.sizeBytes));
  return parts.join(' \u00b7 ');
}

function midTruncate(name: string, max = 40): string {
  if (name.length <= max) return name;
  const extIdx = name.lastIndexOf('.');
  const ext = extIdx > 0 ? name.slice(extIdx) : '';
  const keep = max - ext.length - 1;
  return name.slice(0, Math.ceil(keep / 2)) + '…' + name.slice(extIdx - Math.floor(keep / 2));
}

function FileIcon({ fileType }: { fileType: string }) {
  const colors: Record<string, string> = {
    video: '#06B6D4',
    image: '#F97316',
    audio: '#EC4899',
    code: '#A78BFA',
    text: '#FAFAFA',
  };
  const color = colors[fileType] ?? 'rgba(255,255,255,0.3)';
  const letters: Record<string, string> = {
    video: '▶',
    image: '⬛',
    audio: '♪',
    code: '</>',
    text: '≡',
  };
  const letter = letters[fileType] ?? '?';
  return (
    <div style={{
      width: 32, height: 32,
      borderRadius: 6,
      background: `${color}22`,
      border: `1px solid ${color}44`,
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      fontSize: fileType === 'code' ? 9 : 14,
      color,
      flexShrink: 0,
      fontFamily: 'var(--font-mono)',
    }}>
      {letter}
    </div>
  );
}

// suppress unused warning — kept for potential external use
void getTypeIcon;

export function ResultItem({ result, isSelected, onClick, onDoubleClick }: ResultItemProps) {
  const shortDir = getShortDir(result.filePath);
  const scorePercent = Math.round(result.score * 100);

  return (
    <div
      className="result-item"
      role="option"
      aria-selected={isSelected}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      style={{
        ...styles.container,
        background: isSelected ? 'var(--bg-selected)' : 'transparent',
        borderRadius: 'var(--radius-row)',
        transform: isSelected ? 'scale(1.005)' : 'none',
        transition: 'background 0.1s ease, transform 0.1s ease',
      }}
    >
      <FileIcon fileType={result.fileType} />
      <div style={styles.info}>
        <div style={styles.fileName}>{midTruncate(result.fileName)}</div>
        {shortDir ? (
          <div style={styles.breadcrumb}>{shortDir}</div>
        ) : null}
        <div style={{ ...styles.secondary, color: getTypeColor(result.fileType) }}>
          {getSecondaryText(result)}
        </div>
      </div>
      {result.score > 0 ? (
        <span className="score-badge" style={styles.scoreBadge}>{scorePercent}%</span>
      ) : null}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    alignItems: 'center',
    padding: '10px 16px',
    cursor: 'pointer',
    gap: '10px',
  },
  info: {
    flex: 1,
    overflow: 'hidden',
  },
  fileName: {
    fontSize: '16px',
    fontWeight: 600,
    fontFamily: 'var(--font-sans)',
    color: 'var(--text-primary)',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    lineHeight: '20px',
  },
  secondary: {
    fontSize: '11px',
    fontFamily: 'var(--font-sans)',
    lineHeight: '16px',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  breadcrumb: {
    fontSize: 11,
    fontFamily: 'var(--font-sans)',
    color: 'var(--text-tertiary)',
    lineHeight: '15px',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  scoreBadge: {
    fontSize: '10px',
    fontFamily: 'var(--font-sans)',
    color: 'var(--text-tertiary)',
    flexShrink: 0,
    alignSelf: 'center',
    opacity: 0,
    transition: 'opacity 0.15s ease',
  },
};
