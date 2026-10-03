"use client";

import { useState } from "react";

/** Asks for a display name and creates the user. */
export function NamePrompt({ onCreate }: { onCreate: (displayName: string) => Promise<void> }) {
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const trimmed = name.trim();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!trimmed) return;
    setBusy(true);
    setError(null);
    try {
      await onCreate(trimmed);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="mx-auto mt-32 flex w-full max-w-sm flex-col gap-3 px-6">
      <h1 className="text-2xl font-semibold tracking-tight">Collab Tasks</h1>
      <p className="text-sm text-muted">
        Pick a display name. Each tab can be someone different.
      </p>
      <label className="flex flex-col gap-1 text-sm">
        Display name
        <input
          autoFocus
          maxLength={40}
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="rounded border border-line-strong bg-surface px-3 py-2"
        />
      </label>
      <button
        type="submit"
        disabled={!trimmed || busy}
        className="rounded bg-accent px-3 py-2 text-sm font-medium text-on-accent enabled:hover:bg-accent-hover disabled:opacity-40"
      >
        Continue
      </button>
      {error && <p className="text-sm text-danger-ink">{error}</p>}
    </form>
  );
}
