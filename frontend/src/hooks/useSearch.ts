import { useState, useEffect, useRef, useCallback } from 'react';
import { Search } from '../../wailsjs/go/main/App';
import { main } from '../../wailsjs/go/models';

export type SearchResultDTO = main.SearchResultDTO;

export function useSearch() {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchResultDTO[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [isSearching, setIsSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const performSearch = useCallback(async (q: string) => {
    if (!q.trim()) {
      setResults([]);
      setSelectedIndex(0);
      setIsSearching(false);
      return;
    }

    setIsSearching(true);
    setError(null);

    try {
      const res = await Search(q);
      setResults(res || []);
      setSelectedIndex(0);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setResults([]);
    } finally {
      setIsSearching(false);
    }
  }, []);

  useEffect(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }

    debounceRef.current = setTimeout(() => {
      performSearch(query);
    }, 300);

    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, [query, performSearch]);

  return {
    query,
    setQuery,
    results,
    selectedIndex,
    setSelectedIndex,
    isSearching,
    error,
  };
}
