import { useEffect, useState } from 'react';
import { Minus, PauseCircle, Plus, RefreshCw } from 'lucide-react';
import {
  GetIndexingSettings,
  PauseIndexing,
  ReindexNow,
  SetIndexingSettings,
} from '../../../wailsjs/go/app/App';
import { useIndexingStatus } from '../../hooks/useIndexingStatus';

const WORKERS_MIN = 1;
const WORKERS_MAX = 32;
const RATE_MIN = 1;
const RATE_MAX = 10000;

interface IndexingSettings {
  workersSaved: number;
  workersRuntime: number;
  rateLimitSaved: number;
  rateLimitRuntime: number;
}

const clamp = (n: number, min: number, max: number) => Math.max(min, Math.min(max, n));

export function IndexingTab() {
  const status = useIndexingStatus();

  const [settings, setSettings] = useState<IndexingSettings | null>(null);
  const [draftWorkers, setDraftWorkers] = useState<number>(0);
  const [draftRate, setDraftRate] = useState<number>(0);
  const [rateText, setRateText] = useState<string>('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string>('');

  useEffect(() => {
    GetIndexingSettings().then((s) => {
      setSettings(s);
      setDraftWorkers(s.workersSaved);
      setDraftRate(s.rateLimitSaved);
      setRateText(String(s.rateLimitSaved));
    });
  }, []);

  const pct = status.totalFiles > 0
    ? Math.round((status.indexedFiles / status.totalFiles) * 100)
    : 0;

  const etaMinutes = status.totalFiles > 0 && status.isRunning
    ? Math.max(0, Math.round(((status.totalFiles - status.indexedFiles) / Math.max(1, status.indexedFiles)) * 2))
    : 0;

  const handlePause = () => PauseIndexing();
  const handleReindex = () => ReindexNow();

  const dirty =
    settings !== null &&
    (draftWorkers !== settings.workersSaved || draftRate !== settings.rateLimitSaved);

  const restartPending =
    settings !== null && settings.workersSaved !== settings.workersRuntime;

  const stepWorkers = (delta: number) => {
    setDraftWorkers((w) => clamp(w + delta, WORKERS_MIN, WORKERS_MAX));
  };

  const handleRateChange = (value: string) => {
    setRateText(value);
    const n = parseInt(value, 10);
    if (!isNaN(n)) setDraftRate(n);
  };

  const handleRateBlur = () => {
    const clamped = clamp(draftRate || RATE_MIN, RATE_MIN, RATE_MAX);
    setDraftRate(clamped);
    setRateText(String(clamped));
  };

  const handleSave = async () => {
    if (!settings) return;
    setSaving(true);
    setSaveError('');
    try {
      await SetIndexingSettings(draftWorkers, draftRate);
      setSettings({
        workersSaved: draftWorkers,
        workersRuntime: settings.workersRuntime,
        rateLimitSaved: draftRate,
        rateLimitRuntime: draftRate,
      });
    } catch (err) {
      setSaveError(String(err));
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = () => {
    if (!settings) return;
    setDraftWorkers(settings.workersSaved);
    setDraftRate(settings.rateLimitSaved);
    setRateText(String(settings.rateLimitSaved));
    setSaveError('');
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div style={styles.statusCard}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{
              width: 10, height: 10, borderRadius: '50%',
              background: status.isRunning ? 'var(--accent, #7c6fe0)' : 'var(--accent-green)',
              boxShadow: status.isRunning ? '0 0 8px var(--accent, #7c6fe0)' : '0 0 8px var(--accent-green)',
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

      <div>
        <h3 style={styles.sectionLabel}>Performance</h3>
        <div style={styles.settingsList}>
          <div style={styles.settingRow}>
            <div style={{ minWidth: 0 }}>
              <div style={styles.settingName}>Concurrent workers</div>
              <div style={styles.settingDesc}>Files processed in parallel during indexing</div>
              {restartPending && (
                <div style={styles.restartHint}>Restart app to apply</div>
              )}
            </div>
            <div style={styles.stepper}>
              <button
                style={styles.stepBtn}
                onClick={() => stepWorkers(-1)}
                disabled={!settings || draftWorkers <= WORKERS_MIN}
                aria-label="Decrease workers"
              >
                <Minus size={12} />
              </button>
              <div style={styles.stepValue}>{settings ? draftWorkers : '—'}</div>
              <button
                style={styles.stepBtn}
                onClick={() => stepWorkers(1)}
                disabled={!settings || draftWorkers >= WORKERS_MAX}
                aria-label="Increase workers"
              >
                <Plus size={12} />
              </button>
            </div>
          </div>
          <div style={{ ...styles.settingRow, borderBottom: 'none' }}>
            <div style={{ minWidth: 0 }}>
              <div style={styles.settingName}>Embedder rate limit</div>
              <div style={styles.settingDesc}>Requests per minute to Gemini Embedding API</div>
            </div>
            <div style={styles.rateField}>
              <input
                style={styles.rateInput}
                type="number"
                min={RATE_MIN}
                max={RATE_MAX}
                value={rateText}
                onChange={(e) => handleRateChange(e.target.value)}
                onBlur={handleRateBlur}
                disabled={!settings}
                aria-label="Embedder rate limit"
              />
              <span style={styles.rateSuffix}>/ min</span>
            </div>
          </div>
        </div>

        {(dirty || saveError) && (
          <div style={styles.actionsRow}>
            {saveError && <div style={styles.errorText}>{saveError}</div>}
            <div style={{ flex: 1 }} />
            <button style={styles.cancelBtn} onClick={handleCancel} disabled={saving}>
              Cancel
            </button>
            <button
              style={{ ...styles.saveBtn, opacity: saving ? 0.6 : 1 }}
              onClick={handleSave}
              disabled={saving || !dirty}
            >
              {saving ? 'Saving…' : 'Save'}
            </button>
          </div>
        )}
      </div>

      <div>
        <h3 style={styles.sectionLabel}>Behavior</h3>
        <div style={styles.settingsList}>
          <div style={styles.settingRow}>
            <div style={{ minWidth: 0 }}>
              <div style={{ ...styles.settingName, color: 'var(--text-secondary)' }}>
                Pause when on battery
              </div>
              <div style={styles.settingDesc}>Avoid heavy CPU and network use when unplugged</div>
            </div>
            <div style={styles.comingSoonPill}>Coming soon</div>
          </div>
          <div style={{ ...styles.settingRow, borderBottom: 'none' }}>
            <div style={{ minWidth: 0 }}>
              <div style={{ ...styles.settingName, color: 'var(--text-secondary)' }}>
                Index OCR for images
              </div>
              <div style={styles.settingDesc}>Extract text from screenshots and scanned PDFs</div>
            </div>
            <div style={styles.comingSoonPill}>Coming soon</div>
          </div>
        </div>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  statusCard: {
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-lg, 12px)',
    padding: '16px 20px',
  },
  statusTitle: {
    fontSize: 16,
    fontWeight: 600,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
  },
  pauseBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    background: 'var(--bg-selected)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    color: 'var(--text-secondary)',
    fontSize: 13,
    padding: '6px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  reindexBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    background: 'var(--bg-selected)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    color: 'var(--text-secondary)',
    fontSize: 13,
    padding: '6px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  progressBar: {
    height: 6,
    background: 'var(--bg-selected)',
    borderRadius: 100,
    marginTop: 14,
    overflow: 'hidden',
  },
  progressFill: {
    height: '100%',
    background: 'var(--accent, #7c6fe0)',
    borderRadius: 100,
    transition: 'width 0.5s ease',
  },
  progressLabel: {
    fontSize: 12,
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-sans)',
  },
  sectionLabel: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    margin: '0 0 10px',
    fontFamily: 'var(--font-sans)',
  },
  settingsList: {
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-lg, 12px)',
    overflow: 'hidden',
  },
  settingRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '14px 16px',
    background: 'var(--bg-surface)',
    borderBottom: '1px solid var(--border)',
    gap: 16,
  },
  settingName: {
    fontSize: 14,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
    marginBottom: 3,
  },
  settingDesc: {
    fontSize: 12,
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-sans)',
  },
  restartHint: {
    fontSize: 11,
    color: 'var(--accent-warning)',
    fontWeight: 500,
    fontFamily: 'var(--font-sans)',
    marginTop: 4,
  },
  stepper: {
    display: 'flex',
    alignItems: 'center',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    overflow: 'hidden',
    flexShrink: 0,
  },
  stepBtn: {
    width: 28,
    height: 28,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'transparent',
    border: 'none',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
  },
  stepValue: {
    minWidth: 36,
    height: 28,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    color: 'var(--text-primary)',
  },
  rateField: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '4px 10px',
    background: 'var(--bg-base)',
    border: '1px solid var(--border-focus)',
    borderRadius: 'var(--radius-md, 8px)',
    flexShrink: 0,
  },
  rateInput: {
    width: 56,
    background: 'transparent',
    border: 'none',
    outline: 'none',
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    textAlign: 'right',
  },
  rateSuffix: {
    fontSize: 12,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-sans)',
  },
  actionsRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    marginTop: 12,
  },
  errorText: {
    fontSize: 12,
    color: 'var(--accent-warning)',
    fontFamily: 'var(--font-sans)',
  },
  cancelBtn: {
    padding: '6px 14px',
    background: 'var(--bg-selected)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    color: 'var(--text-secondary)',
    fontSize: 13,
    fontWeight: 500,
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  saveBtn: {
    padding: '6px 14px',
    background: 'var(--accent, #7c6fe0)',
    border: 'none',
    borderRadius: 'var(--radius-md, 8px)',
    color: '#FFFFFF',
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  comingSoonPill: {
    padding: '3px 10px',
    background: 'var(--bg-selected)',
    border: '1px solid var(--border)',
    borderRadius: 100,
    fontSize: 10,
    fontWeight: 500,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-sans)',
    flexShrink: 0,
  },
};
