import { useEffect, useState, type ReactNode } from "react";
import { connectWs, wsUrl, type WsStatus } from "../../../lib/wsClient";
import type { Tick } from "../../../types/api";
import { PriceStreamContext } from "../context";
import type { PriceRow } from "../types";

/** Mở một kết nối WebSocket duy nhất cho cả app; mọi trang đọc giá qua usePriceStream. */
export function PriceStreamProvider({ children }: { children: ReactNode }) {
  const [prices, setPrices] = useState<Record<string, PriceRow>>({});
  const [status, setStatus] = useState<WsStatus>("connecting");

  useEffect(
    () =>
      connectWs(wsUrl("/ws"), {
        onStatus: setStatus,
        onMessage: (data) => {
          const t: Tick = JSON.parse(data);
          setPrices((p) => ({ ...p, [t.symbol]: { ...t, prev: p[t.symbol]?.price } }));
        },
      }),
    [],
  );

  return <PriceStreamContext value={{ prices, status }}>{children}</PriceStreamContext>;
}
