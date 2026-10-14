import { Card } from "../components/ui/Card";
import { PriceTable } from "../features/price-board/components/PriceTable";
import { WatchToggle } from "../features/watchlist/components/WatchToggle";

export function BoardPage() {
  return (
    <Card title="Bảng giá">
      <PriceTable actions={(symbol) => <WatchToggle symbol={symbol} />} />
    </Card>
  );
}
