# Chuyển tiền giữa hai ví ảo: transaction, FOR UPDATE và idempotency key

Tính năng: mỗi user có một ví ảo (tiền VND), có thể chuyển tiền cho user khác bằng username. Nghe đơn giản, nhưng đây là bài toán kinh điển trong phỏng vấn backend vì nó gom đủ các chủ đề khó: **kiểu dữ liệu cho tiền**, **transaction**, **khoá dòng**, **deadlock**, **request bị gửi lặp**.

Tài liệu đi qua lý thuyết trước (mục 1–5), rồi đọc code từng khối (mục 6–9), cuối cùng là test và câu hỏi phỏng vấn.

Nếu bạn đến từ C#/.NET hoặc PHP/Laravel, các khung **≈ .NET** và **≈ Laravel** so sánh với thứ bạn đã quen.

## Mục lục

1. [Bức tranh tổng thể](#1-bức-tranh-tổng-thể)
2. [Vì sao tiền là số nguyên, không bao giờ là float](#2-vì-sao-tiền-là-số-nguyên-không-bao-giờ-là-float)
3. [Transaction từng bước và FOR UPDATE làm gì](#3-transaction-từng-bước-và-for-update-làm-gì)
4. [Deadlock và thứ tự khoá](#4-deadlock-và-thứ-tự-khoá)
5. [Idempotency key](#5-idempotency-key)
6. [Migration 0007](#6-migration-0007)
7. [store.go](#7-storego)
8. [service.go](#8-servicego)
9. [handler.go và main.go](#9-handlergo-và-maingo)
10. [Test](#10-test)
11. [Thử bằng curl](#11-thử-bằng-curl)
12. [Câu hỏi phỏng vấn có thể gặp](#12-câu-hỏi-phỏng-vấn-có-thể-gặp)

---

## 1. Bức tranh tổng thể

| Method | Path | Làm gì |
|---|---|---|
| GET | `/api/wallet` | Ví của tôi. Lần đầu gọi thì tạo ví với 1.000.000đ demo |
| POST | `/api/transfers` | Chuyển tiền. Header `Idempotency-Key` bắt buộc, body `{"to_username":"bob","amount":50000}` |
| GET | `/api/transfers?limit=20` | Lịch sử chuyển đi và nhận về, mới nhất trước |

Cả ba route đều bọc bởi `auth.RequireAuth` (xem [auth.md](auth.md) và [watchlist.md](watchlist.md)): user id luôn lấy từ access token.

Các file:

```
internal/wallet/
├── errors.go                     lỗi nghiệp vụ (ErrInsufficientFunds, ErrIdempotencyConflict, ...)
├── store.go                      SQL thô với pgx, transaction chuyển tiền     ≈ Repository
├── service.go                    kiểm tra input, tìm người nhận, so request hash  ≈ Service
├── handler.go                    HTTP: đọc header/JSON, đổi lỗi thành status   ≈ Controller
├── service_test.go               test service với store giả
├── handler_test.go               test HTTP với httptest
└── store_integration_test.go     test với Postgres thật: 50 giao dịch song song
```

Mã lỗi HTTP:

| Tình huống | Status |
|---|---|
| Không có / sai token | 401 |
| JSON sai, amount không phải số nguyên 1..10^12, thiếu Idempotency-Key, chuyển cho chính mình | 400 |
| Không có user nhận | 404 |
| Không đủ tiền | 422 |
| Idempotency-Key đã dùng cho request **khác** | 422 |
| Thành công | 201 |
| Gửi lại đúng request cũ (cùng key, cùng nội dung) | 200 + header `Idempotent-Replayed: true` |

---

## 2. Vì sao tiền là số nguyên, không bao giờ là float

Float (`float64` trong Go, `double` trong C#) lưu số theo **hệ nhị phân**. Nhiều số thập phân rất bình thường như 0.1 không biểu diễn chính xác được trong hệ nhị phân, giống 1/3 không viết hết được trong hệ thập phân (0.3333...).

```go
fmt.Println(0.1 + 0.2)         // 0.30000000000000004
fmt.Println(0.1+0.2 == 0.3)    // false
```

Cộng trừ hàng triệu lần, sai số tích luỹ thành tiền thật bị lệch. Kiểm toán không chấp nhận "lệch một chút".

Cách làm chuẩn: lưu **số nguyên theo đơn vị nhỏ nhất** của loại tiền.

- USD có cent: 12,34 USD lưu là `1234`.
- **VND không có đơn vị lẻ** (không có "xu" trong thực tế), nên đơn vị nhỏ nhất là 1 đồng: 50.000đ lưu là `50000`.

Ở đây dùng `BIGINT` trong Postgres, `int64` trong Go: tối đa khoảng 9,2 × 10^18, thừa cho VND.

**Còn `NUMERIC`/`DECIMAL` thì sao?** `NUMERIC(19,4)` trong Postgres, `decimal` trong C#, cũng **chính xác** (lưu theo hệ thập phân). Nó hợp khi cần số lẻ (tỷ giá, lãi suất, tiền có 4 chữ số thập phân). Nhưng với VND thì không cần số lẻ; còn Go không có kiểu decimal sẵn (phải dùng thư viện như `shopspring/decimal`), và phép tính NUMERIC chậm hơn số nguyên. Nên `BIGINT` là lựa chọn đơn giản nhất mà vẫn đúng.

> **≈ .NET:** trong C# bạn hay dùng `decimal` cho tiền, đó là đúng. Sai là dùng `double`. Go không có `decimal` nên dân Go dùng `int64` đơn vị nhỏ nhất.

Code cũng chặn float **ngay từ JSON**: field `Amount int64` khiến `encoding/json` từ chối `1.5`, `50000.0`, `5e4`, `"50000"` và số vượt int64 ngay lúc decode (đã viết test cho từng trường hợp trong `handler_test.go`). Tiền không bao giờ đi qua `float64`.

---

## 3. Transaction từng bước và FOR UPDATE làm gì

Chuyển tiền gồm nhiều câu SQL. Chúng phải **cùng thành công hoặc cùng thất bại** (atomic): không bao giờ được có cảnh A bị trừ tiền mà B chưa được cộng. Đó là việc của transaction.

Các bước trong `Store.Transfer`, tất cả trong **một** transaction:

| Bước | Làm gì |
|---|---|
| (a) | *(service làm trước, ngoài transaction)* tìm user nhận theo username, tìm hoặc tạo hai ví |
| (b) | Tìm giao dịch đã có với cùng (ví gửi, idempotency key). Có thì trả lại luôn, không chuyển tiền |
| (c) | `SELECT ... FROM wallets WHERE id = ANY($1) ORDER BY id FOR UPDATE`: khoá cả hai dòng ví |
| (d) | Số dư ví gửi < số tiền → `ErrInsufficientFunds` (rollback) |
| (e) | `UPDATE` trừ ví gửi, `UPDATE` cộng ví nhận |
| (f) | `INSERT INTO transfers`. Trùng key (lỗi 23505) → rollback, đọc lại giao dịch đã lưu |
| (g) | `COMMIT` |

### Không có FOR UPDATE thì sao?

A có 100.000đ. Hai request chuyển 80.000đ đến cùng lúc (A bấm hai lần, hoặc hai thiết bị):

```
Thời gian   Transaction 1                        Transaction 2
   │        BEGIN                                BEGIN
   │        SELECT balance → 100.000             SELECT balance → 100.000    ← cả hai thấy đủ tiền
   │        100.000 ≥ 80.000, OK                 100.000 ≥ 80.000, OK
   │        UPDATE balance = balance - 80.000
   │        COMMIT                    (A còn 20.000)
   │                                             UPDATE balance = balance - 80.000
   ▼                                             → vi phạm CHECK (balance >= 0), lỗi
```

May là có `CHECK (balance >= 0)` chặn ở cuối, nhưng request 2 nhận lỗi 500 thay vì 422 "không đủ tiền". Tệ hơn, nếu code ghi `UPDATE balance = 20000` (con số tính ở Go) thay vì `balance - 80000`, thì T2 ghi đè kết quả của T1 (lost update) và tiền được tạo ra từ hư không.

### Có FOR UPDATE

```
Thời gian   Transaction 1                        Transaction 2
   │        BEGIN                                BEGIN
   │        SELECT ... FOR UPDATE
   │          → khoá ví A, ví B; A = 100.000
   │                                             SELECT ... FOR UPDATE
   │                                               → ví A đang bị T1 khoá: ĐỨNG CHỜ ⏳
   │        100.000 ≥ 80.000, OK                   ⏳
   │        UPDATE A -80.000, UPDATE B +80.000     ⏳
   │        INSERT transfers                       ⏳
   │        COMMIT → nhả khoá                      ⏳
   │                                               → được khoá, đọc bản MỚI NHẤT: A = 20.000
   │                                             20.000 < 80.000 → ErrInsufficientFunds
   ▼                                             ROLLBACK → client nhận 422
```

`FOR UPDATE` = "tôi sắp sửa dòng này, ai muốn sửa (hoặc cũng FOR UPDATE) thì xếp hàng". Khoá được giữ tới khi transaction COMMIT hoặc ROLLBACK. Đọc bình thường (`SELECT` không có FOR UPDATE) **không** bị chặn: Postgres dùng MVCC, người đọc thấy bản đã commit gần nhất.

Ở mức isolation mặc định READ COMMITTED, khi T2 hết chờ, Postgres trả cho T2 phiên bản **mới nhất** của dòng (bản T1 vừa commit), nên số dư T2 đọc được là chính xác.

> **≈ .NET (EF Core):** EF không có API riêng cho FOR UPDATE, bạn viết:
> ```csharp
> await using var tx = await db.Database.BeginTransactionAsync(IsolationLevel.ReadCommitted);
> var wallets = await db.Wallets
>     .FromSqlRaw("SELECT * FROM wallets WHERE id = ANY({0}) ORDER BY id FOR UPDATE", ids)
>     .ToListAsync();
> // kiểm tra số dư, sửa entity, db.Transfers.Add(...)
> await db.SaveChangesAsync();
> await tx.CommitAsync();
> ```
> **≈ Laravel:** `DB::transaction(function () { Wallet::whereIn('id', $ids)->orderBy('id')->lockForUpdate()->get(); ... });`. `lockForUpdate()` sinh ra đúng `FOR UPDATE`.

---

## 4. Deadlock và thứ tự khoá

Deadlock: hai transaction, mỗi bên giữ một khoá và chờ khoá bên kia đang giữ. Không bên nào đi tiếp được.

Giả sử code khoá "ví gửi trước, ví nhận sau", và cùng lúc có A→B và B→A:

```
Thời gian   T1: A → B                           T2: B → A
   │        khoá ví A ✔                          khoá ví B ✔
   │        khoá ví B → chờ T2 ⏳                 khoá ví A → chờ T1 ⏳
   │              ⏳  ←──── chờ nhau mãi mãi ────→  ⏳
   │        (sau deadlock_timeout = 1 giây, Postgres phát hiện vòng chờ)
   ▼        tiếp tục                             ERROR: deadlock detected (40P01), bị huỷ
```

Postgres tự phát hiện deadlock và huỷ một transaction, nên hệ thống không treo hẳn. Nhưng một request bị lỗi oan, và mỗi lần như vậy mất khoảng 1 giây chờ.

**Cách chữa: mọi transaction khoá theo cùng một thứ tự.** Code sắp theo `id` của ví:

```sql
SELECT id::text, balance FROM wallets WHERE id = ANY($1) ORDER BY id FOR UPDATE
```

Giả sử id của A nhỏ hơn B. Bây giờ cả A→B lẫn B→A đều khoá A trước:

```
Thời gian   T1: A → B                           T2: B → A
   │        khoá A ✔                             khoá A → chờ T1 ⏳  (chưa giữ khoá nào)
   │        khoá B ✔                               ⏳
   │        UPDATE, INSERT, COMMIT → nhả A, B      ⏳
   │                                             khoá A ✔, khoá B ✔
   ▼                                             UPDATE, INSERT, COMMIT
```

T2 phải chờ ngay từ khoá đầu tiên, khi nó chưa giữ gì, nên không thể có vòng chờ. Postgres khoá các dòng theo đúng thứ tự trả ra sau `ORDER BY` (bước khoá dòng chạy sau bước sắp xếp trong kế hoạch thực thi).

**Đã thử thật:** tạm sửa câu SQL thành khoá ví gửi trước (`ORDER BY (id = <ví gửi>) DESC`) rồi chạy integration test 50 giao dịch A↔B song song 3 lần: **97 lỗi `deadlock detected (40P01)`**, mỗi lần chạy mất 25–39 giây. Với `ORDER BY id`: 0 lỗi, khoảng 0,25 giây.

Một chỗ deadlock khác khó thấy hơn: **tạo ví**. Nếu tạo ví trong cùng transaction, A→B (tạo ví A rồi B) và B→A (tạo ví B rồi A) với hai ví mới cũng có thể chờ nhau, vì INSERT trùng unique phải chờ transaction kia kết thúc mới biết có trùng hay không. Vì vậy service tạo ví **trước**, ngoài transaction chuyển tiền (tạo ví là thao tác idempotent, commit riêng cũng không sao).

---

## 5. Idempotency key

Mạng không tin cậy. Client gửi request chuyển tiền, server chuyển xong, nhưng response bị mất (timeout, mất sóng). Client không biết tiền đã đi chưa, nên **gửi lại**. Không có cơ chế chống lặp thì tiền bị chuyển hai lần.

"Idempotent" nghĩa là gọi một lần hay nhiều lần cho cùng kết quả. GET, PUT, DELETE vốn idempotent; POST thì không. Idempotency key biến POST thành idempotent:

1. Client sinh một chuỗi ngẫu nhiên (nên là UUID) **cho mỗi lần chuyển tiền**, gửi trong header `Idempotency-Key`.
2. Gửi lại (retry) thì dùng **đúng key cũ**.
3. Server lưu key cùng giao dịch. Thấy key đã có → không chuyển nữa, trả lại kết quả cũ.

Đây đúng là cách **Stripe** làm: mọi POST của Stripe nhận header `Idempotency-Key`; gửi lại cùng key thì Stripe trả lại response đã lưu và gắn header `Idempotent-Replayed: true`; gửi cùng key với tham số khác thì Stripe báo lỗi. Code ở đây làm theo y hệt (cả tên header `Idempotent-Replayed`).

Ba chi tiết thiết kế:

**Key duy nhất theo ví gửi**, không phải toàn cục: `UNIQUE (from_wallet_id, idempotency_key)`. Alice và Bob tình cờ cùng dùng key `"abc"` thì không sao. Nếu unique toàn cục, Bob có thể dò xem key nào Alice đã dùng.

**Request hash**: lưu `sha256("to_wallet_id|amount")` cùng key. Gửi lại cùng key nhưng khác người nhận hoặc số tiền gần như chắc chắn là bug phía client (dùng lại key cho giao dịch mới). Nếu chỉ trả kết quả cũ, client tưởng giao dịch mới đã thành công. Nên trả lỗi. Hash được tính trên dạng **chuẩn hoá** (id ví, không phải username), nên `"to_username":"Bob"` và `"bob"` cho cùng hash.

**422 hay 409?** Chọn **422** cho "cùng key, khác nội dung", theo bản nháp IETF *The Idempotency-Key HTTP Header Field*: 422 = request dùng sai key; 409 được dành cho trường hợp request trước cùng key **vẫn đang chạy**. Ở đây request đến sau chỉ việc chờ khoá rồi nhận kết quả cũ (xem dưới), nên không cần 409.

**Hai request cùng key đến cùng một lúc** (double-click): cả hai cùng qua bước (b) vì chưa có gì được commit.

```
Thời gian   T1 (key = k)                         T2 (key = k)
   │        (b) chưa có k                        (b) chưa có k        ← cả hai đều qua
   │        (c) khoá ví ✔                        (c) chờ T1 ⏳
   │        (e) UPDATE, (f) INSERT k, COMMIT       ⏳
   │                                             (c) khoá ví ✔, (e) UPDATE
   │                                             (f) INSERT k → ERROR 23505 unique_violation
   │                                             ROLLBACK (hoàn lại UPDATE ở bước e, nhả khoá)
   ▼                                             đọc giao dịch k của T1 → trả lại, replayed
```

Ràng buộc `UNIQUE` trong DB là lưới an toàn **thật**: dù code có sơ hở gì, DB không cho tồn tại hai giao dịch cùng key. Code chỉ cần nhận ra lỗi `23505` với đúng tên constraint `transfers_idempotency_key`, rollback, và đọc lại.

> Postgres ghi một dòng `ERROR: duplicate key value violates unique constraint "transfers_idempotency_key"` vào log mỗi lần đi đường này. Đó là dự kiến, không phải bug.

> **≈ .NET (EF Core):** cấu hình `HasIndex(t => new { t.FromWalletId, t.IdempotencyKey }).IsUnique()`, rồi `catch (DbUpdateException ex) when (ex.InnerException is PostgresException { SqlState: "23505" })`. Lưu ý: sau lỗi đó, DbContext vẫn còn entity hỏng trong change tracker, phải tạo context mới hoặc `ChangeTracker.Clear()` trước khi đọc lại.

Giới hạn của cách làm này (nên nói thẳng khi phỏng vấn): chỉ lưu giao dịch **thành công**. Request lỗi (ví dụ không đủ tiền) không được lưu, nên gửi lại cùng key sau khi nạp thêm tiền sẽ chạy lại từ đầu. Stripe lưu cả response lỗi. Và `balance` trả về khi replay là số dư **hiện tại**, không phải số dư ngay sau giao dịch gốc (muốn chính xác thì lưu cả response body).

---

## 6. Migration 0007

```sql
CREATE TABLE wallets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    balance    BIGINT NOT NULL CHECK (balance >= 0),
    currency   CHAR(3) NOT NULL DEFAULT 'VND',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- `user_id ... UNIQUE`: mỗi user đúng một ví. Unique cũng tạo index, nên `WHERE user_id = $1` nhanh, và là thứ `ON CONFLICT (user_id)` dựa vào.
- `CHECK (balance >= 0)`: lưới an toàn cuối cùng. Code đã kiểm tra số dư, nhưng nếu có bug thì DB vẫn từ chối. Nguyên tắc: **ràng buộc quan trọng phải nằm trong DB**, không chỉ trong code.
- `CHAR(3)`: mã tiền tệ ISO 4217 luôn 3 ký tự (VND, USD).
- Không có `DEFAULT` cho `balance`: code luôn phải nói rõ số dư ban đầu.

```sql
CREATE TABLE transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id  UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id    UUID NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    idempotency_key TEXT NOT NULL,
    request_hash    CHAR(64) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_idempotency_key UNIQUE (from_wallet_id, idempotency_key)
);
```

- Khoá ngoại **không** có `ON DELETE CASCADE`: lịch sử tiền không nên tự biến mất. Hệ quả: xoá user đã có giao dịch sẽ bị DB chặn (vì xoá user → xoá ví → vướng transfers). Thực tế người ta "xoá mềm" (đánh dấu `deleted_at`) thay vì xoá hẳn.
- `CHECK (amount > 0)`: chuyển 0đ hay số âm (tức là "rút" tiền của người khác) bị chặn ở DB.
- `CHECK (from_wallet_id <> to_wallet_id)`: không tự chuyển cho mình.
- `request_hash CHAR(64)`: SHA-256 là 32 byte = 64 ký tự hex.
- Đặt tên constraint `transfers_idempotency_key` để Go so `pgErr.ConstraintName` chắc chắn, không phụ thuộc tên Postgres tự sinh.

```sql
CREATE INDEX idx_transfers_from_created ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX idx_transfers_to_created   ON transfers (to_wallet_id, created_at DESC);
```

Lịch sử một ví = giao dịch có `from_wallet_id` là ví đó + giao dịch có `to_wallet_id` là ví đó, mới nhất trước. Mỗi index phục vụ một nửa, đã sắp sẵn theo thời gian giảm dần nên `ORDER BY created_at DESC LIMIT n` chỉ đọc n dòng đầu.

Down migration xoá `transfers` trước (vì nó tham chiếu `wallets`), rồi `wallets`. Đã thử `up` → `down 1` → `up` trên DB local.

---

## 7. store.go

### Struct

```go
type Wallet struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Balance   int64     `json:"balance"` // đồng, số nguyên
	...
}

type Transfer struct {
	ID           string    `json:"id"`
	...
	IdempotencyKey string `json:"-"`
	RequestHash    string `json:"-"`
}
```

- `json:"-"`: không bao giờ xuất ra JSON (giống `PasswordHash` trong auth). Lịch sử của người nhận cũng chứa giao dịch này, mà key là của người gửi.

```go
type HistoryItem struct {
	Transfer
	Direction    string `json:"direction"`
	Counterparty string `json:"counterparty"`
}
```

- `Transfer` không có tên field: đây là **embedding** (nhúng). Các field của `Transfer` được "kéo lên" `HistoryItem`: viết `h.Amount` thay cho `h.Transfer.Amount`, và JSON phẳng `{"id":..., "amount":..., "direction":"in", ...}`. ≈ kế thừa `class HistoryItem : Transfer` trong C#, nhưng thực chất là composition.

```go
type TransferInput struct { FromWalletID, ToWalletID string; Amount int64; IdempotencyKey, RequestHash string }
type TransferResult struct {
	Transfer Transfer `json:"transfer"`
	Balance  int64    `json:"balance"`
}
```

- Gom tham số vào struct thay vì hàm 5 tham số: gọi hàm rõ ràng (`FromWalletID: ...`), không nhầm thứ tự hai chuỗi id.

### EnsureWallet

```go
func (s *Store) EnsureWallet(ctx context.Context, userID string, initialBalance int64) (Wallet, error) {
	const selectSQL = `SELECT ` + walletColumns + ` FROM wallets WHERE user_id = $1`
	w, err := scanWallet(s.db.QueryRow(ctx, selectSQL, userID))
	if !errors.Is(err, pgx.ErrNoRows) {
		return w, err // đã có ví (err = nil), hoặc lỗi thật
	}
```

- SELECT trước: hầu hết các lần gọi là ví đã có.
- `if !errors.Is(err, pgx.ErrNoRows)`: nếu lỗi **không phải** "không có dòng" thì return ngay. Gồm hai trường hợp: `err == nil` (tìm thấy ví) hoặc lỗi thật (mất kết nối...). Gọn hơn viết hai `if`.

```go
	_, err = s.db.Exec(ctx,
		`INSERT INTO wallets (user_id, balance) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`,
		userID, initialBalance)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return Wallet{}, ErrUserNotFound
	}
	if err != nil {
		return Wallet{}, err
	}
	return scanWallet(s.db.QueryRow(ctx, selectSQL, userID))
}
```

- Chưa có ví thì INSERT. Hai request đầu tiên của cùng user đến cùng lúc: cả hai SELECT thấy chưa có, cả hai INSERT; một cái thành công, cái kia gặp conflict và `DO NOTHING`. Cả hai SELECT lại ra **cùng một ví**. Không có lỗi, không có ví đôi.
- `23503` = foreign_key_violation: user_id không còn trong bảng users (token còn hạn nhưng user đã bị xoá). Đổi thành `ErrUserNotFound` → 401.
- `initialBalance` (1.000.000đ) là "tiền từ trên trời rơi xuống", **chỉ cho demo**. Hệ thống thật: tiền chỉ vào ví qua nạp tiền, có bút toán đối ứng.

**Bài học từ integration test:** bản đầu tiên INSERT ... ON CONFLICT luôn (không SELECT trước). Test 10 request cùng key vẫn pass, nhưng đường xử lý 23505 **không bao giờ chạy**. Lý do: INSERT ... ON CONFLICT vào một ví đang bị transaction khác UPDATE phải **chờ** transaction đó kết thúc (Postgres cần biết dòng đang sửa còn hay không để quyết định có conflict). Vậy là mọi request bị xếp hàng ngay ở EnsureWallet, trước cả bước (b). Đổi sang SELECT trước thì các request chạy song song thật, và đường 23505 được thực thi (12 lần trong 10 lượt chạy test, đã kiểm bằng log tạm). Một dòng code "vô hại" có thể biến một hệ thống song song thành tuần tự.

### UserIDByUsername

```go
	err := s.db.QueryRow(ctx,
		`SELECT id::text FROM users WHERE lower(username) = lower($1)`, username).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRecipientNotFound
	}
```

- Bảng `users` thuộc package auth, nhưng chỉ cần một câu SELECT nên đọc thẳng, không kéo cả `auth.Service` vào wallet.
- `lower(username)` khớp unique index `idx_users_username` (migration 0005), nên tra cứu dùng index.

### querier và findReplay

```go
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

- `*pgxpool.Pool` và `pgx.Tx` đều có method `QueryRow` với chữ ký này, nên cả hai đều "là" `querier` (Go không cần khai báo `implements`). Nhờ vậy `findReplay` chạy được cả **trong** transaction (bước b) lẫn **ngoài** (sau khi rollback ở bước f).

```go
func findReplay(ctx context.Context, q querier, fromWalletID, key string) (res TransferResult, found bool, err error) {
	res.Transfer, err = scanTransfer(q.QueryRow(ctx,
		`SELECT `+transferColumns+` FROM transfers
		 WHERE from_wallet_id = $1 AND idempotency_key = $2`, fromWalletID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return TransferResult{}, false, nil
	}
	...
	err = q.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, fromWalletID).Scan(&res.Balance)
	return res, true, err
}
```

- **Named return values** `(res TransferResult, found bool, err error)`: các biến kết quả được khai báo sẵn, dùng luôn trong thân hàm. Ở đây giúp đọc chữ ký là hiểu ý nghĩa ba giá trị trả về.
- Câu WHERE dùng đúng cặp cột của unique constraint, nên tra bằng index đó.
- "Không thấy" không phải lỗi: trả `found = false, err = nil`.

### Transfer: transaction chuyển tiền

```go
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TransferResult{}, false, err
	}
	defer tx.Rollback(ctx)
```

Giống `RotateRefreshToken` trong auth: `defer tx.Rollback` đảm bảo mọi nhánh return sớm đều huỷ transaction, kể cả hoàn lại tiền đã UPDATE. Rollback sau Commit thì không làm gì.

```go
	// (b)
	if res, found, err := findReplay(ctx, tx, in.FromWalletID, in.IdempotencyKey); err != nil || found {
		return res, found, err
	}
```

- Khai báo biến ngay trong `if`; các biến này chỉ sống trong khối `if` nên không đè `err` bên ngoài.
- Có lỗi, hoặc tìm thấy giao dịch cũ → return luôn. Request gửi lại không cần khoá ví nào, nên rất nhẹ.
- Store **không** so request hash, chỉ trả giao dịch cũ kèm `RequestHash`. Quyết định "trùng hay xung đột" nằm ở service, để test được bằng store giả.

```go
	// (c)
	rows, err := tx.Query(ctx,
		`SELECT id::text, balance FROM wallets WHERE id = ANY($1) ORDER BY id FOR UPDATE`,
		[]string{in.FromWalletID, in.ToWalletID})
```

- `= ANY($1)` với `$1` là mảng: ≈ `IN (...)` nhưng chỉ một tham số. pgx tự đổi `[]string` của Go thành mảng `uuid[]` của Postgres.
- `ORDER BY id FOR UPDATE`: khoá hai dòng theo thứ tự id, xem mục 4.

```go
	balances := map[string]int64{}
	for rows.Next() {
		var id string
		var bal int64
		if err := rows.Scan(&id, &bal); err != nil {
			rows.Close()
			return TransferResult{}, false, err
		}
		balances[id] = bal
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return TransferResult{}, false, err
	}
	if len(balances) != 2 {
		return TransferResult{}, false, errors.New("wallet: transfer wallets not found")
	}
```

- Đọc hai dòng vào map `id → số dư`, vì thứ tự trả về là theo id chứ không theo "gửi/nhận".
- `rows.Close()` gọi tay (không `defer`) vì trong cùng transaction phải đóng rows xong mới chạy được câu tiếp theo trên connection đó.
- `rows.Err()`: lỗi xảy ra giữa chừng khi đọc (mất kết nối...) chỉ lộ ra ở đây, không ở `rows.Next()`.
- Thiếu dòng nào thì là lỗi lập trình (service đã tạo ví trước) → 500.

```go
	// (d)
	if balances[in.FromWalletID] < in.Amount {
		return TransferResult{}, false, ErrInsufficientFunds
	}
```

Số dư đọc **sau khi đã khoá**, nên chắc chắn đúng tới khi commit. `return` → `defer Rollback` nhả khoá.

```go
	// (e)
	var res TransferResult
	if err := tx.QueryRow(ctx,
		`UPDATE wallets SET balance = balance - $2, updated_at = now() WHERE id = $1 RETURNING balance`,
		in.FromWalletID, in.Amount).Scan(&res.Balance); err != nil {
		return TransferResult{}, false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance + $2, updated_at = now() WHERE id = $1`,
		in.ToWalletID, in.Amount); err != nil {
		return TransferResult{}, false, err
	}
```

- `balance = balance - $2`: phép trừ chạy **trong DB** trên dòng đang khoá. Không bao giờ tính số mới ở Go rồi ghi đè (`SET balance = $2`); đó là nguồn gốc của lỗi lost update.
- `RETURNING balance`: lấy luôn số dư mới của ví gửi để trả cho client, khỏi SELECT thêm.

```go
	// (f)
	res.Transfer, err = scanTransfer(tx.QueryRow(ctx,
		`INSERT INTO transfers (from_wallet_id, to_wallet_id, amount, idempotency_key, request_hash)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+transferColumns, ...))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "transfers_idempotency_key" {
		if err := tx.Rollback(ctx); err != nil {
			return TransferResult{}, false, err
		}
		res, found, err := findReplay(ctx, s.db, in.FromWalletID, in.IdempotencyKey)
		if err == nil && !found {
			err = errors.New("wallet: idempotency key conflict but transfer not found")
		}
		return res, found, err
	}
	if err != nil {
		return TransferResult{}, false, err
	}
```

- `errors.As(err, &pgErr)`: kiểm tra trong chuỗi lỗi có `*pgconn.PgError` không, có thì gán vào `pgErr` để đọc `Code` và `ConstraintName`. ≈ `catch (PostgresException ex) when (ex.SqlState == "23505")`.
- Vì sao rollback **tay** ngay, không đợi `defer`? Sau một lỗi, transaction Postgres ở trạng thái "aborted": mọi câu tiếp theo đều bị từ chối cho tới khi rollback. Và quan trọng hơn: transaction này đang **giữ khoá hai ví**; rollback sớm thì request khác được đi tiếp ngay. Rollback cũng hoàn lại hai câu UPDATE ở bước (e).
- Đọc lại bằng `s.db` (pool), không phải `tx` đã đóng.
- `!found` sau 23505 không thể xảy ra (dòng trùng đã commit), nên nếu có thì báo lỗi 500 thay vì giả vờ thành công.

```go
	// (g)
	return res, false, tx.Commit(ctx)
```

Chỉ tới đây tiền mới thật sự đổi chủ. Nếu server chết trước dòng này, kết nối đứt và Postgres tự rollback: không có trạng thái "đã trừ mà chưa cộng".

### ListTransfers

```sql
SELECT t.id::text, ..., t.direction, u.username
FROM (
    (SELECT id, from_wallet_id, to_wallet_id, amount, created_at,
            'out' AS direction, to_wallet_id AS other_wallet_id FROM transfers
     WHERE from_wallet_id = $1 ORDER BY created_at DESC LIMIT $2)
    UNION ALL
    (SELECT id, from_wallet_id, to_wallet_id, amount, created_at,
            'in' AS direction, from_wallet_id AS other_wallet_id FROM transfers
     WHERE to_wallet_id = $1 ORDER BY created_at DESC LIMIT $2)
) t
JOIN wallets w ON w.id = t.other_wallet_id
JOIN users u ON u.id = w.user_id
ORDER BY t.created_at DESC, t.id
LIMIT $2
```

- Cách viết dễ nghĩ ra là `WHERE from_wallet_id = $1 OR to_wallet_id = $1 ORDER BY created_at DESC`. Nhưng với `OR`, Postgres khó dùng một index để đọc sẵn theo thứ tự thời gian, thường phải lấy **hết** giao dịch của ví rồi mới sắp xếp.
- Tách thành hai nhánh: nhánh "tiền ra" đọc index `(from_wallet_id, created_at DESC)`, nhánh "tiền vào" đọc index `(to_wallet_id, created_at DESC)`, mỗi nhánh chỉ lấy tối đa `limit` dòng. Gộp lại (tối đa 2 × limit dòng), sắp lại, cắt còn `limit`.
- `UNION ALL` thay vì `UNION`: `UNION` loại dòng trùng (tốn công sắp xếp/so sánh). Hai nhánh không bao giờ trùng nhau vì `CHECK (from <> to)`.
- `other_wallet_id`: ví của bên kia, dùng để JOIN lấy username (`counterparty`).
- `ORDER BY ..., t.id`: hai giao dịch cùng `created_at` thì thứ tự vẫn cố định.

---

## 8. service.go

### Hằng số

```go
const (
	DemoStartingBalance int64 = 1_000_000
	MaxTransferAmount   int64 = 1_000_000_000_000
	maxIdempotencyKeyLen = 100
	...
)
```

- `1_000_000`: Go cho phép `_` trong số để dễ đọc (như C# 7+).
- `MaxTransferAmount` = 1.000 tỷ đồng: chặn số vô lý, và đảm bảo cộng vào số dư không tràn int64.
- Tên viết hoa (`DemoStartingBalance`) là exported, test và package khác đọc được; viết thường thì chỉ trong package.

### Interface store

```go
type store interface {
	EnsureWallet(ctx context.Context, userID string, initialBalance int64) (Wallet, error)
	UserIDByUsername(ctx context.Context, username string) (string, error)
	Transfer(ctx context.Context, in TransferInput) (TransferResult, bool, error)
	ListTransfers(ctx context.Context, walletID string, limit int) ([]HistoryItem, error)
}

var _ store = (*Store)(nil)
```

Cùng mẫu với auth và watchlist: interface khai báo ở phía dùng, nhỏ, để test thay bằng `memStore`.

### Transfer

```go
func (s *Service) Transfer(ctx context.Context, userID string, req TransferRequest) (TransferResult, bool, error) {
	if n := len(req.IdempotencyKey); n < 1 || n > maxIdempotencyKeyLen {
		return TransferResult{}, false, ErrInvalidIdempotencyKey
	}
	toUsername := strings.TrimSpace(req.ToUsername)
	if toUsername == "" { ... ErrInvalidInput }
	if req.Amount <= 0 || req.Amount > MaxTransferAmount { ... ErrInvalidInput }
```

- Kiểm tra rẻ trước, gọi DB sau. Key giới hạn 100 ký tự để không ai lưu chuỗi khổng lồ vào DB.
- `fmt.Errorf("%w: ...", ErrInvalidInput)`: `%w` bọc lỗi gốc, nên `errors.Is(err, ErrInvalidInput)` vẫn đúng mà message có thêm chi tiết.

```go
	toUserID, err := s.store.UserIDByUsername(ctx, toUsername)
	if err != nil {
		return TransferResult{}, false, err
	}
	if toUserID == userID {
		return TransferResult{}, false, ErrSelfTransfer
	}
```

So **user id**, không so username: `"Alice"` và `"alice"` là một người. DB cũng có `CHECK (from <> to)` làm lưới an toàn.

```go
	from, err := s.store.EnsureWallet(ctx, userID, DemoStartingBalance)
	...
	to, err := s.store.EnsureWallet(ctx, toUserID, DemoStartingBalance)
	...
	hash := requestHash(to.ID, req.Amount)
	res, replayed, err := s.store.Transfer(ctx, TransferInput{...})
	if err != nil {
		return TransferResult{}, false, err
	}
	if replayed && res.Transfer.RequestHash != hash {
		return TransferResult{}, false, ErrIdempotencyConflict
	}
	return res, replayed, nil
```

- Bước (a): tạo ví ngoài transaction, lý do ở mục 4.
- Store trả giao dịch cũ (`replayed = true`) kèm hash đã lưu; service so với hash của request hiện tại. Khác → `ErrIdempotencyConflict`, không chuyển tiền.

### requestHash

```go
func requestHash(toWalletID string, amount int64) string {
	sum := sha256.Sum256([]byte(toWalletID + "|" + strconv.FormatInt(amount, 10)))
	return hex.EncodeToString(sum[:])
}
```

- Chuỗi chuẩn hoá `"<id ví nhận>|<số tiền>"`. Không hash nguyên body JSON vì `{"amount":1,"to_username":"bob"}` và `{"to_username":"Bob", "amount":1}` cùng ý nghĩa nhưng khác byte.
- `|` ngăn cách để hai field không "dính" vào nhau gây trùng giả.
- `sha256.Sum256` trả mảng `[32]byte`; `sum[:]` đổi thành slice để `hex.EncodeToString` nhận.

### History

`limit = 0` (không truyền) → mặc định 20; ngoài 1–100 → `ErrInvalidInput`. Cùng quy ước với `price.Service.History`.

---

## 9. handler.go và main.go

```go
func NewHandler(svc *Service, requireAuth func(http.Handler) http.Handler) *Handler
```

Nhận middleware đã dựng sẵn, giống watchlist (lý do ở [watchlist.md](watchlist.md#4-handlergo-nhận-middleware-chứ-không-nhận-tokens)).

### writeError

```go
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidIdempotencyKey), errors.Is(err, ErrSelfTransfer):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrRecipientNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInsufficientFunds), errors.Is(err, ErrIdempotencyConflict):
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrUserNotFound):
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
	default:
		httpx.Internal(w, err)
	}
}
```

Một chỗ duy nhất đổi lỗi nghiệp vụ → HTTP status, dùng chung cho ba route. Lỗi lạ → 500, log chi tiết ở server, client chỉ thấy `"internal error"`.

### transfer

```go
	var body struct {
		ToUsername string `json:"to_username"`
		Amount     int64  `json:"amount"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body: amount must be an integer number of dong")
		return
	}
```

- `Amount int64`: decoder từ chối `1.5`, `50000.0`, `5e4`, `"50000"`, `99999999999999999999` (tràn int64). Không cần `UseNumber()`: `UseNumber` chỉ có tác dụng khi decode vào `interface{}`; decode thẳng vào `int64` đã đủ chặt.
- `DisallowUnknownFields()`: gõ nhầm `"amout"` thì báo lỗi luôn. Không có dòng này, field lạ bị bỏ qua, `Amount` = 0 và client nhận một lỗi khó hiểu.
- `MaxBytesReader(..., 1<<10)`: body tối đa 1KB.

```go
	res, replayed, err := h.svc.Transfer(r.Context(), uid, TransferRequest{
		ToUsername:     body.ToUsername,
		Amount:         body.Amount,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
		httpx.JSON(w, http.StatusOK, res)
		return
	}
	httpx.JSON(w, http.StatusCreated, res)
```

- `r.Header.Get` không phân biệt hoa thường tên header (`idempotency-key` cũng được).
- Header phải `Set` **trước** khi ghi status/body (`httpx.JSON` gọi `WriteHeader`); sau đó mọi thay đổi header bị bỏ qua.
- 201 lần đầu, 200 khi replay: client phân biệt được, và header giống Stripe.

### history

Đọc `?limit=` bằng `strconv.Atoi`; không phải số → 400. Cùng mẫu với `price/handler.go`.

### main.go

```go
	wallets := wallet.NewService(wallet.NewStore(db))
	...
	requireAuth := auth.RequireAuth(tokens)
	watchlist.NewHandler(watch, requireAuth).Register(mux)
	wallet.NewHandler(wallets, requireAuth).Register(mux)
```

Biến tên `wallets` (không phải `wallet`) để không che mất tên package `wallet`.

---

## 10. Test

### service_test.go: memStore

Store giả trong bộ nhớ. Một `sync.Mutex` khoá cả hàm `Transfer`, đóng vai transaction + FOR UPDATE. Các test table-driven:

- `TestTransferValidation`: thiếu key, key 101 ký tự (100 thì được), thiếu người nhận, amount 0 / âm / quá max, người nhận không tồn tại, tự chuyển (kể cả `"ALICE"`), không đủ tiền, chuyển đúng toàn bộ số dư. Lỗi thì kiểm tra không có giao dịch nào được lưu.
- `TestTransferIdempotency`: gửi lại y hệt → replayed, cùng id; `"BOB"` thay `"bob"` → vẫn là replay (hash chuẩn hoá); khác amount / khác người nhận → `ErrIdempotencyConflict`; key khác → giao dịch mới. Mỗi case kiểm tra số dòng giao dịch và số dư.
- `TestReplayAfterBalanceDrained`: chuyển hết tiền, gửi lại → vẫn trả kết quả cũ chứ không báo "không đủ tiền".

### handler_test.go: httptest

Dùng middleware thật `auth.RequireAuth(testTokens)`. Kiểm tra: 401 cho cả ba route khi không có token; GET wallet trả 1.000.000 VND; bảng status code cho POST (thiếu key, `1.5`, `50000.0`, `5e4`, `"50000"`, tràn int64, field lạ, 0, âm, quá max, tự chuyển, 404, 422); replay trả 200 + header + cùng transfer id; khác body → 422; response không lộ key/hash; lịch sử của người nhận có `direction: "in"`; `limit` sai → 400.

### store_integration_test.go: Postgres thật

Unit test với store giả không chứng minh được gì về khoá của Postgres. Test này chạy với DB thật, **mặc định bị bỏ qua** (`t.Skip`) nếu không set `TEST_DATABASE_URL`:

```bash
cd backend
TEST_DATABASE_URL="postgres://app:app@localhost:5432/priceboard?sslmode=disable" \
  go test -count=1 -v -run Integration ./internal/wallet/
```

- Mỗi lần chạy tạo user mới với email `wit_<thời điểm>_<i>@test.local`, và `t.Cleanup` xoá transfers rồi users (ví tự xoá theo cascade).
- `cfg.MaxConns = 20`: pool mặc định chỉ có vài connection, tăng lên để các goroutine thật sự chạy song song trong DB.
- `start := make(chan struct{})` rồi `close(start)`: mọi goroutine chờ ở `<-start`, đóng channel là tất cả cùng xuất phát. Cách "bắn súng hiệu lệnh" phổ biến trong Go.

Ba test:

1. **50 giao dịch song song**, nửa A→B nửa B→A. Kiểm tra: tổng tiền A + B không đổi (2.000.000), không ví nào âm, không có lỗi nào ngoài "không đủ tiền" (deadlock 40P01 sẽ làm test fail), số dòng transfers = số lần gọi thành công. Kết quả: 50/50 thành công, A = 1.003.425, B = 996.575, tổng 2.000.000.
2. **Cùng một Idempotency-Key gửi 10 lần đồng thời.** Kiểm tra: đúng 1 dòng transfers, cả 10 lời gọi nhận cùng transfer id, đúng 1 lời gọi không phải replay, tiền chỉ bị trừ một lần. Sau đó dùng lại key với amount khác → `ErrIdempotencyConflict`.
3. **Không đủ tiền và lịch sử**: không đủ tiền thì số dư giữ nguyên; cùng key `"k1"` ở hai ví gửi khác nhau là hai giao dịch hợp lệ; lịch sử sắp đúng thứ tự, đúng `direction` và `counterparty`.

`-race` (phát hiện data race) cần cgo, Windows ở đây thiếu, nên chạy trong Docker:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.27.2 go test -race ./...
```

---

## 11. Thử bằng curl

```bash
# Đăng nhập (xem auth.md), lấy access token của alice; đã có user bob
TOKEN=$(curl -s localhost:8080/api/auth/login -d '{"email":"alice@example.com","password":"password123"}' | jq -r .access_token)
H="Authorization: Bearer $TOKEN"

curl -s localhost:8080/api/wallet -H "$H"                      # balance: 1000000

KEY=$(uuidgen)
curl -si localhost:8080/api/transfers -H "$H" -H "Idempotency-Key: $KEY" \
  -d '{"to_username":"bob","amount":50000}'                      # 201, balance: 950000
curl -si localhost:8080/api/transfers -H "$H" -H "Idempotency-Key: $KEY" \
  -d '{"to_username":"bob","amount":50000}'                      # 200, Idempotent-Replayed: true, balance vẫn 950000
curl -si localhost:8080/api/transfers -H "$H" -H "Idempotency-Key: $KEY" \
  -d '{"to_username":"bob","amount":1}'                          # 422 key đã dùng cho request khác
curl -si localhost:8080/api/transfers -H "$H" -d '{"to_username":"bob","amount":1}'   # 400 thiếu key
curl -si localhost:8080/api/transfers -H "$H" -H "Idempotency-Key: x" \
  -d '{"to_username":"bob","amount":1.5}'                        # 400 không phải số nguyên

curl -s "localhost:8080/api/transfers?limit=5" -H "$H"           # [{..., "direction":"out", "counterparty":"bob"}]
```

---

## 12. Câu hỏi phỏng vấn có thể gặp

**Vì sao không dùng float cho tiền?**
Float là số nhị phân, không biểu diễn chính xác 0.1, sai số tích luỹ qua nhiều phép tính. Dùng số nguyên theo đơn vị nhỏ nhất (VND: đồng, USD: cent), hoặc kiểu decimal chính xác (`NUMERIC`, `decimal` trong C#). Ở đây `BIGINT`/`int64`, và JSON decode thẳng vào `int64` nên `1.5` bị từ chối.

**READ COMMITTED khác SERIALIZABLE thế nào? Sao không dùng SERIALIZABLE cho khỏi khoá tay?**
READ COMMITTED (mặc định của Postgres): mỗi câu lệnh thấy dữ liệu đã commit tại thời điểm câu lệnh bắt đầu; hai transaction có thể cùng đọc một số dư rồi cùng quyết định dựa trên nó, nên cần `FOR UPDATE` để xếp hàng. SERIALIZABLE: Postgres đảm bảo kết quả giống như các transaction chạy lần lượt; nếu phát hiện xung đột thì huỷ một transaction với lỗi `40001 serialization_failure`, và **ứng dụng phải tự retry**. Dùng SERIALIZABLE cũng đúng, nhưng phải viết vòng retry, và khi tranh chấp cao (nhiều người chuyển vào cùng một ví) thì tỉ lệ huỷ cao. Với bài toán "khoá đúng hai dòng đã biết trước", READ COMMITTED + `FOR UPDATE` đơn giản và dễ đoán hơn. (Còn REPEATABLE READ: cả transaction thấy một snapshot; UPDATE dòng đã bị người khác sửa sẽ lỗi `40001`, cũng phải retry.)

**Pessimistic locking và optimistic locking khác gì? Ở đây dùng cái nào?**
Pessimistic (bi quan): khoá trước rồi mới sửa (`FOR UPDATE`), người đến sau chờ. Hợp khi xung đột thường xảy ra và không muốn retry, như chuyển tiền. Optimistic (lạc quan): không khoá; mỗi dòng có cột `version`, UPDATE kèm `WHERE version = <version đã đọc>`; nếu 0 dòng bị sửa thì có người sửa trước, báo lỗi hoặc retry. Hợp khi xung đột hiếm, như sửa hồ sơ. ≈ `[ConcurrencyCheck]` / `IsRowVersion()` trong EF Core, ném `DbUpdateConcurrencyException`. Code này dùng pessimistic.

**Có thể làm không cần SELECT FOR UPDATE không?**
Có một cách gọn: `UPDATE wallets SET balance = balance - $2 WHERE id = $1 AND balance >= $2`, kiểm tra `RowsAffected() == 1`. Câu UPDATE tự khoá dòng và kiểm tra số dư trong một bước. Nhưng vẫn phải khoá hai ví theo thứ tự cố định để tránh deadlock, và khó trả thông tin chi tiết. Cách hiện tại tường minh hơn, dễ giải thích.

**Deadlock là gì, tránh thế nào?**
Hai transaction mỗi bên giữ một khoá và chờ khoá của bên kia. Tránh bằng cách mọi transaction khoá tài nguyên theo cùng một thứ tự (ở đây `ORDER BY id`). Postgres vẫn tự phát hiện và huỷ một bên sau `deadlock_timeout`, nhưng đó là lỗi cho người dùng. Đã đo: khoá "ví gửi trước" → 97 deadlock trong 3 lần chạy 50 giao dịch; `ORDER BY id` → 0.

**Server chết giữa chừng khi đang chuyển tiền thì sao?**
Mọi thay đổi nằm trong một transaction. Chưa COMMIT thì kết nối đứt và Postgres rollback toàn bộ: không có trạng thái "đã trừ A chưa cộng B". Đã COMMIT thì Postgres đã ghi WAL xuống đĩa, dữ liệu bền vững (durability, chữ D trong ACID). Trường hợp khó là commit xong nhưng server chết trước khi gửi response: client không biết kết quả. Đó chính là lúc idempotency key phát huy: client gửi lại cùng key, nhận 200 với giao dịch cũ, không bị trừ tiền lần hai.

**Hai request cùng Idempotency-Key đến cùng lúc thì sao?**
Cả hai cùng qua bước kiểm tra vì chưa có gì được commit. Request sau chờ khoá ví, rồi INSERT bị unique constraint chặn (`23505`); code rollback và đọc giao dịch request trước đã lưu. Unique constraint trong DB là đảm bảo thật, code chỉ xử lý lỗi cho đẹp. Integration test bắn 10 request cùng key: đúng 1 dòng.

**Vì sao key unique theo (ví gửi, key) chứ không toàn cục?**
Key do client sinh; hai user khác nhau trùng key là chuyện có thể. Unique toàn cục còn cho phép dò key của người khác.

**Bước tiếp theo nếu làm thật: double-entry ledger (kế toán kép)?**
Hiện tại `balance` là con số được cập nhật trực tiếp, `transfers` là log. Hệ thống tài chính thật thường dùng **sổ cái kép**: mỗi giao dịch tạo ít nhất hai bút toán (`ledger_entries`) có tổng bằng 0, ví dụ `-50.000` ở tài khoản A và `+50.000` ở tài khoản B. Số dư = tổng các bút toán của tài khoản (có thể cache lại trong `balance`). Lợi ích: kiểm toán được (tổng toàn hệ thống luôn bằng 0, lệch là có bug), không bao giờ sửa/xoá bút toán (sai thì ghi bút toán đảo), và tiền "từ trên trời" như 1.000.000đ demo phải đi từ một tài khoản hệ thống (ví dụ "nguồn khuyến mãi"), nên luôn biết tiền từ đâu ra. Bảng `transfers` hiện tại là bước đầu: nó đã là bản ghi bất biến, chỉ thêm, không sửa.

**Có thể cải thiện gì thêm?**
Lưu cả response (kể cả lỗi) cho idempotency như Stripe; hết hạn key sau 24 giờ; đa tiền tệ (kiểm tra cùng `currency` trước khi chuyển); rate limit; thông báo realtime cho người nhận qua WebSocket sẵn có; phân trang lịch sử bằng cursor (`created_at < $cursor`) thay vì chỉ `limit`.
