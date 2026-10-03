"use client";

import { useEffect, useEffectEvent, useRef, useState } from "react";

/**
 * A text field that commits on blur (and on Enter, for one line). Until the
 * user types, it shows the live value, even with focus, so remote edits
 * appear. On blur it saves only what the user changed.
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
  // What the user typed, and the live value it started from. Null until they type.
  const [draft, setDraft] = useState<{ base: string; text: string } | null>(null);
  const field = useRef<HTMLInputElement & HTMLTextAreaElement>(null);

  const commit = useEffectEvent(() => {
    if (draft && draft.text !== draft.base) onCommit(draft.text);
    setDraft(null);
  });
  // A native listener: React's onBlur listens for focusout, which Safari
  // doesn't send when the window loses focus.
  useEffect(() => {
    const el = field.current;
    el?.addEventListener("blur", commit);
    return () => el?.removeEventListener("blur", commit);
  }, []);

  const props = {
    ref: field,
    "aria-label": label,
    value: draft?.text ?? value,
    placeholder,
    maxLength,
    onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
      setDraft({ base: draft?.base ?? value, text: e.target.value }),
    className: `w-full rounded border border-transparent bg-transparent px-2 py-1 hover:border-line-strong focus:border-accent-strong focus:outline-none ${className}`,
  };
  return multiline ? (
    <textarea rows={6} {...props} />
  ) : (
    <input {...props} onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()} />
  );
}
