import { useState, useEffect, useCallback } from 'react';
import { GetFolders, RemoveFolder } from '../../wailsjs/go/main/App';
import { EventsEmit, EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';

interface FolderManagerProps {
  onClose: () => void;
}

type ConfirmState = {
  path: string;
} | null;

export function FolderManager({ onClose }: FolderManagerProps) {
  const [folders, setFolders] = useState<string[]>([]);
  const [confirm, setConfirm] = useState<ConfirmState>(null);

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
          <span style={styles.title}>Indexed Folders</span>
          <button style={styles.closeBtn} onClick={onClose}>&times;</button>
        </div>

        <div style={styles.body}>
          {folders.length === 0 ? (
            <div style={styles.empty}>No folders indexed. Add a folder to get started.</div>
          ) : (
            folders.map((folder) => (
              <div key={folder} style={styles.folderRow}>
                <span style={styles.folderPath}>{folder}</span>
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

        <div style={styles.footer}>
          <button style={styles.addBtn} onClick={handleAddFolder}>
            + Add Folder
          </button>
        </div>
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
};
