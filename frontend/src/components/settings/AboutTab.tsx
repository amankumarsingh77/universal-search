import { ExternalLink } from 'lucide-react';
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
          onClick={() => BrowserOpenURL('https://aistudio.google.com/apikey')}
        >
          <ExternalLink size={15} />
          Get a Gemini API Key
        </button>
        <button
          style={styles.linkBtn}
          onClick={() => BrowserOpenURL('https://github.com')}
        >
          <ExternalLink size={15} />
          View on GitHub
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
      borderBottom: last ? 'none' : '1px solid #1B1D22',
    }}>
      <span style={{ fontSize: 14, color: '#8A8F98', fontFamily: 'var(--font-sans, system-ui)' }}>{label}</span>
      <span style={{ fontSize: 14, color: '#E6E8EC', fontFamily: 'var(--font-mono, monospace)' }}>{value}</span>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  heading: {
    fontSize: 28,
    fontWeight: 700,
    color: '#E6E8EC',
    margin: '0 0 8px',
    fontFamily: 'var(--font-sans, system-ui)',
  },
  subtext: {
    fontSize: 14,
    color: '#8A8F98',
    margin: 0,
    lineHeight: 1.6,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  infoCard: {
    background: '#0F1014',
    border: '1px solid #1B1D22',
    borderRadius: 10,
    overflow: 'hidden',
  },
  linkBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    background: 'transparent',
    border: '1px solid #23252B',
    borderRadius: 8,
    color: '#C8CDD4',
    fontSize: 14,
    padding: '10px 14px',
    cursor: 'pointer',
    textAlign: 'left' as const,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  legal: {
    fontSize: 12,
    color: '#4A4E57',
    margin: 0,
    lineHeight: 1.6,
    fontFamily: 'var(--font-sans, system-ui)',
  },
};
