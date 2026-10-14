import { useQuery } from "@tanstack/react-query";
import { api } from "../../../lib/apiClient";
import type { Tick } from "../../../types/api";

export function getHistory(symbol: string, limit = 200): Promise<Tick[]> {
  return api.get<Tick[]>(`/api/prices/${encodeURIComponent(symbol)}/history?limit=${limit}`);
}

/** Lịch sử giá (cũ → mới), tự làm mới mỗi 5 giây. */
export function useHistory(symbol: string, limit = 200) {
  return useQuery({
    queryKey: ["history", symbol, limit],
    queryFn: () => getHistory(symbol, limit),
    refetchInterval: 5_000,
  });
}
