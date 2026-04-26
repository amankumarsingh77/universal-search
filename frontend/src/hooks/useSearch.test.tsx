import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useSearch } from './useSearch';

// Mock Wails bindings
vi.mock('../../wailsjs/go/app/App', () => ({
  PreEmbedQuery: vi.fn(() => Promise.resolve()),
  ParseQuery: vi.fn(),
  SearchWithFilters: vi.fn(),
  GetFolders: vi.fn(() => Promise.resolve([])),
}));

// Mock Wails runtime
vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => vi.fn()),
  EventsOff: vi.fn(),
}));

import { ParseQuery, SearchWithFilters } from '../../wailsjs/go/app/App';

const mockParseQuery = ParseQuery as ReturnType<typeof vi.fn>;
const mockSearchWithFilters = SearchWithFilters as ReturnType<typeof vi.fn>;

describe('useSearch', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: false });
    mockParseQuery.mockReset();
    mockSearchWithFilters.mockReset();
    // Default: search returns empty results with no error
    mockSearchWithFilters.mockResolvedValue({ results: [], relaxationBanner: '', errorCode: '' });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('sets errorCode when ParseQuery returns ERR_QUERY_PARSE_FAILED and clears results', async () => {
    mockParseQuery.mockResolvedValue({
      errorCode: 'ERR_QUERY_PARSE_FAILED',
      chips: [],
      semanticQuery: '',
      hasFilters: false,
      cacheHit: false,
      isOffline: false,
    });

    const { result } = renderHook(() => useSearch());

    // Set the query
    act(() => { result.current.setQuery('hello world'); });

    // Advance past all timers (300ms debounce + 800ms parse)
    await act(async () => { await vi.runAllTimersAsync(); });

    expect(result.current.errorCode).toBe('ERR_QUERY_PARSE_FAILED');
    expect(result.current.results).toHaveLength(0);
  });

  it('sets retryAfterMs when ParseQuery returns ERR_QUERY_RATE_LIMITED', async () => {
    mockParseQuery.mockResolvedValue({
      errorCode: 'ERR_QUERY_RATE_LIMITED',
      retryAfterMs: 5000,
      chips: [],
      semanticQuery: '',
      hasFilters: false,
      cacheHit: false,
      isOffline: false,
    });

    const { result } = renderHook(() => useSearch());

    act(() => { result.current.setQuery('invoices last week'); });

    await act(async () => { await vi.runAllTimersAsync(); });

    expect(result.current.errorCode).toBe('ERR_QUERY_RATE_LIMITED');
    expect(result.current.retryAfterMs).toBe(5000);
  });

  it('sets warning and continues to call SearchWithFilters when ParseQuery returns warning', async () => {
    const fakeResult = {
      filePath: '/a/b.txt',
      fileName: 'b.txt',
      fileType: 'text',
      extension: 'txt',
      sizeBytes: 100,
      thumbnailPath: '',
      startTime: 0,
      endTime: 0,
      score: 0.9,
      modifiedAt: 0,
    };
    mockParseQuery.mockResolvedValue({
      warning: 'query_parse_timeout',
      chips: [],
      semanticQuery: 'hello',
      hasFilters: false,
      cacheHit: false,
      isOffline: false,
    });
    mockSearchWithFilters.mockResolvedValue({
      results: [fakeResult],
      relaxationBanner: '',
      errorCode: '',
    });

    const { result } = renderHook(() => useSearch());

    act(() => { result.current.setQuery('hello world'); });

    await act(async () => { await vi.runAllTimersAsync(); });

    expect(result.current.warning).toBe('query_parse_timeout');
    expect(result.current.errorCode).toBe('');
    expect(mockSearchWithFilters).toHaveBeenCalled();
  });

  it('propagates errorCode from SearchWithFilters when parse succeeds', async () => {
    mockParseQuery.mockResolvedValue({
      chips: [],
      semanticQuery: 'cats',
      hasFilters: false,
      cacheHit: false,
      isOffline: false,
    });
    mockSearchWithFilters.mockResolvedValue({
      results: [],
      relaxationBanner: '',
      errorCode: 'ERR_EMBED_FAILED',
    });

    const { result } = renderHook(() => useSearch());

    act(() => { result.current.setQuery('cats'); });

    await act(async () => { await vi.runAllTimersAsync(); });

    expect(result.current.errorCode).toBe('ERR_EMBED_FAILED');
  });

  it('clears errorCode when query changes', async () => {
    mockParseQuery.mockResolvedValueOnce({
      errorCode: 'ERR_QUERY_PARSE_FAILED',
      chips: [],
      semanticQuery: '',
      hasFilters: false,
      cacheHit: false,
      isOffline: false,
    });
    mockSearchWithFilters.mockResolvedValue({ results: [], relaxationBanner: '', errorCode: '' });

    const { result } = renderHook(() => useSearch());

    act(() => { result.current.setQuery('bad query'); });
    await act(async () => { await vi.runAllTimersAsync(); });

    // errorCode was set
    expect(result.current.errorCode).toBe('ERR_QUERY_PARSE_FAILED');

    // Now change query — errorCode should clear immediately
    mockParseQuery.mockResolvedValue({
      chips: [],
      semanticQuery: 'new',
      hasFilters: false,
      cacheHit: false,
      isOffline: false,
    });

    act(() => { result.current.setQuery('new query'); });

    // errorCode should be cleared synchronously by the keystroke effect
    expect(result.current.errorCode).toBe('');
  });

  it('issues exactly one SearchWithFilters call when ParseQuery resolves', async () => {
    mockParseQuery.mockResolvedValue({
      chips: [
        { clauseKey: 'file_type=video', label: 'video', field: 'file_type', op: 'eq', value: 'video' },
      ],
      semanticQuery: 'bowling',
      hasFilters: true,
      cacheHit: false,
      isOffline: false,
    });

    const { result } = renderHook(() => useSearch());

    act(() => { result.current.setQuery('bowling video'); });

    await act(async () => { await vi.runAllTimersAsync(); });

    // Pre-fix this would be 2: one from the 300ms debounce (stale state) and
    // one from the chip-change effect after parse completes.
    expect(mockSearchWithFilters).toHaveBeenCalledTimes(1);
  });
});
