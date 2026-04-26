// highlight.ts — splits a filename string into matched/unmatched segments
// given a list of highlight ranges.
//
// Assumption: backend byte offsets are produced from ASCII-dominant filenames,
// and we treat them as JS string (UTF-16 code unit) indices. For filenames that
// contain multibyte Unicode characters the highlight may be off — we degrade
// gracefully by clamping out-of-bounds indices and never crashing.

export interface HighlightRange {
  start: number;
  end: number;
}

export interface Segment {
  text: string;
  matched: boolean;
}

// mergeRanges merges overlapping/adjacent ranges and returns them sorted by start.
function mergeRanges(ranges: HighlightRange[], textLen: number): HighlightRange[] {
  if (ranges.length === 0) return [];

  // Clamp and filter degenerate ranges
  const clamped = ranges
    .map((r) => ({
      start: Math.max(0, Math.min(r.start, textLen)),
      end: Math.max(0, Math.min(r.end, textLen)),
    }))
    .filter((r) => r.start < r.end);

  if (clamped.length === 0) return [];

  clamped.sort((a, b) => a.start - b.start);

  const merged: HighlightRange[] = [{ ...clamped[0] }];
  for (let i = 1; i < clamped.length; i++) {
    const last = merged[merged.length - 1];
    const cur = clamped[i];
    if (cur.start <= last.end) {
      // Overlapping or adjacent — extend
      last.end = Math.max(last.end, cur.end);
    } else {
      merged.push({ ...cur });
    }
  }
  return merged;
}

/**
 * splitHighlights splits `text` into alternating matched/unmatched segments.
 * Returns an empty array for empty text, or a single unmatched segment when
 * `ranges` is empty or all ranges are degenerate.
 */
export function splitHighlights(text: string, ranges: HighlightRange[]): Segment[] {
  if (text.length === 0) return [];
  if (ranges.length === 0) return [{ text, matched: false }];

  const merged = mergeRanges(ranges, text.length);
  if (merged.length === 0) return [{ text, matched: false }];

  const segments: Segment[] = [];
  let cursor = 0;

  for (const range of merged) {
    if (cursor < range.start) {
      segments.push({ text: text.slice(cursor, range.start), matched: false });
    }
    segments.push({ text: text.slice(range.start, range.end), matched: true });
    cursor = range.end;
  }

  if (cursor < text.length) {
    segments.push({ text: text.slice(cursor), matched: false });
  }

  return segments;
}
