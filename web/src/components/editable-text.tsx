"use client";

import { useState } from "react";

/**
 * A text field that commits on blur (and on Enter, for one line). While the
 * user isn't editing, it shows the live value, so remote edits appear.
 */
export function EditableText({
  value,
  onCommit,
  label,
  multiline = false,
  placeholder,
  className = "",
  maxLength,
}: {
  value: string;
  onCommit: (value: string) => void;
  label: string;
  multiline?: boolean;
  placeholder?: string;
  className?: string;
  maxLength?: number;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const props = {
    "aria-label": label,
    value: draft ?? value,
    placeholder,
    maxLength,
    onFocus: () => setDraft(value),
    onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setDraft(e.target.value),
    onBlur: () => {
      if (draft !== null && draft !== value) onCommit(draft);
      setDraft(null);
    },
    className: `w-full rounded border border-transparent bg-transparent px-2 py-1 hover:border-line-strong focus:border-accent-strong focus:outline-none ${className}`,
  };
  return multiline ? (
    <textarea rows={6} {...props} />
  ) : (
    <input {...props} onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()} />
  );
}
