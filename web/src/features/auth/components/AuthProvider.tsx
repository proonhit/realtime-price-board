import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { refreshSession } from "../../../lib/apiClient";
import { session } from "../../../lib/session";
import { meKey, useMe } from "../api/auth";
import { AuthContext } from "../context";
import type { AuthState } from "../types";

// Query chứa dữ liệu riêng của từng user, phải xoá khi phiên kết thúc để user sau
// (cùng trình duyệt) không thấy dữ liệu của user trước.
const userQueryKeys = [meKey, ["watchlist"]];

/** Biết ai đang đăng nhập và chia sẻ cho cả app; mọi trang đọc qua useAuth. */
export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();

  // Đọc access token từ session store; component render lại mỗi khi token đổi.
  const accessToken = useSyncExternalStore(session.subscribe, session.getAccessToken);

  // Mới mở trang mà còn refresh token (từ lần trước) → đang khôi phục phiên.
  const [restoring, setRestoring] = useState(() => session.getRefreshToken() !== null);

  useEffect(() => {
    if (!restoring) return;
    // StrictMode chạy effect hai lần, nhưng refreshSession là single-flight nên chỉ có một request.
    refreshSession().finally(() => setRestoring(false));
  }, [restoring]);

  useEffect(
    () =>
      session.subscribe(() => {
        if (session.getAccessToken() === null) {
          userQueryKeys.forEach((queryKey) => qc.removeQueries({ queryKey }));
        }
      }),
    [qc],
  );

  const me = useMe(accessToken !== null && !restoring);

  let value: AuthState;
  if (restoring) value = { user: null, status: "loading" };
  else if (accessToken === null) value = { user: null, status: "anonymous" };
  else if (me.data) value = { user: me.data, status: "authenticated" };
  else if (me.isPending) value = { user: null, status: "loading" };
  else value = { user: null, status: "anonymous" }; // /me lỗi (ví dụ mất mạng)

  return <AuthContext value={value}>{children}</AuthContext>;
}
