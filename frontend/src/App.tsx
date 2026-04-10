import { useState, useCallback, useEffect } from 'react';
import { SearchBar } from './components/SearchBar';
import { ResultsList } from './components/ResultsList';
import { PreviewPanel } from './components/PreviewPanel';
import { IndexingBar } from './components/IndexingBar';
import { FolderManager } from './components/FolderManager';
import { useSearch } from './hooks/useSearch';
import { useIndexingStatus } from './hooks/useIndexingStatus';
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime';
import { OpenFile, OpenFolder, HideWindow, GetFolders } from '../wailsjs/go/main/App';

function App() {
  const {
    query,
    setQuery,
    results,
    selectedIndex,
    setSelectedIndex,
    isSearching,
  } = useSearch();

  const indexingStatus = useIndexingStatus();

  const [showFolderManager, setShowFolderManager] = useState(false);
  const [hasFolders, setHasFolders] = useState(true);

  useEffect(() => {
    GetFolders().then(folders => setHasFolders(folders.length > 0));
  }, []);

  useEffect(() => {
    const _cancel = EventsOn('open-folder-manager', () => {
      setShowFolderManager(true);
    });
    return () => {
      EventsOff('open-folder-manager');
    };
  }, []);

  useEffect(() => {
    const _cancel = EventsOn('window-shown', () => {
      const input = document.querySelector('input[type="text"]') as HTMLInputElement;
      if (input) input.focus();
    });
    return () => {
      EventsOff('window-shown');
    };
  }, []);

  const selectedResult = results.length > 0 ? results[selectedIndex] ?? null : null;

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === 'C') {
        e.preventDefault();
        if (results[selectedIndex]) {
          navigator.clipboard.writeText(results[selectedIndex].filePath);
        }
        return;
      }

      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          setSelectedIndex((prev: number) =>
            Math.min(prev + 1, results.length - 1)
          );
          break;

        case 'ArrowUp':
          e.preventDefault();
          setSelectedIndex((prev: number) => Math.max(prev - 1, 0));
          break;

        case 'Enter':
          if (selectedResult) {
            if (e.ctrlKey || e.metaKey) {
              OpenFolder(selectedResult.filePath);
            } else {
              OpenFile(selectedResult.filePath);
            }
            HideWindow();
          }
          break;

        case 'Escape':
          if (query) {
            setQuery('');
          } else {
            HideWindow();
          }
          break;
      }
    },
    [results, selectedIndex, selectedResult, query, setQuery, setSelectedIndex]
  );

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  return (
    <div style={styles.root}>
      <SearchBar
        query={query}
        onQueryChange={setQuery}
        isSearching={isSearching}
      />
      <div style={styles.body}>
        <ResultsList
          results={results}
          selectedIndex={selectedIndex}
          onSelect={setSelectedIndex}
          onOpen={(path) => { OpenFile(path); HideWindow(); }}
          hasFolders={hasFolders}
          query={query}
          onAddFolder={() => setShowFolderManager(true)}
        />
        <PreviewPanel result={selectedResult} onOpenFolder={(path) => { OpenFolder(path); HideWindow(); }} />
      </div>
      <IndexingBar status={indexingStatus} />
      {showFolderManager && (
        <FolderManager onClose={() => setShowFolderManager(false)} />
      )}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  root: {
    display: 'flex',
    flexDirection: 'column',
    height: '100vh',
    width: '100vw',
    background: 'var(--bg-base)',
    overflow: 'hidden',
  },
  body: {
    display: 'flex',
    flex: 1,
    overflow: 'hidden',
  },
};

export default App;
