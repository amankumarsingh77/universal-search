import { useState, useEffect, useCallback } from 'react';
import { GetFolders, RemoveFolder, AddIgnoredFolder, GetIgnoredFolders, RemoveIgnoredFolder, ReindexFolder } from '../../wailsjs/go/main/App';
import { EventsEmit, EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';

interface FolderManagerProps {
  onClose: () => void;
}

type ConfirmState = {
  path: string;
} | null;

const DEFAULT_IGNORE_PATTERNS = new Set([
  'node_modules', '.git', 'venv', '.venv', '__pycache__', '.mypy_cache',
  'dist', 'build', '.next', '.nuxt', 'out', 'target', '.gradle', '.idea',
  '.vscode', 'Pods', 'vendor', '.cache', '.sass-cache', 'coverage',
]);

export function FolderManager({ onClose }: FolderManagerProps) {
  const [folders, setFolders] = useState<string[]>([]);
  const [confirm, setConfirm] = useState<ConfirmState>(null);
  const [activeTab, setActiveTab] = useState<'indexed' | 'ignored'>('indexed');
  const [ignoredPatterns, setIgnoredPatterns] = useState<string[]>([]);
  const [newPattern, setNewPattern] = useState('');
  const [reindexingFolder, setReindexingFolder] = useState<string | null>(null);

  const loadFolders = useCallback(async () => {
    try {
      const result = await GetFolders();
      setFolders(result || []);
    } catch (err) {
      console.error('Failed to load folders:', err);
    }
  }, []);

  useEffect(() => {
    loadFolders();
  }, [loadFolders]);

  useEffect(() => {
    EventsOn('folders-changed', () => {
      loadFolders();
    });
    return () => {
      EventsOff('folders-changed');
    };
  }, [loadFolders]);

  const loadIgnoredPatterns = useCallback(async () => {
    try {
      const result = await GetIgnoredFolders();
      setIgnoredPatterns(result || []);
    } catch (err) {
      console.error('Failed to load ignored patterns:', err);
    }
  }, []);

  useEffect(() => {
    loadIgnoredPatterns();
  }, [loadIgnoredPatterns]);

  const handleAddFolder = () => {
    EventsEmit('add-folder-request');
  };

  const handleRemove = async (path: string, deleteData: boolean) => {
    try {
      await RemoveFolder(path, deleteData);
      setConfirm(null);
      await loadFolders();
    } catch (err) {
      console.error('Failed to remove folder:', err);
    }
  };

  const handleAddPattern = async () => {
    const trimmed = newPattern.trim();
    if (!trimmed) return;
    try {
      await AddIgnoredFolder(trimmed);
      setNewPattern('');
      await loadIgnoredPatterns();
    } catch (err) {
      console.error('Failed to add pattern:', err);
    }
  };

  const handleRemovePattern = async (pattern: string) => {
    try {
      await RemoveIgnoredFolder(pattern);
      await loadIgnoredPatterns();
    } catch (err) {
      console.error('Failed to remove pattern:', err);
    }
  };

  const handleReindex = async (path: string) => {
    if (reindexingFolder) return;
    setReindexingFolder(path);
    try {
      await ReindexFolder(path);
    } catch (err) {
      console.error('Failed to reindex folder:', err);
    } finally {
      setTimeout(() => setReindexingFolder(null), 1500);
    }
  };

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (confirm) {
          setConfirm(null);
        } else {
          onClose();
        }
      }
    },
    [confirm, onClose]
  );

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  return (
    <div style={styles.backdrop} onClick={onClose}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>Folders</span>
          <button style={styles.closeBtn} onClick={onClose}>&times;</button>
        </div>

        <div style={styles.tabs}>
          <button
            style={activeTab === 'indexed' ? styles.tabActive : styles.tab}
            onClick={() => setActiveTab('indexed')}
          >
            Indexed Folders
          </button>
          <button
            style={activeTab === 'ignored' ? styles.tabActive : styles.tab}
            onClick={() => setActiveTab('ignored')}
          >
            Ignored Folders
          </button>
        </div>

        {activeTab === 'indexed' && (
          <div style={styles.body}>
            {folders.length === 0 ? (
              <div style={styles.empty}>No folders indexed. Add a folder to get started.</div>
            ) : (
              folders.map((folder) => (
                <div key={folder} style={styles.folderRow}>
                  <span style={styles.folderPath}>{folder}</span>
                  <button
                    style={styles.reindexBtn}
                    onClick={() => handleReindex(folder)}
                    title="Reindex folder"
                    disabled={reindexingFolder === folder}
                  >
                    {reindexingFolder === folder ? 'Reindexing...' : '↺'}
                  </button>
                  <button
                    style={styles.removeBtn}
                    onClick={() => setConfirm({ path: folder })}
                    title="Remove folder"
                  >
                    &times;
                  </button>
                </div>
              ))
            )}
          </div>
        )}

        {activeTab === 'ignored' && (
          <div style={styles.body}>
            {ignoredPatterns.length === 0 ? (
              <div style={styles.empty}>No ignore patterns. Add one below.</div>
            ) : (
              ignoredPatterns.map((pattern) => (
                <div key={pattern} style={styles.folderRow}>
                  <span style={styles.folderPath}>{pattern}</span>
                  {DEFAULT_IGNORE_PATTERNS.has(pattern) && (
                    <span style={styles.defaultBadge}>default</span>
                  )}
                  <button
                    style={styles.removeBtn}
                    onClick={() => handleRemovePattern(pattern)}
                    title="Remove pattern"
                  >
                    &times;
                  </button>
                </div>
              ))
            )}
          </div>
        )}

        {confirm && (
          <div style={styles.confirmOverlay}>
            <div style={styles.confirmBox}>
              <div style={styles.confirmText}>
                Remove <strong>{confirm.path.split('/').pop()}</strong>?
              </div>
              <div style={styles.confirmButtons}>
                <button
                  style={styles.dangerBtn}
                  onClick={() => handleRemove(confirm.path, true)}
                >
                  Remove &amp; Delete Data
                </button>
                <button
                  style={styles.secondaryBtn}
                  onClick={() => handleRemove(confirm.path, false)}
                >
                  Remove &amp; Keep Data
                </button>
                <button
                  style={styles.secondaryBtn}
                  onClick={() => setConfirm(null)}
                >
                  Cancel
                </button>
              </div>
            </div>
          </div>
        )}

        {activeTab === 'indexed' && (
          <div style={styles.footer}>
            <button style={styles.addBtn} onClick={handleAddFolder}>
              + Add Folder
            </button>
          </div>
        )}
        {activeTab === 'ignored' && (
          <div style={styles.footer}>
            <div style={styles.addRow}>
              <input
                style={styles.patternInput}
                type="text"
                placeholder="e.g. node_modules"
                value={newPattern}
                onChange={(e) => setNewPattern(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleAddPattern()}
              />
              <button style={styles.addBtn} onClick={handleAddPattern}>
                Add
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  backdrop: {
    position: 'fixed',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    background: 'rgba(0, 0, 0, 0.6)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 1000,
  },
  modal: {
    background: 'var(--bg-surface)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    width: '480px',
    maxHeight: '400px',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '12px 16px',
    borderBottom: '1px solid var(--border)',
  },
  title: {
    fontSize: '14px',
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  closeBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-secondary)',
    fontSize: '18px',
    cursor: 'pointer',
    padding: '0 4px',
    lineHeight: 1,
  },
  tabs: {
    display: 'flex',
    borderBottom: '1px solid var(--border)',
  },
  tab: {
    flex: 1,
    background: 'none',
    border: 'none',
    borderBottom: '2px solid transparent',
    padding: '8px 0',
    fontSize: '13px',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  } as React.CSSProperties,
  tabActive: {
    flex: 1,
    background: 'none',
    border: 'none',
    borderBottom: '2px solid var(--accent, #7c6fe0)',
    padding: '8px 0',
    fontSize: '13px',
    color: 'var(--text-primary)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
    fontWeight: 600,
  } as React.CSSProperties,
  body: {
    flex: 1,
    overflowY: 'auto',
    padding: '8px 0',
  },
  empty: {
    padding: '24px 16px',
    textAlign: 'center',
    fontSize: '13px',
    color: 'var(--text-tertiary)',
  },
  folderRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '6px 16px',
    gap: '8px',
  },
  folderPath: {
    fontSize: '12px',
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    flex: 1,
  },
  defaultBadge: {
    fontSize: '10px',
    padding: '1px 5px',
    borderRadius: '3px',
    background: 'rgba(124,111,224,0.15)',
    color: 'var(--text-tertiary)',
    flexShrink: 0,
    marginRight: '4px',
  },
  reindexBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-tertiary)',
    fontSize: '14px',
    cursor: 'pointer',
    padding: '2px 6px',
    borderRadius: 'var(--radius-sm, 4px)',
    lineHeight: 1,
    flexShrink: 0,
  },
  removeBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-tertiary)',
    fontSize: '16px',
    cursor: 'pointer',
    padding: '2px 6px',
    borderRadius: 'var(--radius-sm, 4px)',
    lineHeight: 1,
    flexShrink: 0,
  },
  confirmOverlay: {
    padding: '12px 16px',
    borderTop: '1px solid var(--border)',
    background: 'var(--bg-surface-2, var(--bg-surface))',
  },
  confirmBox: {
    display: 'flex',
    flexDirection: 'column',
    gap: '10px',
  },
  confirmText: {
    fontSize: '13px',
    color: 'var(--text-primary)',
  },
  confirmButtons: {
    display: 'flex',
    gap: '8px',
    flexWrap: 'wrap',
  },
  dangerBtn: {
    background: '#e53935',
    border: 'none',
    borderRadius: 'var(--radius-sm, 4px)',
    color: '#fff',
    fontSize: '12px',
    padding: '5px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  secondaryBtn: {
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm, 4px)',
    color: 'var(--text-secondary)',
    fontSize: '12px',
    padding: '5px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  footer: {
    padding: '10px 16px',
    borderTop: '1px solid var(--border)',
  },
  addBtn: {
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm, 4px)',
    color: 'var(--text-secondary)',
    fontSize: '12px',
    padding: '5px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
    width: '100%',
  },
  addRow: {
    display: 'flex',
    gap: '6px',
  },
  patternInput: {
    flex: 1,
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm, 4px)',
    color: 'var(--text-primary)',
    fontSize: '12px',
    padding: '5px 8px',
    fontFamily: 'var(--font-mono)',
    outline: 'none',
  },
};
