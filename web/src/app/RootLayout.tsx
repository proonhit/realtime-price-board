import { NavLink, Outlet } from "react-router";
import { Badge } from "../components/ui/Badge";
import { usePriceStream } from "../features/price-board/hooks/usePriceStream";

const statusLabel = { connecting: "Đang kết nối", open: "Trực tuyến", closed: "Mất kết nối" };
const statusTone = { connecting: "neutral", open: "success", closed: "danger" } as const;

export function RootLayout() {
  const { status } = usePriceStream();

  return (
    <div className="layout">
      <header className="topbar">
        <span className="brand">Realtime Price Board</span>
        <nav>
          <NavLink to="/" end>
            Bảng giá
          </NavLink>
          <NavLink to="/watchlist">Theo dõi</NavLink>
        </nav>
        <Badge tone={statusTone[status]}>{statusLabel[status]}</Badge>
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  );
}
