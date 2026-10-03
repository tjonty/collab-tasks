import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import type { WsServerMessage } from "@/lib/api/types";

import { Connection, type ConnectionStatus } from "./connection";

/** A WebSocket the test drives by hand. */
class FakeSocket {
  static all: FakeSocket[] = [];
  readyState = 0;
  sent: unknown[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: ((e: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(public url: string) {
    FakeSocket.all.push(this);
  }
  send(data: string) {
    this.sent.push(JSON.parse(data));
  }
  close() {
    this.drop(1000);
  }
  // The server's side:
  accept() {
    this.readyState = 1;
    this.onopen?.();
  }
  push(msg: WsServerMessage) {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
  drop(code = 1006) {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.onclose?.({ code });
  }
}

const latest = () => FakeSocket.all[FakeSocket.all.length - 1];

function setup(random = () => 0.5) {
  const statuses: ConnectionStatus[] = [];
  const messages: WsServerMessage[] = [];
  const opened = vi.fn();
  const network = new EventTarget();
  let online = true;
  const conn = new Connection({
    url: "ws://api/ws?userId=u1",
    onOpen: opened,
    onMessage: (m) => messages.push(m),
    onStatus: (s) => statuses.push(s),
    WebSocket: FakeSocket as unknown as typeof WebSocket,
    random,
    network,
    isOnline: () => online,
  });
  return {
    conn, statuses, messages, opened, network,
    goOffline: () => {
      online = false;
      network.dispatchEvent(new Event("offline"));
    },
    goOnline: () => {
      online = true;
      network.dispatchEvent(new Event("online"));
    },
  };
}

beforeEach(() => {
  FakeSocket.all = [];
  vi.useFakeTimers();
});
afterEach(() => vi.useRealTimers());

describe("Connection", () => {
  test("connects, reports Live once open, and passes messages both ways", () => {
    const { conn, statuses, messages, opened } = setup();
    conn.start();
    expect(latest().url).toBe("ws://api/ws?userId=u1");
    expect(statuses).toEqual(["connecting"]);

    latest().accept();
    expect(statuses).toEqual(["connecting", "live"]);
    expect(opened).toHaveBeenCalledTimes(1);

    expect(conn.send({ type: "subscribeProjects" })).toBe(true);
    expect(latest().sent).toEqual([{ type: "subscribeProjects" }]);
    latest().push({ type: "caughtUp", projectId: "p", seq: 3 });
    expect(messages).toEqual([{ type: "caughtUp", projectId: "p", seq: 3 }]);
  });

  test("sending before the socket is open reports false", () => {
    const { conn } = setup();
    conn.start();
    expect(conn.send({ type: "subscribeProjects" })).toBe(false);
    expect(latest().sent).toEqual([]);
  });

  test("a dropped connection reconnects with backoff, doubling from 0.5 s to 10 s", () => {
    const { conn, statuses } = setup();
    conn.start();
    latest().drop();
    expect(statuses.at(-1)).toBe("reconnecting");

    for (const delay of [500, 1000, 2000, 4000, 8000, 10000, 10000]) {
      const before = FakeSocket.all.length;
      vi.advanceTimersByTime(delay - 1);
      expect(FakeSocket.all.length, `before ${delay} ms`).toBe(before);
      vi.advanceTimersByTime(1);
      expect(FakeSocket.all.length, `after ${delay} ms`).toBe(before + 1);
      latest().drop();
    }
  });

  test("the backoff has up to 25% jitter either way", () => {
    for (const [random, delay] of [[0, 375], [1, 625]] as const) {
      FakeSocket.all = [];
      const { conn } = setup(() => random);
      conn.start();
      latest().drop();
      vi.advanceTimersByTime(delay - 1);
      expect(FakeSocket.all).toHaveLength(1);
      vi.advanceTimersByTime(1);
      expect(FakeSocket.all).toHaveLength(2);
      conn.stop();
    }
  });

  test("a successful reconnect resets the backoff and runs onOpen again", () => {
    const { conn, opened, statuses } = setup();
    conn.start();
    latest().accept();
    latest().drop();
    vi.advanceTimersByTime(500);
    latest().drop();
    vi.advanceTimersByTime(1000);
    latest().accept();
    expect(opened).toHaveBeenCalledTimes(2);
    expect(statuses.at(-1)).toBe("live");

    latest().drop();
    vi.advanceTimersByTime(500);
    expect(FakeSocket.all).toHaveLength(4);
  });

  test("going offline shows Offline; coming back online reconnects at once", () => {
    const { conn, statuses, goOffline, goOnline } = setup();
    conn.start();
    latest().accept();
    goOffline();
    latest().drop();
    expect(statuses.at(-1)).toBe("offline");

    vi.advanceTimersByTime(300); // well before the backoff would fire
    const before = FakeSocket.all.length;
    goOnline();
    expect(FakeSocket.all).toHaveLength(before + 1);
    expect(statuses.at(-1)).toBe("reconnecting");
  });

  test("stop closes the socket for good", () => {
    const { conn } = setup();
    conn.start();
    latest().accept();
    conn.stop();
    expect(latest().readyState).toBe(3);
    vi.advanceTimersByTime(60_000);
    expect(FakeSocket.all).toHaveLength(1);
  });
});
