// The WebSocket to /ws (spec section 4): JSON messages both ways, and a
// reconnect with backoff whenever it drops (0.5 s doubling to 10 s, with
// jitter so a restarted server isn't hit by every tab at once). It knows
// nothing about projects: onOpen is the caller's cue to subscribe again.
import type { WsClientMessage, WsServerMessage } from "@/lib/api/types";

export type ConnectionStatus = "connecting" | "live" | "reconnecting" | "offline";

export interface ConnectionOptions {
  url: string;
  /** Called on every (re)connect: send the subscriptions. */
  onOpen: () => void;
  onMessage: (msg: WsServerMessage) => void;
  onStatus: (status: ConnectionStatus) => void;
  /** Tests pass fakes for these; the defaults are the browser's. */
  WebSocket?: typeof WebSocket;
  random?: () => number;
  network?: EventTarget;
  isOnline?: () => boolean;
}

const FIRST_DELAY = 500;
const MAX_DELAY = 10_000;
const OPEN = 1; // WebSocket.OPEN

export class Connection {
  private socket: WebSocket | null = null;
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private stopped = true;

  constructor(private opts: ConnectionOptions) {}

  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    this.network()?.addEventListener("online", this.onOnline);
    this.network()?.addEventListener("offline", this.onOffline);
    this.opts.onStatus("connecting");
    this.open();
  }

  /** Sends a message if the socket is open; reports whether it did. */
  send(msg: WsClientMessage): boolean {
    if (this.socket?.readyState !== OPEN) return false;
    this.socket.send(JSON.stringify(msg));
    return true;
  }

  stop(): void {
    this.stopped = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.network()?.removeEventListener("online", this.onOnline);
    this.network()?.removeEventListener("offline", this.onOffline);
    const socket = this.socket;
    this.socket = null;
    socket?.close();
  }

  private open(): void {
    const WS = this.opts.WebSocket ?? WebSocket;
    const socket = new WS(this.opts.url);
    this.socket = socket;
    socket.onopen = () => {
      if (socket !== this.socket) return;
      this.attempt = 0;
      this.opts.onStatus("live");
      this.opts.onOpen();
    };
    socket.onmessage = (e: MessageEvent) => {
      if (socket !== this.socket) return;
      let msg: WsServerMessage;
      try {
        msg = JSON.parse(e.data);
      } catch {
        return;
      }
      // The server couldn't act on something we sent; the connection stays open.
      if (msg.type === "error") console.warn(`WebSocket error from the server: ${msg.code}: ${msg.message}`);
      this.opts.onMessage(msg);
    };
    socket.onclose = () => {
      if (socket !== this.socket) return;
      this.socket = null;
      if (!this.stopped) this.reconnectLater();
    };
  }

  private reconnectLater(): void {
    this.opts.onStatus(this.isOnline() ? "reconnecting" : "offline");
    const base = Math.min(FIRST_DELAY * 2 ** this.attempt, MAX_DELAY);
    const delay = base * (0.75 + 0.5 * (this.opts.random ?? Math.random)());
    this.attempt++;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.open();
    }, delay);
  }

  private onOnline = (): void => {
    if (this.stopped) return;
    if (this.socket?.readyState === OPEN) {
      this.opts.onStatus("live");
      return;
    }
    // Reconnect now rather than waiting out the backoff; an attempt started
    // while offline would only fail.
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    const stale = this.socket;
    this.socket = null;
    stale?.close();
    this.attempt = 0;
    this.opts.onStatus("reconnecting");
    this.open();
  };

  private onOffline = (): void => {
    if (this.stopped) return;
    // The socket is as good as dead, even if it hasn't noticed: after a Wi-Fi
    // drop or a network change it can linger without ever closing. Drop it;
    // onOnline reconnects, and the backoff keeps trying in case the browser
    // was wrong about being offline.
    const socket = this.socket;
    this.socket = null;
    socket?.close();
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.reconnectLater();
  };

  private isOnline(): boolean {
    if (this.opts.isOnline) return this.opts.isOnline();
    return typeof navigator === "undefined" || navigator.onLine;
  }

  private network(): EventTarget | undefined {
    return this.opts.network ?? (typeof window === "undefined" ? undefined : window);
  }
}
