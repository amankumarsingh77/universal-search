import { ReindexNow } from '../../wailsjs/go/main/App';

interface ReindexBannerProps {
  errorCode: string;
}

export default function ReindexBanner({ errorCode }: ReindexBannerProps) {
  if (errorCode !== 'ERR_MODEL_MISMATCH') return null;

  const handleReindex = () => {
    ReindexNow().catch(() => {});
  };

  return (
    <div data-testid="reindex-banner" style={styles.banner}>
      <span style={styles.message}>
        Your index was built with a different embedding model. Reindex required.
      </span>
      <button type="button" style={styles.button} onClick={handleReindex}>
        Reindex now
      </button>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  banner: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
    padding: '10px 16px',
    margin: '8px 12px',
    borderRadius: 'var(--radius-md, 8px)',
    background: 'var(--surface-warning, rgba(255, 176, 32, 0.12))',
    border: '1px solid var(--border-warning, rgba(255, 176, 32, 0.4))',
    color: 'var(--text-primary, #fff)',
    fontSize: 13,
  },
  message: {
    flex: 1,
  },
  button: {
    padding: '6px 12px',
    borderRadius: 'var(--radius-sm, 6px)',
    background: 'var(--accent, #4f8ef7)',
    color: '#fff',
    border: 'none',
    cursor: 'pointer',
    fontSize: 12,
    fontWeight: 500,
  },
};
