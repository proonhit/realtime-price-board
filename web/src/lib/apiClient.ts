// Gọi REST API bằng đường dẫn tương đối: dev đi qua Vite proxy, production đi qua nginx.
// Tự gắn access token và tự refresh một lần khi gặp 401.

import type { TokenPair } from "../types/api";
import { session } from "./session";

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

// Các endpoint này tự xử lý token, gặp 401 không được refresh rồi gọi lại
// (ví dụ 401 của /login nghĩa là sai mật khẩu, refresh cũng vô ích).
const NO_REFRESH = ["/api/auth/login", "/api/auth/register", "/api/auth/refresh", "/api/auth/logout"];

function send(method: string, path: string, body: unknown, token: string | null): Promise<Response> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (token) headers.Authorization = `Bearer ${token}`;

  return fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

// Single-flight: mọi request cùng lúc gặp 401 dùng chung MỘT lời gọi /refresh.
// Bắt buộc vì refresh token chỉ dùng được một lần (rotation): nếu hai request
// cùng gửi refresh token cũ, cái thứ hai bị từ chối và người dùng bị đăng xuất.
let refreshing: Promise<boolean> | null = null;

/** Đổi refresh token lấy cặp token mới. Trả true nếu thành công. */
export function refreshSession(): Promise<boolean> {
  if (!refreshing) {
    refreshing = doRefresh().finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}

async function doRefresh(): Promise<boolean> {
  const refreshToken = session.getRefreshToken();
  if (!refreshToken) {
    session.clear();
    return false;
  }

  let res: Response;
  try {
    res = await send("POST", "/api/auth/refresh", { refresh_token: refreshToken }, null);
  } catch {
    // Lỗi mạng: server chưa từ chối token, giữ nguyên phiên để lần sau thử lại.
    return false;
  }
  if (!res.ok) {
    // Server từ chối (hết hạn, đã thu hồi, đã dùng rồi): phiên chấm dứt.
    // Ai đang subscribe session (AuthProvider) sẽ cập nhật giao diện.
    session.clear();
    return false;
  }

  const pair: TokenPair = await res.json();
  session.set(pair.access_token, pair.refresh_token);
  return true;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const token = session.getAccessToken();
  let res = await send(method, path, body, token);

  if (res.status === 401 && token && !NO_REFRESH.includes(path)) {
    // Nếu trong lúc chờ, request khác đã refresh xong thì token đã đổi: chỉ cần gọi lại.
    const ok = session.getAccessToken() !== token || (await refreshSession());
    const fresh = session.getAccessToken();
    if (ok && fresh) res = await send(method, path, body, fresh);
  }

  if (!res.ok) {
    let message = res.statusText;
    try {
      message = (await res.json()).error ?? message;
    } catch {
      // body không phải JSON, giữ statusText
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body: unknown) => request<T>("POST", path, body),
  delete: <T = void>(path: string) => request<T>("DELETE", path),
};
