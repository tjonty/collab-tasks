"use client";

import { createContext, useCallback, useContext, useState, type ReactNode } from "react";

interface Toast {
  id: number;
  text: string;
}

const ToastContext = createContext<(text: string) => void>(() => {});

let nextId = 1;

/** Short messages in the corner, e.g. when the server rejects a change. */
export function Toaster({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const show = useCallback((text: string) => {
    const id = nextId++;
    setToasts((ts) => [...ts, { id, text }]);
    setTimeout(() => setToasts((ts) => ts.filter((t) => t.id !== id)), 6000);
  }, []);
  return (
    <ToastContext.Provider value={show}>
      {children}
      <div aria-live="polite" className="fixed bottom-4 right-4 z-50 flex w-80 flex-col gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            role="alert"
            className="rounded-md bg-zinc-900 px-4 py-3 text-sm text-white shadow-lg dark:bg-zinc-100 dark:text-zinc-900"
          >
            {t.text}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): (text: string) => void {
  return useContext(ToastContext);
}
