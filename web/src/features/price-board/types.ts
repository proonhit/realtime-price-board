import type { Tick } from "../../types/api";
import type { WsStatus } from "../../lib/wsClient";

/** Giá hiện tại của một mã, kèm giá trước đó để biết chiều tăng/giảm. */
export type PriceRow = Tick & { prev?: number };

export type PriceStream = {
  prices: Record<string, PriceRow>;
  status: WsStatus;
};

export type Direction = "up" | "down" | "flat";

export function direction(row: PriceRow): Direction {
  if (row.prev === undefined || row.prev === row.price) return "flat";
  return row.price > row.prev ? "up" : "down";
}
