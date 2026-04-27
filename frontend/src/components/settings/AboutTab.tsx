import { ExternalLink, Flag } from 'lucide-react';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';

export function AboutTab() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <div>
        <h2 style={styles.heading}>Findo</h2>
        <p style={styles.subtext}>
          Fast local file search powered by vector embeddings and Gemini AI.
        </p>
      </div>

      <div style={styles.infoCard}>
        <InfoRow label="Version" value="0.1.0" />
        <InfoRow label="Embedding model" value="gemini-embedding-2-preview" />
        <InfoRow label="Dimensions" value="768-dim (MRL)" />
        <InfoRow label="Storage" value="SQLite + HNSW" last />
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <button
          style={styles.linkBtn}
          onClick={() => BrowserOpenURL('https://github.com/amankumarsingh77/findo')}
        >
          <ExternalLink size={15} />
          View on GitHub
        </button>
        <button
          style={styles.linkBtn}
          onClick={() => BrowserOpenURL('https://github.com/amankumarsingh77/findo/issues')}
        >
          <Flag size={15} />
          Report an issue
        </button>
      </div>

      <p style={styles.legal}>
        Findo is a local-first desktop application. Your files and search queries never leave your device except for embedding requests sent to Google's Gemini API.
      </p>
    </div>
  );
}

function InfoRow({ label, value, last }: { label: string; value: string; last?: boolean }) {
  return (
    <div style={{
      display: 'flex',
      justifyContent: 'space-between',
      alignItems: 'center',
      padding: '12px 16px',
      borderBottom: last ? 'none' : '1px solid var(--border)',
    }}>
      <span style={{ fontSize: 14, color: 'var(--text-secondary)', fontFamily: 'var(--font-sans)' }}>{label}</span>
      <span style={{ fontSize: 14, color: 'var(--text-primary)', fontFamily: 'var(--font-mono)' }}>{value}</span>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  heading: {
    fontSize: 28,
    fontWeight: 700,
    color: 'var(--text-primary)',
    margin: '0 0 8px',
    fontFamily: 'var(--font-sans)',
  },
  subtext: {
    fontSize: 14,
    color: 'var(--text-secondary)',
    margin: 0,
    lineHeight: 1.6,
    fontFamily: 'var(--font-sans)',
  },
  infoCard: {
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-lg, 12px)',
    overflow: 'hidden',
  },
  linkBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    background: 'transparent',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    color: 'var(--text-secondary)',
    fontSize: 14,
    padding: '10px 14px',
    cursor: 'pointer',
    textAlign: 'left' as const,
    fontFamily: 'var(--font-sans)',
  },
  legal: {
    fontSize: 12,
    color: 'var(--text-tertiary)',
    margin: 0,
    lineHeight: 1.6,
    fontFamily: 'var(--font-sans)',
  },
};
