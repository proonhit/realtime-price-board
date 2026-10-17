import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../../../lib/apiClient";
import { session } from "../../../lib/session";
import type { TokenPair, User } from "../../../types/api";

export const meKey = ["me"];

export type LoginInput = { email: string; password: string };
export type RegisterInput = LoginInput & { username: string };

export const login = (input: LoginInput) => api.post<TokenPair>("/api/auth/login", input);
export const register = (input: RegisterInput) => api.post<User>("/api/auth/register", input);
export const getMe = () => api.get<User>("/api/auth/me");

/** Báo backend thu hồi refresh token, rồi xoá phiên ở trình duyệt dù backend có lỗi hay không. */
export async function logout(): Promise<void> {
  const refreshToken = session.getRefreshToken();
  try {
    if (refreshToken) await api.post<void>("/api/auth/logout", { refresh_token: refreshToken });
  } finally {
    session.clear();
  }
}

/** Sau khi có cặp token: đặt sẵn user vào cache (khỏi gọi /me) rồi lưu token. */
function startSession(qc: QueryClient, pair: TokenPair) {
  qc.setQueryData(meKey, pair.user);
  session.set(pair.access_token, pair.refresh_token);
}

/** User đang đăng nhập. Chỉ bật khi đã có access token. */
export function useMe(enabled: boolean) {
  return useQuery({
    queryKey: meKey,
    queryFn: getMe,
    enabled,
    staleTime: Infinity, // thông tin user ít đổi, không cần tự refetch
    retry: false,
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: login,
    onSuccess: (pair) => startSession(qc, pair),
  });
}

/** Đăng ký xong tự đăng nhập luôn bằng chính email/mật khẩu vừa nhập. */
export function useRegister() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: RegisterInput) => {
      await register(input);
      return login({ email: input.email, password: input.password });
    },
    onSuccess: (pair) => startSession(qc, pair),
  });
}

// Xoá cache của user (me, watchlist...) nằm trong AuthProvider, vì phiên còn có thể
// kết thúc khi refresh thất bại chứ không chỉ khi bấm Đăng xuất.
export function useLogout() {
  return useMutation({ mutationFn: logout });
}
