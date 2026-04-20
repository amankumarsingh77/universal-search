interface EmptyStateProps {
  variant: 'no-query' | 'no-results';
  query?: string;
}

function SearchIcon() {
  return (
    <svg
      width={48}
      height={48}
      viewBox="0 0 48 48"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      style={{ opacity: 0.6 }}
    >
      <circle
        cx="21"
        cy="21"
        r="13"
        stroke="var(--text-tertiary)"
        strokeWidth="2.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <line
        x1="30.5"
        y1="30.5"
        x2="40"
        y2="40"
        stroke="var(--text-tertiary)"
        strokeWidth="2.5"
        strokeLinecap="round"
      />
    </svg>
  );
}

export function EmptyState({ variant, query }: EmptyStateProps) {
  const primaryText =
    variant === 'no-query' ? (
      'Type to search your files'
    ) : (
      <>No results for &ldquo;{query}&rdquo;</>
    );

  const secondaryText =
    variant === 'no-query'
      ? 'Try: "photos from last week"'
      : 'Try a different search or check your indexed folders';

  return (
    <div style={styles.container}>
      <SearchIcon />
      <span style={styles.primary}>{primaryText}</span>
      <span style={styles.secondary}>{secondaryText}</span>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'column',
    gap: 12,
    padding: '40px 20px',
    textAlign: 'center',
  },
  primary: {
    fontSize: 15,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-sans)',
  },
  secondary: {
    fontSize: 12,
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-sans)',
  },
};
