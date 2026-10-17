import { Link, useLocation } from "react-router";
import { Button } from "../../components/ui/Button";
import { useAuth } from "../../features/auth/hooks/useAuth";
import { loginPath } from "../../features/auth/types";
import { WatchToggle } from "../../features/watchlist/components/WatchToggle";

/**
 * Ghép hai feature auth + watchlist (chỉ tầng pages được làm việc này, vì feature
 * không import lẫn nhau). Chỉ render WatchToggle khi đã đăng nhập, nên query
 * GET /api/watchlist không bao giờ chạy cho khách và không sinh ra loạt lỗi 401.
 */
export function WatchAction({ symbol }: { symbol: string }) {
  const { status } = useAuth();
  const { pathname, search } = useLocation();

  if (status === "authenticated") return <WatchToggle symbol={symbol} />;

  if (status === "loading") {
    // Giữ chỗ cho nút để bảng không bị nhảy khi trạng thái đăng nhập đã rõ.
    return (
      <Button variant="ghost" className="star" disabled aria-label={`Theo dõi ${symbol}`}>
        ☆
      </Button>
    );
  }

  const label = `Đăng nhập để theo dõi ${symbol}`;
  return (
    <Link to={loginPath(pathname + search)} className="btn btn-ghost star watch-login" aria-label={label} title={label}>
      ☆
    </Link>
  );
}
