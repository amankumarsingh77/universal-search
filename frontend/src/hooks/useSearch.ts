import { useReducer, useState, useEffect, useRef, useCallback } from 'react';
import { Search, PreEmbedQuery, ParseQuery, SearchWithFilters } from '../../wailsjs/go/main/App';
import { main } from '../../wailsjs/go/models';
import {
  searchReducer,
  initialSearchState,
  type ChipDTO,
} from '../state/searchReducer';

export type SearchResultDTO = main.SearchResultDTO;

export function useSearch() {
  const [nlState, dispatch] = useReducer(searchReducer, initialSearchState);

  const [results, setResults] = useState<SearchResultDTO[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [isSearching, setIsSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isOffline, setIsOffline] = useState(false);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const preEmbedRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const parseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const searchSeqRef = useRef(0);
  // Track current phase outside reducer to avoid stale closure issues
  const phaseRef = useRef(nlState.phase);
  phaseRef.current = nlState.phase;

  const performSearch = useCallback(async (
    q: string,
    semanticQuery: string,
    chips: ChipDTO[],
    denyList: string[],
  ) => {
    if (!q.trim()) {
      setResults([]);
      setSelectedIndex(0);
      setIsSearching(false);
      return;
    }

    setIsSearching(true);
    setError(null);

    const seq = ++searchSeqRef.current;

    try {
      let res: SearchResultDTO[];
      if (chips.length > 0 || denyList.length > 0) {
        const withFilters = await SearchWithFilters(q, semanticQuery, denyList);
        res = withFilters?.results || [];
        if (withFilters?.relaxationBanner) {
          dispatch({ type: 'BANNER_SET', payload: withFilters.relaxationBanner });
        }
      } else {
        res = await Search(q);
      }

      if (seq !== searchSeqRef.current) return;
      setResults(res || []);
      setSelectedIndex(0);
    } catch (err) {
      if (seq !== searchSeqRef.current) return;
      setError(err instanceof Error ? err.message : String(err));
      setResults([]);
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
    // Allow PARSE_COMPLETE by briefly resetting phase
    dispatch({ type: 'PARSE_COMPLETE', payload: { chips: nlState.chips, semanticQuery: nlState.semanticQuery } });
    runParseQuery(nlState.raw);
  }, [nlState.raw, nlState.chips, nlState.semanticQuery, runParseQuery]);

  // Effect for debounced search + parse query timer (triggered by raw query changes)
  useEffect(() => {
    const q = nlState.raw;

    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (preEmbedRef.current) clearTimeout(preEmbedRef.current);
    if (parseTimerRef.current) clearTimeout(parseTimerRef.current);

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

    // 300ms debounce for search
    debounceRef.current = setTimeout(() => {
      performSearch(q, nlState.semanticQuery, nlState.chips, nlState.chipDenyList);
    }, 300);

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (preEmbedRef.current) clearTimeout(preEmbedRef.current);
      if (parseTimerRef.current) clearTimeout(parseTimerRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nlState.raw]);

  // Re-run search when chips/denyList change (user dismissed a chip)
  useEffect(() => {
    if (nlState.raw.trim()) {
      performSearch(nlState.raw, nlState.semanticQuery, nlState.chips, nlState.chipDenyList);
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
    isOffline,
  };
}
