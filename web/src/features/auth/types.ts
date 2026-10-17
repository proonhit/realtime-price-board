import type { User } from "../../types/api";

export type AuthStatus = "loading" | "authenticated" | "anonymous";

/** Thứ AuthProvider chia sẻ cho cả app qua useAuth(). */
export type AuthState = {
  user: User | null;
  status: AuthStatus;
};

/** Đường dẫn trang đăng nhập, kèm trang cần quay lại sau khi đăng nhập xong. */
export function loginPath(next: string): string {
  return `/login?next=${encodeURIComponent(next)}`;
}

/**
 * Chỉ nhận `next` là đường dẫn nội bộ ("/watchlist"). Chặn "https://evil.com" và
 * "//evil.com" để link đăng nhập không bị lợi dụng chuyển người dùng sang trang lạ
 * (lỗi open redirect).
 */
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return "/";
  return next;
}
