import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { useAuth } from "../hooks/useAuth";
import { loginPath } from "../types";

/** Bọc trang cần đăng nhập. Chưa đăng nhập thì chuyển sang /login?next=<trang hiện tại>. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth();
  const { pathname, search } = useLocation();

  if (status === "loading") {
    return (
      <p className="empty" role="status">
        <span className="spinner" aria-hidden="true" /> Đang kiểm tra đăng nhập…
      </p>
    );
  }
  if (status === "anonymous") return <Navigate to={loginPath(pathname + search)} replace />;
  return children;
}
