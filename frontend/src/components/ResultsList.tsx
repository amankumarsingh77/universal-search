import { useRef, useEffect } from 'react';
import { ResultItem } from './ResultItem';
import type { SearchResultDTO } from '../hooks/useSearch';

interface ResultsListProps {
  results: SearchResultDTO[];
  selectedIndex: number;
  onSelect: (index: number) => void;
  onOpen: (filePath: string) => void;
}

export function ResultsList({ results, selectedIndex, onSelect, onOpen }: ResultsListProps) {
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

  if (results.length === 0) {
    return (
      <div style={styles.container}>
        <div style={styles.empty}>
          <span style={styles.emptyIcon}>🔍</span>
          <span style={styles.emptyText}>Type to search your files</span>
        </div>
      </div>
    );
  }

  return (
    <div ref={listRef} style={styles.container}>
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
};
