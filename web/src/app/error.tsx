"use client"; // Error boundaries must be Client Components

export default function Error({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return (
    <main className="mx-auto mt-24 flex max-w-md flex-col gap-3 px-6">
      <h2 className="text-lg font-semibold">Something went wrong</h2>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">{error.message}</p>
      <button type="button" onClick={() => retry()} className="self-start rounded border px-3 py-1 text-sm">
        Try again
      </button>
    </main>
  );
}
