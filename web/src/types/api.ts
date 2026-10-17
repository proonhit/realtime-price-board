// Kiểu dữ liệu khớp với backend/openapi.yaml.

export type Tick = {
  symbol: string;
  price: number;
  ts: number; // unix milliseconds
};

export type User = {
  id: string;
  email: string;
  username: string;
  created_at: string; // ISO 8601
  last_login_at: string | null; // null nếu chưa đăng nhập lần nào
};

/** Kết quả của /api/auth/login và /api/auth/refresh. */
export type TokenPair = {
  access_token: string;
  refresh_token: string;
  expires_in: number; // số giây access token còn hiệu lực
  user: User;
};
