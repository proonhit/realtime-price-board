import { Button } from "../../../components/ui/Button";
import { useAddToWatchlist, useRemoveFromWatchlist, useWatchlist } from "../api/watchlist";

/** Nút ☆/★ để thêm hoặc bỏ một mã khỏi danh sách theo dõi. */
export function WatchToggle({ symbol }: { symbol: string }) {
  const { data: list = [] } = useWatchlist();
  const add = useAddToWatchlist();
  const remove = useRemoveFromWatchlist();

  const watched = list.includes(symbol);
  const busy = add.isPending || remove.isPending;
  const label = watched ? `Bỏ theo dõi ${symbol}` : `Theo dõi ${symbol}`;

  return (
    <Button
      variant="ghost"
      className={watched ? "star on" : "star"}
      aria-label={label}
      aria-pressed={watched}
      title={label}
      disabled={busy}
      onClick={() => (watched ? remove.mutate(symbol) : add.mutate(symbol))}
    >
      {watched ? "★" : "☆"}
    </Button>
  );
}
