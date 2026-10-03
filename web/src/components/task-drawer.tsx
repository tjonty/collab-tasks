"use client";

import { useEffect, useMemo, useState } from "react";
import { v7 as uuidv7 } from "uuid";

import type { Change, Priority } from "@/lib/api/types";
import { dependentsOf } from "@/lib/domain/workflow";
import { useTask, useTaskList, useThread, usePending } from "@/lib/sync/hooks";
import type { ProjectSync } from "@/lib/sync/store";
import { parseFieldValue, pendingFields, waitingOn } from "@/lib/sync/view";
import { PRIORITIES, PRIORITY_LABEL, STATUS_LABEL } from "@/lib/ui/labels";
import { useUsers } from "@/lib/users";

import { DeleteTaskButton } from "./delete-task-button";
import { DependencyPicker } from "./dependency-picker";
import { EditableText } from "./editable-text";
import { useSession } from "./session";
import { StatusSelect } from "./status-select";

/** One task, every field editable, with its live comment thread. */
export function TaskDrawer({ sync, taskId, onClose }: { sync: ProjectSync; taskId: string; onClose: () => void }) {
  const { api } = useSession();
  const task = useTask(sync, taskId);
  const thread = useThread(sync, taskId);
  const pending = usePending(sync);
  const list = useTaskList(sync);
  const lookup = useMemo(() => new Map(list.map((t) => [t.id, t])), [list]);
  const saving = useMemo(() => pendingFields(pending, taskId), [pending, taskId]);
  const people = useMemo(() => [...(task?.assignedTo ?? []), ...(thread ?? []).map((c) => c.author)], [task?.assignedTo, thread]);
  const { list: users, nameOf: userName } = useUsers(api, people);

  useEffect(() => {
    void sync.openTask(taskId);
  }, [sync, taskId]);

  const send = (change: Change) => sync.send(change);
  const savingMark = (field: string) =>
    saving.has(field) ? <span className="ml-2 text-xs font-normal text-muted">saving…</span> : null;

  if (!task) {
    return (
      <Panel onClose={onClose}>
        <p className="text-sm text-muted">{sync.isDeleted(taskId) ? "This task was deleted." : "Loading task…"}</p>
      </Panel>
    );
  }

  const waiting = waitingOn(task, (id) => lookup.get(id));
  const candidates = list.filter((t) => t.id !== task.id);

  return (
    <Panel onClose={onClose}>
      <EditableText
        label="Title"
        value={task.title}
        maxLength={200}
        className="text-lg font-semibold"
        onCommit={(title) => title.trim() && send({ type: "task.update", taskId, title })}
      />
      {savingMark("title")}

      <Field label="Status" saving={savingMark("status")}>
        <StatusSelect
          status={task.status}
          waitingOn={waiting.map((t) => t.title)}
          onChange={(to) => send({ type: "task.setStatus", taskId, to })}
        />
      </Field>

      <Field label="Priority" saving={savingMark("priority")}>
        <select
          aria-label="Priority"
          value={task.priority}
          onChange={(e) => send({ type: "task.update", taskId, priority: e.target.value as Priority })}
          className="rounded border border-line-strong bg-surface px-2 py-1"
        >
          {PRIORITIES.map((p) => (
            <option key={p} value={p}>
              {PRIORITY_LABEL[p]}
            </option>
          ))}
        </select>
      </Field>

      <Field label="Assignees" saving={savingMark("assignedTo")}>
        <Chips
          items={task.assignedTo}
          label={userName}
          onRemove={(userId) => send({ type: "task.removeAssignee", taskId, userId })}
        />
        <select
          aria-label="Add assignee"
          value=""
          onChange={(e) => e.target.value && send({ type: "task.addAssignee", taskId, userId: e.target.value })}
          className="mt-1 w-full rounded border border-line-strong bg-surface px-2 py-1 text-sm"
        >
          <option value="">Add an assignee…</option>
          {users
            .filter((u) => !task.assignedTo.includes(u.id))
            .map((u) => (
              <option key={u.id} value={u.id}>
                {u.displayName}
              </option>
            ))}
        </select>
      </Field>

      <Field label="Tags" saving={savingMark("tags")}>
        <Chips items={task.tags} label={(t) => t} onRemove={(tag) => send({ type: "task.removeTag", taskId, tag })} />
        <AddTag onAdd={(tag) => send({ type: "task.addTag", taskId, tag })} />
      </Field>

      <Field label="Depends on" saving={savingMark("dependencies")}>
        <ul className="flex flex-col gap-1 text-sm">
          {task.dependencies.map((id) => {
            const dep = lookup.get(id);
            return (
              <li key={id} className="flex items-center justify-between gap-2">
                <span className={dep?.status === "done" ? "text-muted line-through" : ""}>
                  {dep?.title ?? "…"} {dep && <span className="text-xs text-muted">({STATUS_LABEL[dep.status]})</span>}
                </span>
                <button
                  type="button"
                  aria-label={`Remove dependency ${dep?.title ?? id}`}
                  onClick={() => send({ type: "task.removeDependency", taskId, dependsOnId: id })}
                  className="text-xs text-muted hover:text-danger-ink"
                >
                  Remove
                </button>
              </li>
            );
          })}
        </ul>
        <div className="mt-1">
          <DependencyPicker
            task={task}
            candidates={candidates}
            lookup={(id) => lookup.get(id)}
            onAdd={(dependsOnId) => send({ type: "task.addDependency", taskId, dependsOnId })}
          />
        </div>
      </Field>

      {task.details ? (
        <>
          <Field label="Custom fields" saving={savingMark("customFields")}>
            <CustomFields
              fields={task.details.customFields}
              onSet={(key, value) => send({ type: "task.update", taskId, customFields: { [key]: value } })}
            />
          </Field>
          <Field label="Description" saving={savingMark("description")}>
            <EditableText
              label="Description"
              multiline
              maxLength={10_000}
              placeholder="Add a description…"
              value={task.details.description}
              onCommit={(description) => send({ type: "task.update", taskId, description })}
            />
          </Field>
        </>
      ) : (
        <p className="text-sm text-muted">Loading details…</p>
      )}

      <div className="flex justify-end">
        <DeleteTaskButton
          dependents={dependentsOf(list, taskId).length}
          onDelete={() => {
            send({ type: "task.delete", taskId });
            onClose();
          }}
        />
      </div>

      <Comments
        comments={thread ?? []}
        sending={pending.filter((p) => p.change.type === "comment.create" && p.change.taskId === taskId).length}
        userName={userName}
        onPost={(content) => send({ type: "comment.create", id: uuidv7(), taskId, content })}
      />
    </Panel>
  );
}

function Panel({ children, onClose }: { children: React.ReactNode; onClose: () => void }) {
  return (
    <aside
      aria-label="Task"
      className="flex w-[28rem] max-w-full shrink-0 flex-col gap-4 overflow-y-auto border-l border-line bg-surface p-5"
    >
      <button type="button" onClick={onClose} className="self-end text-sm text-muted hover:text-ink">
        Close
      </button>
      {children}
    </aside>
  );
}

function Field({ label, saving, children }: { label: string; saving?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-1">
      <h3 className="text-xs font-medium uppercase tracking-wide text-muted">
        {label}
        {saving}
      </h3>
      {children}
    </section>
  );
}

function Chips<T extends string>({ items, label, onRemove }: { items: T[]; label: (item: T) => string; onRemove: (item: T) => void }) {
  return (
    <div className="flex flex-wrap gap-1">
      {items.map((item) => (
        <span key={item} className="inline-flex items-center gap-1 rounded bg-subtle px-2 py-0.5 text-xs">
          {label(item)}
          <button type="button" aria-label={`Remove ${label(item)}`} onClick={() => onRemove(item)} className="text-muted hover:text-danger-ink">
            ×
          </button>
        </span>
      ))}
    </div>
  );
}

function AddTag({ onAdd }: { onAdd: (tag: string) => void }) {
  const [tag, setTag] = useState("");
  return (
    <form
      className="mt-1"
      onSubmit={(e) => {
        e.preventDefault();
        if (tag.trim()) onAdd(tag.trim());
        setTag("");
      }}
    >
      <input
        aria-label="Add tag"
        placeholder="Add a tag and press Enter"
        maxLength={30}
        value={tag}
        onChange={(e) => setTag(e.target.value)}
        className="w-full rounded border border-line-strong bg-surface px-2 py-1 text-sm"
      />
    </form>
  );
}

function CustomFields({
  fields,
  onSet,
}: {
  fields: Record<string, string | number | boolean>;
  onSet: (key: string, value: string | number | boolean | null) => void;
}) {
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  return (
    <div className="flex flex-col gap-1 text-sm">
      {Object.entries(fields)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([k, v]) => (
          <div key={k} className="flex items-center gap-2">
            <span className="w-32 shrink-0 truncate text-muted">{k}</span>
            <EditableText label={`Custom field ${k}`} value={String(v)} onCommit={(raw) => onSet(k, parseFieldValue(raw))} />
            <button type="button" aria-label={`Remove custom field ${k}`} onClick={() => onSet(k, null)} className="text-xs text-muted hover:text-danger-ink">
              ×
            </button>
          </div>
        ))}
      <form
        className="flex gap-1"
        onSubmit={(e) => {
          e.preventDefault();
          if (!key.trim()) return;
          onSet(key.trim(), parseFieldValue(value));
          setKey("");
          setValue("");
        }}
      >
        <input aria-label="New field name" placeholder="Field" maxLength={40} value={key} onChange={(e) => setKey(e.target.value)} className="w-32 rounded border border-line-strong bg-surface px-2 py-1" />
        <input aria-label="New field value" placeholder="Value" maxLength={500} value={value} onChange={(e) => setValue(e.target.value)} className="min-w-0 flex-1 rounded border border-line-strong bg-surface px-2 py-1" />
        <button type="submit" className="rounded border border-line-strong px-2 py-1 hover:bg-subtle">
          Add
        </button>
      </form>
    </div>
  );
}

function Comments({
  comments,
  sending,
  userName,
  onPost,
}: {
  comments: { id: string; author: string; content: string; timestamp: string }[];
  sending: number;
  userName: (id: string) => string;
  onPost: (content: string) => void;
}) {
  const [content, setContent] = useState("");
  return (
    <section className="flex flex-col gap-2 border-t border-line pt-4">
      <h3 className="text-xs font-medium uppercase tracking-wide text-muted">Comments</h3>
      <ol className="flex flex-col gap-3">
        {comments.map((c) => (
          <li key={c.id} className="text-sm">
            <span className="font-medium">{userName(c.author)}</span>{" "}
            <time className="text-xs text-muted">{new Date(c.timestamp).toLocaleString()}</time>
            <p className="whitespace-pre-wrap">{c.content}</p>
          </li>
        ))}
      </ol>
      {sending > 0 && <p className="text-xs text-muted">Sending {sending === 1 ? "a comment" : `${sending} comments`}…</p>}
      <form
        className="flex flex-col gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (!content.trim()) return;
          onPost(content.trim());
          setContent("");
        }}
      >
        <textarea
          aria-label="New comment"
          rows={3}
          maxLength={5000}
          placeholder="Write a comment…"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          className="rounded border border-line-strong bg-surface px-2 py-1 text-sm"
        />
        <button type="submit" disabled={!content.trim()} className="self-end rounded bg-accent px-3 py-1 text-sm text-on-accent enabled:hover:bg-accent-hover disabled:opacity-40">
          Comment
        </button>
      </form>
    </section>
  );
}
