import { FileSearch, Sparkles } from 'lucide-react';

export type MatchKind = 'filename' | 'content' | 'both';

interface MatchKindIconProps {
  kind: MatchKind;
  size?: number;
}

const KIND_LABELS: Record<MatchKind, string> = {
  filename: 'Matched by filename',
  content: 'Matched by content',
  both: 'Matched by filename and content',
};

export function MatchKindIcon({ kind, size = 13 }: MatchKindIconProps) {
  const color = 'var(--text-tertiary)';
  const label = KIND_LABELS[kind];

  if (kind === 'filename') {
    return (
      <span style={styles.iconWrap} title={label} aria-label={label}>
        <FileSearch size={size} color={color} strokeWidth={1.8} aria-hidden="true" />
      </span>
    );
  }

  if (kind === 'content') {
    return (
      <span style={styles.iconWrap} title={label} aria-label={label}>
        <Sparkles size={size} color={color} strokeWidth={1.8} aria-hidden="true" />
      </span>
    );
  }

  // 'both' — render both icons side by side at slight overlap
  return (
    <span style={styles.bothWrap} title={label} aria-label={label}>
      <span style={styles.bothFirst}>
        <FileSearch size={size} color={color} strokeWidth={1.8} aria-hidden="true" />
      </span>
      <span style={styles.bothSecond}>
        <Sparkles size={size} color={color} strokeWidth={1.8} aria-hidden="true" />
      </span>
    </span>
  );
}

const styles: Record<string, React.CSSProperties> = {
  iconWrap: {
    display: 'inline-flex',
    alignItems: 'center',
    flexShrink: 0,
  },
  bothWrap: {
    display: 'inline-flex',
    alignItems: 'center',
    flexShrink: 0,
    position: 'relative',
  },
  bothFirst: {
    display: 'inline-flex',
    alignItems: 'center',
  },
  bothSecond: {
    display: 'inline-flex',
    alignItems: 'center',
    marginLeft: '-3px',
  },
};
