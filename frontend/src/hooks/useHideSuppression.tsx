import { createContext, useCallback, useContext, useMemo, useRef } from 'react';

// The app hides itself on window blur so the Raycast-style surface disappears
// whenever you click away. Native OS dialogs (directory picker, file picker)
// steal focus from the Wails window, which would otherwise hide the parent and
// dismiss the dialog along with it. Code that opens a native dialog must wrap
// the call in `withSuppressedHide` so blur events during the dialog's lifetime
// are ignored. Counter (not boolean) handles nested or back-to-back dialogs.

type HideSuppressionContextValue = {
  isSuppressed: () => boolean;
  withSuppressedHide: <T>(fn: () => Promise<T>) => Promise<T>;
};

const HideSuppressionContext = createContext<HideSuppressionContextValue | null>(null);

// Time after a wrapped call resolves during which blur is still ignored.
// Trailing blur events from the closing dialog can fire a few frames late;
// this covers the gap without introducing user-visible latency.
const TRAILING_GRACE_MS = 150;

export function HideSuppressionProvider({ children }: { children: React.ReactNode }) {
  const counterRef = useRef(0);

  const isSuppressed = useCallback(() => counterRef.current > 0, []);

  const withSuppressedHide = useCallback(async <T,>(fn: () => Promise<T>): Promise<T> => {
    counterRef.current++;
    try {
      return await fn();
    } finally {
      setTimeout(() => {
        counterRef.current = Math.max(0, counterRef.current - 1);
      }, TRAILING_GRACE_MS);
    }
  }, []);

  const value = useMemo(() => ({ isSuppressed, withSuppressedHide }), [isSuppressed, withSuppressedHide]);

  return <HideSuppressionContext.Provider value={value}>{children}</HideSuppressionContext.Provider>;
}

export function useHideSuppression(): HideSuppressionContextValue {
  const ctx = useContext(HideSuppressionContext);
  if (!ctx) {
    throw new Error('useHideSuppression must be used inside <HideSuppressionProvider>');
  }
  return ctx;
}
