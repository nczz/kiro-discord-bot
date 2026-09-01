export type TransportStatus = "connecting" | "connected" | "reconnecting" | "disconnected" | "error";

export interface RelayFrame {
  peerID: number;
  payload: Uint8Array;
}

export interface WebShareTransportOptions {
  roomID: string;
  onStatus(status: TransportStatus, detail?: string): void;
  onFrame(frame: RelayFrame): void;
  onError?(error: unknown): void;
}

export class WebShareTransport {
  readonly roomID: string;
  private ws: WebSocket | undefined;
  private stopped = false;
  private reconnectAttempt = 0;
  private reconnectTimer: number | undefined;

  constructor(private readonly options: WebShareTransportOptions) {
    this.roomID = options.roomID;
  }

  connect(): void {
    this.stopped = false;
    this.openSocket(this.reconnectAttempt > 0 ? "reconnecting" : "connecting");
  }

  stop(): void {
    this.stopped = true;
    if (this.reconnectTimer !== undefined) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }
    this.ws?.close(1000, "client stopped");
    this.ws = undefined;
    this.options.onStatus("disconnected");
  }

  send(payload: Uint8Array, targetPeerID = 0): boolean {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return false;
    const frame = new Uint8Array(4 + payload.length);
    new DataView(frame.buffer).setUint32(0, targetPeerID, false);
    frame.set(payload, 4);
    this.ws.send(frame);
    return true;
  }

  private openSocket(status: TransportStatus): void {
    this.options.onStatus(status);
    const ws = new WebSocket(relayURL(this.roomID));
    ws.binaryType = "arraybuffer";
    this.ws = ws;

    ws.addEventListener("open", () => {
      this.reconnectAttempt = 0;
      this.options.onStatus("connected");
    });

    ws.addEventListener("message", (event) => {
      try {
        const data = event.data instanceof ArrayBuffer ? new Uint8Array(event.data) : undefined;
        if (!data || data.length < 4) throw new Error("invalid_relay_frame");
        const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
        const peerID = view.getUint32(0, false);
        this.options.onFrame({ peerID, payload: data.slice(4) });
      } catch (error) {
        this.options.onError?.(error);
      }
    });

    ws.addEventListener("error", () => {
      this.options.onStatus("error");
    });

    ws.addEventListener("close", () => {
      if (this.ws === ws) this.ws = undefined;
      if (this.stopped) return;
      this.scheduleReconnect();
    });
  }

  private scheduleReconnect(): void {
    this.reconnectAttempt += 1;
    const delay = Math.min(15000, 500 * 2 ** Math.min(this.reconnectAttempt, 5));
    this.options.onStatus("reconnecting", `${delay}ms`);
    this.reconnectTimer = window.setTimeout(() => this.openSocket("reconnecting"), delay);
  }
}

function relayURL(roomID: string): string {
  const url = new URL(window.location.href);
  url.hash = "";
  url.pathname = `/r/${encodeURIComponent(roomID)}`;
  url.search = "?role=guest";
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}
