# Watchlist riêng cho từng user: đọc hiểu từng thay đổi

Trước đây cả hệ thống chỉ có **một** watchlist chung: ai gọi `POST /api/watchlist` cũng sửa cùng một danh sách. Sau khi có đăng nhập ([auth.md](auth.md)), mỗi user có danh sách của riêng mình. Đường dẫn API giữ nguyên, chỉ khác: phải gửi `Authorization: Bearer <access token>`.

Tài liệu này đi qua từng file đã đổi, theo thứ tự: migration → store → service → handler → main.go → test.

## Mục lục

1. [Migration 0006](#1-migration-0006)
2. [store.go](#2-storego)
3. [service.go](#3-servicego)
4. [handler.go: nhận middleware chứ không nhận Tokens](#4-handlergo-nhận-middleware-chứ-không-nhận-tokens)
5. [main.go](#5-maingo)
6. [Test](#6-test)
7. [Thử bằng curl](#7-thử-bằng-curl)
8. [Câu hỏi phỏng vấn có thể gặp](#8-câu-hỏi-phỏng-vấn-có-thể-gặp)

---

## 1. Migration 0006

`0006_watchlist_per_user.up.sql`:

```sql
DELETE FROM watchlist;
```

Dòng cũ không biết thuộc về user nào. Có ba lựa chọn: xoá, gán cho một user mặc định, hoặc copy cho mọi user. Đây là dữ liệu dev nên chọn cách đơn giản nhất là xoá, và ghi rõ lý do trong comment. Nếu là production thì **không** được làm vậy.

```sql
ALTER TABLE watchlist
    ADD COLUMN user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE;
```

- `NOT NULL`: thêm cột NOT NULL vào bảng đang có dữ liệu sẽ fail (dòng cũ không có giá trị). Bảng đã rỗng ở bước trên nên không sao.
- `REFERENCES users(id)`: khoá ngoại, không thể thêm watchlist cho user không tồn tại.
- `ON DELETE CASCADE`: xoá user thì các dòng watchlist của họ tự xoá theo. ≈ `OnDelete(DeleteBehavior.Cascade)` trong EF Core, `->cascadeOnDelete()` trong Laravel migration.

```sql
ALTER TABLE watchlist DROP CONSTRAINT watchlist_pkey;
ALTER TABLE watchlist ADD PRIMARY KEY (user_id, symbol);
```

- `watchlist_pkey` là tên Postgres tự đặt cho khoá chính khi tạo bảng (`<bảng>_pkey`).
- Khoá chính mới là **khoá kép** `(user_id, symbol)`: alice có một dòng FPT, bob có một dòng FPT khác, nhưng alice không có hai dòng FPT.
- Thứ tự cột quan trọng: index B-tree của khoá chính sắp theo `user_id` trước, rồi `symbol`. Câu `WHERE user_id = $1 ORDER BY symbol` đọc thẳng index đó, không cần index riêng.

`0006_watchlist_per_user.down.sql` đưa bảng về hình dạng cũ:

```sql
ALTER TABLE watchlist DROP CONSTRAINT watchlist_pkey;

DELETE FROM watchlist w
USING watchlist older
WHERE w.symbol = older.symbol
  AND (older.added_at, older.user_id) < (w.added_at, w.user_id);

ALTER TABLE watchlist DROP COLUMN user_id;
ALTER TABLE watchlist ADD PRIMARY KEY (symbol);
```

- Nếu alice và bob cùng theo dõi FPT thì sau khi bỏ `user_id` sẽ có hai dòng FPT, và `ADD PRIMARY KEY (symbol)` fail. Nên phải bỏ dòng trùng trước.
- `DELETE ... USING` là cú pháp riêng của Postgres để join khi xoá: với mỗi dòng `w`, nếu có dòng `older` cùng symbol mà "nhỏ hơn" thì xoá `w`. Kết quả: mỗi symbol chỉ còn dòng nhỏ nhất.
- `(a, b) < (c, d)` là so sánh tuple (row comparison): so `a` với `c` trước, bằng nhau thì so `b` với `d`. Dùng `user_id` làm tiêu chí phụ vì nhiều dòng có thể có cùng `added_at` (ví dụ chèn trong cùng transaction, `now()` trả cùng một giá trị).
- `DROP COLUMN user_id` cũng tự xoá khoá ngoại trên cột đó.

Đã thử: up → thêm 2 user cùng theo dõi FPT, VNM (4 dòng) → `down 1` còn 2 dòng, khoá chính `(symbol)` → `up` lại thành công.

---

## 2. store.go

Mọi method nhận thêm `userID` và mọi câu SQL có `user_id = $1`:

```go
func (s *Store) List(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Query(ctx,
		`SELECT symbol FROM watchlist WHERE user_id = $1 ORDER BY symbol`, userID)
```

- `$1` là tham số, pgx gửi riêng giá trị cho Postgres, nên không bị SQL injection. ≈ `@userId` trong Dapper/EF, `?` trong Laravel query builder.
- `userID` là string; Postgres suy ra `$1` có kiểu `uuid` (vì so với cột UUID) và pgx gửi string dưới dạng uuid. String không phải UUID hợp lệ thì Postgres báo lỗi; nhưng userID luôn đến từ JWT do chính server ký, nên không gặp.

```go
	`INSERT INTO watchlist (user_id, symbol) VALUES ($1, $2) ON CONFLICT DO NOTHING`
```

`ON CONFLICT DO NOTHING`: thêm mã đã có (trùng khoá chính `(user_id, symbol)`) thì bỏ qua thay vì lỗi. Thêm hai lần cho cùng kết quả (idempotent).

```go
	`DELETE FROM watchlist WHERE user_id = $1 AND symbol = $2`
```

Đây là chỗ quan trọng nhất về bảo mật: **thiếu `user_id = $1` ở câu DELETE thì bob xoá được mã của alice**. Lỗi kiểu này gọi là IDOR / Broken Object Level Authorization, đứng đầu OWASP API Top 10. `tag.RowsAffected() > 0` cho biết có xoá được dòng nào; với bob, FPT của alice "không tồn tại" nên trả 404.

---

## 3. service.go

```go
type store interface {
	List(ctx context.Context, userID string) ([]string, error)
	Add(ctx context.Context, userID, symbol string) error
	Remove(ctx context.Context, userID, symbol string) (bool, error)
}

var _ store = (*Store)(nil)
```

- Interface thêm tham số `userID`. `userID, symbol string` là cách Go viết gọn hai tham số cùng kiểu.
- `var _ store = (*Store)(nil)`: thêm dòng này giống package auth. Nếu quên sửa `Store` cho khớp interface, compiler báo lỗi ngay ở đây thay vì ở `main.go`.

Logic nghiệp vụ không đổi: chuẩn hoá mã (`price.NormalizeSymbol`), gọi store, trả `ErrNotInWatchlist` khi xoá mã không có. Service chỉ chuyển tiếp `userID`; nó không tự lấy user từ đâu cả. Như vậy service không biết gì về HTTP hay JWT, test chỉ cần truyền chuỗi `"alice"`.

---

## 4. handler.go: nhận middleware chứ không nhận Tokens

```go
type Handler struct {
	svc         *Service
	requireAuth func(http.Handler) http.Handler
}

func NewHandler(svc *Service, requireAuth func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, requireAuth: requireAuth}
}
```

Có hai cách:

| | `NewHandler(svc, tokens auth.Tokens)` | `NewHandler(svc, requireAuth func(http.Handler) http.Handler)` (đã chọn) |
|---|---|---|
| Watchlist biết gì | Biết token là JWT, cầm cả secret | Chỉ biết "có một hàm bọc handler" |
| Đổi cách xác thực (session cookie, API key) | Sửa watchlist | Chỉ sửa main.go |
| Test | Phải tạo Tokens thật | Có thể truyền middleware giả |

`func(http.Handler) http.Handler` là "hình dạng" chuẩn của middleware trong Go: nhận handler tiếp theo, trả handler mới bọc ngoài nó. `auth.RequireAuth(tokens)` trả đúng kiểu này. ≈ `app.UseMiddleware<T>()` / `[Authorize]` trong ASP.NET, `->middleware('auth')` trong Laravel route.

Watchlist vẫn import package auth, nhưng chỉ để gọi `auth.UserIDFrom`. Lý do: key của context (`ctxKey`) là kiểu chưa export trong auth, nên chỉ auth đọc được giá trị đã gắn. Đây là phụ thuộc nhỏ và ổn định.

```go
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/watchlist", h.requireAuth(http.HandlerFunc(h.list)))
	...
}
```

- `http.HandlerFunc(h.list)` đổi method `list(w, r)` thành một `http.Handler` (kiểu có method `ServeHTTP`).
- `h.requireAuth(...)` bọc nó lại: request đi qua middleware trước, không có token hợp lệ thì trả 401 và `h.list` không bao giờ chạy.
- Dùng `mux.Handle` (nhận `http.Handler`) thay cho `mux.HandleFunc` (nhận hàm) vì giờ ta truyền handler đã bọc.

```go
func userID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
	}
	return id, ok
}
```

- Hàm trợ giúp dùng chung cho ba handler. Trả `ok = false` khi context không có user id, tức route không được bọc bởi `RequireAuth` (lỗi lập trình). Khi đó trả 401 thay vì chạy tiếp với `userID = ""`. Nguyên tắc "fail closed": thiếu thông tin xác thực thì từ chối, không đoán.
- Mỗi handler mở đầu bằng:

```go
	uid, ok := userID(w, r)
	if !ok {
		return
	}
```

Hàm `userID` đã ghi response 401, handler chỉ cần `return`. Phần còn lại giống trước, chỉ truyền thêm `uid` vào service.

**User id lấy từ đâu?** Luôn từ token (qua context), không bao giờ từ body hay query (`?user_id=...`). Nếu nhận từ client thì ai cũng giả làm người khác được.

---

## 5. main.go

```go
	requireAuth := auth.RequireAuth(tokens)
	watchlist.NewHandler(watch, requireAuth).Register(mux)
```

Middleware được dựng **một lần** từ `tokens` (cùng secret mà auth dùng để ký), rồi truyền cho package cần bảo vệ route. Package sau (ví dụ ví tiền) dùng lại đúng biến `requireAuth` này.

---

## 6. Test

`service_test.go`: `memStore` giờ là `map[string][]string`, key là user id, mô phỏng khoá chính `(user_id, symbol)`.

- `TestRemove` (table-driven) có case `"other user's symbol"`: bob xoá FPT của alice → `ErrNotInWatchlist`.
- `TestListIsolatedPerUser`: alice thêm FPT, bob thêm VNM, mỗi người chỉ thấy mã của mình.

`handler_test.go` dùng **middleware thật** `auth.RequireAuth(testTokens)` với secret riêng cho test, và `testTokens.IssueAccess("alice")` để ký token. Như vậy test đi đúng đường của request thật:

- `TestHandlerRequiresAuth`: cả GET, POST, DELETE không có token hoặc token rác → 401.
- `TestHandlerStatusCodes`: 200, 400 (JSON hỏng, mã lạ), 404.
- `TestHandlerIsolatesUsers`: alice thêm FPT, bob thêm VNM qua HTTP; mỗi người GET chỉ thấy mã của mình; bob DELETE FPT → 404, alice DELETE FPT → 204.
- `TestHandlerWithoutUserInContext`: truyền middleware "cho qua" (không gắn user id) → handler vẫn trả 401. Đây là lợi ích của việc nhận middleware làm tham số: dễ test cả nhánh "quên bọc".

---

## 7. Thử bằng curl

```bash
TOKEN=$(curl -s localhost:8080/api/auth/login -d '{"email":"alice@example.com","password":"password123"}' | jq -r .access_token)

curl -i localhost:8080/api/watchlist                                   # 401
curl -s localhost:8080/api/watchlist -H "Authorization: Bearer $TOKEN"  # []
curl -s localhost:8080/api/watchlist -H "Authorization: Bearer $TOKEN" -d '{"symbol":"fpt"}'  # ["FPT"]
curl -i -X DELETE localhost:8080/api/watchlist/FPT -H "Authorization: Bearer $TOKEN"          # 204
```

Lưu ý: frontend hiện chưa gửi token, nên trang Watchlist trên web sẽ nhận 401 cho tới khi có màn hình đăng nhập.

---

## 8. Câu hỏi phỏng vấn có thể gặp

**Làm sao đảm bảo user A không đọc/xoá được dữ liệu của user B?**
User id chỉ lấy từ token đã kiểm chữ ký, và mọi câu SQL đều lọc `user_id = $1`. Test `TestHandlerIsolatesUsers` kiểm tra đúng điều đó. Thiếu điều kiện này là lỗi IDOR/BOLA.

**Vì sao xoá mã của người khác trả 404 chứ không phải 403?**
Với bob, dòng đó không tồn tại trong "thế giới" của bob. Trả 403 sẽ tiết lộ là tài nguyên có tồn tại (của người khác).

**Vì sao khoá chính là `(user_id, symbol)` chứ không thêm cột `id` tự tăng?**
Cặp này vốn đã duy nhất và là thứ ta tra cứu. Khoá kép vừa chặn trùng vừa là index cho câu List. Thêm `id` thì vẫn phải có unique `(user_id, symbol)`, tức thêm một index thừa.

**Vì sao handler nhận middleware chứ không nhận `auth.Tokens`?**
Dependency inversion: watchlist chỉ cần "một thứ bảo vệ route", không cần biết chi tiết JWT. Đổi cách xác thực không phải sửa watchlist, và test truyền middleware giả được.

**Migration xoá dữ liệu cũ có ổn không?**
Chỉ vì đây là dữ liệu dev. Với production cần kế hoạch: gán cho user mặc định, hoặc chạy theo nhiều bước (thêm cột nullable → backfill → đặt NOT NULL).
