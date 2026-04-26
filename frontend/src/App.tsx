import { useState, useCallback, useEffect, useRef } from 'react';
import { SearchBar } from './components/SearchBar';
import { ResultsList } from './components/ResultsList';
import { EmptyState } from './components/EmptyState';
import { PreviewPanel } from './components/PreviewPanel';
import { IndexingBar } from './components/IndexingBar';
import { FailuresModal } from './components/FailuresModal';
import { FolderManager } from './components/FolderManager';
import ApiKeyDialog from './components/ApiKeyDialog';
import Toast from './components/Toast';
import ReindexBanner from './components/ReindexBanner';
import ErrorBanner from './components/ErrorBanner';
import WarningChip from './components/WarningChip';
import { OnboardingOverlay } from './components/OnboardingOverlay';
import { SettingsButton } from './components/SettingsButton';
import { SettingsPanel } from './components/SettingsPanel';
import type { SettingsTab } from './components/SettingsPanel';
import { useSearch } from './hooks/useSearch';
import { useIndexingStatus } from './hooks/useIndexingStatus';
import { useHideSuppression } from './hooks/useHideSuppression';
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime';
import { OpenFile, OpenFolder, HideWindow, GetFolders, GetHasGeminiKey, GetOnboarded, MarkOnboarded, ReindexNow } from '../wailsjs/go/app/App';

function App() {
  const {
    query,
    setQuery,
    results,
    selectedIndex,
    setSelectedIndex,
    isSearching,
    chips,
    banner,
    removeChip,
    forceParseQuery,
    forceSearch,
    isOffline,
    errorCode,
    warning,
    retryAfterMs,
  } = useSearch();

  const indexingStatus = useIndexingStatus();

  const [indexingDismissed, setIndexingDismissed] = useState(false);
  const [showFailuresModal, setShowFailuresModal] = useState(false);
  const prevIsRunningRef = useRef(false);

  useEffect(() => {
    if (indexingStatus.isRunning && !prevIsRunningRef.current) {
      // New indexing run started — reset dismissed state so bar reappears
      setIndexingDismissed(false);
      // Close failures modal so stale content is not shown (REQ-056)
      setShowFailuresModal(false);
    }
    prevIsRunningRef.current = indexingStatus.isRunning;
  }, [indexingStatus.isRunning]);

  const [onboarded, setOnboarded] = useState(true);
  const [showFolderManager, setShowFolderManager] = useState(false);
  const [hasFolders, setHasFolders] = useState(true);
  const [hasApiKey, setHasApiKey] = useState(true);
  const [showApiKeyDialog, setShowApiKeyDialog] = useState(false);
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null);

  // Settings panel state
  const [showSettings, setShowSettings] = useState(false);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('folders');

  useEffect(() => {
    GetFolders().then(folders => setHasFolders(folders.length > 0));
    GetHasGeminiKey().then(setHasApiKey).catch(() => setHasApiKey(false));
    GetOnboarded().then(v => setOnboarded(v));
  }, []);

  useEffect(() => {
    const _cancel = EventsOn('open-folder-manager', () => {
      setSettingsTab('folders');
      setShowSettings(true);
    });
    return () => {
      EventsOff('open-folder-manager');
    };
  }, []);

  useEffect(() => {
    const _cancel = EventsOn('open-api-key-dialog', () => {
      setSettingsTab('api-key');
      setShowSettings(true);
    });
    return () => {
      EventsOff('open-api-key-dialog');
    };
  }, []);

  useEffect(() => {
    const _cancel = EventsOn('window-shown', () => {
      // When the global hotkey re-shows the window over an open settings
      // panel, do not yank focus to the search input — leave whatever the
      // user was on focused so the panel survives intact.
      const w = window as unknown as Record<string, unknown>;
      if (w.__settingsPanelOpen) return;
      const input = document.querySelector('input[type="text"]') as HTMLInputElement;
      if (input) input.focus();
    });
    return () => {
      EventsOff('window-shown');
    };
  }, []);

  useEffect(() => {
    const _cancel = EventsOn('backend-error', (payload: { code?: string; message?: string }) => {
      const code = payload?.code ?? 'ERR_INTERNAL';
      const message = payload?.message ?? 'An unexpected error occurred';
      setToast({ message: `${message} (${code})`, type: 'error' });
    });
    return () => {
      EventsOff('backend-error');
    };
  }, []);

  const selectedResult = results.length > 0 ? results[selectedIndex] ?? null : null;
  const previewOpen = selectedResult !== null;

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      // While the settings panel is open, the panel owns keyboard input
      // (Escape closes it, no Arrow/Enter shortcuts apply, and Cmd-R is
      // not a re-index gesture from this layer). Settings has its own
      // capture-phase Escape listener; this guard prevents stale handlers
      // from this component firing on top of it.
      if (showSettings) return;

      if ((e.metaKey || e.ctrlKey) && e.key === 'r') {
        e.preventDefault();
        ReindexNow();
        setToast({ message: 'Re-indexing all folders…', type: 'success' });
        return;
      }

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
    [results, selectedIndex, selectedResult, query, setQuery, setSelectedIndex, showSettings]
  );

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  const { isSuppressed } = useHideSuppression();
  useEffect(() => {
    // Skip the auto-hide while a native OS dialog (e.g. directory picker) is
    // open. The dialog steals focus, fires blur on the Wails window, and then
    // the modal sheet gets dismissed along with its now-hidden parent —
    // exactly the bug users reported with "Add folder".
    // Also skip if settings panel or popover is open.
    const handleBlur = () => {
      if (isSuppressed()) return;
      const w = window as unknown as Record<string, unknown>;
      if (w.__settingsPanelOpen) return;
      HideWindow();
    };
    window.addEventListener('blur', handleBlur);
    return () => window.removeEventListener('blur', handleBlur);
  }, [isSuppressed]);

  return (
    <div style={styles.root}>
      <SearchBar
        query={query}
        onQueryChange={setQuery}
        isSearching={isSearching}
        isOffline={isOffline}
        chips={chips}
        onChipRemove={removeChip}
        banner={banner}
        onForceParseQuery={forceParseQuery}
        warningChip={
          warning === 'query_parse_timeout'
            ? <WarningChip key={warning} label="Query understanding was slow" />
            : undefined
        }
        rightSlot={
          <SettingsButton onClick={() => { setSettingsTab('folders'); setShowSettings(true); }} />
        }
      />
      {errorCode === 'ERR_MODEL_MISMATCH' ? (
        <ReindexBanner errorCode={errorCode} />
      ) : errorCode ? (
        <ErrorBanner
          code={errorCode}
          retryAfterMs={retryAfterMs}
          onRetry={errorCode.startsWith('ERR_QUERY_') ? forceParseQuery : forceSearch}
        />
      ) : null}
      <div style={styles.body}>
        {(() => {
          const trimmedQuery = query.trim();
          const hasQuery = trimmedQuery.length > 0;
          const showOnboarding = !hasFolders || !hasApiKey;
          const showEmptyHint = results.length === 0 && !hasQuery && !showOnboarding;
          const showNoResults = results.length === 0 && hasQuery && !isSearching && !showOnboarding;
          if (showEmptyHint) return <EmptyState variant="no-query" />;
          if (showNoResults) return <EmptyState variant="no-results" query={trimmedQuery} />;
          return (
            <ResultsList
              results={results}
              selectedIndex={selectedIndex}
              onSelect={setSelectedIndex}
              onOpen={(path) => { OpenFile(path); HideWindow(); }}
              hasFolders={hasFolders}
              hasApiKey={hasApiKey}
              onAddFolder={() => setShowFolderManager(true)}
              onSetApiKey={() => setShowApiKeyDialog(true)}
            />
          );
        })()}
        {previewOpen && (
          <PreviewPanel result={selectedResult} onOpenFolder={(path) => { OpenFolder(path); HideWindow(); }} />
        )}
      </div>
      {!indexingDismissed && (
        <IndexingBar
          status={indexingStatus}
          onDismiss={() => setIndexingDismissed(true)}
          onViewFailures={() => setShowFailuresModal(true)}
        />
      )}
      <FailuresModal
        open={showFailuresModal}
        onClose={() => setShowFailuresModal(false)}
        groups={indexingStatus.failedFileGroups ?? []}
      />
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
      {!onboarded && (
        <OnboardingOverlay onDismiss={() => { MarkOnboarded(); setOnboarded(true); }} />
      )}
      <SettingsPanel
        open={showSettings}
        activeTab={settingsTab}
        onTabChange={setSettingsTab}
        onClose={() => setShowSettings(false)}
        onSuccess={(msg) => setToast({ message: msg, type: 'success' })}
        onError={(msg) => setToast({ message: msg, type: 'error' })}
      />
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  root: {
    display: 'flex',
    flexDirection: 'column',
    width: '100%',
    height: '100%',
    overflow: 'hidden',
  },
  body: {
    display: 'flex',
    flex: 1,
    overflow: 'hidden',
  },
};

export default App;
