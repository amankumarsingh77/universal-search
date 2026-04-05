import type { SearchResultDTO } from '../hooks/useSearch';
import { formatSize } from '../utils/format';
import { VideoPreview } from './previews/VideoPreview';
import { ImagePreview } from './previews/ImagePreview';
import { TextPreview } from './previews/TextPreview';
import { AudioPreview } from './previews/AudioPreview';

interface PreviewPanelProps {
  result: SearchResultDTO | null;
}

function getDirectoryPath(filePath: string): string {
  const parts = filePath.split('/');
  parts.pop();
  return parts.join('/');
}

function renderPreview(result: SearchResultDTO) {
  switch (result.fileType) {
    case 'video':
      return <VideoPreview result={result} />;
    case 'image':
      return <ImagePreview result={result} />;
    case 'audio':
      return <AudioPreview result={result} />;
    case 'text':
    case 'code':
      return <TextPreview result={result} />;
    default:
      return (
        <div style={styles.genericPreview}>
          <span style={styles.genericIcon}>📁</span>
          <span style={styles.genericText}>No preview available</span>
        </div>
      );
  }
}

export function PreviewPanel({ result }: PreviewPanelProps) {
  if (!result) {
    return (
      <div style={styles.container}>
        <div style={styles.emptyState}>
          <span style={styles.emptyIcon}>👀</span>
          <span style={styles.emptyTitle}>No file selected</span>
          <span style={styles.emptyHint}>
            Select a search result to preview
          </span>
        </div>
      </div>
    );
  }

  return (
    <div style={styles.container}>
      <div style={styles.header}>
        <h2 style={styles.fileName}>{result.fileName}</h2>
        <p style={styles.filePath}>{getDirectoryPath(result.filePath)}</p>
      </div>

      <div style={styles.previewArea}>
        {renderPreview(result)}
      </div>

      <div style={styles.metadata}>
        <MetaItem label="Type" value={result.fileType} />
        <MetaItem label="Extension" value={result.extension} />
        <MetaItem label="Size" value={formatSize(result.sizeBytes)} />
      </div>

      <div style={styles.actions}>
        <span style={styles.shortcut}>
          <kbd style={styles.kbd}>Enter</kbd> Open file
        </span>
        <span style={styles.shortcut}>
          <kbd style={styles.kbd}>Ctrl+Enter</kbd> Open folder
        </span>
      </div>
    </div>
  );
}

function MetaItem({ label, value }: { label: string; value: string }) {
  return (
    <div style={styles.metaItem}>
      <span style={styles.metaLabel}>{label}</span>
      <span style={styles.metaValue}>{value}</span>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    padding: '20px',
    overflowY: 'auto',
    gap: '16px',
  },
  emptyState: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100%',
    gap: '8px',
  },
  emptyIcon: {
    fontSize: '40px',
    opacity: 0.3,
  },
  emptyTitle: {
    fontSize: '15px',
    color: 'var(--text-secondary)',
    fontWeight: 500,
  },
  emptyHint: {
    fontSize: '13px',
    color: 'var(--text-tertiary)',
  },
  header: {
    flexShrink: 0,
  },
  fileName: {
    fontSize: '18px',
    fontWeight: 600,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-primary)',
    margin: 0,
    lineHeight: '24px',
    wordBreak: 'break-all',
  },
  filePath: {
    fontSize: '12px',
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
    margin: '4px 0 0',
    wordBreak: 'break-all',
    lineHeight: '18px',
  },
  previewArea: {
    flexShrink: 0,
  },
  metadata: {
    display: 'flex',
    gap: '16px',
    flexWrap: 'wrap',
    padding: '12px 0',
    borderTop: '1px solid var(--border)',
    flexShrink: 0,
  },
  metaItem: {
    display: 'flex',
    flexDirection: 'column',
    gap: '2px',
  },
  metaLabel: {
    fontSize: '10px',
    color: 'var(--text-tertiary)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.5px',
    fontWeight: 600,
  },
  metaValue: {
    fontSize: '13px',
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
  },
  actions: {
    display: 'flex',
    gap: '16px',
    marginTop: 'auto',
    paddingTop: '12px',
    borderTop: '1px solid var(--border)',
    flexShrink: 0,
  },
  shortcut: {
    fontSize: '12px',
    color: 'var(--text-tertiary)',
    display: 'flex',
    alignItems: 'center',
    gap: '6px',
  },
  kbd: {
    display: 'inline-block',
    padding: '1px 6px',
    fontSize: '11px',
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    background: 'var(--bg-surface-2)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm)',
  },
  genericPreview: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '40px',
    gap: '12px',
    background: 'var(--bg-surface-2)',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border)',
  },
  genericIcon: {
    fontSize: '36px',
    opacity: 0.4,
  },
  genericText: {
    fontSize: '13px',
    color: 'var(--text-tertiary)',
  },
};
