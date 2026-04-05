import { useState } from 'react';
import { PauseIndexing, ResumeIndexing } from '../../wailsjs/go/main/App';
import type { IndexingStatus } from '../hooks/useIndexingStatus';

interface IndexingBarProps {
  status: IndexingStatus;
}

function getFileName(path: string): string {
  const parts = path.split('/');
  return parts[parts.length - 1] || path;
}

export function IndexingBar({ status }: IndexingBarProps) {
  const [expanded, setExpanded] = useState(false);

  if (!status.isRunning && status.indexedFiles === 0) {
    return null;
  }

  const processed = status.indexedFiles + status.failedFiles;
  const progress = status.totalFiles > 0
    ? Math.round((processed / status.totalFiles) * 100)
    : 0;

  const isComplete = !status.isRunning && processed > 0;
  const hasErrors = status.failedFiles > 0;

  const handlePauseResume = () => {
    if (status.paused) {
      ResumeIndexing();
    } else {
      PauseIndexing();
    }
  };

  return (
    <div style={styles.container}>
      <div
        style={styles.header}
        onClick={() => !isComplete && setExpanded(!expanded)}
      >
        <div style={styles.headerLeft}>
          {!isComplete && (
            <span style={styles.chevron}>{expanded ? '▾' : '▸'}</span>
          )}
          {isComplete ? (
            <span style={hasErrors ? styles.warningText : styles.completeText}>
              {hasErrors
                ? `⚠ ${status.indexedFiles.toLocaleString()} indexed, ${status.failedFiles.toLocaleString()} failed`
                : `✓ ${status.indexedFiles.toLocaleString()} files indexed`}
            </span>
          ) : status.quotaPaused ? (
            <span style={styles.warningText}>
              Indexing paused — API quota exhausted
              {status.quotaResumeAt && `, will retry at ${new Date(status.quotaResumeAt).toLocaleTimeString()}`}
            </span>
          ) : (
            <span style={styles.statusText}>
              Indexing {processed.toLocaleString()}/{status.totalFiles.toLocaleString()} files
              {hasErrors && ` (${status.failedFiles.toLocaleString()} failed)`}
            </span>
          )}
        </div>

        {!isComplete && (
          <div style={styles.progressWrap}>
            <div style={styles.progressTrack}>
              <div
                style={{
                  ...styles.progressFill,
                  width: `${progress}%`,
                }}
              />
            </div>
            <span style={styles.progressLabel}>{progress}%</span>
          </div>
        )}
      </div>

      {expanded && !isComplete && (
        <div style={styles.details}>
          {status.currentFile && (
            <div style={styles.detailRow}>
              <span style={styles.detailLabel}>Currently:</span>
              <span style={styles.detailValue}>{getFileName(status.currentFile)}</span>
            </div>
          )}
          <button
            onClick={handlePauseResume}
            style={styles.pauseButton}
          >
            {status.paused ? '▶ Resume' : '⏸ Pause'}
          </button>
        </div>
      )}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    borderTop: '1px solid var(--border)',
    background: 'var(--bg-surface)',
    flexShrink: 0,
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 16px',
    cursor: 'pointer',
    userSelect: 'none',
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
  },
  chevron: {
    fontSize: '10px',
    color: 'var(--text-secondary)',
    width: '12px',
  },
  statusText: {
    fontSize: '12px',
    color: 'var(--text-secondary)',
  },
  completeText: {
    fontSize: '12px',
    color: 'var(--accent-green)',
  },
  warningText: {
    fontSize: '12px',
    color: '#e5a00d',
  },
  progressWrap: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
  },
  progressTrack: {
    width: '120px',
    height: '4px',
    background: 'var(--bg-surface-2)',
    borderRadius: '2px',
    overflow: 'hidden',
  },
  progressFill: {
    height: '100%',
    background: 'var(--accent-green)',
    borderRadius: '2px',
    transition: 'width 0.3s ease',
  },
  progressLabel: {
    fontSize: '11px',
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-mono)',
    minWidth: '32px',
    textAlign: 'right',
  },
  details: {
    padding: '0 16px 10px 36px',
    display: 'flex',
    flexDirection: 'column',
    gap: '6px',
  },
  detailRow: {
    display: 'flex',
    gap: '6px',
    fontSize: '11px',
  },
  detailLabel: {
    color: 'var(--text-tertiary)',
    flexShrink: 0,
  },
  detailValue: {
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  pauseButton: {
    alignSelf: 'flex-start',
    background: 'var(--bg-surface-2)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm)',
    color: 'var(--text-secondary)',
    fontSize: '11px',
    padding: '3px 10px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
};
