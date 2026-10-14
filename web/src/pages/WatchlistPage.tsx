import { Card } from "../components/ui/Card";
import { PriceTable } from "../features/price-board/components/PriceTable";
import { useWatchlist } from "../features/watchlist/api/watchlist";
import { WatchToggle } from "../features/watchlist/components/WatchToggle";

export function WatchlistPage() {
  const { data: symbols, isPending, isError } = useWatchlist();

  return (
    <Card title="Danh sách theo dõi">
      {isPending ? (
        <p className="empty">Đang tải…</p>
      ) : isError ? (
        <p className="empty">Không tải được danh sách theo dõi.</p>
      ) : symbols.length === 0 ? (
        <p className="empty">Chưa theo dõi mã nào. Bấm ☆ ở Bảng giá để thêm.</p>
      ) : (
        <PriceTable symbols={symbols} actions={(symbol) => <WatchToggle symbol={symbol} />} />
      )}
    </Card>
  );
}
