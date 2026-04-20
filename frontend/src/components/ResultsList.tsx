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
  onAddFolder: () => void;
  onSetApiKey: () => void;
}

export function ResultsList({ results, selectedIndex, onSelect, onOpen, hasFolders, hasApiKey, onAddFolder, onSetApiKey }: ResultsListProps) {
  const listRef = useRef<HTMLDivElement>(null);
  const itemHeight = 52;

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

  // No folders (first-launch onboarding)
  if (!hasFolders) {
    return (
      <div style={styles.container}>
        <div style={styles.empty}>
          <span style={styles.emptyText}>No folders indexed yet</span>
          <button style={styles.addFolderBtn} onClick={onAddFolder}>
            Add folder
          </button>
        </div>
      </div>
    );
  }

  // Has folders but no API key
  if (!hasApiKey) {
    return (
      <div style={styles.container}>
        <div style={styles.empty}>
          <span style={styles.emptyText}>Gemini API key not configured</span>
          <span style={styles.emptySubText}>Indexing and search require a valid API key.</span>
          <button style={styles.addFolderBtn} onClick={onSetApiKey}>
            Set API key
          </button>
        </div>
      </div>
    );
  }

  // Results exist
  return (
    <div ref={listRef} style={styles.container} role="listbox" aria-label="Search results">
      {results.map((result, index) => (
        <div
          key={`${result.filePath}-${result.startTime}-${index}`}
          style={{
            animation: 'rowEnter 180ms ease both',
            animationDelay: `${Math.min(index, 7) * 30}ms`,
          }}
        >
          <ResultItem
            result={result}
            isSelected={index === selectedIndex}
            onClick={() => onSelect(index)}
            onDoubleClick={() => onOpen(result.filePath)}
          />
        </div>
      ))}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    height: '100%',
    overflowY: 'auto',
    overflowX: 'hidden',
    padding: '8px',
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
  emptyText: {
    fontSize: '13px',
    color: 'var(--text-secondary)',
    textAlign: 'center',
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
};
