import { Link, useParams } from "react-router";
import { Card } from "../components/ui/Card";
import { ApiError } from "../lib/apiClient";
import { formatPrice, formatTime } from "../lib/format";
import { usePriceStream } from "../features/price-board/hooks/usePriceStream";
import { direction } from "../features/price-board/types";
import { useHistory } from "../features/price-history/api/getHistory";
import { PriceChart } from "../features/price-history/components/PriceChart";
import { WatchToggle } from "../features/watchlist/components/WatchToggle";

export function SymbolPage() {
  const code = (useParams().code ?? "").toUpperCase();
  const live = usePriceStream().prices[code];
  const history = useHistory(code);

  if (history.error instanceof ApiError && history.error.status === 404) {
    return (
      <Card title="Không tìm thấy mã">
        <p className="empty">
          Mã <b>{code}</b> không tồn tại. <Link to="/">Về bảng giá</Link>
        </p>
      </Card>
    );
  }

  // Nối giá realtime vào cuối lịch sử để biểu đồ chạy liên tục giữa các lần refetch.
  const ticks = history.data ?? [];
  const lastTs = ticks.at(-1)?.ts ?? 0;
  const chartTicks = live && live.ts > lastTs ? [...ticks, live] : ticks;

  return (
    <Card
      title={
        <>
          <Link to="/" className="back" aria-label="Về bảng giá">
            ←
          </Link>{" "}
          {code}
        </>
      }
      actions={<WatchToggle symbol={code} />}
    >
      {live && (
        <p className={`quote price ${direction(live)}`}>
          {formatPrice(live.price)} <small className="muted">lúc {formatTime(live.ts)}</small>
        </p>
      )}
      {history.isPending ? <p className="empty">Đang tải lịch sử…</p> : <PriceChart ticks={chartTicks} />}
    </Card>
  );
}
