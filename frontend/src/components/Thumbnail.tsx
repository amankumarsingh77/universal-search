import { useState } from 'react';

interface ThumbnailProps {
  fileType: string;      // "image" | "video" | "audio" | "code" | "text" | "document" | other
  thumbnailPath?: string; // absolute disk path; only meaningful when fileType === "image"
  size?: number;          // default 32
}

// Colors from ResultItem.tsx:68-73 palette
const TYPE_COLORS: Record<string, string> = {
  video: '#06B6D4',
  image: '#F97316',
  audio: '#EC4899',
  code: '#A78BFA',
  text: '#FAFAFA',
  document: '#FAFAFA',
};

function getTypeColor(fileType: string): string {
  return TYPE_COLORS[fileType] ?? 'rgba(255,255,255,0.3)';
}

// Stroke-based SVG icons keyed by file type
const TYPE_ICONS: Record<string, (size: number, color: string) => JSX.Element> = {
  image: (size, color) => {
    const p = size * 0.18;
    const w = size - p * 2;
    const h = size - p * 2;
    const r = size * 0.09;
    // Frame with mountain/sun outline
    const sunCx = p + w * 0.3;
    const sunCy = p + h * 0.35;
    const sunR = w * 0.12;
    // Mountain path: left peak and right peak inside frame
    const mLeft = p + w * 0.08;
    const mRight = p + w * 0.92;
    const mBottom = p + h * 0.8;
    const peak1x = p + w * 0.42;
    const peak1y = p + h * 0.28;
    const peak2x = p + w * 0.75;
    const peak2y = p + h * 0.48;
    return (
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect x={p} y={p} width={w} height={h} rx={r} stroke={color} strokeWidth={size * 0.06} />
        <circle cx={sunCx} cy={sunCy} r={sunR} stroke={color} strokeWidth={size * 0.055} />
        <polyline
          points={`${mLeft},${mBottom} ${peak1x},${peak1y} ${peak2x},${peak2y} ${mRight},${mBottom}`}
          stroke={color}
          strokeWidth={size * 0.055}
          strokeLinejoin="round"
        />
      </svg>
    );
  },

  video: (size, color) => {
    const p = size * 0.12;
    const w = size - p * 2;
    const h = size - p * 2;
    const r = size * 0.18;
    // Play triangle inside rounded rect
    const tx = p + w * 0.38;
    const ty = p + h * 0.28;
    const tbx = p + w * 0.38;
    const tby = p + h * 0.72;
    const trx = p + w * 0.82;
    const try_ = size / 2;
    return (
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect x={p} y={p} width={w} height={h} rx={r} stroke={color} strokeWidth={size * 0.06} />
        <polygon
          points={`${tx},${ty} ${tbx},${tby} ${trx},${try_}`}
          stroke={color}
          strokeWidth={size * 0.055}
          strokeLinejoin="round"
          fill={color}
          fillOpacity={0.35}
        />
      </svg>
    );
  },

  audio: (size, color) => {
    // Simple waveform: 5 vertical bars of varying height
    const barW = size * 0.08;
    const gap = size * 0.055;
    const totalW = 5 * barW + 4 * gap;
    const startX = (size - totalW) / 2;
    const cy = size / 2;
    const heights = [0.28, 0.55, 0.7, 0.45, 0.25].map(h => h * size);
    return (
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} fill="none" xmlns="http://www.w3.org/2000/svg">
        {heights.map((h, i) => {
          const x = startX + i * (barW + gap);
          const r = barW / 2;
          return (
            <rect
              key={i}
              x={x}
              y={cy - h / 2}
              width={barW}
              height={h}
              rx={r}
              fill={color}
              fillOpacity={0.85}
            />
          );
        })}
      </svg>
    );
  },

  code: (size, color) => {
    // </> as paths: left chevron (<), slash (/), right chevron (>)
    const sw = size * 0.065;
    const cy = size / 2;
    // Left chevron <
    const lx1 = size * 0.38; const ly1 = size * 0.28;
    const lx2 = size * 0.22; const ly2 = cy;
    const lx3 = size * 0.38; const ly3 = size * 0.72;
    // Slash /
    const sx1 = size * 0.57; const sy1 = size * 0.25;
    const sx2 = size * 0.43; const sy2 = size * 0.75;
    // Right chevron >
    const rx1 = size * 0.62; const ry1 = size * 0.28;
    const rx2 = size * 0.78; const ry2 = cy;
    const rx3 = size * 0.62; const ry3 = size * 0.72;
    return (
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} fill="none" xmlns="http://www.w3.org/2000/svg">
        <polyline points={`${lx1},${ly1} ${lx2},${ly2} ${lx3},${ly3}`} stroke={color} strokeWidth={sw} strokeLinecap="round" strokeLinejoin="round" />
        <line x1={sx1} y1={sy1} x2={sx2} y2={sy2} stroke={color} strokeWidth={sw} strokeLinecap="round" />
        <polyline points={`${rx1},${ry1} ${rx2},${ry2} ${rx3},${ry3}`} stroke={color} strokeWidth={sw} strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  },

  text: (size, color) => {
    // Three horizontal lines (file-lines glyph)
    const sw = size * 0.065;
    const lx1 = size * 0.22;
    const lx2 = size * 0.78;
    const y1 = size * 0.35;
    const y2 = size * 0.5;
    const y3 = size * 0.65;
    return (
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} fill="none" xmlns="http://www.w3.org/2000/svg">
        <line x1={lx1} y1={y1} x2={lx2} y2={y1} stroke={color} strokeWidth={sw} strokeLinecap="round" />
        <line x1={lx1} y1={y2} x2={lx2} y2={y2} stroke={color} strokeWidth={sw} strokeLinecap="round" />
        <line x1={lx1} y1={y3} x2={size * 0.58} y2={y3} stroke={color} strokeWidth={sw} strokeLinecap="round" />
      </svg>
    );
  },
};

// Classic file-with-folded-corner outline
function FileIcon(size: number, color: string, opacity = 1): JSX.Element {
  const sw = size * 0.065;
  const pl = size * 0.2;
  const pr = size * 0.8;
  const pt = size * 0.1;
  const pb = size * 0.9;
  const fold = size * 0.25;
  // Main outline with top-right fold
  const d = [
    `M ${pl} ${pt}`,
    `L ${pr - fold} ${pt}`,
    `L ${pr} ${pt + fold}`,
    `L ${pr} ${pb}`,
    `L ${pl} ${pb}`,
    `Z`,
  ].join(' ');
  // Fold crease
  const fc = [
    `M ${pr - fold} ${pt}`,
    `L ${pr - fold} ${pt + fold}`,
    `L ${pr} ${pt + fold}`,
  ].join(' ');
  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} fill="none" xmlns="http://www.w3.org/2000/svg" style={{ opacity }}>
      <path d={d} stroke={color} strokeWidth={sw} strokeLinejoin="round" />
      <path d={fc} stroke={color} strokeWidth={sw} strokeLinejoin="round" />
    </svg>
  );
}

TYPE_ICONS['document'] = (size, color) => FileIcon(size, color, 1);

function getIcon(fileType: string, size: number, color: string): JSX.Element {
  const fn = TYPE_ICONS[fileType];
  if (fn) return fn(size, color);
  return FileIcon(size, color, 0.45);
}

export function Thumbnail({ fileType, thumbnailPath, size = 32 }: ThumbnailProps) {
  const [imgError, setImgError] = useState(false);

  const color = getTypeColor(fileType);
  const showImg = fileType === 'image' && thumbnailPath && !imgError;

  if (showImg) {
    return (
      <div style={{ ...styles.tile(size, color), overflow: 'hidden', background: 'transparent', border: 'none' }}>
        <img
          src={`file://${encodeURI(thumbnailPath!)}`}
          alt=""
          onError={() => setImgError(true)}
          style={{
            width: size,
            height: size,
            objectFit: 'cover',
            borderRadius: size * 0.19,
            display: 'block',
          }}
        />
      </div>
    );
  }

  return (
    <div style={styles.tile(size, color)}>
      {getIcon(fileType, size * 0.65, color)}
    </div>
  );
}

const styles = {
  tile: (size: number, color: string): React.CSSProperties => ({
    width: size,
    height: size,
    borderRadius: size * 0.19,
    background: `${color}1A`,
    border: `1px solid ${color}33`,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
  }),
};
