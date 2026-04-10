import { useEffect, useState } from 'react';
import { GetFilePreview } from '../../../wailsjs/go/main/App';

interface Props {
  filePath: string;
}

export function TextPreview({ filePath }: Props) {
  const [content, setContent] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [isBinary, setIsBinary] = useState(false);

  useEffect(() => {
    setLoading(true);
    setContent(null);
    setIsBinary(false);
    GetFilePreview(filePath)
      .then(text => { setContent(text); })
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : String(err);
        if (msg.includes('binary')) setIsBinary(true);
        // other errors: show nothing (content stays null)
      })
      .finally(() => setLoading(false));
  }, [filePath]);

  if (loading) {
    return <div style={styles.message}>Loading preview...</div>;
  }
  if (isBinary) {
    return <div style={styles.message}>Binary file — no preview available</div>;
  }
  if (!content) {
    return <div style={styles.message}>Could not read file</div>;
  }

  return (
    <pre style={styles.code}>{content}</pre>
  );
}

const styles: Record<string, React.CSSProperties> = {
  message: {
    padding: '16px',
    color: 'var(--text-tertiary)',
    fontSize: 13,
  },
  code: {
    margin: 0,
    padding: '12px 16px',
    fontFamily: 'monospace',
    fontSize: 12,
    lineHeight: 1.5,
    color: 'var(--text-secondary)',
    backgroundColor: 'var(--bg-surface-2)',
    overflowX: 'auto',
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-all',
    borderRadius: 4,
  },
};
