// Nơi giữ token đăng nhập. Không phụ thuộc React để apiClient (trong lib) dùng được.
//
// - Access token: chỉ giữ trong bộ nhớ (biến JS). Reload trang là mất, nhưng nhờ vậy
//   không nằm trong localStorage để script lạ đọc lén lâu dài.
// - Refresh token: lưu localStorage để reload trang vẫn khôi phục được phiên qua /refresh.
//   Đánh đổi: nếu trang bị XSS, script độc đọc được refresh token này. Cách mạnh hơn là
//   backend đặt refresh token vào cookie httpOnly (JS không đọc được) — xem docs/learn/auth-frontend.md.

const REFRESH_KEY = "rpb.refresh_token";

let accessToken: string | null = null;
const listeners = new Set<() => void>();

// localStorage có thể ném lỗi (chế độ riêng tư, bị chặn cookie), nên bọc try/catch.
function readRefresh(): string | null {
  try {
    return localStorage.getItem(REFRESH_KEY);
  } catch {
    return null;
  }
}

function writeRefresh(token: string | null) {
  try {
    if (token === null) localStorage.removeItem(REFRESH_KEY);
    else localStorage.setItem(REFRESH_KEY, token);
  } catch {
    // Không lưu được thì phiên chỉ sống tới khi reload trang.
  }
}

function notify() {
  listeners.forEach((fn) => fn());
}

export const session = {
  getAccessToken: (): string | null => accessToken,
  getRefreshToken: readRefresh,

  /** Lưu cặp token mới (sau login hoặc refresh). */
  set(access: string, refresh: string) {
    accessToken = access;
    writeRefresh(refresh);
    notify();
  },

  /** Xoá phiên (logout hoặc refresh thất bại). */
  clear() {
    accessToken = null;
    writeRefresh(null);
    notify();
  },

  /** Đăng ký nghe thay đổi; trả về hàm huỷ đăng ký. Dạng này khớp useSyncExternalStore. */
  subscribe(fn: () => void): () => void {
    listeners.add(fn);
    return () => {
      listeners.delete(fn);
    };
  },
};
