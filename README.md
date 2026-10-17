# realtime-price-board

Bảng giá chứng khoán realtime đơn giản: giá được sinh giả lập, đẩy tới trình duyệt qua WebSocket và lưu lịch sử trong PostgreSQL.

**Stack:** Go · React (TypeScript, Vite) · Redis · WebSocket · PostgreSQL · Docker Compose

## Kiến trúc

```
┌──────────┐  publish   ┌─────────┐  subscribe  ┌───────────┐  WebSocket  ┌─────────┐
│ producer │ ─────────▶ │  Redis  │ ──────────▶ │    api    │ ──────────▶ │   web   │
│   (Go)   │  HSET      │ Pub/Sub │             │ (Go, N    │  REST       │ (React, │
└────┬─────┘  latest    │ + hash  │ ◀────────── │ instance) │ ◀────────── │  nginx) │
     │ INSERT           └─────────┘  snapshot   └─────┬─────┘             └─────────┘
     ▼                                                │ SELECT
┌────────────┐                                        │
│ PostgreSQL │ ◀──────────────────────────────────────┘
└────────────┘
```

- **producer** sinh giá cho 5 mã mỗi 500ms, ghi giá mới nhất vào Redis hash `price:latest`, publish lên kênh `ticks` và lưu lịch sử vào bảng `ticks`.
- **api** subscribe kênh `ticks` rồi phát tới mọi client WebSocket. Client mới kết nối nhận ngay snapshot giá mới nhất. Vì giá đi qua Redis Pub/Sub nên có thể chạy nhiều instance api song song.
- **web** là SPA React, được nginx phục vụ. nginx proxy `/api` và `/ws` sang api nên frontend chỉ dùng đường dẫn tương đối.
- **migrate** (golang-migrate) chạy migrations trước khi api và producer khởi động.

## Chạy

```bash
docker compose up --build
```

| URL | |
|---|---|
| http://localhost:3000 | Giao diện |
| http://localhost:8080/api/prices | API trực tiếp |

Công cụ quản trị (RedisInsight :5540, pgAdmin :5050):

```bash
docker compose --profile tools up
```

### Phát triển local

```bash
docker compose up -d postgres redis migrate   # hạ tầng

cd backend
go run ./cmd/producer &
go run ./cmd/api
go test ./...

cd web
npm install
npm run dev        # http://localhost:5173, Vite proxy /api và /ws sang :8080
```

## API

Đặc tả đầy đủ: [backend/openapi.yaml](backend/openapi.yaml).

| Method | Path | Mô tả |
|---|---|---|
| GET | `/ws` | WebSocket, mỗi message là một tick `{symbol, price, ts}` |
| GET | `/api/prices` | Giá mới nhất của mọi mã |
| GET | `/api/prices/{symbol}/history?limit=100` | Lịch sử giá (cũ → mới), `limit` 1–1000 |
| GET | `/api/watchlist` | Danh sách mã đang theo dõi của user (cần Bearer token) |
| POST | `/api/watchlist` | Thêm mã, body `{"symbol": "FPT"}` (cần Bearer token) |
| DELETE | `/api/watchlist/{symbol}` | Bỏ mã khỏi danh sách (cần Bearer token) |
| GET | `/api/wallet` | Ví ảo của user, tạo lần đầu với 1.000.000 VND demo (cần Bearer token) |
| POST | `/api/transfers` | Chuyển tiền, header `Idempotency-Key`, body `{"to_username": "bob", "amount": 50000}` (cần Bearer token) |
| GET | `/api/transfers?limit=20` | Lịch sử chuyển/nhận tiền, `limit` 1–100 (cần Bearer token) |

## Cấu trúc thư mục

```
backend/
├── cmd/
│   ├── api/              # REST + WebSocket
│   └── producer/         # sinh giá
├── internal/
│   ├── price/            # model, generator, store (Postgres), cache (Redis), service, handler
│   ├── watchlist/        # danh sách theo dõi (riêng từng user)
│   ├── wallet/           # ví ảo + chuyển tiền (transaction, FOR UPDATE, idempotency key)
│   ├── realtime/         # hub WebSocket + Redis Pub/Sub
│   └── platform/         # config, postgres, redis, httpx
├── migrations/
└── openapi.yaml

web/src/
├── app/                  # main, router, providers, layout
├── pages/                # Board, Symbol, Watchlist: ghép các feature lại
├── features/
│   ├── price-board/      # WebSocket stream + bảng giá
│   ├── price-history/    # API lịch sử + biểu đồ
│   └── watchlist/        # API + nút theo dõi
├── components/ui/        # Button, Badge, Card
├── lib/                  # apiClient, wsClient, format
└── types/                # kiểu dữ liệu của API
```

Backend chia package theo tính năng, không chia theo tầng (controller/service/repository). Frontend chia theo feature: `pages` import `features`; `features` chỉ import `components/ui`, `lib`, `types` và không import lẫn nhau.

## Hướng phát triển

- Frontend: màn hình đăng nhập và gửi Bearer token (watchlist API giờ yêu cầu đăng nhập)
- Giảm tải ghi cho Postgres: batch insert hoặc TimescaleDB
- Thử nhiều instance api: bỏ `ports` của service `api` trong compose rồi `docker compose up --scale api=3`
