import { PauseCircle, RefreshCw } from 'lucide-react';
import { PauseIndexing, ReindexNow } from '../../../wailsjs/go/app/App';
import { useIndexingStatus } from '../../hooks/useIndexingStatus';

export function IndexingTab() {
  const status = useIndexingStatus();

  const pct = status.totalFiles > 0
    ? Math.round((status.indexedFiles / status.totalFiles) * 100)
    : 0;

  const etaMinutes = status.totalFiles > 0 && status.isRunning
    ? Math.max(0, Math.round(((status.totalFiles - status.indexedFiles) / Math.max(1, status.indexedFiles)) * 2))
    : 0;

  const handlePause = () => PauseIndexing();
  const handleReindex = () => ReindexNow();

  // Read-only config values from defaults (no setter API exists)
  const workerCount = 4;
  const rateLimit = 55;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {/* Status card */}
      <div style={styles.statusCard}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{
              width: 10, height: 10, borderRadius: '50%',
              background: status.isRunning ? '#60A5FA' : '#4ADE80',
              boxShadow: status.isRunning ? '0 0 8px #60A5FA' : '0 0 8px #4ADE80',
            }} />
            <span style={styles.statusTitle}>
              {status.isRunning ? 'Indexing in progress' : 'Up to date'}
            </span>
          </div>
          {status.isRunning ? (
            <button style={styles.pauseBtn} onClick={handlePause}>
              <PauseCircle size={14} /> Pause
            </button>
          ) : (
            <button style={styles.reindexBtn} onClick={handleReindex}>
              <RefreshCw size={14} /> Re-index
            </button>
          )}
        </div>

        {status.isRunning && (
          <>
            <div style={styles.progressBar}>
              <div style={{ ...styles.progressFill, width: `${pct}%` }} />
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 6 }}>
              <span style={styles.progressLabel}>
                {status.indexedFiles.toLocaleString()} / {status.totalFiles.toLocaleString()} files · {pct}%
              </span>
              {etaMinutes > 0 && (
                <span style={styles.progressLabel}>~{etaMinutes} min remaining</span>
              )}
            </div>
          </>
        )}
      </div>

      {/* Performance section */}
      <div>
        <h3 style={styles.sectionLabel}>Performance</h3>
        <div style={styles.settingsList}>
          <div style={styles.settingRow}>
            <div>
              <div style={styles.settingName}>Concurrent workers</div>
              <div style={styles.settingDesc}>Files processed in parallel during indexing</div>
            </div>
            <div style={styles.readonlyValue}>{workerCount}</div>
          </div>
          <div style={{ ...styles.settingRow, borderBottom: 'none' }}>
            <div>
              <div style={styles.settingName}>Embedder rate limit</div>
              <div style={styles.settingDesc}>Requests per minute to Gemini Embedding API</div>
            </div>
            <div style={styles.readonlyValue}>{rateLimit} / min</div>
          </div>
        </div>
        <div style={styles.readonlyNote}>
          These values are set in config.toml and are read-only here.
        </div>
      </div>

      {/* Behavior section */}
      <div>
        <h3 style={styles.sectionLabel}>Behavior</h3>
        <div style={styles.settingsList}>
          <div style={styles.settingRow}>
            <div>
              <div style={styles.settingName}>Pause when on battery</div>
              <div style={styles.settingDesc}>Avoid heavy CPU and network use when unplugged</div>
            </div>
            <div style={styles.toggleDisabled} title="Set in config.toml">
              <div style={styles.toggleKnob} />
            </div>
          </div>
          <div style={{ ...styles.settingRow, borderBottom: 'none' }}>
            <div>
              <div style={styles.settingName}>Index OCR for images</div>
              <div style={styles.settingDesc}>Extract text from screenshots and scanned PDFs</div>
            </div>
            <div style={styles.toggleDisabled} title="Set in config.toml">
              <div style={styles.toggleKnob} />
            </div>
          </div>
        </div>
        <div style={styles.readonlyNote}>
          Behavior toggles are not configurable via UI in this version.
        </div>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  statusCard: {
    background: '#0F1014',
    border: '1px solid #1B1D22',
    borderRadius: 12,
    padding: '16px 20px',
  },
  statusTitle: {
    fontSize: 16,
    fontWeight: 600,
    color: '#E6E8EC',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  pauseBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    background: '#1B1D22',
    border: '1px solid #2A2D35',
    borderRadius: 8,
    color: '#C8CDD4',
    fontSize: 13,
    padding: '6px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  reindexBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    background: '#1B1D22',
    border: '1px solid #2A2D35',
    borderRadius: 8,
    color: '#C8CDD4',
    fontSize: 13,
    padding: '6px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  progressBar: {
    height: 6,
    background: '#1B1D22',
    borderRadius: 100,
    marginTop: 14,
    overflow: 'hidden',
  },
  progressFill: {
    height: '100%',
    background: '#5865F2',
    borderRadius: 100,
    transition: 'width 0.5s ease',
  },
  progressLabel: {
    fontSize: 12,
    color: '#6B6F76',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  sectionLabel: {
    fontSize: 14,
    fontWeight: 600,
    color: '#C8CDD4',
    margin: '0 0 10px',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  settingsList: {
    border: '1px solid #1B1D22',
    borderRadius: 10,
    overflow: 'hidden',
  },
  settingRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '14px 16px',
    background: '#0F1014',
    borderBottom: '1px solid #1B1D22',
    gap: 16,
  },
  settingName: {
    fontSize: 14,
    color: '#E6E8EC',
    fontFamily: 'var(--font-sans, system-ui)',
    marginBottom: 3,
  },
  settingDesc: {
    fontSize: 12,
    color: '#6B6F76',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  readonlyValue: {
    fontSize: 14,
    color: '#8A8F98',
    background: '#15171C',
    border: '1px solid #1B1D22',
    borderRadius: 8,
    padding: '6px 14px',
    flexShrink: 0,
    fontFamily: 'var(--font-mono, monospace)',
  },
  readonlyNote: {
    fontSize: 12,
    color: '#4A4E57',
    marginTop: 6,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  toggleDisabled: {
    width: 44,
    height: 24,
    background: '#2A2D35',
    borderRadius: 100,
    padding: 3,
    display: 'flex',
    alignItems: 'center',
    flexShrink: 0,
    opacity: 0.5,
    cursor: 'not-allowed',
  },
  toggleKnob: {
    width: 18,
    height: 18,
    background: '#6B6F76',
    borderRadius: '50%',
  },
};
