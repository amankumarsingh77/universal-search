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

export function ResultItem({ result, isSelected, onClick, onDoubleClick }: ResultItemProps) {
  const hasThumbnail = result.thumbnailPath && result.thumbnailPath.length > 0;
  const shortDir = getShortDir(result.filePath);
  const scorePercent = Math.round(result.score * 100);

  return (
    <div
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      style={{
        ...styles.container,
        background: isSelected ? 'var(--bg-selected)' : 'transparent',
        borderLeft: isSelected ? '2px solid var(--accent-green)' : '2px solid transparent',
      }}
    >
      <div style={styles.thumbnail}>
        {hasThumbnail ? (
          <img
            src={`/localfile/${result.thumbnailPath}`}
            alt=""
            style={styles.thumbImage}
            onError={(e) => {
              (e.target as HTMLImageElement).style.display = 'none';
              (e.target as HTMLImageElement).nextElementSibling?.removeAttribute('style');
            }}
          />
        ) : null}
        <span
          style={{
            ...styles.thumbFallback,
            display: hasThumbnail ? 'none' : 'flex',
          }}
        >
          {getTypeIcon(result.fileType)}
        </span>
      </div>
      <div style={styles.info}>
        <div style={styles.fileName}>{result.fileName}</div>
        {shortDir ? (
          <div style={styles.breadcrumb}>{shortDir}</div>
        ) : null}
        <div style={{ ...styles.secondary, color: getTypeColor(result.fileType) }}>
          {getSecondaryText(result)}
        </div>
      </div>
      {result.score > 0 ? (
        <div style={styles.scoreBadge}>{scorePercent}%</div>
      ) : null}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    alignItems: 'center',
    paddingTop: '6px',
    paddingBottom: '6px',
    paddingLeft: '12px',
    paddingRight: '12px',
    minHeight: '56px',
    cursor: 'pointer',
    gap: '10px',
    transition: 'background 0.1s ease',
  },
  thumbnail: {
    width: '40px',
    height: '40px',
    borderRadius: 'var(--radius-sm)',
    overflow: 'hidden',
    flexShrink: 0,
    background: 'var(--bg-surface-2)',
    position: 'relative',
  },
  thumbImage: {
    width: '100%',
    height: '100%',
    objectFit: 'cover',
  },
  thumbFallback: {
    width: '100%',
    height: '100%',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: '18px',
  },
  info: {
    flex: 1,
    overflow: 'hidden',
  },
  fileName: {
    fontSize: '13px',
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-primary)',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    lineHeight: '18px',
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
  },
};
