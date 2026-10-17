# Đăng nhập ở frontend: đọc hiểu từng dòng

Tài liệu này đi qua phần đăng nhập của `web/` (React + React Query + react-router). Phần backend đã có ở [auth.md](auth.md); ở đây ta chỉ nói phía trình duyệt: giữ token ở đâu, gắn token vào request thế nào, khi access token hết hạn thì làm gì, và bảo vệ trang `/watchlist` ra sao.

Các khung **≈ .NET** và **≈ Laravel** so sánh với thứ bạn đã quen.

## Mục lục

1. [Bức tranh tổng thể](#1-bức-tranh-tổng-thể)
2. [Bốn luồng chính](#2-bốn-luồng-chính)
3. [Giữ token ở đâu: quyết định và đánh đổi](#3-giữ-token-ở-đâu-quyết-định-và-đánh-đổi)
4. [lib/session.ts](#4-libsessionts)
5. [lib/apiClient.ts và single-flight refresh](#5-libapiclientts-và-single-flight-refresh)
6. [types/api.ts](#6-typesapits)
7. [features/auth/](#7-featuresauth)
8. [Pages, router, layout, providers](#8-pages-router-layout-providers)
9. [Nút theo dõi cho khách: WatchAction](#9-nút-theo-dõi-cho-khách-watchaction)
10. [Xoá cache React Query khi đăng xuất](#10-xoá-cache-react-query-khi-đăng-xuất)
11. [RequireAuth so với `[Authorize]` và middleware `auth`](#11-requireauth-so-với-authorize-và-middleware-auth)
12. [Những gì chưa làm](#12-những-gì-chưa-làm)
13. [Câu hỏi phỏng vấn có thể gặp](#13-câu-hỏi-phỏng-vấn-có-thể-gặp)

---

## 1. Bức tranh tổng thể

```
web/src/
├── lib/
│   ├── session.ts        MỚI  giữ access token (RAM) + refresh token (localStorage)
│   └── apiClient.ts      SỬA  gắn Bearer, 401 → refresh một lần → gọi lại
├── types/api.ts          SỬA  thêm User, TokenPair
├── features/auth/        MỚI
│   ├── api/auth.ts            login/register/logout/me + hook React Query
│   ├── components/
│   │   ├── AuthProvider.tsx   khôi phục phiên lúc mở trang, chia sẻ { user, status }
│   │   ├── RequireAuth.tsx    chặn trang cần đăng nhập
│   │   ├── LoginForm.tsx
│   │   ├── RegisterForm.tsx
│   │   └── Field.tsx          ô nhập có label + lỗi (dùng chung cho 2 form)
│   ├── hooks/useAuth.ts
│   ├── context.ts
│   ├── types.ts               AuthState, loginPath, safeNext
│   └── validation.ts          kiểm tra input giống backend, dịch lỗi server
├── pages/
│   ├── LoginPage.tsx     MỚI
│   ├── RegisterPage.tsx  MỚI
│   ├── shared/WatchAction.tsx  MỚI  ghép auth + watchlist
│   ├── BoardPage.tsx, SymbolPage.tsx  SỬA  dùng WatchAction
└── app/
    ├── router.tsx        SỬA  /login, /register, bọc /watchlist bằng RequireAuth
    ├── RootLayout.tsx    SỬA  tên user + "Đăng xuất" / link "Đăng nhập"
    ├── providers.tsx     SỬA  thêm AuthProvider
    └── index.css         SỬA  style form, spinner, nút primary
```

Vì sao `session.ts` nằm trong `lib/` chứ không trong `features/auth/`? Quy tắc của dự án: `features` được import `lib`, nhưng `lib` **không** được import `features`. `apiClient.ts` (trong `lib`) cần đọc token, nên kho token cũng phải ở `lib`. Nó cũng không dùng React, để code ngoài React (apiClient) gọi được.

## 2. Bốn luồng chính

### 2.1 Đăng nhập

```
LoginForm            useLogin (React Query)       apiClient            Backend
   │ submit               │                          │                    │
   │ validateLogin() ok   │                          │                    │
   │ ───── mutate ──────▶ │ login() ───────────────▶ │ POST /api/auth/login ─▶ │
   │                      │                          │ ◀── 200 TokenPair ──── │
   │                      │ onSuccess:               │                    │
   │                      │  setQueryData(["me"], user)                   │
   │                      │  session.set(access, refresh)                 │
   │                      │       │ notify()                              │
   │                      │       ▼                                       │
   │               AuthProvider render lại → status = "authenticated"     │
   │               LoginPage thấy authenticated → <Navigate to={next}>    │
```

### 2.2 Reload trang (khôi phục phiên)

Reload làm mất mọi biến JS, tức mất access token. Refresh token vẫn còn trong localStorage.

```
Mở trang
   │
   ▼
AuthProvider: có refresh token? ── không ──▶ status = "anonymous"
   │ có
   ▼
status = "loading", gọi refreshSession()
   │  POST /api/auth/refresh {refresh_token}
   ├── 200 → session.set(access mới, refresh mới)  (refresh cũ bị thu hồi)
   │         │
   │         ▼
   │       useMe bật → GET /api/auth/me (Bearer access) → status = "authenticated"
   │
   └── 401 → session.clear() → status = "anonymous"
```

### 2.3 Access token hết hạn giữa chừng: 401 → refresh → gọi lại

Access token sống 15 phút. Sau đó mọi request có token sẽ bị 401.

```
WatchToggle A ── GET /api/watchlist (Bearer cũ) ──▶ 401 ─┐
WatchToggle B ── GET /api/watchlist (Bearer cũ) ──▶ 401 ─┤
                                                         ▼
                                     refreshSession()  ← A tạo promise
                                     refreshSession()  ← B nhận LẠI promise đó
                                                         │ chỉ MỘT POST /api/auth/refresh
                                                         ▼
                                     session.set(access mới, refresh mới)
A ── gọi lại với Bearer mới ──▶ 200
B ── gọi lại với Bearer mới ──▶ 200
```

Nếu refresh thất bại: `session.clear()` → AuthProvider thành `anonymous` → `RequireAuth` chuyển sang `/login?next=...`, request gốc ném `ApiError(401)`.

### 2.4 Đăng xuất

```
Bấm "Đăng xuất"
   │ navigate("/")
   │ logout.mutate()
   ▼
POST /api/auth/logout {refresh_token}   (backend thu hồi refresh token, trả 204)
   │ finally (kể cả khi lỗi mạng)
   ▼
session.clear() → listener trong AuthProvider:
   removeQueries(["me"]), removeQueries(["watchlist"])
   → header hiện "Đăng nhập", nút ☆ thành link đăng nhập
```

Access token không thu hồi được (backend không lưu nó), nhưng trình duyệt đã vứt nó khỏi RAM và nó tự hết hạn sau tối đa 15 phút.

## 3. Giữ token ở đâu: quyết định và đánh đổi

| Cách | Reload còn không | XSS đọc được không | CSRF | Ghi chú |
|---|---|---|---|---|
| Cả hai token trong localStorage | Còn | Đọc được cả hai, lâu dài | Không bị | Đơn giản nhất, rủi ro nhất |
| **Access trong RAM, refresh trong localStorage** (dự án này) | Còn (nhờ /refresh) | Đọc được refresh token | Không bị | Cân bằng, backend không cần đổi |
| Access trong RAM, refresh trong cookie `httpOnly` | Còn | **Không** đọc được refresh token | Phải chống CSRF (`SameSite`, CSRF token) | Mạnh nhất, cần backend đặt cookie |
| Chỉ trong RAM | Mất, phải đăng nhập lại | Chỉ đọc được lúc đang mở trang | Không bị | Phiền cho người dùng |

**XSS** (Cross-Site Scripting) là khi kẻ xấu chạy được JavaScript trên trang của mình (ví dụ chèn `<script>` qua một ô nhập không được escape). Script đó chạy với quyền như code của ta: đọc được `localStorage`, gọi API. Nên:

- Access token trong RAM: script độc vẫn có thể gọi API *trong lúc trang đang mở*, nhưng khó lấy token mang đi dùng lâu, và token chỉ sống 15 phút.
- Refresh token trong localStorage: script độc **đọc được** và mang đi đổi lấy access token mới trong 7 ngày. Rotation giúp phát hiện phần nào (ai dùng sau sẽ bị từ chối), nhưng không ngăn được.

**Hướng tốt hơn (chưa làm):** backend trả refresh token trong cookie `Set-Cookie: refresh_token=...; HttpOnly; Secure; SameSite=Strict; Path=/api/auth`. JS không đọc được cookie `HttpOnly`, trình duyệt tự gửi nó tới `/api/auth/refresh`. Đổi lại phải lo CSRF và cấu hình cookie cho đúng domain. Vì backend hiện trả token trong JSON body, frontend chọn phương án ở giữa.

React tự escape mọi chuỗi khi render `{...}`, nên XSS ở React chủ yếu đến từ `dangerouslySetInnerHTML`, thư viện bên thứ ba, hoặc URL `javascript:`. Dự án không dùng những thứ đó.

> **≈ .NET:** ASP.NET Core Identity mặc định dùng cookie `HttpOnly` (cookie authentication), chính là phương án mạnh nhất ở bảng trên. JWT + localStorage thường gặp ở SPA gọi Web API.
> **≈ Laravel:** Sanctum cho SPA cùng domain dùng cookie session + CSRF (`/sanctum/csrf-cookie`), còn token API (`plainTextToken`) thì client tự cất, giống dự án này.

## 4. lib/session.ts

```ts
const REFRESH_KEY = "rpb.refresh_token";

let accessToken: string | null = null;
const listeners = new Set<() => void>();
```

- `REFRESH_KEY`: tên khoá trong localStorage, có tiền tố `rpb.` để không đụng app khác cùng domain (ví dụ localhost).
- `accessToken`: một biến ở cấp module. Trong ES module, biến này chỉ có một bản cho cả app (giống `static` field trong C#). Reload trang là mất.
- `listeners`: danh sách hàm cần gọi khi token đổi. Dùng `Set` để thêm/xoá nhanh và không trùng.

```ts
function readRefresh(): string | null {
  try {
    return localStorage.getItem(REFRESH_KEY);
  } catch {
    return null;
  }
}
```

localStorage có thể ném lỗi (Safari chế độ riêng tư cũ, trình duyệt chặn lưu trữ). Bọc `try/catch` để app vẫn chạy, chỉ là không nhớ phiên qua lần reload. `writeRefresh` làm tương tự: `null` thì `removeItem`, có giá trị thì `setItem`.

```ts
function notify() {
  listeners.forEach((fn) => fn());
}
```

Gọi mọi listener. Đây là **observer pattern**, giống `event` trong C# hoặc `Event::dispatch` trong Laravel.

```ts
export const session = {
  getAccessToken: (): string | null => accessToken,
  getRefreshToken: readRefresh,
  set(access, refresh) { accessToken = access; writeRefresh(refresh); notify(); },
  clear() { accessToken = null; writeRefresh(null); notify(); },
  subscribe(fn) { listeners.add(fn); return () => { listeners.delete(fn); }; },
};
```

- `set` gọi sau login và sau mỗi lần refresh. `clear` gọi khi logout hoặc refresh thất bại.
- `subscribe` trả về hàm huỷ đăng ký. Đúng chữ ký mà `useSyncExternalStore` của React cần (`subscribe(callback) => unsubscribe`), nên AuthProvider dùng thẳng được.
- Hàm huỷ viết `{ listeners.delete(fn); }` có ngoặc nhọn để trả `void`, không trả `boolean` của `Set.delete`.

## 5. lib/apiClient.ts và single-flight refresh

### 5.1 Gửi request

```ts
const NO_REFRESH = ["/api/auth/login", "/api/auth/register", "/api/auth/refresh", "/api/auth/logout"];
```

Các endpoint này **không** được tự refresh khi gặp 401. 401 của `/login` nghĩa là sai mật khẩu; refresh rồi gọi lại vô nghĩa. 401 của `/refresh` mà lại refresh tiếp thì thành vòng lặp vô hạn. Còn `/api/auth/me` là endpoint được bảo vệ bình thường nên vẫn được refresh như `/api/watchlist`.

```ts
function send(method, path, body, token): Promise<Response> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (token) headers.Authorization = `Bearer ${token}`;
  return fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
}
```

Tách riêng phần "gửi" để gửi lại được với token mới. Token truyền vào làm tham số (không đọc thẳng từ session bên trong) để lúc gửi lại biết chắc dùng token nào.

> **≈ .NET:** giống một `DelegatingHandler` gắn header `Authorization` cho `HttpClient`.
> **≈ Laravel:** giống `Http::withToken($token)`.

### 5.2 Single-flight

```ts
let refreshing: Promise<boolean> | null = null;

export function refreshSession(): Promise<boolean> {
  if (!refreshing) {
    refreshing = doRefresh().finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}
```

- Lần gọi đầu: `refreshing` đang `null` → bắt đầu `doRefresh()` và **cất promise** vào `refreshing`.
- Các lần gọi tiếp theo khi promise chưa xong: trả lại **đúng promise đó**. Không có request `/refresh` thứ hai.
- `.finally(...)`: xong (thành công hay thất bại) thì đặt về `null`, lần hết hạn sau sẽ refresh mới.

**Vì sao bắt buộc?** Backend dùng **rotation**: mỗi refresh token chỉ dùng được một lần; dùng xong backend thu hồi nó và cấp cái mới. Giả sử trang có 10 nút ☆ cùng gọi API lúc access token vừa hết hạn, nếu không có single-flight:

```
Request 1: POST /refresh {R1} → 200, R1 bị thu hồi, nhận R2
Request 2: POST /refresh {R1} → 401 (R1 đã dùng rồi!)  → session.clear() → người dùng bị đăng xuất
```

Ở nhiều hệ thống khác còn tệ hơn: backend coi việc dùng lại token đã thu hồi là dấu hiệu bị đánh cắp và thu hồi cả chuỗi token (backend của dự án hiện chỉ trả 401). Single-flight đảm bảo cả 10 request chờ chung một lần refresh.

JavaScript trong trình duyệt chạy **một luồng** (event loop), nên đoạn `if (!refreshing) refreshing = ...` không bị hai luồng chen nhau như trong C#. Không cần `lock` hay `SemaphoreSlim`; chỉ cần nhớ promise.

> **≈ .NET:** giống cache một `Task` dùng chung (`Lazy<Task<T>>`) để nhiều caller `await` cùng một việc.

Bonus: React StrictMode chạy effect hai lần ở môi trường dev. Nhờ single-flight, AuthProvider gọi `refreshSession()` hai lần nhưng chỉ có một request.

### 5.3 doRefresh

```ts
async function doRefresh(): Promise<boolean> {
  const refreshToken = session.getRefreshToken();
  if (!refreshToken) { session.clear(); return false; }

  let res: Response;
  try {
    res = await send("POST", "/api/auth/refresh", { refresh_token: refreshToken }, null);
  } catch {
    return false;
  }
  if (!res.ok) { session.clear(); return false; }

  const pair: TokenPair = await res.json();
  session.set(pair.access_token, pair.refresh_token);
  return true;
}
```

- Không có refresh token → không thể khôi phục, xoá phiên.
- `fetch` ném lỗi (mất mạng, server tắt) → **không** xoá phiên: server chưa hề nói token sai. Trả `false`, lần sau thử lại.
- Server trả lỗi (401: hết hạn / đã thu hồi / đã dùng) → phiên chấm dứt, `session.clear()`. AuthProvider đang lắng nghe nên giao diện tự cập nhật, `RequireAuth` tự chuyển sang trang đăng nhập.
- Thành công → lưu **cả hai** token mới. Phải lưu refresh token mới vì cái cũ đã bị thu hồi.

### 5.4 request: 401 → refresh → gọi lại một lần

```ts
async function request<T>(method, path, body?) {
  const token = session.getAccessToken();
  let res = await send(method, path, body, token);

  if (res.status === 401 && token && !NO_REFRESH.includes(path)) {
    const ok = session.getAccessToken() !== token || (await refreshSession());
    const fresh = session.getAccessToken();
    if (ok && fresh) res = await send(method, path, body, fresh);
  }
  // ...phần dưới giữ nguyên: !res.ok → ApiError, 204 → undefined, còn lại → JSON
}
```

Từng điều kiện:

- `res.status === 401`: chỉ xử lý "chưa xác thực". 403 (không có quyền) thì refresh cũng vô ích.
- `token`: request phải **có gửi** token. Khách chưa đăng nhập nhận 401 là đúng, không refresh.
- `!NO_REFRESH.includes(path)`: xem 5.1.
- `session.getAccessToken() !== token`: trong lúc request này chờ phản hồi, có thể một request khác đã refresh xong. Token đã đổi thì chỉ cần gọi lại, không refresh thêm lần nữa.
- Toán tử `||` ngắt sớm: vế trái `true` thì không chạy `refreshSession()`.
- Chỉ gọi lại **một lần**. Lần thứ hai vẫn 401 thì ném lỗi luôn, tránh vòng lặp.

API công khai `api.get/post/delete` và `ApiError` giữ nguyên, nên mọi hook React Query cũ (`useHistory`, `useWatchlist`...) chạy y như trước mà tự có token.

## 6. types/api.ts

```ts
export type User = {
  id: string;
  email: string;
  username: string;
  created_at: string;            // JSON không có kiểu Date, ngày giờ đến dưới dạng chuỗi ISO 8601
  last_login_at: string | null;  // null nếu chưa đăng nhập lần nào
};

export type TokenPair = {
  access_token: string;
  refresh_token: string;
  expires_in: number;            // giây
  user: User;
};
```

Khớp schema `User` và `TokenPair` trong `backend/openapi.yaml`. Tên trường giữ `snake_case` y như JSON để khỏi phải chuyển đổi. TypeScript chỉ kiểm tra lúc build; lúc chạy `res.json()` trả gì thì nhận nấy (giống `JsonSerializer.Deserialize<T>` không validate).

## 7. features/auth/

### 7.1 types.ts

```ts
export type AuthStatus = "loading" | "authenticated" | "anonymous";
export type AuthState = { user: User | null; status: AuthStatus };
```

Ba trạng thái chứ không phải `boolean isLoggedIn`. Lúc mới mở trang ta **chưa biết** (đang gọi /refresh); nếu chỉ có `true/false` thì `false` sẽ đẩy người dùng sang trang đăng nhập dù một giây sau họ được khôi phục phiên.

```ts
export function loginPath(next: string): string {
  return `/login?next=${encodeURIComponent(next)}`;
}
```

`encodeURIComponent` để `/symbol/FPT?x=1` không làm hỏng query string của `/login`.

```ts
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return "/";
  return next;
}
```

Chống **open redirect**: kẻ xấu gửi link `https://app/login?next=https://evil.com`, người dùng đăng nhập xong bị đưa sang trang giả. Chỉ nhận đường dẫn bắt đầu bằng một dấu `/`. `//evil.com` bị chặn vì trình duyệt hiểu nó là `https://evil.com` (URL không có scheme); `/\evil.com` bị chặn vì một số trình duyệt đổi `\` thành `/`.

> **≈ .NET:** `Url.IsLocalUrl(returnUrl)` / `LocalRedirect`.
> **≈ Laravel:** `redirect()->intended()` chỉ dùng URL đã lưu trong session của chính app.

### 7.2 context.ts và hooks/useAuth.ts

Cùng khuôn với `price-board` (`PriceStreamContext` + `usePriceStream`):

```ts
export const AuthContext = createContext<AuthState | null>(null);

export function useAuth(): AuthState {
  const auth = useContext(AuthContext);
  if (!auth) throw new Error("useAuth must be used inside <AuthProvider>");
  return auth;
}
```

Giá trị mặc định `null` + `throw` giúp phát hiện ngay khi quên bọc `<AuthProvider>`, thay vì lỗi kỳ lạ ở chỗ khác. Context được tách file riêng khỏi component vì plugin `react-refresh` (hot reload) yêu cầu file component chỉ export component.

> **≈ .NET:** Context giống một service đăng ký trong DI container; `useAuth()` giống inject `IHttpContextAccessor` để đọc `User`.

### 7.3 api/auth.ts

```ts
export const meKey = ["me"];
export const login = (input: LoginInput) => api.post<TokenPair>("/api/auth/login", input);
export const register = (input: RegisterInput) => api.post<User>("/api/auth/register", input);
export const getMe = () => api.get<User>("/api/auth/me");
```

Hàm gọi API thuần, giống `watchlist.ts`. `meKey` là khoá cache React Query cho user hiện tại.

```ts
export async function logout(): Promise<void> {
  const refreshToken = session.getRefreshToken();
  try {
    if (refreshToken) await api.post<void>("/api/auth/logout", { refresh_token: refreshToken });
  } finally {
    session.clear();
  }
}
```

`try/finally`: dù backend lỗi hay mất mạng, phiên ở trình duyệt **vẫn** bị xoá. Người dùng bấm "Đăng xuất" thì phải thấy mình đã đăng xuất. Lỗi (nếu có) vẫn ném ra cho `useMutation` biết.

```ts
function startSession(qc: QueryClient, pair: TokenPair) {
  qc.setQueryData(meKey, pair.user);
  session.set(pair.access_token, pair.refresh_token);
}
```

Login trả sẵn `user`, nên ghi thẳng vào cache `["me"]` (khỏi gọi `/me` thêm một lần). Ghi cache **trước** rồi mới `session.set`: khi `set` báo cho AuthProvider, dữ liệu user đã có sẵn, AuthProvider chuyển thẳng sang `authenticated` mà không qua `loading`.

```ts
export function useMe(enabled: boolean) {
  return useQuery({ queryKey: meKey, queryFn: getMe, enabled, staleTime: Infinity, retry: false });
}
```

- `enabled`: chỉ gọi `/me` khi đã có access token.
- `staleTime: Infinity`: thông tin user hầu như không đổi trong phiên, không cần React Query tự refetch khi focus cửa sổ.
- `retry: false`: lỗi xác thực không nên thử lại; apiClient đã lo refresh.

```ts
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
```

Đăng ký xong tự đăng nhập luôn: backend `/register` chỉ trả `User`, không trả token, nên gọi tiếp `/login` bằng chính email/mật khẩu vừa nhập. Gộp trong một `mutationFn` để `isPending` phủ cả hai bước (nút bị khoá suốt quá trình).

`useLogout` chỉ bọc `logout`. Việc xoá cache nằm ở AuthProvider (mục 10).

### 7.4 components/AuthProvider.tsx

```ts
const userQueryKeys = [meKey, ["watchlist"]];
```

Danh sách query chứa dữ liệu **riêng từng user**. `["watchlist"]` chỉ là một chuỗi khoá, không phải import từ feature watchlist, nên không phạm quy tắc "feature không import lẫn nhau". Thêm feature riêng tư mới thì thêm khoá vào đây.

```ts
const accessToken = useSyncExternalStore(session.subscribe, session.getAccessToken);
```

`useSyncExternalStore` là hook chính thức của React để đọc một kho dữ liệu nằm ngoài React. Tham số 1: hàm đăng ký nghe; tham số 2: hàm đọc giá trị hiện tại. Mỗi khi `session.set/clear` gọi `notify()`, React đọc lại token và render lại nếu nó đổi. Không cần tự viết `useState` + `useEffect` để đồng bộ.

```ts
const [restoring, setRestoring] = useState(() => session.getRefreshToken() !== null);

useEffect(() => {
  if (!restoring) return;
  refreshSession().finally(() => setRestoring(false));
}, [restoring]);
```

- Mở trang mà localStorage còn refresh token → `restoring = true` → gọi refresh.
- `useState(() => ...)` (truyền hàm): chỉ đọc localStorage một lần lúc khởi tạo, không đọc lại mỗi lần render.
- Refresh xong (thành công hay không) → `restoring = false`. Kết quả nằm trong session: thành công thì có access token, thất bại thì session đã bị `clear`.

```ts
useEffect(
  () =>
    session.subscribe(() => {
      if (session.getAccessToken() === null) {
        userQueryKeys.forEach((queryKey) => qc.removeQueries({ queryKey }));
      }
    }),
  [qc],
);
```

Đăng ký nghe session; arrow function trả về luôn hàm huỷ đăng ký của `subscribe`, React sẽ gọi nó khi component unmount (dọn dẹp). Khi token về `null` (logout **hoặc** refresh thất bại) thì xoá cache của user.

```ts
const me = useMe(accessToken !== null && !restoring);

let value: AuthState;
if (restoring) value = { user: null, status: "loading" };
else if (accessToken === null) value = { user: null, status: "anonymous" };
else if (me.data) value = { user: me.data, status: "authenticated" };
else if (me.isPending) value = { user: null, status: "loading" };
else value = { user: null, status: "anonymous" };
```

Tính trạng thái từ dữ liệu, không lưu thêm state riêng (ít chỗ để lệch nhau). Đọc từ trên xuống:

1. Đang khôi phục → chưa biết.
2. Không có token → khách.
3. Có token và đã có user → đã đăng nhập.
4. Có token, đang gọi `/me` → chưa biết.
5. Có token nhưng `/me` lỗi (mất mạng) → coi như khách.

`<AuthContext value={value}>`: React 19 cho dùng thẳng context làm provider (không cần `.Provider`).

### 7.5 components/RequireAuth.tsx

```tsx
export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth();
  const { pathname, search } = useLocation();

  if (status === "loading") return <p className="empty" role="status"><span className="spinner" aria-hidden="true" /> Đang kiểm tra đăng nhập…</p>;
  if (status === "anonymous") return <Navigate to={loginPath(pathname + search)} replace />;
  return children;
}
```

- `loading` → hiện spinner. `role="status"` để trình đọc màn hình đọc dòng chữ; spinner `aria-hidden` vì chỉ để trang trí.
- `anonymous` → `<Navigate replace>` sang `/login?next=/watchlist`. `replace` thay mục lịch sử hiện tại, bấm Back không quay lại trang bị chặn rồi bị đẩy đi lần nữa.
- `authenticated` → hiện trang.

### 7.6 Field.tsx, LoginForm.tsx, RegisterForm.tsx

`Field` là ô nhập có label, gợi ý, lỗi:

```tsx
const hintId = hint ? `${id}-hint` : undefined;
const errorId = error ? `${id}-error` : undefined;
const describedBy = [hintId, errorId].filter(Boolean).join(" ") || undefined;

<label htmlFor={id}>{label}</label>
<input id={id} aria-invalid={error ? true : undefined} aria-describedby={describedBy} {...input} />
```

- `htmlFor` nối label với ô nhập (bấm vào chữ thì focus ô, trình đọc màn hình đọc tên ô).
- `aria-describedby` trỏ tới id của gợi ý và lỗi: khi focus ô, trình đọc màn hình đọc cả "Mật khẩu tối thiểu 8 ký tự".
- `aria-invalid` báo ô đang sai; CSS cũng dùng nó để tô viền đỏ.
- `{...input}`: các thuộc tính còn lại (`type`, `autoComplete`, `value`, `onChange`) truyền thẳng xuống `<input>`.

`Field` nằm trong `features/auth` (không phải `components/ui`) vì hiện chỉ auth dùng. Khi feature thứ hai cần thì mới chuyển lên `ui`.

LoginForm:

```tsx
const onSubmit = (e: FormEvent<HTMLFormElement>) => {
  e.preventDefault();
  const input = { email: email.trim(), password };
  const found = validateLogin(input);
  setErrors(found);
  if (Object.keys(found).length === 0) login.mutate(input);
};
```

- `preventDefault()`: chặn trình duyệt submit form kiểu truyền thống (tải lại trang).
- `trim()` email giống backend (`normalizeEmail`). Mật khẩu **không** trim: dấu cách là một phần của mật khẩu.
- Có lỗi thì hiện lỗi, không gọi API.
- `<form noValidate>`: tắt bong bóng kiểm tra mặc định của trình duyệt để hiện thông báo tiếng Việt của mình, nhưng vẫn giữ `type="email"` để bàn phím điện thoại hiện phím `@`.
- `autoComplete`: `email`, `current-password` (login), `username`, `new-password` (đăng ký). Trình quản lý mật khẩu dựa vào đây để tự điền đúng chỗ, và gợi ý mật khẩu mạnh ở ô `new-password`.
- Nút submit `disabled={login.isPending}`: chống bấm hai lần gửi hai request.
- Lỗi server hiện trong `<p role="alert">`: trình đọc màn hình đọc ngay khi nó xuất hiện.

Form **không** tự điều hướng sau khi đăng nhập. Nó chỉ cập nhật session; `LoginPage` thấy `status === "authenticated"` thì `<Navigate>`. Một nguồn sự thật duy nhất, và mở `/login` khi đã đăng nhập cũng tự được chuyển đi.

### 7.7 validation.ts

```ts
const emailRe = /^[^\s@]+@[^\s@]+$/;
const usernameRe = /^[A-Za-z0-9_]{3,50}$/;
const byteLength = (s: string) => new TextEncoder().encode(s).length;
```

- Email: kiểm tra thô "có đúng một @, không có khoảng trắng". Backend kiểm kỹ hơn (`mail.ParseAddress`). Muốn biết email có thật thì phải gửi mail xác nhận.
- Username: y hệt regex của backend.
- Mật khẩu 8–72 **byte** chứ không phải ký tự: bcrypt chỉ dùng 72 byte đầu, và chữ "ệ" chiếm 3 byte UTF-8. `"a".length` trong JS đếm đơn vị UTF-16, không phải byte, nên phải dùng `TextEncoder`.

Kiểm tra ở client chỉ để **báo sớm** cho người dùng. Backend vẫn phải kiểm lại vì ai cũng có thể gọi API bằng curl, bỏ qua form.

```ts
const serverMessages: Record<string, string> = {
  "invalid email or password": "Email hoặc mật khẩu không đúng.",
  "email is already registered": "Email này đã được đăng ký.",
  "username is already taken": "Tên đăng nhập đã có người dùng.",
};

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return serverMessages[err.message] ?? err.message;
  return "Không kết nối được máy chủ. Vui lòng thử lại.";
}
```

Backend trả lỗi tiếng Anh trong `{error}`. Dịch các lỗi đã biết; lỗi lạ thì hiện nguyên văn để không giấu thông tin. Lỗi không phải `ApiError` (thường là `TypeError` của `fetch` khi mất mạng) → thông báo chung.

## 8. Pages, router, layout, providers

**LoginPage / RegisterPage**

```tsx
const [params] = useSearchParams();
const next = safeNext(params.get("next"));
const { status } = useAuth();
if (status === "authenticated") return <Navigate to={next} replace />;
```

Đọc `?next=` từ URL, lọc qua `safeNext`. Đã đăng nhập thì chuyển đi. Link chuyển qua lại giữa hai trang giữ nguyên `next`.

**router.tsx**

```tsx
{ path: "watchlist", element: (<RequireAuth><WatchlistPage /></RequireAuth>) },
{ path: "login", element: <LoginPage /> },
{ path: "register", element: <RegisterPage /> },
```

Bọc ở router (không phải bên trong WatchlistPage) để nhìn file router là biết ngay trang nào cần đăng nhập, giống đọc `routes/web.php`.

**providers.tsx**

```tsx
<QueryClientProvider client={queryClient}>
  <AuthProvider>
    <PriceStreamProvider>{children}</PriceStreamProvider>
  </AuthProvider>
</QueryClientProvider>
```

AuthProvider phải nằm **trong** QueryClientProvider vì nó dùng `useQuery`/`useQueryClient`. Thứ tự với PriceStreamProvider không quan trọng (WebSocket giá là công khai).

**RootLayout.tsx**

```tsx
{auth.user ? (
  <div className="user">
    <span className="muted">{auth.user.username}</span>
    <Button variant="ghost" disabled={logout.isPending}
      onClick={() => { navigate("/"); logout.mutate(); }}>Đăng xuất</Button>
  </div>
) : (
  auth.status === "anonymous" && <NavLink to="/login">Đăng nhập</NavLink>
)}
```

- Đang `loading` thì không hiện gì, tránh nháy chữ "Đăng nhập" rồi đổi thành tên user.
- `navigate("/")` **trước** `logout.mutate()`: nếu đang ở `/watchlist` mà xoá phiên trước, `RequireAuth` sẽ kịp đẩy sang `/login?next=/watchlist`. Về trang chủ trước thì đăng xuất xong người dùng ở lại bảng giá.

## 9. Nút theo dõi cho khách: WatchAction

Vấn đề: bảng giá có nút ☆ ở mỗi dòng. `WatchToggle` gọi `useWatchlist()` → `GET /api/watchlist`. Khi watchlist yêu cầu đăng nhập, khách mở bảng giá sẽ nhận một loạt 401 vô ích.

Các phương án:

| Phương án | Vấn đề |
|---|---|
| `WatchToggle` gọi `useAuth()` | Feature watchlist import feature auth: **phạm quy tắc** |
| Thêm prop `enabled` cho `WatchToggle` → `useWatchlist(enabled)` | Được, nhưng khách vẫn thấy nút ☆ bấm vào thì 401, phải xử lý thêm |
| **Trang quyết định render gì** (đã chọn) | Không đổi gì trong feature watchlist |

`pages/shared/WatchAction.tsx` (tầng pages được phép import nhiều feature):

```tsx
const { status } = useAuth();
if (status === "authenticated") return <WatchToggle symbol={symbol} />;
if (status === "loading") return <Button variant="ghost" className="star" disabled aria-label={`Theo dõi ${symbol}`}>☆</Button>;
return <Link to={loginPath(pathname + search)} className="btn btn-ghost star watch-login" aria-label={`Đăng nhập để theo dõi ${symbol}`}>☆</Link>;
```

- Đã đăng nhập: `WatchToggle` như cũ.
- Đang kiểm tra: ☆ bị khoá, giữ chỗ để bảng không nhảy.
- Khách: ☆ là **link** sang `/login?next=<trang hiện tại>`. Nhìn giống nút nhưng là `<Link>` vì bấm vào thì chuyển trang (ngữ nghĩa đúng cho trình đọc màn hình). `aria-label` nói rõ "Đăng nhập để theo dõi FPT"; chữ dài này không lặp lại trên từng dòng của bảng.

Vì khách **không bao giờ mount** `WatchToggle`, `useWatchlist` không chạy, không có 401 nào. `BoardPage` và `SymbolPage` đổi `WatchToggle` → `WatchAction`. `WatchlistPage` vẫn dùng `WatchToggle` trực tiếp vì nó nằm sau `RequireAuth`.

Đây là ý tưởng **composition**: feature nhỏ, không biết nhau; trang ghép chúng lại, giống `PriceTable` nhận prop `actions` từ trang.

## 10. Xoá cache React Query khi đăng xuất

React Query giữ dữ liệu trong bộ nhớ theo khoá (`["watchlist"]`, `["me"]`). Khoá không chứa user id. Nếu không xoá:

```
A đăng nhập → cache ["watchlist"] = ["FPT", "VNM"]
A đăng xuất
B đăng nhập → WatchToggle đọc cache → thấy ★ ở FPT, VNM của A (cho tới khi refetch xong)
```

Đó là rò rỉ dữ liệu giữa hai người dùng chung máy. Cách xử lý: khi phiên kết thúc, `qc.removeQueries({ queryKey })` cho từng khoá riêng tư. Lần sau đăng nhập, query không có dữ liệu cũ, phải tải mới.

Tại sao đặt trong listener của AuthProvider mà không trong `onSuccess` của `useLogout`? Phiên có **hai** cách kết thúc: bấm Đăng xuất, và refresh thất bại (token hết hạn 7 ngày, bị thu hồi). Cả hai đều đi qua `session.clear()`, nên lắng nghe ở đó là bao được cả hai, chỉ viết một chỗ.

Tại sao không `queryClient.clear()` cho gọn? `clear()` xoá **mọi** query, kể cả lịch sử giá công khai đang hiện trên biểu đồ, làm trang đang mở phải tải lại không cần thiết. `removeQueries` theo khoá chỉ xoá đúng thứ riêng tư.

`removeQueries` khác `invalidateQueries`: invalidate chỉ đánh dấu "cũ" và **vẫn giữ** dữ liệu cũ để hiển thị trong lúc refetch, đúng thứ ta không muốn ở đây.

## 11. RequireAuth so với `[Authorize]` và middleware `auth`

| | ASP.NET `[Authorize]` | Laravel `->middleware('auth')` | `RequireAuth` (React) |
|---|---|---|---|
| Chạy ở đâu | Server | Server | Trình duyệt |
| Chưa đăng nhập | 401, hoặc redirect `/Account/Login?ReturnUrl=...` | redirect `route('login')`, lưu `url.intended` | `<Navigate to="/login?next=...">` |
| Có bảo mật thật không | **Có** | **Có** | **Không**: chỉ là trải nghiệm người dùng |

Điều quan trọng nhất: `RequireAuth` **không bảo vệ dữ liệu**. Ai cũng mở DevTools sửa được code JS chạy trên máy họ. Thứ bảo vệ dữ liệu là backend: middleware `RequireAuth` của Go trả 401 cho `/api/watchlist` khi không có token hợp lệ. `RequireAuth` ở frontend chỉ giúp người dùng không thấy một trang trống toàn lỗi, mà được dẫn tới trang đăng nhập rồi quay lại đúng chỗ.

`?next=` ≈ `ReturnUrl` của ASP.NET ≈ `redirect()->intended()` của Laravel.

## 12. Những gì chưa làm

- **Refresh token trong cookie `httpOnly`** (mục 3): cần đổi backend.
- **Nhiều tab**: hai tab cùng mở, cùng refresh bằng một refresh token cũ → tab chậm hơn bị 401 và đăng xuất. Single-flight chỉ chống trong **một** tab. Cách sửa: dùng `BroadcastChannel` hoặc sự kiện `storage` để các tab chia sẻ token, hoặc Web Locks API (`navigator.locks.request`) để chỉ một tab refresh tại một thời điểm. Đăng xuất ở tab này cũng chưa tự đăng xuất tab kia.
- **Refresh chủ động**: hiện chờ 401 mới refresh. Có thể đặt hẹn giờ refresh trước khi `expires_in` hết, đỡ một request hỏng.
- **Đăng ký thành công nhưng login lỗi** (rất hiếm, ví dụ mất mạng đúng lúc đó): tài khoản đã tạo, form báo lỗi; bấm lại sẽ gặp "Email này đã được đăng ký" và người dùng cần sang trang đăng nhập.
- **Test tự động** cho frontend (Vitest + Testing Library + MSW để giả lập API) chưa có.

## 13. Câu hỏi phỏng vấn có thể gặp

**1. Bạn lưu JWT ở đâu, tại sao?**
Access token trong RAM, refresh token trong localStorage. RAM giảm thời gian token bị lộ (mất khi đóng/reload trang, sống 15 phút). Refresh token cần sống qua reload nên phải lưu bền; localStorage đơn giản và backend hiện trả token trong JSON. Tôi biết điểm yếu là XSS đọc được localStorage; phương án tốt hơn là cookie `HttpOnly; Secure; SameSite` do backend đặt, kèm chống CSRF.

**2. localStorage và cookie khác nhau thế nào về bảo mật?**
localStorage: JS đọc được (lộ khi XSS), trình duyệt không tự gửi (không bị CSRF). Cookie `HttpOnly`: JS không đọc được (an toàn hơn với XSS), nhưng trình duyệt tự gửi kèm request nên phải chống CSRF bằng `SameSite` hoặc CSRF token.

**3. Khi access token hết hạn thì app làm gì?**
Request nhận 401 → apiClient gọi `/api/auth/refresh` một lần → lưu cặp token mới → gọi lại request gốc. Nếu refresh cũng thất bại thì xoá phiên, giao diện chuyển sang trang đăng nhập với `?next=` để quay lại sau.

**4. Single-flight refresh là gì, sao cần?**
Nhiều request cùng gặp 401 thì chỉ có một lời gọi refresh, các request khác chờ chung promise đó. Cần vì refresh token xoay vòng (dùng một lần): hai lời gọi song song gửi cùng token cũ thì lời gọi thứ hai bị từ chối và người dùng bị đăng xuất oan.

**5. JS chạy một luồng, sao vẫn có "race condition"?**
Một luồng nghĩa là không có hai đoạn code chạy *cùng lúc*, nhưng có nhiều việc bất đồng bộ *đang chờ* cùng lúc (nhiều `fetch` đang bay). Race ở đây là giữa các request mạng, không phải giữa các thread. Vì thế chỉ cần lưu một promise dùng chung, không cần lock.

**6. Vì sao không refresh khi `/login` trả 401?**
401 của `/login` nghĩa là sai mật khẩu, refresh không giải quyết được. Và nếu `/refresh` trả 401 mà lại gọi refresh tiếp thì thành vòng lặp vô hạn.

**7. RequireAuth có bảo mật không?**
Không. Nó chỉ điều hướng giao diện. Code frontend chạy trên máy người dùng, ai cũng sửa được. Bảo mật thật nằm ở backend kiểm token cho từng request. So với `[Authorize]` của ASP.NET hay middleware `auth` của Laravel thì những thứ đó chạy ở server nên mới là kiểm soát truy cập thật.

**8. Tại sao có trạng thái "loading" chứ không chỉ đăng nhập / chưa đăng nhập?**
Lúc mở trang, app đang hỏi backend (refresh). Nếu coi "chưa biết" là "chưa đăng nhập" thì người dùng đã đăng nhập bị đá sang `/login` mỗi lần reload, rồi bị đá về lại. Trạng thái thứ ba tránh nháy trang.

**9. Đăng xuất xong, dữ liệu của user cũ còn trong React Query không?**
Không. Khi phiên kết thúc, AuthProvider gọi `removeQueries` cho `["me"]` và `["watchlist"]`. Không dùng `invalidateQueries` vì nó vẫn giữ dữ liệu cũ để hiển thị trong lúc tải lại. Không dùng `clear()` vì nó xoá cả dữ liệu công khai.

**10. Open redirect là gì, bạn chống thế nào?**
Trang đăng nhập nhận `?next=`. Nếu nhận mọi URL, kẻ xấu gửi link `?next=https://evil.com` để sau khi đăng nhập người dùng bị đưa sang trang giả. `safeNext` chỉ nhận đường dẫn nội bộ bắt đầu bằng một dấu `/`, chặn `//evil.com` và `/\evil.com`.

**11. Sao kiểm tra form ở client rồi mà backend vẫn kiểm?**
Client-side validation là trải nghiệm (báo lỗi ngay). Ai cũng gọi API trực tiếp được, nên backend là nơi duy nhất đáng tin. Hai bên dùng cùng luật (regex username, 8–72 byte mật khẩu) để không báo lỗi khác nhau.

**12. Sao đếm mật khẩu theo byte?**
bcrypt chỉ dùng 72 byte đầu. Ký tự có dấu tiếng Việt chiếm 2–3 byte UTF-8, nên mật khẩu 30 ký tự có dấu có thể vượt 72 byte. `string.length` của JS đếm đơn vị UTF-16, nên dùng `TextEncoder` để đếm đúng byte.

**13. Features không được import lẫn nhau, vậy nút theo dõi biết trạng thái đăng nhập bằng cách nào?**
Không cần biết. Tầng pages (được phép dùng nhiều feature) có component `WatchAction` đọc `useAuth()`: đã đăng nhập thì render `WatchToggle`, khách thì render link đăng nhập. Feature watchlist không đổi dòng nào, và query watchlist không bao giờ chạy cho khách.

**14. `useSyncExternalStore` dùng để làm gì?**
Để component React đọc một kho dữ liệu nằm ngoài React (ở đây là `session`) mà vẫn render lại đúng lúc dữ liệu đổi, an toàn với concurrent rendering. Cần hai hàm: `subscribe` và `getSnapshot`.

**15. Mở hai tab thì sao?**
Hiện chưa xử lý: hai tab có thể cùng refresh bằng một token và tab chậm hơn bị đăng xuất. Hướng sửa: `BroadcastChannel`/sự kiện `storage` để chia sẻ token mới giữa các tab, hoặc Web Locks API để chỉ một tab refresh.
