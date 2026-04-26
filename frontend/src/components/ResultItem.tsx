import type { SearchResultDTO } from '../hooks/useSearch';
import { formatSize } from '../utils/format';
import { Thumbnail } from './Thumbnail';
import { MatchKindIcon } from './MatchKindIcon';
import { splitHighlights } from '../lib/highlight';

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
      <Thumbnail fileType={result.fileType} thumbnailPath={result.thumbnailPath} />
      <div style={styles.info}>
        <div style={styles.fileNameRow}>
          <MatchKindIcon kind={result.matchKind ?? 'content'} size={13} />
          <div style={styles.fileName}>
            {splitHighlights(midTruncate(result.fileName), result.highlights ?? []).map(
              (seg, i) =>
                seg.matched ? (
                  <span key={i} className="filename-highlight">{seg.text}</span>
                ) : (
                  <span key={i}>{seg.text}</span>
                ),
            )}
          </div>
        </div>
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
  fileNameRow: {
    display: 'flex',
    alignItems: 'center',
    gap: '5px',
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
