import { describe, it, expect } from 'vitest';
import { splitHighlights, type HighlightRange, type Segment } from './highlight';

describe('splitHighlights', () => {
  it('returns empty array for empty text', () => {
    expect(splitHighlights('', [])).toEqual([]);
  });

  it('returns single unmatched segment when no ranges', () => {
    expect(splitHighlights('hello.txt', [])).toEqual([
      { text: 'hello.txt', matched: false },
    ]);
  });

  it('highlights a full match', () => {
    const result = splitHighlights('hello', [{ start: 0, end: 5 }]);
    expect(result).toEqual([{ text: 'hello', matched: true }]);
  });

  it('highlights a middle range', () => {
    const result = splitHighlights('report_2024_final.pdf', [{ start: 7, end: 11 }]);
    expect(result).toEqual([
      { text: 'report_', matched: false },
      { text: '2024', matched: true },
      { text: '_final.pdf', matched: false },
    ]);
  });

  it('highlights a leading range', () => {
    const result = splitHighlights('budget.xlsx', [{ start: 0, end: 6 }]);
    expect(result).toEqual([
      { text: 'budget', matched: true },
      { text: '.xlsx', matched: false },
    ]);
  });

  it('highlights a trailing range', () => {
    const result = splitHighlights('budget.xlsx', [{ start: 7, end: 11 }]);
    expect(result).toEqual([
      { text: 'budget.', matched: false },
      { text: 'xlsx', matched: true },
    ]);
  });

  it('merges overlapping ranges', () => {
    const ranges: HighlightRange[] = [
      { start: 2, end: 6 },
      { start: 4, end: 9 },
    ];
    const result = splitHighlights('hello_world', ranges);
    expect(result).toEqual([
      { text: 'he', matched: false },
      { text: 'llo_wor', matched: true },
      { text: 'ld', matched: false },
    ]);
  });

  it('merges adjacent ranges', () => {
    const ranges: HighlightRange[] = [
      { start: 0, end: 3 },
      { start: 3, end: 6 },
    ];
    const result = splitHighlights('abcdef.txt', ranges);
    expect(result).toEqual([
      { text: 'abcdef', matched: true },
      { text: '.txt', matched: false },
    ]);
  });

  it('handles multiple non-overlapping ranges', () => {
    const ranges: HighlightRange[] = [
      { start: 0, end: 3 },
      { start: 5, end: 8 },
    ];
    const result = splitHighlights('abc_def_ghi', ranges);
    expect(result).toEqual([
      { text: 'abc', matched: true },
      { text: '_d', matched: false },
      { text: 'ef_', matched: true },
      { text: 'ghi', matched: false },
    ]);
  });

  it('clamps out-of-bounds end', () => {
    const result = splitHighlights('hello', [{ start: 2, end: 100 }]);
    expect(result).toEqual([
      { text: 'he', matched: false },
      { text: 'llo', matched: true },
    ]);
  });

  it('clamps out-of-bounds start', () => {
    // start > textLen: the range is degenerate after clamping
    const result = splitHighlights('hello', [{ start: 10, end: 20 }]);
    expect(result).toEqual([{ text: 'hello', matched: false }]);
  });

  it('handles degenerate range (start == end after clamping)', () => {
    const result = splitHighlights('hello', [{ start: 3, end: 3 }]);
    expect(result).toEqual([{ text: 'hello', matched: false }]);
  });

  it('handles unsorted ranges', () => {
    const ranges: HighlightRange[] = [
      { start: 6, end: 9 },
      { start: 0, end: 3 },
    ];
    const result = splitHighlights('abc_def_ghi', ranges);
    expect(result).toEqual([
      { text: 'abc', matched: true },
      { text: '_de', matched: false },
      { text: 'f_g', matched: true },
      { text: 'hi', matched: false },
    ]);
  });

  it('produces only matched segments when ranges cover entire string', () => {
    const result = splitHighlights('file.txt', [{ start: 0, end: 8 }]);
    expect(result).toEqual([{ text: 'file.txt', matched: true }]);
  });

  it('returns array with Segment shape', () => {
    const result: Segment[] = splitHighlights('test', [{ start: 1, end: 3 }]);
    for (const seg of result) {
      expect(typeof seg.text).toBe('string');
      expect(typeof seg.matched).toBe('boolean');
    }
  });
});
