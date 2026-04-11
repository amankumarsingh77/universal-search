import { useRef, useEffect } from 'react';
import { ResultItem } from './ResultItem';
import type { SearchResultDTO } from '../hooks/useSearch';

interface ResultsListProps {
  results: SearchResultDTO[];
  selectedIndex: number;
  onSelect: (index: number) => void;
  onOpen: (filePath: string) => void;
  hasFolders: boolean;
  hasApiKey: boolean;
  query: string;
  onAddFolder: () => void;
  onSetApiKey: () => void;
}

export function ResultsList({ results, selectedIndex, onSelect, onOpen, hasFolders, hasApiKey, query, onAddFolder, onSetApiKey }: ResultsListProps) {
  const listRef = useRef<HTMLDivElement>(null);
  const itemHeight = 56;

  // Scroll selected item into view
  useEffect(() => {
    if (!listRef.current) return;
    const container = listRef.current;
    const scrollTop = container.scrollTop;
    const containerHeight = container.clientHeight;
    const itemTop = selectedIndex * itemHeight;
    const itemBottom = itemTop + itemHeight;

    if (itemTop < scrollTop) {
      container.scrollTop = itemTop;
    } else if (itemBottom > scrollTop + containerHeight) {
      container.scrollTop = itemBottom - containerHeight;
    }
  }, [selectedIndex]);

  // No results + query non-empty
  if (query && results.length === 0) {
    return (
      <div style={styles.container}>
        <div style={styles.empty}>
          <span style={styles.emptyIcon}>🔍</span>
          <span style={styles.emptyText}>No results for '{query}'</span>
        </div>
      </div>
    );
  }

  // No query + no folders (first-launch onboarding)
  if (!query && !hasFolders) {
    return (
      <div style={styles.container}>
        <div style={styles.empty}>
          <span style={styles.emptyIcon}>📁</span>
          <span style={styles.emptyText}>No folders indexed yet</span>
          <button style={styles.addFolderBtn} onClick={onAddFolder}>
            Add folder
          </button>
        </div>
      </div>
    );
  }

  // No query + has folders but no API key
  if (!query && results.length === 0 && !hasApiKey) {
    return (
      <div style={styles.container}>
        <div style={styles.empty}>
          <span style={styles.emptyIcon}>🔑</span>
          <span style={styles.emptyText}>Gemini API key not configured</span>
          <span style={styles.emptySubText}>Indexing and search require a valid API key.</span>
          <button style={styles.addFolderBtn} onClick={onSetApiKey}>
            Set API key
          </button>
        </div>
      </div>
    );
  }

  // No query + has folders
  if (!query && results.length === 0) {
    return (
      <div style={styles.container}>
        <div style={styles.empty}>
          <span style={styles.emptyIcon}>🔍</span>
          <span style={styles.emptyText}>Type to search your files</span>
          {!hasApiKey && (
            <span style={styles.apiKeyWarning}>
              No API key set —{' '}
              <button style={styles.apiKeyLink} onClick={onSetApiKey}>
                configure now
              </button>
            </span>
          )}
        </div>
      </div>
    );
  }

  // Results exist
  return (
    <div ref={listRef} style={styles.container}>
      {query && results.length > 0 && (
        <div style={styles.resultCount}>
          Showing {results.length} {results.length === 1 ? 'result' : 'results'}
        </div>
      )}
      {results.map((result, index) => (
        <ResultItem
          key={`${result.filePath}-${result.startTime}-${index}`}
          result={result}
          isSelected={index === selectedIndex}
          onClick={() => onSelect(index)}
          onDoubleClick={() => onOpen(result.filePath)}
        />
      ))}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    width: '290px',
    height: '100%',
    overflowY: 'auto',
    overflowX: 'hidden',
    borderRight: '1px solid var(--border)',
    flexShrink: 0,
  },
  empty: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100%',
    gap: '8px',
    padding: '24px',
  },
  emptyIcon: {
    fontSize: '32px',
    opacity: 0.4,
  },
  emptyText: {
    fontSize: '13px',
    color: 'var(--text-secondary)',
    textAlign: 'center',
  },
  resultCount: {
    padding: '4px 12px',
    fontSize: 11,
    color: 'var(--text-tertiary)',
    borderBottom: '1px solid var(--border)',
  },
  addFolderBtn: {
    color: 'var(--accent-green)',
    background: 'transparent',
    border: '1px solid var(--accent-green)',
    borderRadius: 4,
    padding: '4px 10px',
    cursor: 'pointer',
    fontSize: 12,
    marginTop: 8,
  },
  emptySubText: {
    fontSize: '11px',
    color: 'var(--text-tertiary)',
    textAlign: 'center',
  },
  apiKeyWarning: {
    fontSize: '11px',
    color: 'var(--text-tertiary)',
    marginTop: 4,
  },
  apiKeyLink: {
    background: 'none',
    border: 'none',
    padding: 0,
    color: 'var(--accent-green)',
    cursor: 'pointer',
    fontSize: '11px',
    textDecoration: 'underline',
    fontFamily: 'var(--font-sans)',
  },
};
