// WebSocket tự kết nối lại với backoff tăng dần (0.5s → 10s).

export type WsStatus = "connecting" | "open" | "closed";

type Handlers = {
  onMessage: (data: string) => void;
  onStatus: (status: WsStatus) => void;
};

export function wsUrl(path: string): string {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${location.host}${path}`;
}

/** Mở kết nối và trả về hàm đóng hẳn (không kết nối lại nữa). */
export function connectWs(url: string, { onMessage, onStatus }: Handlers): () => void {
  let ws: WebSocket | undefined;
  let retry: ReturnType<typeof setTimeout> | undefined;
  let delay = 500;
  let stopped = false;

  const open = () => {
    onStatus("connecting");
    ws = new WebSocket(url);
    ws.onopen = () => {
      delay = 500;
      onStatus("open");
    };
    ws.onmessage = (e) => onMessage(e.data);
    ws.onclose = () => {
      if (stopped) return;
      onStatus("closed");
      retry = setTimeout(open, delay);
      delay = Math.min(delay * 2, 10_000);
    };
  };

  open();
  return () => {
    stopped = true;
    clearTimeout(retry);
    ws?.close();
  };
}
