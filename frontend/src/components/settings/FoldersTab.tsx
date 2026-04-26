import { useState, useEffect, useCallback } from 'react';
import { Folder, MoreHorizontal, RotateCcw, X } from 'lucide-react';
import { GetFolders, RemoveFolder, PickAndAddFolder, ReindexFolder, GetIgnoredFolders, AddIgnoredFolder, RemoveIgnoredFolder } from '../../../wailsjs/go/app/App';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';
import { useHideSuppression } from '../../hooks/useHideSuppression';

type ConfirmState = { path: string } | null;

export function FoldersTab() {
  const [folders, setFolders] = useState<string[]>([]);
  const [ignoredPatterns, setIgnoredPatterns] = useState<string[]>([]);
  const [newPattern, setNewPattern] = useState('');
  const [confirm, setConfirm] = useState<ConfirmState>(null);
  const [reindexingFolder, setReindexingFolder] = useState<string | null>(null);
  const [pickingFolder, setPickingFolder] = useState(false);
  const [openMenu, setOpenMenu] = useState<string | null>(null);
  const { withSuppressedHide } = useHideSuppression();

  const loadFolders = useCallback(async () => {
    try {
      const result = await GetFolders();
      setFolders(result || []);
    } catch (err) {
      console.error('Failed to load folders:', err);
    }
  }, []);

  const loadIgnoredPatterns = useCallback(async () => {
    try {
      const result = await GetIgnoredFolders();
      setIgnoredPatterns(result || []);
    } catch (err) {
      console.error('Failed to load ignored patterns:', err);
    }
  }, []);

  useEffect(() => {
    loadFolders();
    loadIgnoredPatterns();
  }, [loadFolders, loadIgnoredPatterns]);

  useEffect(() => {
    EventsOn('folders-changed', () => loadFolders());
    return () => EventsOff('folders-changed');
  }, [loadFolders]);

  const handleAddFolder = async () => {
    if (pickingFolder) return;
    setPickingFolder(true);
    try {
      await withSuppressedHide(() => PickAndAddFolder());
    } catch (err) {
      console.error('Failed to add folder:', err);
    } finally {
      setPickingFolder(false);
    }
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

  const handleReindex = async (path: string) => {
    if (reindexingFolder) return;
    setReindexingFolder(path);
    setOpenMenu(null);
    try {
      await ReindexFolder(path);
    } catch (err) {
      console.error('Failed to reindex folder:', err);
    } finally {
      setTimeout(() => setReindexingFolder(null), 1500);
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

  const shortenPath = (p: string) => p.replace(/^\/Users\/[^/]+/, '~');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflowY: 'auto' }}>
      <h2 style={styles.heading}>Indexed Folders</h2>
      <p style={styles.subtext}>Folders Findo will scan and keep up to date. Files in excluded subpaths are ignored.</p>

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <button
          style={styles.addBtn}
          onClick={handleAddFolder}
          disabled={pickingFolder}
        >
          <Folder size={16} />
          {pickingFolder ? 'Opening…' : 'Add Folder'}
        </button>
        <span style={styles.metaBadge}>
          {folders.length} folder{folders.length !== 1 ? 's' : ''} · {folders.length * 0 || '–'} files indexed
        </span>
      </div>

      {folders.length === 0 ? (
        <div style={styles.empty}>No folders indexed yet. Add a folder to get started.</div>
      ) : (
        <div style={styles.folderList}>
          {folders.map((folder) => (
            <div key={folder} style={styles.folderRow}>
              <Folder size={18} color="var(--text-tertiary)" style={{ flexShrink: 0, marginRight: 12 }} />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={styles.folderPath}>{shortenPath(folder)}</div>
                <div style={styles.folderMeta}>
                  {reindexingFolder === folder ? 'Reindexing…' : 'last scanned recently'}
                </div>
              </div>
              <span style={{
                ...styles.statusPill,
                background: reindexingFolder === folder ? 'rgba(124,111,224,0.15)' : 'rgba(16,185,129,0.15)',
                color: reindexingFolder === folder ? 'var(--accent, #7c6fe0)' : 'var(--accent-green)',
              }}>
                <span style={{
                  width: 6, height: 6, borderRadius: '50%',
                  background: reindexingFolder === folder ? 'var(--accent, #7c6fe0)' : 'var(--accent-green)',
                  display: 'inline-block', marginRight: 5
                }} />
                {reindexingFolder === folder ? 'Indexing' : 'Up to date'}
              </span>
              <div style={{ position: 'relative' }}>
                <button
                  style={styles.iconBtn}
                  onClick={() => setOpenMenu(openMenu === folder ? null : folder)}
                  title="Options"
                >
                  <MoreHorizontal size={16} />
                </button>
                {openMenu === folder && (
                  <div style={styles.dropMenu}>
                    <button style={styles.dropItem} onClick={() => handleReindex(folder)}>
                      <RotateCcw size={13} style={{ marginRight: 6 }} /> Rescan
                    </button>
                    <button style={{ ...styles.dropItem, color: '#e53935' }} onClick={() => { setConfirm({ path: folder }); setOpenMenu(null); }}>
                      <X size={13} style={{ marginRight: 6 }} /> Remove
                    </button>
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {confirm && (
        <div style={styles.confirmOverlay}>
          <div style={styles.confirmText}>
            Remove <strong>{confirm.path.split('/').pop()}</strong>?
          </div>
          <div style={styles.confirmBtns}>
            <button style={styles.dangerBtn} onClick={() => handleRemove(confirm.path, true)}>
              Remove &amp; Delete Data
            </button>
            <button style={styles.secondaryBtn} onClick={() => handleRemove(confirm.path, false)}>
              Keep Data
            </button>
            <button style={styles.secondaryBtn} onClick={() => setConfirm(null)}>
              Cancel
            </button>
          </div>
        </div>
      )}

      <div style={{ marginTop: 24 }}>
        <h3 style={styles.sectionLabel}>Excluded patterns</h3>
        <div style={styles.chipCluster}>
          {ignoredPatterns.map((p) => (
            <span key={p} style={styles.chip}>
              {p}
              <button style={styles.chipRemove} onClick={() => handleRemovePattern(p)}>
                <X size={11} />
              </button>
            </span>
          ))}
          <div style={styles.addPatternRow}>
            <input
              style={styles.patternInput}
              type="text"
              placeholder="+ Add pattern"
              value={newPattern}
              onChange={(e) => setNewPattern(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleAddPattern()}
            />
            {newPattern.trim() && (
              <button style={styles.addPatternBtn} onClick={handleAddPattern}>Add</button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  heading: {
    fontSize: 22,
    fontWeight: 700,
    color: 'var(--text-primary)',
    margin: '0 0 8px',
    fontFamily: 'var(--font-sans)',
  },
  subtext: {
    fontSize: 14,
    color: 'var(--text-secondary)',
    margin: '0 0 20px',
    lineHeight: 1.5,
    fontFamily: 'var(--font-sans)',
  },
  addBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    background: 'var(--accent, #7c6fe0)',
    border: 'none',
    borderRadius: 'var(--radius-md, 8px)',
    color: '#fff',
    fontSize: 14,
    fontWeight: 600,
    padding: '8px 16px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  metaBadge: {
    fontSize: 13,
    color: 'var(--text-tertiary)',
  },
  empty: {
    padding: '24px 0',
    fontSize: 14,
    color: 'var(--text-tertiary)',
    textAlign: 'center',
  },
  folderList: {
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-lg, 12px)',
    overflow: 'hidden',
  },
  folderRow: {
    display: 'flex',
    alignItems: 'center',
    padding: '12px 16px',
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-surface)',
    gap: 8,
  },
  folderPath: {
    fontSize: 14,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
  },
  folderMeta: {
    fontSize: 12,
    color: 'var(--text-tertiary)',
    marginTop: 2,
  },
  statusPill: {
    display: 'inline-flex',
    alignItems: 'center',
    padding: '3px 10px',
    borderRadius: 100,
    fontSize: 12,
    fontWeight: 500,
    flexShrink: 0,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  iconBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    padding: '4px',
    display: 'flex',
    alignItems: 'center',
    borderRadius: 4,
  },
  dropMenu: {
    position: 'absolute',
    right: 0,
    top: '100%',
    background: 'var(--bg-surface)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    boxShadow: '0 4px 16px rgba(0,0,0,0.5)',
    zIndex: 100,
    minWidth: 130,
    padding: '4px 0',
  },
  dropItem: {
    display: 'flex',
    alignItems: 'center',
    width: '100%',
    background: 'none',
    border: 'none',
    color: 'var(--text-primary)',
    fontSize: 13,
    padding: '7px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
    textAlign: 'left' as const,
  },
  confirmOverlay: {
    marginTop: 12,
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md, 8px)',
    padding: 14,
  },
  confirmText: {
    fontSize: 14,
    color: 'var(--text-primary)',
    marginBottom: 10,
    fontFamily: 'var(--font-sans)',
  },
  confirmBtns: {
    display: 'flex',
    gap: 8,
    flexWrap: 'wrap' as const,
  },
  dangerBtn: {
    background: '#e53935',
    border: 'none',
    borderRadius: 'var(--radius-sm, 4px)',
    color: '#fff',
    fontSize: 12,
    padding: '6px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  secondaryBtn: {
    background: 'transparent',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm, 4px)',
    color: 'var(--text-secondary)',
    fontSize: 12,
    padding: '6px 12px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  sectionLabel: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    margin: '0 0 12px',
    fontFamily: 'var(--font-sans)',
  },
  chipCluster: {
    display: 'flex',
    flexWrap: 'wrap' as const,
    gap: 8,
    alignItems: 'center',
  },
  chip: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    padding: '5px 10px',
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm, 4px)',
    fontSize: 13,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
  },
  chipRemove: {
    background: 'none',
    border: 'none',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    padding: 0,
    display: 'flex',
    alignItems: 'center',
  },
  addPatternRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
  },
  patternInput: {
    background: 'transparent',
    border: '1px dashed var(--border)',
    borderRadius: 'var(--radius-sm, 4px)',
    color: 'var(--text-tertiary)',
    fontSize: 13,
    padding: '5px 10px',
    fontFamily: 'var(--font-mono)',
    outline: 'none',
    cursor: 'text',
    width: 120,
  },
  addPatternBtn: {
    background: 'var(--bg-surface-2, var(--bg-surface))',
    border: 'none',
    borderRadius: 'var(--radius-sm, 4px)',
    color: 'var(--text-secondary)',
    fontSize: 12,
    padding: '5px 10px',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
};
