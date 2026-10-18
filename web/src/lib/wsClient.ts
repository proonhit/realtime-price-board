// WebSocket tự kết nối lại với backoff tăng dần (0.5s → 10s) và jitter ngẫu nhiên.
// File này không phụ thuộc React; hook useWebSocket là lớp mỏng bọc bên ngoài.

export type WsStatus = "connecting" | "open" | "closed";

type Handlers = {
  onMessage: (data: string) => void;
  onStatus: (status: WsStatus) => void;
};

const MIN_DELAY = 500;
const MAX_DELAY = 10_000;

export function wsUrl(path: string): string {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${location.host}${path}`;
}

/**
 * Nhân delay với hệ số ngẫu nhiên 0.8–1.2 (±20%).
 * Khi server khởi động lại, mọi client mất kết nối cùng lúc; nếu không có jitter
 * chúng sẽ thử lại đúng cùng thời điểm và dồn tải lên server (thundering herd).
 */
function withJitter(delay: number): number {
  return delay * (0.8 + Math.random() * 0.4);
}

/** Mở kết nối và trả về hàm đóng hẳn (không kết nối lại nữa). */
export function connectWs(url: string, { onMessage, onStatus }: Handlers): () => void {
  let ws: WebSocket | undefined;
  let retry: ReturnType<typeof setTimeout> | undefined;
  let delay = MIN_DELAY;
  let stopped = false;

  const open = () => {
    // Huỷ lần thử đang chờ (nếu có) để luôn chỉ có một socket và một hẹn giờ.
    clearTimeout(retry);
    retry = undefined;

    onStatus("connecting");
    const socket = new WebSocket(url);
    ws = socket;

    socket.onopen = () => {
      delay = MIN_DELAY;
      onStatus("open");
    };
    socket.onmessage = (e) => onMessage(e.data);
    socket.onclose = () => {
      // Bỏ qua sự kiện của socket cũ đã bị thay thế, hoặc khi đã gọi hàm đóng.
      if (stopped || socket !== ws) return;
      onStatus("closed");
      retry = setTimeout(open, withJitter(delay));
      delay = Math.min(delay * 2, MAX_DELAY);
    };
  };

  // Mạng có lại thì thử ngay, không chờ hết thời gian backoff.
  const onOnline = () => {
    if (retry === undefined) return; // đang kết nối hoặc đã mở: không làm gì
    delay = MIN_DELAY;
    open();
  };

  window.addEventListener("online", onOnline);
  open();

  return () => {
    stopped = true;
    clearTimeout(retry);
    window.removeEventListener("online", onOnline);
    ws?.close();
  };
}
