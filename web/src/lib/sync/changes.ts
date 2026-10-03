// Sending changes (spec sections 5 and 6). Each change gets a UUIDv7
// changeId, so retrying a POST is always safe. A change stays pending, so the
// UI can show "saving…", until its own event comes back through the stream.
import { v7 as uuidv7 } from "uuid";

import type { Change, ErrorCode } from "@/lib/api/types";

/** What one POST of a change came back with. */
export type PostOutcome =
  | { kind: "ok"; seq: number }
  /** The server turned the change down (409, 422 ...): retrying won't help. */
  | { kind: "rejected"; code: ErrorCode; message: string }
  /** The network failed or the server was unavailable: retry. */
  | { kind: "retry"; reason: string };

export interface Pending {
  changeId: string;
  change: Change;
  /** sending: being posted; waiting: accepted, its event not yet applied; failed: gave up retrying. */
  state: "sending" | "waiting" | "failed";
  /** The seq the server gave it, once accepted. */
  seq?: number;
}

export interface SenderOptions {
  post: (changeId: string, change: Change) => Promise<PostOutcome>;
  /** The seq of the last event the client applied. */
  lastAppliedSeq: () => number;
  /** Called when the server rejects a change, to tell the user. */
  onRejected: (change: Change, code: ErrorCode, message: string) => void;
  sleep?: (ms: number) => Promise<void>;
}

/** Waits between attempts: 0.5 s doubling to 8 s, then give up. */
const RETRY_DELAYS = [500, 1000, 2000, 4000, 8000];

const wait = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

export class ChangeSender {
  private pending = new Map<string, Pending>();
  private snapshot: Pending[] = [];
  private listeners = new Set<() => void>();

  constructor(private opts: SenderOptions) {}

  /** Sends a change and returns its changeId. */
  send(change: Change): string {
    const changeId = uuidv7();
    this.set({ changeId, change, state: "sending" });
    void this.deliver(changeId);
    return changeId;
  }

  /** The change's event was applied. */
  confirmed(changeId: string): void {
    this.remove(changeId);
  }

  /** The client applied the log up to seq: accepted changes at or before it are done. */
  caughtUp(lastAppliedSeq: number): void {
    for (const p of this.pending.values()) {
      if (p.state === "waiting" && p.seq !== undefined && p.seq <= lastAppliedSeq) this.remove(p.changeId);
    }
  }

  /** Tries a failed change again, with the same changeId. */
  retry(changeId: string): void {
    const p = this.pending.get(changeId);
    if (p?.state !== "failed") return;
    this.set({ ...p, state: "sending" });
    void this.deliver(changeId);
  }

  /** Gives up on a failed change. */
  discard(changeId: string): void {
    this.remove(changeId);
  }

  /** The pending changes, oldest first. The same array until one changes. */
  list(): Pending[] {
    return this.snapshot;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private async deliver(changeId: string): Promise<void> {
    const sleep = this.opts.sleep ?? wait;
    for (let attempt = 0; ; attempt++) {
      const p = this.pending.get(changeId);
      if (!p) return; // confirmed or discarded meanwhile
      let outcome: PostOutcome;
      try {
        outcome = await this.opts.post(changeId, p.change);
      } catch (err) {
        outcome = { kind: "retry", reason: String(err) };
      }
      const now = this.pending.get(changeId);
      if (!now) return;
      switch (outcome.kind) {
        case "ok":
          if (outcome.seq <= this.opts.lastAppliedSeq()) this.remove(changeId);
          else this.set({ ...now, state: "waiting", seq: outcome.seq });
          return;
        case "rejected":
          this.remove(changeId);
          this.opts.onRejected(now.change, outcome.code, outcome.message);
          return;
        case "retry":
          if (attempt >= RETRY_DELAYS.length) {
            this.set({ ...now, state: "failed" });
            return;
          }
          await sleep(RETRY_DELAYS[attempt]);
      }
    }
  }

  private set(p: Pending): void {
    this.pending.set(p.changeId, p);
    this.changed();
  }

  private remove(changeId: string): void {
    if (this.pending.delete(changeId)) this.changed();
  }

  private changed(): void {
    this.snapshot = [...this.pending.values()];
    for (const listener of this.listeners) listener();
  }
}
