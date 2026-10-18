import { useState, type ReactNode } from "react";
import { useWebSocket } from "../../../lib/useWebSocket";
import type { Tick } from "../../../types/api";
import { PriceStreamContext } from "../context";
import type { PriceRow } from "../types";

/** Mở một kết nối WebSocket duy nhất cho cả app; mọi trang đọc giá qua usePriceStream. */
export function PriceStreamProvider({ children }: { children: ReactNode }) {
  const [prices, setPrices] = useState<Record<string, PriceRow>>({});

  // Mỗi message là một tick; lưu giá mới và giữ giá cũ vào `prev` để biết tăng hay giảm.
  const { status } = useWebSocket("/ws", (data) => {
    const t: Tick = JSON.parse(data);
    setPrices((p) => ({ ...p, [t.symbol]: { ...t, prev: p[t.symbol]?.price } }));
  });

  return <PriceStreamContext value={{ prices, status }}>{children}</PriceStreamContext>;
}
