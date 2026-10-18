import { NavLink, Outlet, useNavigate } from "react-router";
import { Badge } from "../components/ui/Badge";
import { Button } from "../components/ui/Button";
import { useLogout } from "../features/auth/api/auth";
import { useAuth } from "../features/auth/hooks/useAuth";
import { usePriceStream } from "../features/price-board/hooks/usePriceStream";

const statusLabel = { connecting: "Đang kết nối", open: "Trực tuyến", closed: "Mất kết nối" };
const statusTone = { connecting: "neutral", open: "success", closed: "danger" } as const;

export function RootLayout() {
  const { status } = usePriceStream();
  const auth = useAuth();
  const logout = useLogout();
  const navigate = useNavigate();

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
        {auth.user ? (
          <div className="user">
            <span className="muted">{auth.user.username}</span>
            <Button
              variant="ghost"
              disabled={logout.isPending}
              // Rời trang trước rồi mới đăng xuất: nếu đang ở /watchlist mà đăng xuất trước,
              // RequireAuth sẽ kịp đẩy sang /login?next=/watchlist, không phải điều ta muốn.
              onClick={() => {
                navigate("/");
                logout.mutate();
              }}
            >
              Đăng xuất
            </Button>
          </div>
        ) : (
          auth.status === "anonymous" && <NavLink to="/login">Đăng nhập</NavLink>
        )}
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  );
}
