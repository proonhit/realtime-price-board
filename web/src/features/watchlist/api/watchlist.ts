import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../../lib/apiClient";

const key = ["watchlist"];

export const getWatchlist = () => api.get<string[]>("/api/watchlist");
export const addToWatchlist = (symbol: string) => api.post<string[]>("/api/watchlist", { symbol });
export const removeFromWatchlist = (symbol: string) => api.delete(`/api/watchlist/${encodeURIComponent(symbol)}`);

export function useWatchlist() {
  return useQuery({ queryKey: key, queryFn: getWatchlist });
}

export function useAddToWatchlist() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: addToWatchlist,
    onSuccess: (list) => qc.setQueryData(key, list),
  });
}

export function useRemoveFromWatchlist() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: removeFromWatchlist,
    onSuccess: (_, symbol) => qc.setQueryData<string[]>(key, (list) => list?.filter((s) => s !== symbol)),
  });
}
