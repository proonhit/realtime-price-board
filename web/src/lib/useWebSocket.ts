// Hook React bọc connectWs: mở kết nối khi component mount, đóng khi unmount.

import { useEffect, useRef, useState } from "react";
import { connectWs, wsUrl, type WsStatus } from "./wsClient";

/**
 * Kết nối WebSocket tới `path` (ví dụ "/ws"), tự kết nối lại khi rớt mạng.
 * `onMessage` không cần useCallback: hook luôn gọi bản mới nhất mà không mở lại socket.
 */
export function useWebSocket(path: string, onMessage: (data: string) => void): { status: WsStatus } {
  const [status, setStatus] = useState<WsStatus>("connecting");

  // Giữ callback mới nhất trong ref. Đổi ref không gây render lại và không chạy lại effect bên dưới.
  const onMessageRef = useRef(onMessage);
  useEffect(() => {
    onMessageRef.current = onMessage;
  });

  // Chỉ phụ thuộc `path`: socket chỉ mở lại khi đổi địa chỉ, không phải mỗi lần render.
  // connectWs trả về hàm đóng, React gọi nó khi unmount (StrictMode dev: mount → đóng → mount lại).
  useEffect(
    () =>
      connectWs(wsUrl(path), {
        onStatus: setStatus,
        onMessage: (data) => onMessageRef.current(data),
      }),
    [path],
  );

  return { status };
}
