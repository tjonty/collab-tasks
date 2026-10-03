"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { SessionGate } from "@/components/session";
import { Toaster } from "@/components/toaster";
import { useApplyTheme } from "@/lib/theme-store";

export function Providers({ children }: { children: ReactNode }) {
  // Keeps <html data-theme> right after the inline script in layout.tsx set it.
  useApplyTheme();
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 30_000, refetchOnWindowFocus: false } } }),
  );
  return (
    <QueryClientProvider client={queryClient}>
      <Toaster>
        <SessionGate>{children}</SessionGate>
      </Toaster>
    </QueryClientProvider>
  );
}
