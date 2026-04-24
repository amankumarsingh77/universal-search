import { useReducer, useState, useEffect, useRef, useCallback } from 'react';
import { PreEmbedQuery, ParseQuery, SearchWithFilters } from '../../wailsjs/go/app/App';
import { app } from '../../wailsjs/go/models';
import {
  searchReducer,
  initialSearchState,
  type ChipDTO,
} from '../state/searchReducer';
import { applyClientSideFilters } from '../utils/filterResults';

export type SearchResultDTO = app.SearchResultDTO;

const CLIENT_FILTER_MIN_RESULTS = 5;

export function useSearch() {
  const [nlState, dispatch] = useReducer(searchReducer, initialSearchState);

  const [results, setResults] = useState<SearchResultDTO[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [isSearching, setIsSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isOffline, setIsOffline] = useState(false);
  const [errorCode, setErrorCode] = useState<string>('');
  const [warning, setWarning] = useState<string>('');
  const [retryAfterMs, setRetryAfterMs] = useState<number>(0);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const preEmbedRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const parseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const searchSeqRef = useRef(0);

  // Track current phase outside reducer to avoid stale closure issues
  const phaseRef = useRef(nlState.phase);
  phaseRef.current = nlState.phase;

  // Reflects the current errorCode for use inside debounce callbacks
  const errorCodeRef = useRef('');

  // Holds the raw unfiltered results from the last server fetch.
  // Used for client-side filtering when chips arrive without re-querying the server.
  const unfilteredResultsRef = useRef<SearchResultDTO[]>([]);
  // The semanticQuery that was used for the last server fetch embedding.
  const lastSemanticQueryRef = useRef<string>('');

  // Keep errorCodeRef in sync with state
  useEffect(() => {
    errorCodeRef.current = errorCode;
  }, [errorCode]);

  const performSearch = useCallback(async (
    q: string,
    semanticQuery: string,
    chips: ChipDTO[],
    denyList: string[],
    options: { chipTriggered?: boolean } = {},
  ) => {
    if (!q.trim()) {
      unfilteredResultsRef.current = [];
      lastSemanticQueryRef.current = '';
      setResults([]);
      setSelectedIndex(0);
      setIsSearching(false);
      return;
    }

    // If a parse error is active, skip the search
    if (errorCodeRef.current) return;

    const { chipTriggered = false } = options;

    // Client-side filter path: chips just arrived after the initial search.
    // Try to filter the already-fetched results in-memory to avoid a server round-trip.
    if (chipTriggered && unfilteredResultsRef.current.length > 0) {
      const semanticChanged =
        semanticQuery !== '' && semanticQuery !== lastSemanticQueryRef.current;

      if (!semanticChanged) {
        const filtered = applyClientSideFilters(
          unfilteredResultsRef.current,
          chips,
          denyList,
        );
        if (filtered.length >= CLIENT_FILTER_MIN_RESULTS) {
          // Enough results — use the client-filtered set, skip server call.
          setResults(filtered);
          setSelectedIndex(0);
          return;
        }
        // Too few results after filtering — fall through to server call so
        // the planner can retrieve more results with proper filter pushdown.
      }
      // semanticQuery changed — need new HNSW embedding, fall through to server.
    }

    // Full server call path
    setIsSearching(true);
    setError(null);

    const seq = ++searchSeqRef.current;

    try {
      const withFilters = await SearchWithFilters(q, semanticQuery, denyList);
      const res = withFilters?.results || [];

      if (seq !== searchSeqRef.current) return;

      // Always update the banner — clear it when absent so stale banners don't persist.
      dispatch({ type: 'BANNER_SET', payload: withFilters?.relaxationBanner ?? '' });
      setErrorCode(withFilters?.errorCode ?? '');
      setRetryAfterMs(withFilters?.retryAfterMs ?? 0);
      lastSemanticQueryRef.current = semanticQuery;
      unfilteredResultsRef.current = res;
      setResults(res);
      setSelectedIndex(0);
    } catch (err) {
      if (seq !== searchSeqRef.current) return;
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
      // Backends that surface apperr codes through Wails errors include the
      // code string inside the error message — match it so the banner fires
      // whichever path the backend takes.
      if (msg.includes('ERR_MODEL_MISMATCH')) {
        setErrorCode('ERR_MODEL_MISMATCH');
      }
      setResults([]);
      unfilteredResultsRef.current = [];
    } finally {
      if (seq === searchSeqRef.current) {
        setIsSearching(false);
      }
    }
  }, []);

  const runParseQuery = useCallback(async (q: string) => {
    if (!q.trim()) return;
    try {
      const result = await ParseQuery(q);
      if (result) {
        if (result.errorCode) {
          // Hard failure from parse: block search, surface error
          setErrorCode(result.errorCode);
          setRetryAfterMs(result.retryAfterMs ?? 0);
          setWarning('');
          setResults([]);
          unfilteredResultsRef.current = [];
          errorCodeRef.current = result.errorCode;
          return;
        }

        if (result.warning) {
          // Soft warning: search continues but we surface the warning
          setWarning(result.warning);
          setErrorCode('');
          setRetryAfterMs(0);
          errorCodeRef.current = '';
        } else {
          // All clear
          setWarning('');
          setErrorCode('');
          setRetryAfterMs(0);
          errorCodeRef.current = '';
        }

        dispatch({
          type: 'PARSE_COMPLETE',
          payload: { chips: result.chips || [], semanticQuery: result.semanticQuery || '' },
        });
        setIsOffline(result.isOffline ?? false);
      }
    } catch {
      // Ignore parse errors — fall back to plain search
    }
  }, []);

  const forceParseQuery = useCallback(() => {
    if (parseTimerRef.current) {
      clearTimeout(parseTimerRef.current);
      parseTimerRef.current = null;
    }
    runParseQuery(nlState.raw);
  }, [nlState.raw, runParseQuery]);

  const forceSearch = useCallback(() => {
    // Search-stage errors (ERR_RATE_LIMITED, ERR_EMBED_FAILED) must clear the
    // errorCode gate so performSearch will actually issue the backend call.
    setErrorCode('');
    setRetryAfterMs(0);
    errorCodeRef.current = '';
    performSearch(nlState.raw, nlState.semanticQuery, nlState.chips, nlState.chipDenyList);
  }, [nlState.raw, nlState.semanticQuery, nlState.chips, nlState.chipDenyList, performSearch]);

  // Effect for debounced search + parse query timer (triggered by raw query changes)
  useEffect(() => {
    const q = nlState.raw;

    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (preEmbedRef.current) clearTimeout(preEmbedRef.current);
    if (parseTimerRef.current) clearTimeout(parseTimerRef.current);

    // Clear stale unfiltered results from a previous query
    unfilteredResultsRef.current = [];
    lastSemanticQueryRef.current = '';

    // Clear error state on new keystroke so stale banners don't persist
    setErrorCode('');
    setWarning('');
    setRetryAfterMs(0);
    errorCodeRef.current = '';

    if (q.trim().length >= 3) {
      preEmbedRef.current = setTimeout(() => {
        PreEmbedQuery(q).catch(() => {});
      }, 150);

      // 800ms idle timer for ParseQuery
      parseTimerRef.current = setTimeout(() => {
        // Transition out of typing phase so PARSE_COMPLETE is accepted
        dispatch({
          type: 'PARSE_COMPLETE',
          payload: { chips: [], semanticQuery: '' },
        });
        runParseQuery(q);
      }, 800);
    }

    // 300ms debounce for search — always a full server call (no chips yet)
    debounceRef.current = setTimeout(() => {
      // Skip if a parse error has already fired (rare race, but guards it)
      if (errorCodeRef.current) return;
      performSearch(q, nlState.semanticQuery, nlState.chips, nlState.chipDenyList);
    }, 300);

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (preEmbedRef.current) clearTimeout(preEmbedRef.current);
      if (parseTimerRef.current) clearTimeout(parseTimerRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nlState.raw]);

  // Re-run search when chips/denyList change — try client-side filter first
  useEffect(() => {
    if (nlState.raw.trim() && !errorCodeRef.current) {
      performSearch(
        nlState.raw,
        nlState.semanticQuery,
        nlState.chips,
        nlState.chipDenyList,
        { chipTriggered: true },
      );
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nlState.chips, nlState.chipDenyList]);

  const setQuery = useCallback((q: string) => {
    if (!q) {
      dispatch({ type: 'CLEAR' });
    } else {
      dispatch({ type: 'KEYSTROKE', payload: q });
    }
  }, []);

  const removeChip = useCallback((clauseKey: string) => {
    dispatch({ type: 'CHIP_REMOVED', payload: clauseKey });
  }, []);

  return {
    query: nlState.raw,
    setQuery,
    results,
    selectedIndex,
    setSelectedIndex,
    isSearching,
    error,
    chips: nlState.chips,
    banner: nlState.banner,
    removeChip,
    forceParseQuery,
    forceSearch,
    isOffline,
    errorCode,
    warning,
    retryAfterMs,
  };
}
