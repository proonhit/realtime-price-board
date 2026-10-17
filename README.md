# realtime-price-board

![CI](https://github.com/proonhit/realtime-price-board/actions/workflows/ci.yml/badge.svg)

Bảng giá chứng khoán realtime: giá được sinh giả lập, đẩy tới trình duyệt qua Redis Pub/Sub và WebSocket, lịch sử lưu trong PostgreSQL. Có thêm đăng nhập JWT, watchlist riêng từng user và ví ảo chuyển tiền an toàn khi chạy đồng thời.

Đây là dự án học Go của một lập trình viên .NET. Mục tiêu là làm đúng những phần khó (concurrency, transaction, xác thực, test) ở quy mô nhỏ, không phải làm nhiều tính năng.

**Stack:** Go 1.27 (net/http, pgx, go-redis, gorilla/websocket, golang-jwt) · React 19 + TypeScript + Vite + React Query · Redis 7 · PostgreSQL 17 · Docker Compose · GitHub Actions

## Mục lục

- [Tính năng](#tính-năng)
- [Kiến trúc](#kiến-trúc)
- [Chạy bằng 1 lệnh](#chạy-bằng-1-lệnh)
- [API](#api)
- [Cấu trúc thư mục](#cấu-trúc-thư-mục)
- [Quyết định thiết kế & đánh đổi](#quyết-định-thiết-kế--đánh-đổi)
- [Kiểm thử](#kiểm-thử)
- [Từ .NET sang Go](#từ-net-sang-go--những-gì-tôi-học-được)
- [Hạn chế & hướng phát triển](#hạn-chế--hướng-phát-triển)
- [Tài liệu học](#tài-liệu-học)
- [Ghi chú về việc dùng AI](#ghi-chú-về-việc-dùng-ai)

## Tính năng

| Tính năng | Chi tiết |
|---|---|
| Bảng giá realtime | 6 mã (VNM, FPT, HPG, VIC, MWG, VCB). Giá tăng tô xanh, giảm tô đỏ, có hiệu ứng nháy. Client mới kết nối nhận ngay snapshot giá mới nhất. WebSocket tự kết nối lại với exponential backoff + jitter |
| Biểu đồ lịch sử giá | Trang `/symbol/:code`, biểu đồ đường SVG tự vẽ (không dùng thư viện chart), dữ liệu từ Postgres |
| Đăng nhập JWT | Access token (JWT HS256, 15 phút) + refresh token ngẫu nhiên (7 ngày) được **rotate** mỗi lần refresh, server chỉ lưu hash SHA-256. Mật khẩu hash bằng bcrypt |
| Watchlist riêng từng user | Thêm/bỏ mã theo dõi, trang `/watchlist` yêu cầu đăng nhập |
| Ví ảo và chuyển tiền | Mỗi user một ví, tặng 1.000.000 VND demo. Chuyển tiền trong một transaction, khoá ví bằng `SELECT ... FOR UPDATE` theo thứ tự cố định, chống gửi lặp bằng header `Idempotency-Key`. Tiền là số nguyên VND (`BIGINT`/`int64`), không bao giờ là float. Hiện chỉ có API, chưa có giao diện |
| CI | GitHub Actions: gofmt, `go vet`, `go test -race`, build Go; ESLint + build web; build Docker image |

## Kiến trúc

```
                    ┌──────────────────────────── Redis ───────────────────────────┐
┌──────────┐  HSET  │  hash price:latest        (giá mới nhất mỗi mã)               │
│ producer │ ─────▶ │                                                               │
│   (Go,   │ PUBLISH│  Pub/Sub channel "ticks"  ─────┬──────────────┬─────────────┐ │
│ 1 inst.) │ ─────▶ │                                │ SUBSCRIBE    │             │ │
└────┬─────┘        └────────────────────────────────┼──────────────┼─────────────┼─┘
     │ INSERT ticks                  HGETALL(snapshot)│              │             │
     ▼                                               ▼              ▼             ▼
┌──────────────────────────┐   SELECT/INSERT   ┌──────────┐   ┌──────────┐  ┌──────────┐
│ PostgreSQL               │ ◀──────────────── │  api #1  │   │  api #2  │  │  api #N  │
│  ticks                   │                   │ (Go)     │   │          │  │          │
│  users, refresh_tokens   │                   │ Hub ─▶ WS│   │          │  │          │
│  watchlist               │                   └────┬─────┘   └──────────┘  └──────────┘
│  wallets, transfers      │                        │ REST /api/*  +  WebSocket /ws
└──────────────────────────┘                        ▼
                                             ┌─────────────┐        ┌──────────────┐
                                             │    nginx    │ ◀───── │ trình duyệt  │
                                             │ (web, :3000)│ ─────▶ │ React SPA    │
                                             └─────────────┘        └──────────────┘
                     migrate (golang-migrate) chạy migrations một lần trước api và producer
```

| Thành phần | Vai trò |
|---|---|
| **producer** (`cmd/producer`) | Mỗi `TICK_INTERVAL` sinh giá mới cho mọi mã (random walk). Với từng tick: `HSET price:latest`, `PUBLISH ticks`, rồi `INSERT` vào bảng `ticks`. Đẩy realtime trước, ghi lịch sử sau để giảm độ trễ. Chỉ chạy **một** instance |
| **api** (`cmd/api`) | REST + WebSocket. Một goroutine subscribe kênh `ticks` và đưa message vào `Hub`; `Hub` phát tới mọi kết nối WebSocket của instance đó. Vì mỗi instance tự subscribe Redis nên chạy N instance song song vẫn đúng |
| **web** | SPA React do nginx phục vụ. nginx proxy `/api/` và `/ws` sang api nên frontend chỉ dùng đường dẫn tương đối (dev thì Vite proxy làm việc này) |
| **migrate** | Container `migrate/migrate` chạy `backend/migrations` rồi thoát; compose chỉ khởi động api và producer khi bước này thành công |
| **Redis** | Hash giá mới nhất (để trả snapshot) + Pub/Sub (để phát tick). Không lưu gì lâu dài |
| **PostgreSQL** | Nguồn dữ liệu bền: lịch sử giá, user, refresh token, watchlist, ví và giao dịch |

**Luồng một tick:** producer sinh giá → Redis (hash + publish) → mỗi api nhận qua subscription → `Hub.Broadcast` đẩy vào channel của từng client → goroutine ghi của client gửi frame WebSocket → `PriceStreamProvider` ở frontend cập nhật state → `PriceTable` tô xanh/đỏ.

**Luồng mở trang:** trình duyệt mở `/ws` → api đọc `HGETALL price:latest` và xếp snapshot vào hàng đợi của client **trước** khi đăng ký vào Hub, nên client luôn nhận snapshot trước các tick mới hơn.

**Luồng chuyển tiền:** `POST /api/transfers` → middleware `RequireAuth` kiểm JWT, gắn user id vào `context` → handler đọc header `Idempotency-Key` + JSON → service kiểm tra input, tìm người nhận, đảm bảo hai ví tồn tại → store mở transaction, khoá hai ví, trừ/cộng, ghi `transfers`, commit.

## Chạy bằng 1 lệnh

Cần Docker (Compose v2).

```bash
docker compose up --build
```

| URL | |
|---|---|
| http://localhost:3000 | Giao diện (nginx) |
| http://localhost:8080/api/prices | API trực tiếp |
| http://localhost:8080/healthz | Health check |

> **Luôn dùng `--build` sau khi pull code mới.** Không có `--build`, compose chạy lại image api/producer **cũ** trong khi container `migrate` áp dụng migrations **mới** (thư mục migrations được mount trực tiếp), nên code cũ chạy trên schema mới và lỗi khó hiểu.

Công cụ quản trị (RedisInsight :5540, pgAdmin :5050, đăng nhập `admin@local.dev` / `admin`):

```bash
docker compose --profile tools up
```

Trong compose, producer chạy với `TICK_INTERVAL=2s` cho dễ nhìn; mặc định trong code là 500ms.

### Phát triển local

```bash
docker compose up -d postgres redis migrate   # chỉ hạ tầng trong Docker

cd backend
go run ./cmd/producer &
go run ./cmd/api                              # :8080, giá trị mặc định trỏ tới localhost

cd web
npm install
npm run dev                                   # http://localhost:5173, Vite proxy /api và /ws sang :8080
npm run lint
npm run build                                 # tsc -b && vite build
```

### Biến môi trường

Đọc trong [`backend/internal/platform/config/config.go`](backend/internal/platform/config/config.go). Không set thì dùng mặc định cho local.

| Biến | Mặc định | Dùng bởi | Ý nghĩa |
|---|---|---|---|
| `DATABASE_URL` | `postgres://app:app@localhost:5432/priceboard` | api, producer | Chuỗi kết nối Postgres |
| `REDIS_ADDR` | `localhost:6379` | api, producer | Địa chỉ Redis |
| `HTTP_ADDR` | `:8080` | api | Địa chỉ lắng nghe HTTP |
| `TICK_INTERVAL` | `500ms` | producer | Chu kỳ sinh giá (`time.ParseDuration`) |
| `JWT_SECRET` | `dev-only-insecure-secret` | api | Secret ký JWT HS256. Ngắn hơn 32 byte thì api in cảnh báo khi khởi động |
| `ACCESS_TTL` | `15m` | api | Thời hạn access token |
| `REFRESH_TTL` | `168h` (7 ngày) | api | Thời hạn refresh token |

`docker-compose.yml` đặt sẵn một `JWT_SECRET` 64 ký tự chỉ dùng cho demo; môi trường thật phải thay bằng secret riêng.

## API

Đặc tả đầy đủ (request/response, mã lỗi): [backend/openapi.yaml](backend/openapi.yaml).

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/api/prices` | | Giá mới nhất của mọi mã (từ Redis), sắp theo mã |
| GET | `/api/prices/{symbol}` | | Giá mới nhất của một mã; mã không tồn tại → 404 |
| GET | `/api/prices/{symbol}/history?limit=100` | | Lịch sử giá từ Postgres (cũ → mới), `limit` 1–1000 |
| GET | `/ws` | | WebSocket: snapshot khi kết nối, sau đó mỗi message là một tick `{symbol, price, ts}` |
| POST | `/api/auth/register` | | `{email, username, password}` → 201 `{user}`; trùng email/username (không phân biệt hoa thường) → 409 |
| POST | `/api/auth/login` | | `{email, password}` → 200 `{access_token, refresh_token, expires_in, user}` |
| POST | `/api/auth/refresh` | | `{refresh_token}` → cặp token **mới**; token cũ bị thu hồi |
| POST | `/api/auth/logout` | | `{refresh_token}` → 204 (gọi lại vẫn 204) |
| GET | `/api/auth/me` | Bearer | User hiện tại |
| GET | `/api/watchlist` | Bearer | Danh sách mã đang theo dõi của user |
| POST | `/api/watchlist` | Bearer | Thêm mã, body `{"symbol": "FPT"}` |
| DELETE | `/api/watchlist/{symbol}` | Bearer | Bỏ mã khỏi danh sách |
| GET | `/api/wallet` | Bearer | Ví của user; lần đầu gọi thì tạo ví với 1.000.000 VND demo |
| POST | `/api/transfers` | Bearer | Chuyển tiền. Header `Idempotency-Key` bắt buộc, body `{"to_username": "bob", "amount": 50000}` |
| GET | `/api/transfers?limit=20` | Bearer | Lịch sử chuyển đi/nhận về (mới nhất trước), `limit` 1–100 |
| GET | `/healthz` | | Trả `ok` |

Mã trạng thái của `POST /api/transfers`:

| Tình huống | Status |
|---|---|
| Chuyển thành công | 201 |
| Gửi lại đúng request cũ (cùng key, cùng nội dung) | 200 + header `Idempotent-Replayed: true` |
| Thiếu key, JSON sai, `amount` không phải số nguyên 1..10^12, tự chuyển cho mình | 400 |
| Không có/sai token | 401 |
| Người nhận không tồn tại | 404 |
| Không đủ tiền, hoặc key đã dùng cho request **khác** | 422 |

Ví dụ: đăng nhập rồi chuyển tiền (giả sử đã đăng ký `alice` và `bob`).

```bash
TOKEN=$(curl -s localhost:8080/api/auth/login \
  -d '{"email":"alice@example.com","password":"password123"}' | jq -r .access_token)
KEY=$(uuidgen)

# Lần đầu: 201, số dư 950000
curl -si localhost:8080/api/transfers -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: $KEY" -d '{"to_username":"bob","amount":50000}'

# Gửi lại y hệt (giả lập retry sau timeout): 200, Idempotent-Replayed: true, không trừ tiền lần hai
curl -si localhost:8080/api/transfers -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: $KEY" -d '{"to_username":"bob","amount":50000}'

# Cùng key nhưng khác số tiền: 422
curl -si localhost:8080/api/transfers -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: $KEY" -d '{"to_username":"bob","amount":1}'
```

## Cấu trúc thư mục

```
.
├── docker-compose.yml
├── .github/workflows/ci.yml
├── docs/learn/                 # tài liệu giải thích từng phần (tiếng Việt)
├── backend/
│   ├── Dockerfile              # một Dockerfile, chọn command bằng build arg CMD=api|producer
│   ├── openapi.yaml
│   ├── migrations/             # 0001..0007, mỗi bản có .up.sql và .down.sql
│   ├── cmd/
│   │   ├── api/                # REST + WebSocket, nối dây mọi thứ trong main.go
│   │   └── producer/           # sinh giá
│   └── internal/
│       ├── price/              # Tick, generator, store (Postgres), cache (Redis), service, handler
│       ├── realtime/           # Bus (Redis Pub/Sub), Hub, WebSocket handler
│       ├── auth/               # password, token (JWT), store, service, middleware, handler
│       ├── watchlist/          # watchlist theo user
│       ├── wallet/             # ví, chuyển tiền, idempotency
│       └── platform/           # config, postgres, redis, httpx (helper JSON/lỗi)
└── web/
    ├── nginx.conf
    └── src/
        ├── app/                # main, router, providers, layout
        ├── pages/              # Board, Symbol, Watchlist, Login, Register: ghép feature
        ├── features/
        │   ├── auth/           # AuthProvider, RequireAuth, form đăng nhập/đăng ký
        │   ├── price-board/    # WebSocket stream + bảng giá
        │   ├── price-history/  # API lịch sử + biểu đồ
        │   └── watchlist/      # API + nút theo dõi
        ├── components/ui/      # Button, Badge, Card
        ├── lib/                # apiClient (Bearer + refresh), session, wsClient, useWebSocket, format
        └── types/              # kiểu dữ liệu của API
```

Mỗi package trong `internal/` có cùng hình dạng: `store.go` (SQL) → `service.go` (nghiệp vụ) → `handler.go` (HTTP), cộng `errors.go` và test. Frontend theo cùng ý: `pages` import `features`; `features` chỉ import `components/ui`, `lib`, `types` và không import lẫn nhau.

### Vì sao chia theo tính năng, không chia theo tầng

Một cách phổ biến (và là cách tôi quen từ .NET) là `internal/{handler,service,repository,ws}`. Repo này chọn **package-by-feature**:

- **Gắn kết (cohesion):** sửa chuyển tiền chỉ mở `internal/wallet/`. Chia theo tầng thì một tính năng rải ra 3–4 thư mục.
- **Đóng gói thật:** trong Go, package là ranh giới truy cập. Tên viết thường (`store` interface, `requestHash`, `ctxKey`) chỉ thấy được trong package, nên chi tiết của wallet không lọt ra ngoài. Chia theo tầng thì `service` buộc phải import kiểu export từ `repository`, mọi thứ thành public.
- **Ít import chéo:** chỉ có vài phụ thuộc có chủ đích, ví dụ `watchlist` và `wallet` dùng `auth.UserIDFrom`, `realtime` dùng `price.Tick`. Go cấm import vòng, nên chia theo tầng dễ vướng hơn khi các tầng cần kiểu của nhau.
- **Thông lệ cộng đồng Go:** thư viện chuẩn và phần lớn dự án Go tổ chức theo "package cung cấp gì", không theo "package thuộc tầng nào".

Cái giá: người từ .NET/Laravel quen layered sẽ thấy lạ lúc đầu, và quy tắc "handler không gọi thẳng SQL" chỉ là quy ước trong từng package, không được cấu trúc thư mục ép buộc.

## Quyết định thiết kế & đánh đổi

**Dữ liệu**

- **SQL thô với pgx, không ORM/sqlc.** Thấy đúng câu SQL chạy (quan trọng với `FOR UPDATE`, `ON CONFLICT`, `ORDER BY id` để khoá), không có lớp ma thuật. Đổi lại: tự viết `Scan`, lỗi sai tên cột chỉ lộ ra lúc chạy. Nếu số câu query tăng, `sqlc` (sinh code type-safe từ SQL) là bước tiếp theo hợp lý hơn ORM.
- **Migrations bằng golang-migrate**, file `.up.sql`/`.down.sql` đánh số. Migration đã chạy thì không sửa; muốn đổi thì viết bản mới (ví dụ 0005 làm username không phân biệt hoa thường thay vì sửa 0003). Không có auto-diff như EF Core: đổi schema là tự viết SQL.
- **Tiền là số nguyên VND** (`BIGINT` + `int64`, `CHECK (balance >= 0)`). Field `Amount int64` khiến `encoding/json` tự từ chối `1.5`, `5e4`, `"50000"`. Giá cổ phiếu thì vẫn là `float64` trong Go và `NUMERIC(12,2)` trong DB, vì chỉ để hiển thị, không cộng dồn.

**Realtime**

- **Redis Pub/Sub, không Kafka/Redis Streams.** Đơn giản, độ trễ thấp, đủ để N instance api cùng nhận tick. Đổi lại là **at-most-once, không replay**: api mất kết nối Redis vài giây thì mất các tick trong lúc đó. Chấp nhận được vì tick sau ghi đè tick trước, và client nhận lại snapshot khi kết nối lại. Dữ liệu cần bền thì đi Postgres.
- **Hub ngắt client chậm.** Mỗi client có hàng đợi 64 message; `Broadcast` dùng `select` với `default`, hàng đợi đầy thì đóng kết nối của client đó thay vì chặn. Một tab bị treo không làm chậm mọi người khác; client bị ngắt sẽ tự kết nối lại và nhận snapshot.

**Xác thực**

- **Refresh token rotation + lưu hash + `FOR UPDATE`.** Refresh token là 32 byte ngẫu nhiên (không phải JWT), DB chỉ lưu SHA-256, nên lộ DB không lộ token dùng được. Mỗi lần refresh, token cũ bị thu hồi và token mới được lưu trong cùng một transaction; `FOR UPDATE` đảm bảo hai request cùng token đến cùng lúc thì chỉ một request đổi được. Access token là JWT không trạng thái nên không thu hồi được trước hạn: đánh đổi bằng TTL ngắn (15 phút).
- **Access token trong RAM, refresh token trong localStorage**, không phải cookie `httpOnly`. Reload trang vẫn giữ phiên, backend không phải lo CSRF. Đổi lại: nếu bị XSS, script độc đọc được refresh token. Cookie `httpOnly; Secure; SameSite=Strict` an toàn hơn nhưng cần đổi backend; đây là việc nên làm tiếp.
- **Middleware được tạo một lần trong `main.go` và truyền vào handler** dưới dạng `func(http.Handler) http.Handler`. `watchlist` và `wallet` không biết token là JWT, không cần secret, và test có thể truyền middleware thật với secret test.

**Ví**

- **Khoá theo thứ tự cố định:** `SELECT ... WHERE id = ANY($1) ORDER BY id FOR UPDATE`. A→B và B→A cùng lúc đều khoá ví có id nhỏ hơn trước, nên không thể có vòng chờ. Ví được tạo **trước**, ngoài transaction chuyển tiền, để tránh deadlock do hai INSERT cùng chờ nhau.
- **Idempotency theo kiểu Stripe:** key unique theo `(ví gửi, key)`, lưu kèm `sha256(to_wallet_id|amount)`. Cùng key cùng nội dung → 200 + `Idempotent-Replayed: true`, trả lại giao dịch cũ; cùng key khác nội dung → 422 (theo bản nháp IETF; 409 dành cho "request trước vẫn đang chạy"). Hai request cùng key đến đồng thời thì ràng buộc `UNIQUE` trong DB là lưới an toàn thật: request sau gặp lỗi `23505`, rollback, đọc lại giao dịch đã lưu. Giới hạn: chỉ lưu giao dịch thành công; `balance` trả về khi replay là số dư hiện tại.
- **Mỗi user một ví** (`user_id UNIQUE`), tạo lười ở lần gọi đầu với 1.000.000 VND. Đây là "tiền từ trên trời", chỉ hợp lý cho demo; hệ thống thật phải có giao dịch đối ứng (xem double-entry ở phần hạn chế).
- **READ COMMITTED + khoá bi quan**, không SERIALIZABLE. Bài toán khoá đúng hai dòng biết trước thì `FOR UPDATE` dễ đoán hơn và không cần vòng retry cho lỗi `40001`.

Chi tiết và sơ đồ timeline từng trường hợp: [docs/learn/wallet.md](docs/learn/wallet.md), [docs/learn/auth.md](docs/learn/auth.md).

## Kiểm thử

Chạy từ thư mục `backend/`:

```bash
go test ./...                                    # unit test + handler test, không cần DB/Redis
go test ./internal/wallet -run TestTransfer -v   # một nhóm test, in chi tiết
go test -cover ./...
```

| Loại | Ở đâu | Cách làm |
|---|---|---|
| Unit test service | `*/service_test.go` | Table-driven test, `store` thay bằng `memStore` viết tay trong bộ nhớ (không dùng thư viện mock) |
| Handler test | `*/handler_test.go` | `httptest.NewRecorder` + `ServeMux` thật; dùng **middleware thật** `auth.RequireAuth` với secret test để kiểm cả đường 401 |
| WebSocket test | `realtime/handler_test.go` | `httptest.NewServer` + client gorilla thật: snapshot khi kết nối, nhận broadcast, gỡ client khi đóng |
| Hub test | `realtime/hub_test.go` | Client chậm bị ngắt, unregister gọi nhiều lần vẫn an toàn |
| Integration test | `wallet/store_integration_test.go` | Postgres thật; tự `t.Skip` nếu không có `TEST_DATABASE_URL` |

**Integration test với Postgres** (cần `docker compose up -d postgres migrate`):

```bash
cd backend
TEST_DATABASE_URL="postgres://app:app@localhost:5432/priceboard?sslmode=disable" \
  go test -count=1 -v -run Integration ./internal/wallet/
```

Ba test: `TestIntegrationConcurrentTransfersConserveMoney`, `TestIntegrationConcurrentSameIdempotencyKey`, `TestIntegrationInsufficientFundsAndHistory`. Kết quả đã ghi lại trong [docs/learn/wallet.md](docs/learn/wallet.md):

- 50 giao dịch A↔B chạy song song: 50/50 thành công, tổng tiền hai ví giữ nguyên 2.000.000, không ví nào âm, không deadlock.
- Cùng một `Idempotency-Key` gửi 10 lần đồng thời: đúng 1 dòng `transfers`, tiền bị trừ đúng một lần.
- Thí nghiệm thứ tự khoá: tạm đổi sang "khoá ví gửi trước" rồi chạy test 50 giao dịch 3 lần → 97 lỗi `deadlock detected (40P01)`, mỗi lần 25–39 giây. Với `ORDER BY id`: 0 lỗi, khoảng 0,25 giây.

**Race detector.** `-race` cần cgo (trình biên dịch C). Trên Windows không có gcc sẽ báo `-race requires cgo`; chạy trong Docker thay thế (Git Bash):

```bash
docker run --rm -v "$(pwd -W):/src" -w /src golang:1.27.2 go test -race ./...
```

(Trên Linux/macOS dùng `$(pwd)`.)

**CI** ([.github/workflows/ci.yml](.github/workflows/ci.yml)) chạy khi push lên `main` hoặc `dev/**` và khi mở PR vào `main`:

| Job | Bước |
|---|---|
| Backend (Go) | `gofmt -l` (có file chưa format thì fail) → `go vet ./...` → `go test -race -count=1 ./...` → `go build ./...` |
| Web (React) | `npm ci` → `npm run lint` → `npm run build` |
| Docker images | `docker compose build` (chỉ chạy khi hai job trên pass) |

CI chưa chạy integration test với Postgres (chưa có service container), và frontend chưa có test tự động.

## Từ .NET sang Go — những gì tôi học được

<!-- TODO(owner): đọc lại và viết lại bằng trải nghiệm thật của bạn -->

**DI container → nối dây tường minh trong `main.go`.** Không có `services.AddScoped<...>()`. Mọi phụ thuộc được tạo bằng tay theo thứ tự, đọc `cmd/api/main.go` là thấy toàn bộ đồ thị:

```go
wallets := wallet.NewService(wallet.NewStore(db))
requireAuth := auth.RequireAuth(tokens)
wallet.NewHandler(wallets, requireAuth).Register(mux)
```

Lúc đầu tôi thấy thiếu container, sau thấy dễ debug hơn: không có lỗi "không resolve được service" lúc runtime, sai là compile lỗi.

**Interface khai báo ở phía dùng, thoả mãn ngầm.** Trong C# tôi viết `IWalletRepository` cạnh class `WalletRepository : IWalletRepository`. Ở Go, `wallet/service.go` tự khai báo interface nhỏ đúng những gì nó cần, `*Store` không cần biết interface đó tồn tại:

```go
type store interface {
	EnsureWallet(ctx context.Context, userID string, initialBalance int64) (Wallet, error)
	Transfer(ctx context.Context, in TransferInput) (TransferResult, bool, error)
	// ...
}
var _ store = (*Store)(nil) // kiểm tra lúc compile
```

Test chỉ cần một `memStore` có đúng các method đó.

**async/await + Task → goroutine, channel, context.** Không có `async` lan khắp code; hàm Go viết tuần tự, muốn chạy song song thì `go f()`. `CancellationToken` tương ứng `context.Context`, được truyền làm tham số đầu tiên ở mọi hàm I/O. Trong `main.go`, Ctrl+C huỷ `ctx`, vòng subscribe Redis thoát, server shutdown:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
go func() { busErr <- realtime.NewBus(rdb).Subscribe(ctx, hub.Broadcast) }()
select {
case <-ctx.Done():
case err := <-busErr:
	return err
}
```

`Hub` là chỗ tôi phải nghĩ kỹ nhất: `sync.Mutex` bảo vệ map client, mỗi client một channel có buffer, chỉ một goroutine được ghi vào kết nối.

**Exception → error value.** Không có `try/catch`; lỗi là giá trị trả về, phải xử lý ngay. Lỗi nghiệp vụ là biến sentinel (`wallet/errors.go`), handler đổi sang HTTP status bằng `errors.Is`; lỗi của driver thì bóc bằng `errors.As`:

```go
case errors.Is(err, ErrInsufficientFunds), errors.Is(err, ErrIdempotencyConflict):
	// 422
```
```go
if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique violation
```

Dài dòng hơn `catch (DbUpdateException ex) when (...)`, nhưng nhìn hàm là biết đường lỗi nào có thể xảy ra.

**EF Core migrations + LINQ → golang-migrate + SQL thô.** Không còn `dotnet ef migrations add` sinh diff từ model; tôi viết `.up.sql`/`.down.sql` bằng tay và hiểu rõ hơn index, constraint, `FOR UPDATE` thật sự làm gì. Cái mất là type-safety của LINQ; `sqlc` là cách lấy lại phần đó mà vẫn giữ SQL.

**Middleware ASP.NET / `[Authorize]` → bọc `http.Handler`.** Middleware chỉ là hàm nhận handler và trả handler. "`[Authorize]`" là bọc route lúc đăng ký; user id đi qua `context` thay vì `HttpContext.User`:

```go
mux.Handle("POST /api/transfers", h.requireAuth(http.HandlerFunc(h.transfer)))
```

Go 1.22+ `ServeMux` hỗ trợ method và path parameter (`"DELETE /api/watchlist/{symbol}"`, `r.PathValue("symbol")`), nên không cần router bên ngoài.

**xUnit + Moq → table-driven test + fake viết tay.** Không có `[Theory]`/`[InlineData]`; thay bằng slice các case và `t.Run`. Không dùng thư viện mock: fake trong bộ nhớ ngắn và kiểm được trạng thái (số dư, số dòng giao dịch) thay vì kiểm "method X được gọi". `httptest` thay cho `WebApplicationFactory`, nhưng nhẹ hơn nhiều vì handler chỉ là `http.Handler`.

**csproj + NuGet → go.mod.** Một file `go.mod` ngắn, `go.sum` khoá checksum, không có restore riêng. Backend chỉ có 5 phụ thuộc trực tiếp (pgx, go-redis, gorilla/websocket, golang-jwt, x/crypto); phần còn lại là thư viện chuẩn (`net/http`, `encoding/json`, `crypto/sha256`, `log`).

## Hạn chế & hướng phát triển

**Mở rộng**

- **Nhiều instance api** đã chạy được nhờ Pub/Sub. Muốn thử: bỏ `ports` của service `api` trong compose (cổng 8080 cố định không chia cho 3 container được), rồi `docker compose up --build --scale api=3`. nginx resolve tên `api` ra nhiều IP lúc khởi động và luân phiên giữa chúng (đổi số instance thì phải khởi động lại `web`; production nên dùng load balancer thật).
- **Producer chỉ một instance.** Chạy hai cái thì mỗi mã có hai luồng giá. Muốn HA cần leader election (ví dụ khoá Redis có TTL, hoặc Lease của Kubernetes).
- **Độ bền của luồng giá:** Redis Pub/Sub không lưu, không replay. Nếu cần "không mất tick" hoặc consumer khác (cảnh báo giá, tính chỉ báo) thì chuyển sang Redis Streams (consumer group) hoặc Kafka.
- **Kubernetes:** Deployment cho api (nhiều replica, HPA theo CPU/số kết nối), Deployment một replica cho producer, Job cho migrate, readiness/liveness probe qua `/healthz` (hiện chỉ trả `ok`, chưa kiểm DB/Redis).
- **Tải ghi Postgres:** mỗi tick là một `INSERT`. Cải thiện bằng batch insert/`COPY`, TimescaleDB (hypertable + nén), và chính sách xoá dữ liệu cũ (retention).

**Nghiệp vụ và bảo mật**

- **Sổ cái kép (double-entry ledger)** cho ví: mỗi giao dịch là các bút toán có tổng bằng 0, số dư là tổng bút toán; tiền demo phải đi từ một tài khoản hệ thống. Hiện `balance` được cập nhật trực tiếp, `transfers` chỉ là log.
- **Phát hiện tái sử dụng refresh token:** hiện token đã rotate bị từ chối, nhưng chưa thu hồi cả "họ" token. Nếu một token đã thu hồi bị dùng lại (dấu hiệu bị đánh cắp) thì nên thu hồi mọi token sinh ra từ nó.
- **Rate limiting** cho `/api/auth/login` (chống dò mật khẩu) và `/api/transfers`.
- **Refresh token trong cookie `httpOnly`** thay vì localStorage (kèm chống CSRF).
- **Nhiều tab cùng refresh:** single-flight chỉ chống trong một tab; hai tab dùng cùng refresh token thì tab chậm hơn bị đăng xuất. Hướng sửa: `BroadcastChannel` hoặc Web Locks API.
- **Idempotency:** lưu cả response lỗi, cho key hết hạn sau 24 giờ.
- **WebSocket** đang `CheckOrigin` trả `true` (dữ liệu public, chỉ đọc); nếu đẩy dữ liệu riêng tư qua WS thì phải kiểm Origin và xác thực.

**Vận hành và chất lượng**

- **Chưa có test frontend** (hướng: Vitest + Testing Library + MSW). Ví chưa có giao diện.
- **Chưa có observability:** log hiện dùng `log` thường. Nên chuyển sang `log/slog` (log có cấu trúc, request id), thêm metrics Prometheus (số kết nối WS, client bị ngắt, độ trễ tick) và tracing OpenTelemetry.
- **Shutdown chưa drain WebSocket:** `srv.Shutdown` không đóng các kết nối đã hijack. Nên gửi close frame cho mọi client trong Hub trước khi thoát để client kết nối lại sang instance khác gọn gàng.
- **CI chưa chạy integration test** với Postgres service container.

## Tài liệu học

Viết bằng tiếng Việt, giải thích từng dòng và so sánh với .NET/Laravel, mỗi bài có phần câu hỏi phỏng vấn.

| Tài liệu | Nội dung |
|---|---|
| [auth.md](docs/learn/auth.md) | Backend JWT: migrations, bcrypt, ký/kiểm JWT, rotate refresh token bằng transaction + `FOR UPDATE`, middleware |
| [auth-frontend.md](docs/learn/auth-frontend.md) | Frontend đăng nhập: giữ token ở đâu, single-flight refresh khi 401, `AuthProvider`, `RequireAuth` |
| [watchlist.md](docs/learn/watchlist.md) | Chuyển watchlist sang theo user (migration 0006), handler nhận middleware thay vì nhận Tokens |
| [wallet.md](docs/learn/wallet.md) | Chuyển tiền: số nguyên, `FOR UPDATE`, deadlock và thứ tự khoá, idempotency key, integration test |
| [testing.md](docs/learn/testing.md) | Test handler bằng `httptest`, table-driven test, fake và mock, lệnh `go test`, `-race` |
| [ci.md](docs/learn/ci.md) | GitHub Actions từng khối, so sánh Azure DevOps, giải thích `backend/Dockerfile` |
| [frontend-realtime.md](docs/learn/frontend-realtime.md) | WebSocket client, backoff + jitter, `useWebSocket`, Context, tô xanh/đỏ |

## Ghi chú về việc dùng AI

<!-- TODO(owner): kiểm tra lại cho đúng sự thật, bổ sung phần bạn tự làm/tự hiểu -->

Tôi dùng AI nhiều trong dự án này và muốn nói rõ phần nào là của ai.

- **Giai đoạn đầu:** cấu trúc dự án và các tính năng ban đầu (producer, bảng giá realtime, lịch sử giá, watchlist chung) được viết với sự hỗ trợ của AI trong các phiên làm việc trước.
- **Giai đoạn này:** Claude Code (một AI coding agent) viết phần lớn code trên các nhánh riêng: store/service/handler/middleware của auth, đăng nhập ở frontend, watchlist theo user, ví và chuyển tiền, handler test, CI và README này. Các tài liệu trong `docs/learn/` cũng do AI viết để tôi đọc hiểu code.
- **Phần tôi tự viết (có AI review):** migrations `0003_create_users` và `0004_create_refresh_tokens`, phần cơ bản của `auth/password.go` và `auth/token.go`, và endpoint `GET /api/prices/{symbol}` (`GetBySymbol`).

Cách tôi làm việc với AI: giao từng việc nhỏ trên nhánh riêng, đọc diff, đọc tài liệu giải thích trong `docs/learn/`, chạy test và thử bằng curl trước khi merge. Tôi chịu trách nhiệm hiểu và giải thích được mọi dòng code ở đây; nếu có phần nào tôi chưa giải thích được, đó là phần tôi đang học.
