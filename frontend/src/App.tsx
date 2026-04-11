import { useState, useCallback, useEffect, useRef } from 'react';
import { SearchBar } from './components/SearchBar';
import { ResultsList } from './components/ResultsList';
import { PreviewPanel } from './components/PreviewPanel';
import { IndexingBar } from './components/IndexingBar';
import { FolderManager } from './components/FolderManager';
import ApiKeyDialog from './components/ApiKeyDialog';
import Toast from './components/Toast';
import { useSearch } from './hooks/useSearch';
import { useIndexingStatus } from './hooks/useIndexingStatus';
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime';
import { OpenFile, OpenFolder, HideWindow, GetFolders, GetHasGeminiKey } from '../wailsjs/go/main/App';

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

  const [indexingDismissed, setIndexingDismissed] = useState(false);
  const prevIsRunningRef = useRef(false);

  useEffect(() => {
    if (indexingStatus.isRunning && !prevIsRunningRef.current) {
      // New indexing run started — reset dismissed state so bar reappears
      setIndexingDismissed(false);
    }
    prevIsRunningRef.current = indexingStatus.isRunning;
  }, [indexingStatus.isRunning]);

  const [showFolderManager, setShowFolderManager] = useState(false);
  const [hasFolders, setHasFolders] = useState(true);
  const [hasApiKey, setHasApiKey] = useState(true);
  const [showApiKeyDialog, setShowApiKeyDialog] = useState(false);
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null);

  useEffect(() => {
    GetFolders().then(folders => setHasFolders(folders.length > 0));
    GetHasGeminiKey().then(setHasApiKey).catch(() => setHasApiKey(false));
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
    const _cancel = EventsOn('open-api-key-dialog', () => {
      setShowApiKeyDialog(true);
    });
    return () => {
      EventsOff('open-api-key-dialog');
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
          hasApiKey={hasApiKey}
          query={query}
          onAddFolder={() => setShowFolderManager(true)}
          onSetApiKey={() => setShowApiKeyDialog(true)}
        />
        <PreviewPanel result={selectedResult} onOpenFolder={(path) => { OpenFolder(path); HideWindow(); }} />
      </div>
      {!indexingDismissed && (
        <IndexingBar
          status={indexingStatus}
          onDismiss={() => setIndexingDismissed(true)}
        />
      )}
      {showFolderManager && (
        <FolderManager onClose={() => setShowFolderManager(false)} />
      )}
      {showApiKeyDialog && (
        <ApiKeyDialog
          onClose={() => setShowApiKeyDialog(false)}
          onSuccess={() => { setHasApiKey(true); setToast({ message: 'Gemini client initialized successfully', type: 'success' }); }}
          onError={(msg) => setToast({ message: msg, type: 'error' })}
        />
      )}
      {toast && (
        <Toast
          message={toast.message}
          type={toast.type}
          onDismiss={() => setToast(null)}
        />
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
