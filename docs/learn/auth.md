# Đăng nhập bằng JWT: đọc hiểu từng dòng

Tài liệu này đi qua toàn bộ code trong `backend/internal/auth/` theo thứ tự nên đọc. Mục tiêu: sau khi đọc xong, bạn giải thích được từng dòng khi bị hỏi trong buổi phỏng vấn.

Nếu bạn đến từ C#/.NET hoặc PHP/Laravel, các khung **≈ .NET** và **≈ Laravel** so sánh với thứ bạn đã quen.

## Mục lục

1. [Bức tranh tổng thể](#1-bức-tranh-tổng-thể)
2. [Database: migrations 0003, 0004, 0005](#2-database-migrations-0003-0004-0005)
3. [password.go](#3-passwordgo)
4. [token.go](#4-tokengo)
5. [errors.go](#5-errorsgo)
6. [store.go](#6-storego)
7. [service.go](#7-servicego)
8. [middleware.go](#8-middlewarego)
9. [handler.go](#9-handlergo)
10. [Nối dây trong cmd/api/main.go và config](#10-nối-dây-trong-cmdapimaingo-và-config)
11. [Test](#11-test)
12. [Thử bằng curl](#12-thử-bằng-curl)
13. [Câu hỏi phỏng vấn có thể gặp](#13-câu-hỏi-phỏng-vấn-có-thể-gặp)

---

## 1. Bức tranh tổng thể

Có hai loại token:

| | Access token | Refresh token |
|---|---|---|
| Dạng | JWT ký HS256 (`header.payload.signature`) | 32 byte ngẫu nhiên, mã hoá base64url |
| Sống bao lâu | Ngắn: 15 phút (`ACCESS_TTL`) | Dài: 7 ngày (`REFRESH_TTL`) |
| Server lưu gì | Không lưu gì, chỉ kiểm chữ ký | Lưu **hash SHA-256** trong bảng `refresh_tokens` |
| Gửi ở đâu | Header `Authorization: Bearer <token>` mỗi request | Chỉ gửi tới `/api/auth/refresh` và `/api/auth/logout` |
| Thu hồi được không | Không (đợi hết hạn) | Có (`revoked_at`) |

Luồng chính:

```
 Client                                  API (Go)                              Postgres
   │                                        │                                      │
   │ POST /api/auth/register                │                                      │
   │ {email, username, password} ─────────▶ │ validate, bcrypt hash                │
   │                                        │ INSERT INTO users ─────────────────▶ │
   │ ◀──────────────────── 201 {user}       │                                      │
   │                                        │                                      │
   │ POST /api/auth/login                   │                                      │
   │ {email, password} ───────────────────▶ │ SELECT user WHERE lower(email)=... ─▶│
   │                                        │ bcrypt compare                       │
   │                                        │ UPDATE last_login ─────────────────▶ │
   │                                        │ tạo refresh token ngẫu nhiên         │
   │                                        │ INSERT refresh_tokens (hash) ──────▶ │
   │                                        │ ký JWT access token                  │
   │ ◀──── 200 {access_token, refresh_token, expires_in, user}                     │
   │                                        │                                      │
   │ GET /api/auth/me                       │                                      │
   │ Authorization: Bearer <access> ──────▶ │ RequireAuth: kiểm chữ ký + hạn JWT   │
   │                                        │ (không cần DB) → user id vào context │
   │                                        │ SELECT user WHERE id=... ──────────▶ │
   │ ◀──────────────────── 200 {user}       │                                      │
   │                                        │                                      │
   │  ... 15 phút sau, access token hết hạn → 401 ...                              │
   │                                        │                                      │
   │ POST /api/auth/refresh                 │                                      │
   │ {refresh_token} ─────────────────────▶ │ BEGIN                                │
   │                                        │ SELECT ... FOR UPDATE (khoá dòng) ─▶ │
   │                                        │ còn hạn & chưa thu hồi?              │
   │                                        │ UPDATE revoked_at = now() ─────────▶ │
   │                                        │ INSERT token mới ──────────────────▶ │
   │                                        │ COMMIT                               │
   │ ◀──── 200 {access_token mới, refresh_token mới, ...}                          │
   │                                        │                                      │
   │ POST /api/auth/logout                  │                                      │
   │ {refresh_token} ─────────────────────▶ │ UPDATE revoked_at = now() ─────────▶ │
   │ ◀──────────────────── 204              │                                      │
```

Các file và vai trò (giống mọi package khác trong repo, ví dụ `watchlist`):

```
internal/auth/
├── password.go      bcrypt hash / compare
├── token.go         ký và kiểm JWT, sinh refresh token + hash
├── errors.go        các lỗi nghiệp vụ (ErrEmailTaken, ErrInvalidToken, ...)
├── store.go         SQL thô với pgx          ≈ Repository / Eloquent query
├── service.go       logic nghiệp vụ          ≈ Service class
├── middleware.go    RequireAuth              ≈ [Authorize] / middleware('auth')
├── handler.go       HTTP: đọc JSON, gọi service, trả status  ≈ Controller
└── *_test.go        test
```

Vì sao không chia thư mục `controllers/`, `services/`, `repositories/` như .NET/Laravel? Repo này chọn **chia package theo tính năng** (package-by-feature): mọi thứ về auth nằm một chỗ, mở một thư mục là thấy hết. Trong Go, package cũng là ranh giới đóng gói: tên viết thường (`store`, `ctxKey`, `decode`) chỉ dùng được bên trong package, nên chia theo tính năng giúp giấu chi tiết tốt hơn.

---

## 2. Database: migrations 0003, 0004, 0005

### 0003_create_users

```sql
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (lower(email));
```

- `id UUID ... DEFAULT gen_random_uuid()`: DB tự sinh id. Dùng UUID thay vì số tự tăng để người ngoài không đoán được có bao nhiêu user hay id kế tiếp là gì.
- `password_hash`: chỉ lưu hash bcrypt, không bao giờ lưu mật khẩu gốc.
- `TIMESTAMPTZ`: thời gian có múi giờ (Postgres lưu dạng UTC). ≈ `DateTimeOffset` trong .NET.
- `last_login` cho phép NULL: user mới đăng ký chưa đăng nhập lần nào.
- `idx_users_email ON users (lower(email))`: unique index trên **biểu thức** `lower(email)`. Nhờ nó `Alice@x.com` và `alice@x.com` không thể cùng tồn tại.

### 0004_create_refresh_tokens

```sql
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash char(64) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens (user_id);
```

- `REFERENCES users(id) ON DELETE CASCADE`: khoá ngoại; xoá user thì token của user đó tự bị xoá.
- `token_hash char(64)`: SHA-256 ra 32 byte = 64 ký tự hex, luôn đúng 64 nên dùng `char(64)`. `UNIQUE` tạo sẵn index nên tra theo hash rất nhanh.
- `revoked_at`: NULL = còn dùng được; có giá trị = đã thu hồi (khi logout hoặc khi đã đổi sang token mới).
- Index trên `user_id`: để sau này "đăng xuất mọi thiết bị" (`UPDATE ... WHERE user_id = $1`) chạy nhanh.

### 0005_users_case_insensitive_username (mới)

```sql
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_username_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (lower(username));
```

- Khi viết `email ... UNIQUE` trong `CREATE TABLE`, Postgres tự tạo constraint tên `users_email_key`. Nó thừa vì `idx_users_email` (trên `lower(email)`) đã chặn chặt hơn: hai email giống hệt nhau thì `lower()` cũng giống nhau. Bỏ đi thì mỗi lần INSERT bớt phải cập nhật một index.
- Username cũng nên không phân biệt hoa thường (`Alice` và `alice` dễ bị dùng để giả danh nhau), nên thay `users_username_key` bằng unique index trên `lower(username)`. Người dùng vẫn thấy username đúng như họ gõ, chỉ việc so trùng là không phân biệt hoa thường.
- Không sửa 0003/0004 vì chúng đã chạy trên DB. Quy tắc chung: migration đã chạy thì coi như bất biến, muốn đổi thì viết migration mới. ≈ giống hệt `dotnet ef migrations add` hay `php artisan make:migration`.
- File `.down.sql` làm ngược lại: xoá index mới, tạo lại hai constraint cũ.

---

## 3. password.go

```go
func HashPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}
```

- `[]byte(password)`: bcrypt làm việc với byte, không phải string.
- `bcrypt.DefaultCost` = 10, nghĩa là 2^10 vòng lặp. Cost càng cao, hash càng chậm, kẻ trộm DB đoán mật khẩu càng tốn thời gian. Mỗi lần hash mất khoảng vài chục ms: chậm có chủ đích.
- Kết quả có dạng `$2a$10$<salt 22 ký tự><hash 31 ký tự>`. **Salt** (chuỗi ngẫu nhiên) nằm ngay trong chuỗi hash, nên hai người cùng mật khẩu vẫn ra hai hash khác nhau, và không cần cột salt riêng.
- Go không có exception: hàm trả về `(kết quả, error)` và người gọi tự `if err != nil`. ≈ thay cho `try/catch`.

```go
func CheckPassword(hash, plain string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	return err == nil
}
```

- `CompareHashAndPassword` đọc salt và cost từ trong `hash`, hash lại `plain` theo đúng thông số đó rồi so sánh (so sánh constant-time). ≈ `Hash::check()` của Laravel, `PasswordHasher.VerifyHashedPassword` của ASP.NET Identity.

---

## 4. token.go

File này đã có từ trước (comment tiếng Anh). Tóm tắt:

```go
type Tokens struct {
	Secret []byte
	TTL    time.Duration
}
```

Một struct nhỏ giữ secret để ký JWT và thời gian sống của access token. Các method có receiver `(t Tokens)` (giá trị, không phải con trỏ) vì struct không bị sửa.

**`IssueAccess(userID)`**: tạo JWT với các claim chuẩn:
- `sub` (Subject) = user id: token này là của ai.
- `iat` (IssuedAt) = lúc tạo; `exp` (ExpiresAt) = lúc tạo + TTL.
- Ký bằng HS256 (HMAC-SHA256 với `Secret`). Ai không có secret thì không tạo được chữ ký hợp lệ.

**`ParseAccess(token)`**: kiểm chữ ký, kiểm hạn, trả `sub`.
- `jwt.WithValidMethods([]string{"HS256"})`: chỉ chấp nhận HS256. Đây là phòng lỗi kinh điển: kẻ tấn công sửa header thành `"alg":"none"` (không chữ ký) hoặc đổi sang thuật toán khác để lừa server.
- `jwt.WithExpirationRequired()`: token không có `exp` cũng bị từ chối.

**`NewRefreshToken()`**: lấy 32 byte từ `crypto/rand` (nguồn ngẫu nhiên an toàn cho mật mã, khác `math/rand`), mã hoá base64url thành chuỗi 43 ký tự (`raw`), và trả kèm `HashRefreshToken(raw)`.

**`HashRefreshToken(raw)`**: SHA-256 rồi hex. Vì sao refresh token dùng SHA-256 mà mật khẩu dùng bcrypt? Mật khẩu do người đặt, thường yếu, nên cần hash **chậm** để chống đoán. Refresh token là 256 bit ngẫu nhiên, không thể đoán dù hash nhanh, nên SHA-256 đủ và giúp tra cứu bằng index (bcrypt có salt ngẫu nhiên nên không thể `WHERE token_hash = ?`).

---

## 5. errors.go

```go
var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrUserNotFound       = errors.New("user not found")
)
```

- Đây là **sentinel error**: biến lỗi dùng chung, so sánh bằng `errors.Is(err, ErrEmailTaken)`. ≈ thay cho việc định nghĩa `class EmailTakenException : Exception` rồi `catch (EmailTakenException)`.
- Gom vào một file để nhìn là biết package có thể trả những lỗi nghiệp vụ nào. Handler sẽ đổi từng lỗi thành một HTTP status.

---

## 6. store.go

Tầng truy cập DB. ≈ Repository trong .NET, hoặc chỗ bạn gọi `DB::table(...)` / Eloquent trong Laravel. Ở đây viết **SQL thô** với thư viện pgx, không dùng ORM, giống `watchlist/store.go`.

### Struct User

```go
type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
}
```

- Chuỗi trong dấu backtick là **struct tag**: chỉ cho `encoding/json` biết tên field khi chuyển sang JSON. ≈ `[JsonPropertyName("created_at")]`.
- `json:"-"`: không bao giờ đưa field này ra JSON. Nhờ vậy dù handler trả nguyên `User`, hash mật khẩu cũng không lọt ra. ≈ `[JsonIgnore]` hoặc `$hidden = ['password']` trong model Laravel.
- `*time.Time` (con trỏ): để biểu diễn NULL. `nil` → JSON `null`. ≈ `DateTime?` trong C#.
- Tên Go là `LastLoginAt`, cột DB là `last_login`: pgx không map theo tên mà theo **thứ tự** trong `Scan`, nên hai tên khác nhau không sao.

### Store và NewStore

```go
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }
```

- `pgxpool.Pool` là connection pool: giữ sẵn nhiều kết nối, an toàn khi nhiều goroutine (nhiều request) dùng cùng lúc. ≈ `DbContext` + connection pooling của ADO.NET.
- `NewStore` là "constructor" kiểu Go: Go không có `new ClassName()` với constructor, quy ước là hàm `NewXxx`.

### userColumns và scanUser

```go
const userColumns = `id::text, email, username, password_hash, created_at, last_login`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}
```

- Một hằng số chứa danh sách cột, dùng cho mọi câu SELECT/RETURNING, nên thứ tự cột luôn khớp với thứ tự `Scan`.
- `id::text`: ép UUID sang text trong SQL để Go đọc vào `string`, khỏi cần kiểu UUID riêng.
- `row.Scan(&u.ID, ...)`: truyền **địa chỉ** (`&`) các field để pgx ghi giá trị vào.
- `pgx.ErrNoRows`: không có dòng nào. Ta đổi nó thành lỗi nghiệp vụ `ErrUserNotFound`, để tầng service không cần biết gì về pgx.

### CreateUser

```go
func (s *Store) CreateUser(ctx context.Context, email, username, passwordHash string) (User, error) {
	row := s.db.QueryRow(ctx,
		`INSERT INTO users (email, username, password_hash) VALUES ($1, $2, $3)
		 RETURNING `+userColumns,
		email, username, passwordHash)
	u, err := scanUser(row)
```

- `(s *Store)`: đây là method của `Store`. ≈ `this` trong C#.
- `ctx context.Context`: tham số đầu của mọi hàm có I/O. Nếu client ngắt kết nối hay server đang tắt, ctx bị huỷ và câu SQL dừng theo. ≈ `CancellationToken`.
- `$1, $2, $3`: tham số hoá, **chống SQL injection**. Không bao giờ nối chuỗi input vào SQL. ≈ `@p1` trong ADO.NET, `?` trong PDO.
- `RETURNING ...`: Postgres trả về dòng vừa INSERT (kèm `id`, `created_at` do DB sinh) trong cùng một lệnh, khỏi phải SELECT lại.

```go
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch {
		case strings.Contains(pgErr.ConstraintName, "email"):
			return User{}, ErrEmailTaken
		case strings.Contains(pgErr.ConstraintName, "username"):
			return User{}, ErrUsernameTaken
		}
	}
	return u, err
}
```

- `errors.As` kiểm xem `err` có phải (hoặc bọc) một `*pgconn.PgError` không, nếu có thì gán vào `pgErr`. ≈ `catch (PostgresException ex)`.
- `23505` là mã lỗi `unique_violation` chuẩn của Postgres.
- `ConstraintName` là tên index bị vi phạm (`idx_users_email` hoặc `idx_users_username`), nhìn tên là biết trùng cột nào.
- **Vì sao không SELECT kiểm tra trùng trước rồi mới INSERT?** Vì có race condition: hai request đăng ký cùng email đến cùng lúc, cả hai SELECT đều thấy "chưa có", cả hai INSERT. Unique index trong DB là chốt chặn duy nhất chắc chắn đúng, nên ta cứ INSERT và đọc lỗi.

### UserByEmail, UserByID

```go
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.db.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, email))
}
```

- Viết `lower(email) = ...` (đúng biểu thức của index) thì Postgres mới dùng được `idx_users_email`. Nếu viết `email = lower($1)` thì index biểu thức bị bỏ qua.
- `UserByID` tương tự, tra theo khoá chính.

### UpdateLastLogin

```go
func (s *Store) UpdateLastLogin(ctx context.Context, id string) (time.Time, error) {
	var at time.Time
	err := s.db.QueryRow(ctx,
		`UPDATE users SET last_login = now() WHERE id = $1 RETURNING last_login`, id).Scan(&at)
```

- Dùng `now()` của DB (không dùng giờ máy chủ Go) và `RETURNING` để trả đúng giá trị đã lưu, rồi service gán vào user trả về cho client.

### SaveRefreshToken

```go
	_, err := s.db.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, expiresAt)
```

- `Exec` dùng cho lệnh không cần đọc dòng kết quả. `_` bỏ qua giá trị trả về đầu tiên (command tag).
- Chỉ lưu `hash`, không lưu token gốc.

### RotateRefreshToken: transaction và FOR UPDATE

Đây là hàm quan trọng nhất để nói trong phỏng vấn.

```go
func (s *Store) RotateRefreshToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
```

- `Begin` mở transaction. ≈ `using var tx = await conn.BeginTransactionAsync()` / `DB::beginTransaction()`.
- `defer tx.Rollback(ctx)`: `defer` chạy lệnh khi hàm kết thúc, bất kể kết thúc ở `return` nào. Nếu đã `Commit` thành công thì `Rollback` không làm gì; nếu return sớm vì lỗi thì transaction bị huỷ. ≈ khối `using` / `finally` trong C#.

```go
	var (
		userID    string
		expiresAt time.Time
		revokedAt *time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT user_id::text, expires_at, revoked_at FROM refresh_tokens
		 WHERE token_hash = $1 FOR UPDATE`, oldHash).Scan(&userID, &expiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", err
	}
```

- Chú ý dùng `tx.QueryRow` (không phải `s.db.QueryRow`) để câu lệnh chạy **trong** transaction.
- `FOR UPDATE` khoá dòng này cho tới khi transaction kết thúc. Transaction khác cũng muốn `FOR UPDATE` (hay UPDATE) dòng đó sẽ phải **đứng chờ**.
- Không tìm thấy hash → `ErrInvalidToken` (token bịa hoặc đã bị xoá cùng user).

```go
	if revokedAt != nil || !time.Now().Before(expiresAt) {
		return "", ErrInvalidToken
	}
```

- Đã thu hồi (`revokedAt != nil`) hoặc đã hết hạn (bây giờ không còn trước `expiresAt`) → từ chối.

```go
	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1`, oldHash); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, newHash, newExpiresAt); err != nil {
		return "", err
	}
	return userID, tx.Commit(ctx)
}
```

- Thu hồi token cũ, lưu token mới, commit. Hai việc này **cùng thành công hoặc cùng thất bại**: không bao giờ có cảnh token cũ bị thu hồi mà token mới không được lưu (user bị đăng xuất oan) hoặc ngược lại.
- `if _, err := ...; err != nil`: cú pháp Go cho phép khai báo biến ngay trong `if`; `err` này chỉ sống trong khối `if`.

**Vì sao cần FOR UPDATE?** Giả sử không có nó và hai request refresh với **cùng một token** đến cùng lúc (ví dụ hai tab trình duyệt, hoặc kẻ trộm token và chủ thật):

```
Request A                          Request B
SELECT → chưa thu hồi              SELECT → chưa thu hồi     ← cả hai đều thấy hợp lệ
UPDATE revoked_at                  UPDATE revoked_at
INSERT token A'                    INSERT token B'
COMMIT                             COMMIT                    ← một token đổi được hai lần
```

Với `FOR UPDATE`:

```
Request A                          Request B
SELECT ... FOR UPDATE (khoá)       SELECT ... FOR UPDATE → phải chờ A
UPDATE, INSERT, COMMIT (mở khoá)
                                   đọc lại dòng: revoked_at đã có → ErrInvalidToken
```

Ở mức isolation mặc định của Postgres (READ COMMITTED), khi B được mở khoá, Postgres đọc lại phiên bản mới nhất của dòng nên B thấy `revoked_at` đã có giá trị.

### RevokeRefreshToken

```go
	_, err := s.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now()
		 WHERE token_hash = $1 AND revoked_at IS NULL`, hash)
```

- `AND revoked_at IS NULL`: token đã thu hồi thì giữ nguyên thời điểm thu hồi đầu tiên.
- Không kiểm tra có dòng nào bị ảnh hưởng không: logout với token không tồn tại vẫn coi là thành công. Thao tác cho cùng kết quả dù gọi một hay nhiều lần gọi là **idempotent**.

---

## 7. service.go

Logic nghiệp vụ, không biết gì về HTTP hay SQL. ≈ Service class trong .NET/Laravel.

### Interface store

```go
type store interface {
	CreateUser(ctx context.Context, email, username, passwordHash string) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	...
}

var _ store = (*Store)(nil)
```

- `store` (viết thường) là interface **chưa export**, khai báo ở phía **dùng** (service), liệt kê đúng những method service cần.
- Trong Go, một type thoả mãn interface **ngầm định**: `*Store` có đủ method là được, không cần viết `implements`. Vì thế test có thể truyền `memStore` (store giả trong bộ nhớ) vào `NewService` mà `Store` thật không hề biết.
- ≈ .NET: giống `IUserRepository` + DI + Moq. Khác là ở .NET interface thường nằm cạnh class thật và class phải khai báo `: IUserRepository`; Go thì ngược lại.
- `var _ store = (*Store)(nil)`: dòng không chạy gì lúc runtime, chỉ bắt compiler kiểm tra `*Store` có đủ method. Nếu sau này đổi chữ ký một method mà quên sửa, build sẽ báo lỗi ngay tại đây.

### TokenPair, Service, NewService

```go
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
}

type Service struct {
	store      store
	tokens     Tokens
	refreshTTL time.Duration
}

func NewService(s store, tokens Tokens, refreshTTL time.Duration) *Service { ... }
```

- `ExpiresIn` tính bằng giây, theo quy ước của OAuth2, để frontend biết khi nào nên refresh.
- Các phụ thuộc được truyền vào qua constructor. ≈ constructor injection, chỉ là Go làm thủ công trong `main.go` chứ không có DI container.

### Biến cấp package

```go
var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,50}$`)

var dummyHash, _ = HashPassword("dummy-password-for-timing")
```

- Regex được compile **một lần** khi chương trình khởi động, không compile lại mỗi request. `MustCompile` panic nếu regex sai, chấp nhận được vì regex là hằng do ta viết.
- `dummyHash`: một hash bcrypt giả, xem phần Login. `_` bỏ qua lỗi vì hash một chuỗi cố định không thể lỗi.

### Register

```go
func (s *Service) Register(ctx context.Context, email, username, password string) (User, error) {
	email = normalizeEmail(email)
	username = strings.TrimSpace(username)
```

- Chuẩn hoá trước khi kiểm tra: email bỏ khoảng trắng hai đầu và đổi sang chữ thường; username chỉ bỏ khoảng trắng (giữ nguyên hoa thường để hiển thị).

```go
	if !validEmail(email) {
		return User{}, fmt.Errorf("%w: email is not valid", ErrInvalidInput)
	}
```

- `fmt.Errorf("%w: ...", ErrInvalidInput)`: tạo lỗi mới có thông điệp chi tiết (`invalid input: email is not valid`) nhưng **bọc** (`%w` = wrap) `ErrInvalidInput` bên trong. Nhờ vậy handler vẫn `errors.Is(err, ErrInvalidInput)` được, và có thể trả nguyên `err.Error()` cho client. ≈ `new ValidationException("...", inner)`.
- `User{}` là giá trị rỗng (zero value) của struct: khi có lỗi thì giá trị kia không có ý nghĩa.

```go
	if !usernameRe.MatchString(username) {
		return User{}, fmt.Errorf("%w: username must be 3-50 characters of letters, digits or _", ErrInvalidInput)
	}
	if len(password) < 8 || len(password) > 72 {
		return User{}, fmt.Errorf("%w: password must be 8-72 bytes", ErrInvalidInput)
	}
```

- Username: chỉ chữ cái Latin, số, gạch dưới, dài 3 tới 50 (cột DB là `VARCHAR(50)`).
- Mật khẩu: `len(string)` trong Go đếm **byte**, không đếm ký tự. bcrypt chỉ xử lý tối đa 72 byte (phần sau bị cắt bỏ ở nhiều thư viện; thư viện Go thì trả lỗi). Một chữ tiếng Việt có dấu chiếm 2-3 byte trong UTF-8, nên giới hạn theo byte mới chính xác.

```go
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	return s.store.CreateUser(ctx, email, username, hash)
}
```

- Hash rồi giao cho store. Lỗi trùng (`ErrEmailTaken`, `ErrUsernameTaken`) từ store được trả thẳng lên handler.

### Login

```go
func (s *Service) Login(ctx context.Context, email, password string) (TokenPair, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return TokenPair{}, fmt.Errorf("%w: email and password are required", ErrInvalidInput)
	}
```

- Thiếu trường thì báo 400 ngay, không tốn công tra DB.

```go
	user, err := s.store.UserByEmail(ctx, email)
	if errors.Is(err, ErrUserNotFound) {
		CheckPassword(dummyHash, password)
		return TokenPair{}, ErrInvalidCredentials
	}
	if err != nil {
		return TokenPair{}, err
	}
	if !CheckPassword(user.PasswordHash, password) {
		return TokenPair{}, ErrInvalidCredentials
	}
```

Hai kỹ thuật bảo mật nằm ở đây:

1. **Cùng một lỗi cho "không có email" và "sai mật khẩu"**. Nếu trả hai thông báo khác nhau, kẻ tấn công thử hàng loạt email sẽ biết email nào đã có tài khoản (gọi là **user enumeration**), rồi tập trung dò mật khẩu hoặc gửi phishing cho đúng những người đó.
2. **Vẫn chạy bcrypt khi không tìm thấy user**. bcrypt mất vài chục ms. Nếu email không tồn tại mà ta trả về ngay, phản hồi nhanh hơn hẳn và kẻ tấn công chỉ cần đo thời gian là biết (gọi là **timing attack**). So với `dummyHash` (cùng cost) làm hai nhánh mất thời gian gần như nhau. Kết quả so sánh bị bỏ qua.

- Lỗi khác (`err != nil` nhưng không phải NotFound, ví dụ mất kết nối DB) được trả lên để handler trả 500.

```go
	at, err := s.store.UpdateLastLogin(ctx, user.ID)
	if err != nil {
		return TokenPair{}, err
	}
	user.LastLoginAt = &at
```

- Ghi lại lần đăng nhập và cập nhật bản `user` sẽ trả cho client. `&at` lấy địa chỉ của `at` vì field là con trỏ.

```go
	raw, hash, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.store.SaveRefreshToken(ctx, user.ID, hash, time.Now().Add(s.refreshTTL)); err != nil {
		return TokenPair{}, err
	}
	return s.pair(user, raw)
}
```

- Sinh refresh token, lưu hash với hạn = bây giờ + 7 ngày, rồi `pair` ký access token và ghép kết quả. Client nhận `raw`; DB chỉ có `hash`.

### Refresh

```go
func (s *Service) Refresh(ctx context.Context, raw string) (TokenPair, error) {
	if raw == "" {
		return TokenPair{}, ErrInvalidToken
	}
	newRaw, newHash, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	userID, err := s.store.RotateRefreshToken(ctx, HashRefreshToken(raw), newHash, time.Now().Add(s.refreshTTL))
	if err != nil {
		return TokenPair{}, err
	}
```

- Client gửi token gốc; ta hash nó để tra trong DB (DB chỉ có hash).
- Sinh token mới **trước** khi vào transaction để transaction ngắn nhất có thể (giữ khoá càng ngắn càng tốt).
- Mọi kiểm tra (tồn tại, thu hồi, hết hạn) và việc đổi token nằm trong `RotateRefreshToken` (mục 6).

```go
	user, err := s.store.UserByID(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return TokenPair{}, ErrInvalidToken
	}
	if err != nil {
		return TokenPair{}, err
	}
	return s.pair(user, newRaw)
}
```

- Lấy thông tin user để trả kèm. Trường hợp user vừa bị xoá thì coi token là không hợp lệ.

**Rotation là gì, vì sao làm?** Mỗi lần refresh, token cũ chết và client nhận token mới. Nếu refresh token bị lộ, kẻ trộm và chủ thật sẽ "tranh nhau" dùng; ai dùng sau sẽ bị 401, nên vụ lộ token không âm thầm kéo dài 7 ngày. (Bản nâng cao: phát hiện một token đã thu hồi bị dùng lại thì thu hồi luôn mọi token của user đó. Repo hiện chưa làm.)

### Logout

```go
func (s *Service) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	return s.store.RevokeRefreshToken(ctx, HashRefreshToken(raw))
}
```

- Token rỗng: không có gì để thu hồi, coi như thành công (idempotent).
- Lưu ý: access token đã cấp **vẫn dùng được tới khi hết hạn** (tối đa 15 phút), vì JWT không lưu ở server nên không có gì để xoá. Đây là lý do access token phải sống ngắn.

### Me, pair, normalizeEmail, validEmail

```go
func (s *Service) Me(ctx context.Context, userID string) (User, error) {
	return s.store.UserByID(ctx, userID)
}
```

- Chỉ đọc user theo id (id lấy từ token đã được middleware kiểm).

```go
func (s *Service) pair(user User, refreshRaw string) (TokenPair, error) {
	access, err := s.tokens.IssueAccess(user.ID)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refreshRaw,
		ExpiresIn:    int(s.tokens.TTL.Seconds()),
		User:         user,
	}, nil
}
```

- Hàm phụ dùng chung cho Login và Refresh. `TTL.Seconds()` trả `float64` (900.0), ép sang `int`.

```go
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	if len(email) > 255 || strings.Count(email, "@") != 1 {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}
```

- Dài tối đa 255 (theo cột DB), đúng một ký tự `@`.
- `mail.ParseAddress` (thư viện chuẩn) kiểm cú pháp theo RFC 5322. Nó cũng chấp nhận dạng `"Bob <bob@x.com>"`, nên ta thêm điều kiện `addr.Address == email` để chỉ nhận địa chỉ trần.
- Kiểm tra cú pháp không chứng minh email có thật; muốn chắc phải gửi mail xác nhận (chưa làm).

---

## 8. middleware.go

≈ .NET: `[Authorize]` + JWT bearer authentication middleware. ≈ Laravel: `->middleware('auth:sanctum')`.

### ctxKey

```go
type ctxKey struct{}
```

- Key để gắn giá trị vào `context`. Dùng một kiểu riêng, chưa export: package khác không thể tạo ra đúng key này, nên không thể vô tình ghi đè hay đọc trộm. Nếu dùng key là string `"user_id"`, hai package khác nhau dùng cùng chuỗi sẽ đè lên nhau.
- `struct{}` là struct rỗng, không tốn bộ nhớ.

### RequireAuth

```go
func RequireAuth(t Tokens) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
```

Ba tầng hàm lồng nhau, đọc từ ngoài vào:

1. `RequireAuth(t)` nhận cấu hình (Tokens) và trả về một **middleware**.
2. Middleware là hàm `func(next http.Handler) http.Handler`: nhận handler tiếp theo, trả về handler mới bọc quanh nó. Đây là mẫu middleware chuẩn trong Go.
3. Handler mới là một closure (hàm vô danh) xử lý request.

≈ ASP.NET Core: giống `app.Use(next => async ctx => { ...; await next(ctx); })`. ≈ Laravel: giống `handle($request, Closure $next)`.

```go
			scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
				unauthorized(w, "missing bearer token")
				return
			}
```

- `strings.Cut("Bearer abc", " ")` → `"Bearer"`, `"abc"`, `true`. Không có dấu cách thì `ok = false`.
- `EqualFold` so sánh không phân biệt hoa thường (chuẩn HTTP coi `bearer` và `Bearer` như nhau).
- Thiếu header, sai scheme (ví dụ `Basic`), hoặc token rỗng → 401 và **return**, không gọi `next`.

```go
			userID, err := t.ParseAccess(token)
			if err != nil {
				unauthorized(w, "invalid or expired token")
				return
			}
```

- Kiểm chữ ký và hạn. Không cần DB: đây là ưu điểm của JWT (stateless). Không trả chi tiết lỗi cho client.

```go
			ctx := context.WithValue(r.Context(), ctxKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
```

- `context.WithValue` tạo context **mới** chứa thêm user id (context trong Go là bất biến, không sửa được cái cũ). `r.WithContext(ctx)` tạo bản sao request mang context mới, rồi chuyển cho handler tiếp theo.
- ≈ .NET: `HttpContext.Items["UserId"] = ...` hoặc `HttpContext.User`. ≈ Laravel: `$request->attributes->set(...)` / `Auth::user()`.

### UserIDFrom

```go
func UserIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok
}
```

- `ctx.Value(...)` trả về `any` (kiểu bất kỳ). `.(string)` là **type assertion**: thử ép về `string`; dạng hai giá trị `id, ok` không panic nếu sai kiểu hay không có giá trị, mà trả `ok = false`.
- Đây là cách duy nhất để package khác đọc user id, vì `ctxKey` không export. ≈ `User.FindFirst(ClaimTypes.NameIdentifier)` / `auth()->id()`.

### unauthorized

```go
func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	httpx.Error(w, http.StatusUnauthorized, msg)
}
```

- Header `WWW-Authenticate: Bearer` là quy định của chuẩn HTTP cho response 401: báo client cần gửi loại credential nào. Header phải set **trước** khi `httpx.Error` gọi `WriteHeader`.

---

## 9. handler.go

Tầng HTTP. ≈ Controller. Chỉ làm ba việc: đọc input, gọi service, đổi kết quả/lỗi thành HTTP response.

### Handler và Register

```go
type Handler struct {
	svc    *Service
	tokens Tokens
}

func NewHandler(svc *Service, tokens Tokens) *Handler { ... }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/register", h.register)
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.Handle("GET /api/auth/me", RequireAuth(h.tokens)(http.HandlerFunc(h.me)))
}
```

- Từ Go 1.22, pattern của `ServeMux` có cả method (`"POST /api/..."`). Gọi sai method thì mux tự trả 405. ≈ `[HttpPost("api/auth/login")]` / `Route::post(...)`.
- `h.register` là **method value**: một hàm đã gắn sẵn `h`, truyền đi được như biến.
- Dòng `/me`: `http.HandlerFunc(h.me)` đổi hàm thường thành `http.Handler`, rồi `RequireAuth(h.tokens)(...)` bọc nó bằng middleware. Chỉ route này cần đăng nhập. ≈ đặt `[Authorize]` trên đúng một action.

### decode

```go
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(v); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
```

- `http.MaxBytesReader(..., 1<<12)`: đọc tối đa 4096 byte; vượt quá thì Decode lỗi. Chặn client gửi body hàng trăm MB làm server hết RAM. ≈ `[RequestSizeLimit]`.
- `v any`: nhận con trỏ tới struct bất kỳ để decode vào.
- Trả `bool` để handler viết gọn `if !decode(...) { return }`; response 400 đã được ghi bên trong.

### register

```go
	var body struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
```

- Struct vô danh khai báo ngay trong hàm, chỉ dùng cho request này. ≈ một `RegisterRequest` DTO.

```go
	user, err := h.svc.Register(r.Context(), body.Email, body.Username, body.Password)
	switch {
	case errors.Is(err, ErrInvalidInput):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrEmailTaken), errors.Is(err, ErrUsernameTaken):
		httpx.Error(w, http.StatusConflict, err.Error())
	case err != nil:
		httpx.Internal(w, err)
	default:
		httpx.JSON(w, http.StatusCreated, user)
	}
```

- `switch {` không có biểu thức: mỗi `case` là một điều kiện bool, chạy case đầu tiên đúng. Đây là cách viết thay cho chuỗi `if/else if`, cùng phong cách với `watchlist/handler.go`.
- Thứ tự quan trọng: các lỗi đã biết trước, `err != nil` (lỗi lạ) sau, `default` (thành công) cuối.
- `httpx.Internal` log lỗi thật ở server và chỉ trả `"internal error"` cho client: **không bao giờ lộ lỗi nội bộ** (câu SQL, tên bảng...) ra ngoài.
- 201 Created cho tạo mới, 409 Conflict cho trùng.

### login, refresh, logout

- `login`: giống register; `ErrInvalidCredentials` → 401.
- `refresh`: body `{refresh_token}` (struct `refreshBody` dùng chung với logout); `ErrInvalidToken` → 401.
- `logout`: luôn 204 No Content (không có body), trừ khi JSON hỏng (400) hoặc lỗi DB (500). Logout không đòi access token, vì refresh token trong body chính là thứ cần thu hồi, và client có thể logout cả khi access token đã hết hạn.

### me

```go
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
```

- Đọc user id mà middleware đã gắn. `!ok` chỉ xảy ra nếu lập trình viên quên bọc route bằng RequireAuth: kiểm tra phòng thủ.

```go
	user, err := h.svc.Me(r.Context(), userID)
	switch {
	case errors.Is(err, ErrUserNotFound):
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
	...
```

- Token còn hạn nhưng user đã bị xoá → 401 (token không còn đại diện cho ai).

---

## 10. Nối dây trong cmd/api/main.go và config

```go
	cfg := config.Load()
	if len(cfg.JWTSecret) < 32 {
		log.Println("WARNING: JWT_SECRET is unset or shorter than 32 bytes; set a long random value outside local dev")
	}
	...
	tokens := auth.Tokens{Secret: []byte(cfg.JWTSecret), TTL: cfg.AccessTTL}
	authSvc := auth.NewService(auth.NewStore(db), tokens, cfg.RefreshTTL)
	...
	auth.NewHandler(authSvc, tokens).Register(mux)
```

- Tạo các đối tượng theo thứ tự phụ thuộc: Store (cần db) → Service (cần store, tokens) → Handler (cần service, tokens) → đăng ký route. ≈ những gì DI container của ASP.NET (`builder.Services.AddScoped<...>()`) hay service container của Laravel làm tự động, ở đây viết tay cho rõ ràng.
- `tokens` dùng chung cho Service (ký token) và Handler (middleware kiểm token), nên cùng một secret.

**Về JWT_SECRET:** trong `config.go`, giá trị mặc định đổi từ `"secret"` thành `"dev-only-insecure-secret"`. Cách chọn ở đây là đơn giản nhất:

- Giữ một mặc định để `go run ./cmd/api` chạy được ngay khi dev local, không cần set biến môi trường.
- Mặc định này cố ý ngắn hơn 32 byte, nên chỉ cần **một** điều kiện `len < 32` là cảnh báo cả hai trường hợp: quên set biến (đang dùng mặc định) và set secret quá ngắn.
- Chỉ cảnh báo, không dừng chương trình, theo yêu cầu không làm hỏng dev local. Môi trường thật (docker-compose) đã set secret 64 ký tự.
- Vì sao 32 byte? HS256 dùng khoá HMAC-SHA256; khoá ngắn hơn 256 bit làm giảm độ an toàn, và secret ngắn/dễ đoán có thể bị brute-force offline từ một JWT bất kỳ. Tạo secret bằng: `openssl rand -hex 32`.
- Cảnh báo đặt ở `cmd/api` chứ không ở `config.Load()`, vì `cmd/producer` cũng gọi `config.Load()` nhưng không dùng JWT.

---

## 11. Test

Chạy: `cd backend && go test ./...`

### service_test.go: memStore

```go
type memStore struct {
	mu     sync.Mutex
	users  []User
	tokens map[string]*memToken
}
```

- Store giả lưu trong slice/map, thoả mãn interface `store`. Test chạy trong mili giây, không cần Postgres. ≈ fake repository hoặc EF Core InMemory provider; khác với mock (Moq) ở chỗ nó có hành vi thật (nhớ dữ liệu), không cần setup từng lời gọi.
- Fake phải mô phỏng đúng quy tắc của DB thật: so trùng email/username bằng `strings.EqualFold` (không phân biệt hoa thường) như unique index trên `lower()`; `RotateRefreshToken` giữ `mu` suốt hàm, tương đương transaction + `FOR UPDATE`.
- `sync.Mutex` để fake an toàn nếu bị gọi từ nhiều goroutine (và `go test -race` không báo).

### Table-driven test

```go
	tests := []struct {
		name     string
		email    string
		username string
		password string
		wantErr  error
	}{
		{"ok", "bob@example.com", "bob_99", "password123", nil},
		{"duplicate email different case", " ALICE@Example.com ", "alice2", "password123", ErrEmailTaken},
		...
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { ... })
	}
```

- Một bảng các trường hợp + một vòng lặp chạy từng cái làm subtest có tên. Đây là phong cách test chuẩn của Go. ≈ `[Theory] + [InlineData]` của xUnit, `@dataProvider` của PHPUnit.
- `errors.Is(err, tt.wantErr)` với `wantErr = nil` cũng đúng khi `err == nil`, nên một dòng kiểm cho cả case thành công lẫn thất bại.

Các trường hợp đã test: đăng ký ok, trùng email khác hoa thường, trùng username khác hoa thường, email/username/mật khẩu sai định dạng; login ok, sai mật khẩu, email không tồn tại (cùng lỗi); refresh ok và token cũ không dùng lại được; refresh với token lạ, rỗng, đã logout, đã hết hạn; logout gọi nhiều lần; Me.

### handler_test.go: httptest

```go
func do(mux http.Handler, method, path, body, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	...
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
```

- `httptest.NewRequest` tạo request giả, `httptest.NewRecorder` là `ResponseWriter` ghi lại status/header/body. Gọi thẳng `mux.ServeHTTP` mà không cần mở cổng mạng. ≈ `WebApplicationFactory` + `HttpClient` trong ASP.NET, `$this->postJson(...)->assertStatus(201)` trong Laravel.
- Dùng router thật (`Register(mux)`) nên test kiểm luôn pattern route và middleware.
- Có test riêng đảm bảo response đăng ký không chứa chữ `password` (hash không bị lộ).

### middleware_test.go

- Các case: thiếu header, sai scheme, `Bearer ` rỗng, token rác, token hết hạn → 401 và handler phía sau **không chạy**; token hợp lệ (cả `bearer` viết thường) → 200 và handler đọc được đúng user id qua `UserIDFrom`.

---

## 12. Thử bằng curl

Chạy hạ tầng và api (xem README), rồi:

```bash
# Đăng ký → 201
curl -i -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","username":"alice","password":"password123"}'

# Đăng ký lại cùng email (khác hoa thường) → 409
curl -i -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ALICE@example.com","username":"alice2","password":"password123"}'

# Đăng nhập → 200 {access_token, refresh_token, expires_in, user}
curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}'

# Sai mật khẩu hoặc email không tồn tại → cùng 401 {"error":"invalid email or password"}
curl -i -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"wrong-password"}'
```

Lưu token vào biến cho tiện (cần `jq`):

```bash
RESP=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}')
ACCESS=$(echo "$RESP" | jq -r .access_token)
REFRESH=$(echo "$RESP" | jq -r .refresh_token)

# Thông tin user hiện tại → 200; bỏ header → 401
curl -i http://localhost:8080/api/auth/me -H "Authorization: Bearer $ACCESS"
curl -i http://localhost:8080/api/auth/me

# Refresh → 200 với cặp token mới
curl -s -X POST http://localhost:8080/api/auth/refresh \
  -H 'Content-Type: application/json' \
  -d "{\"refresh_token\":\"$REFRESH\"}"

# Dùng lại refresh token cũ → 401 (đã bị rotate)
curl -i -X POST http://localhost:8080/api/auth/refresh \
  -H 'Content-Type: application/json' \
  -d "{\"refresh_token\":\"$REFRESH\"}"

# Đăng xuất → 204 (gọi bao nhiêu lần cũng 204)
curl -i -X POST http://localhost:8080/api/auth/logout \
  -H 'Content-Type: application/json' \
  -d "{\"refresh_token\":\"$REFRESH\"}"
```

Xem thử nội dung JWT: dán access token vào https://jwt.io. Payload chỉ là base64, ai cũng đọc được (`sub`, `iat`, `exp`), nên **không bao giờ để dữ liệu nhạy cảm trong JWT**. Chữ ký chỉ đảm bảo không ai sửa được nội dung.

---

## 13. Câu hỏi phỏng vấn có thể gặp

**Access token và refresh token khác nhau thế nào? Sao không dùng một token sống 7 ngày cho gọn?**
Access token là JWT, server kiểm bằng chữ ký mà không cần tra DB, nên rất nhanh, nhưng không thu hồi được. Vì không thu hồi được nên phải cho nó sống ngắn (15 phút): bị lộ thì thiệt hại có giới hạn. Refresh token sống dài nhưng được lưu (dạng hash) ở DB nên thu hồi được bất cứ lúc nào (logout, rotation), và chỉ gửi tới một endpoint duy nhất, ít bị lộ hơn. Kết hợp hai loại cho cả tốc độ lẫn khả năng thu hồi.

**Vì sao lưu hash của refresh token chứ không lưu token gốc?**
Giống lý do không lưu mật khẩu gốc: nếu DB bị lộ (backup rơi ra ngoài, SQL injection ở chỗ khác), kẻ tấn công chỉ có hash và không dùng được để gọi `/refresh`. Dùng SHA-256 (nhanh) chứ không cần bcrypt vì token là 256 bit ngẫu nhiên, không thể đoán ngược; và SHA-256 không có salt nên tra cứu được bằng index (`WHERE token_hash = $1`).

**Vì sao login sai email và sai mật khẩu trả cùng một lỗi?**
Để chống user enumeration: nếu thông báo khác nhau, kẻ tấn công biết được email nào đã đăng ký. Ngoài thông báo, thời gian phản hồi cũng phải giống nhau, nên khi không có user ta vẫn chạy bcrypt với một hash giả. (Endpoint đăng ký vẫn trả 409 "email đã tồn tại", đó là đánh đổi phổ biến để UX tốt; muốn kín hoàn toàn thì phải xác nhận qua email.)

**FOR UPDATE để làm gì? Không có thì sao?**
Không có nó, hai request refresh cùng lúc với cùng một token đều đọc thấy token hợp lệ và đều nhận token mới: một token dùng được hai lần, phá vỡ rotation. `SELECT ... FOR UPDATE` trong transaction khoá dòng: request thứ hai phải chờ request đầu commit, rồi đọc lại thấy token đã bị thu hồi. Cách khác cũng đúng: một câu `UPDATE ... SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now() RETURNING user_id` (atomic). Dùng FOR UPDATE ở đây rõ ràng hơn khi cần kiểm nhiều điều kiện và làm nhiều bước.

**Tại sao dùng transaction cho rotation?**
Thu hồi token cũ và lưu token mới phải cùng thành công hoặc cùng thất bại. Nếu thu hồi xong mà lưu token mới lỗi, user bị đăng xuất oan; nếu ngược lại, token cũ vẫn sống.

**Lưu token ở đâu trên frontend: localStorage hay httpOnly cookie?**

| | localStorage / bộ nhớ JS | Cookie httpOnly + Secure + SameSite |
|---|---|---|
| XSS (script lạ chạy trên trang) | Script đọc được token và gửi đi nơi khác | Script không đọc được cookie |
| CSRF (trang khác lừa trình duyệt gửi request) | Không bị, vì trình duyệt không tự gửi header `Authorization` | Có nguy cơ, vì trình duyệt tự gửi cookie; cần `SameSite=Lax/Strict` và/hoặc CSRF token |
| Code | Đơn giản, tự thêm header | Server phải set cookie; khác domain thì cấu hình CORS `credentials` phức tạp hơn |

Cách hay được khuyên: giữ access token trong **bộ nhớ** (biến JS, mất khi reload), refresh token trong **cookie httpOnly, Secure, SameSite=Strict, Path=/api/auth**. Reload trang thì gọi `/refresh` để lấy access token mới. API hiện tại trả refresh token trong JSON body cho đơn giản; chuyển sang cookie là một bước nâng cấp hợp lý.

**Logout có làm access token hết hiệu lực không?**
Không. JWT stateless nên server không có gì để xoá; access token vẫn dùng được tới khi hết hạn (tối đa 15 phút). Muốn chặn ngay thì phải có danh sách đen (ví dụ lưu `jti` vào Redis tới khi hết hạn) hoặc giảm TTL, đổi lại phải tra cứu mỗi request.

**JWT có bị mã hoá không? Để gì trong payload?**
Không, chỉ được **ký**. Payload là base64url, ai cũng đọc được. Chỉ để id và các claim cần thiết, không để email, quyền chi tiết nhạy cảm hay mật khẩu.

**Vì sao chỉ chấp nhận HS256 khi parse?**
Để chống tấn công đổi thuật toán: kẻ tấn công sửa header thành `alg: none` (không chữ ký) hoặc thuật toán khác. `jwt.WithValidMethods` khoá đúng thuật toán server dùng.

**HS256 và RS256 khác gì, khi nào dùng cái nào?**
HS256 dùng một secret chung để ký và kiểm; đơn giản, hợp khi chỉ một backend vừa ký vừa kiểm. RS256 ký bằng private key, kiểm bằng public key; hợp khi nhiều service khác cần kiểm token mà không được phép tạo token.

**Vì sao bcrypt chứ không phải SHA-256 cho mật khẩu? Vì sao giới hạn 72 byte?**
bcrypt chậm có chủ đích và có salt, nên brute-force mật khẩu yếu từ DB bị lộ rất tốn kém; SHA-256 quá nhanh (GPU thử hàng tỷ lần mỗi giây). bcrypt chỉ dùng 72 byte đầu; thư viện Go từ chối mật khẩu dài hơn, nên ta kiểm và báo lỗi rõ ràng thay vì trả 500.

**Vì sao không SELECT kiểm tra email trùng trước khi INSERT?**
Race condition: hai request cùng lúc đều thấy "chưa có". Unique index trong DB mới là đảm bảo thật; code đọc mã lỗi `23505` để trả 409.

**Vì sao service phụ thuộc vào interface `store` chứ không phải `*Store`?**
Để test thay bằng store giả trong bộ nhớ, không cần Postgres; và service không dính chặt vào pgx. Trong Go, interface nên được khai báo ở nơi dùng và nhỏ nhất có thể.

**Vì sao key của context là `struct{}` riêng chứ không phải string?**
Để không package nào khác tạo được cùng key, tránh va chạm và ghi đè ngoài ý muốn.

**Có thể cải thiện gì thêm?**
Rate limit cho `/login` (chống dò mật khẩu), phát hiện tái sử dụng refresh token đã rotate để thu hồi cả "họ" token, dọn định kỳ token hết hạn, xác nhận email, chuyển refresh token sang cookie httpOnly. (Watchlist riêng cho từng user đã làm, xem [watchlist.md](watchlist.md): ví dụ đầu tiên dùng `RequireAuth` ngoài package auth.)
