import React, { useRef, useEffect } from 'react';
import { FilterChip } from './FilterChip';
import { FileSearch } from 'lucide-react';
import type { ChipDTO } from '../state/searchReducer';

interface SearchBarProps {
  query: string;
  onQueryChange: (query: string) => void;
  isSearching: boolean;
  isOffline: boolean;
  chips?: ChipDTO[];
  onChipRemove?: (clauseKey: string) => void;
  banner?: string | null;
  onForceParseQuery?: () => void;
  warningChip?: React.ReactNode;
  rightSlot?: React.ReactNode;
}

export function SearchBar({
  query,
  onQueryChange,
  isSearching,
  isOffline,
  chips = [],
  onChipRemove,
  banner,
  onForceParseQuery,
  warningChip,
  rightSlot,
}: SearchBarProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const hasFilenamePrefix = query.toLowerCase().startsWith('f:');

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  return (
    <div style={styles.wrapper}>
      <div style={styles.container} {...{ '--wails-draggable': 'drag' } as React.HTMLAttributes<HTMLDivElement>}>
        <div style={styles.icon}>
          {isSearching ? (
            <svg width="20" height="20" viewBox="0 0 20 20" style={styles.spinner}>
              <circle cx="10" cy="10" r="8" fill="none" stroke="var(--text-secondary)" strokeWidth="2" strokeDasharray="40" strokeDashoffset="10" />
            </svg>
          ) : (
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
              <path
                d="M9 3.5a5.5 5.5 0 100 11 5.5 5.5 0 000-11zM2 9a7 7 0 1112.452 4.391l3.328 3.329a.75.75 0 11-1.06 1.06l-3.329-3.328A7 7 0 012 9z"
                fill="var(--text-secondary)"
              />
            </svg>
          )}
          {isOffline && (
            <span style={styles.offlineDot} title="Offline — filename search only" />
          )}
        </div>
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && onForceParseQuery) {
              onForceParseQuery();
            }
          }}
          placeholder="Search files, images, videos…"
          style={styles.input}
          spellCheck={false}
          autoComplete="off"
          {...{ '--wails-draggable': 'no-drag' } as React.HTMLAttributes<HTMLInputElement>}
        />
        {query && (
          <button
            onClick={() => onQueryChange('')}
            style={styles.clearButton}
            title="Clear search"
            aria-label="Clear search"
            {...{ '--wails-draggable': 'no-drag' } as React.HTMLAttributes<HTMLButtonElement>}
          >
            <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
              <path d="M3.5 3.5l7 7M10.5 3.5l-7 7" stroke="var(--text-secondary)" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        )}
        {rightSlot}
      </div>
      {(chips.length > 0 || warningChip || hasFilenamePrefix) && (
        <div style={styles.chipRow}>
          {hasFilenamePrefix && (
            <span style={styles.filenameChip} aria-label="Filenames only mode">
              <FileSearch size={11} strokeWidth={2} aria-hidden="true" style={{ flexShrink: 0 }} />
              Filenames only
            </span>
          )}
          {chips.map(chip => (
            <FilterChip
              key={chip.clauseKey}
              label={chip.label}
              clauseKey={chip.clauseKey}
              onRemove={onChipRemove ?? (() => {})}
            />
          ))}
          {warningChip}
        </div>
      )}
      {banner && (
        <div style={styles.banner}>{banner}</div>
      )}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  wrapper: {
    display: 'flex',
    flexDirection: 'column',
    borderBottom: '1px solid rgba(255,255,255,0.08)',
    flexShrink: 0,
  },
  container: {
    display: 'flex',
    alignItems: 'center',
    padding: '0 16px',
    height: '64px',
    gap: '12px',
  },
  icon: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    position: 'relative',
  },
  offlineDot: {
    position: 'absolute',
    bottom: -2,
    right: -2,
    width: 8,
    height: 8,
    borderRadius: '50%',
    background: 'var(--text-tertiary)',
    border: '1.5px solid var(--bg-base)',
  },
  spinner: {
    animation: 'spin 1s linear infinite',
  },
  input: {
    flex: 1,
    background: 'transparent',
    border: 'none',
    outline: 'none',
    color: 'var(--text-primary)',
    fontSize: '22px',
    fontWeight: 300,
    fontFamily: 'var(--font-sans)',
    lineHeight: '24px',
  },
  clearButton: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'var(--bg-surface-2)',
    border: 'none',
    borderRadius: 'var(--radius-sm)',
    cursor: 'pointer',
    padding: '4px',
    flexShrink: 0,
  },
  chipRow: {
    display: 'flex',
    flexWrap: 'wrap',
    padding: '0 12px 6px',
    overflow: 'hidden',
  },
  filenameChip: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '4px',
    padding: '3px 10px',
    borderRadius: '100px',
    background: 'rgba(255,255,255,0.10)',
    backdropFilter: 'blur(8px)',
    WebkitBackdropFilter: 'blur(8px)',
    border: '1px solid rgba(255,255,255,0.15)',
    fontSize: '12px',
    color: 'rgba(255,255,255,0.85)',
    margin: '2px',
  },
  banner: {
    padding: '4px 16px 6px',
    fontSize: '12px',
    color: 'rgba(255,255,255,0.6)',
    fontStyle: 'italic',
  },
};
