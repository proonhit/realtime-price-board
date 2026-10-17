import { Card } from "../components/ui/Card";
import { PriceTable } from "../features/price-board/components/PriceTable";
import { WatchAction } from "./shared/WatchAction";

export function BoardPage() {
  return (
    <Card title="Bảng giá">
      <PriceTable actions={(symbol) => <WatchAction symbol={symbol} />} />
    </Card>
  );
}
