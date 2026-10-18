# Test handler HTTP trong Go (net/http/httptest)

Tài liệu này giải thích các file test handler trong `backend/`:

- `internal/price/handler_test.go` – REST API giá
- `internal/watchlist/handler_test.go` – REST API watchlist
- `internal/realtime/handler_test.go` – WebSocket `/ws`

Mục tiêu: đọc xong bạn giải thích được từng dòng, và trả lời được câu hỏi phỏng vấn về testing trong Go.

---

## 1. Test trong Go: kiến thức tối thiểu

- File test có đuôi `_test.go`, nằm cùng thư mục với code. `go build` bỏ qua file này, chỉ `go test` mới biên dịch nó.
- Hàm test có dạng `func TestXxx(t *testing.T)`. Không cần attribute như `[Fact]` (xUnit) hay kế thừa `TestCase` (PHPUnit): chỉ cần đúng tên và đúng chữ ký.
- `t.Errorf(...)` báo lỗi nhưng chạy tiếp; `t.Fatalf(...)` báo lỗi và dừng test (hoặc sub-test) hiện tại ngay.
- Go không có thư viện assert sẵn. Ta viết `if got != want { t.Errorf(...) }`. Nghe dài dòng nhưng không phải học thêm API nào.
- Các test của mình dùng `package price` (cùng package với code), nên truy cập được kiểu/hàm unexported như `historyReader`, `fakeHistory`, `newClient`. Đây gọi là *white-box test*.

## 2. Table-driven test là gì?

Thay vì viết 10 hàm test gần giống nhau, ta viết **một bảng dữ liệu** (slice của struct), mỗi phần tử là một trường hợp, rồi chạy cùng một đoạn logic cho từng dòng:

```go
tests := []struct {
    name       string
    path       string
    wantStatus int
}{
    {"get one symbol", "/api/prices/FPT", 200},
    {"unknown symbol", "/api/prices/AAPL/history", 404},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // gọi API với tt.path, so sánh với tt.wantStatus
    })
}
```

- `t.Run(name, func)` tạo **sub-test**. Mỗi case có tên riêng trong output, fail riêng, và chạy lẻ được bằng `-run 'TestHandler/get_one_symbol'` (dấu cách trong tên được đổi thành `_`).
- Thêm case mới chỉ là thêm một dòng vào bảng.
- So sánh: giống `[Theory]` + `[InlineData]` của xUnit, hay `@dataProvider` của PHPUnit.

## 3. Fake và mock

Handler gọi Service, Service gọi storage (PostgreSQL, Redis). Khi test handler, ta không muốn cần DB thật. Service nhận vào **interface nhỏ** (ví dụ `latestReader` có `All` và `Get`), nên trong test ta truyền vào một struct tự viết có cùng method.

| | Fake | Mock |
|---|---|---|
| Là gì | Một cài đặt đơn giản nhưng **chạy thật** (ví dụ `memStore` lưu trong slice) | Đối tượng được **lập trình kỳ vọng**: "method X phải được gọi 1 lần với tham số Y" |
| Kiểm tra gì | Kết quả đầu ra (state) | Cách các object gọi nhau (interaction) |
| Trong .NET | Tự viết class `InMemoryRepository : IRepository` | `var m = new Mock<IRepo>(); m.Setup(r => r.Get(1)).Returns(x); m.Verify(...)` (Moq) |
| Trong PHP/Laravel | Class in-memory tự viết, hoặc `Event::fake()`, `Http::fake()` | `$this->mock(Repo::class, fn($m) => $m->shouldReceive('get')->once())` (Mockery) |

Dự án này dùng **fake**, không dùng thư viện mock:

- Interface trong Go được thoả mãn *ngầm* (không cần `implements`). Struct nào có đủ method là dùng được, nên viết fake chỉ mất vài dòng.
- Test dựa vào kết quả thay vì "đã gọi method nào" nên ít bị vỡ khi refactor code bên trong.

Các fake đang có:

- `fakeHistory`, `fakeLatest` (`price/service_test.go`) – trả dữ liệu cố định.
- `memStore` (`watchlist/service_test.go`) – watchlist lưu trong slice.
- `errStore` (mới, trong mỗi `handler_test.go`) – luôn trả lỗi, để test nhánh 500.
- `fakeSnapshot` (`realtime/handler_test.go`) – thay cho `price.Service.Latest`.

Vì file test cùng package, `handler_test.go` dùng lại luôn `fakeHistory`/`memStore` đã khai báo trong `service_test.go`, không cần viết lại.

## 4. httptest.NewRecorder và httptest.NewServer

| | `httptest.NewRecorder()` | `httptest.NewServer(handler)` |
|---|---|---|
| Có mạng không | Không. Gọi thẳng `mux.ServeHTTP(rec, req)` như gọi một hàm | Có. Mở cổng TCP thật trên `127.0.0.1:<cổng ngẫu nhiên>` |
| Request tạo bằng | `httptest.NewRequest(method, path, body)` | Client thật: `http.Get(srv.URL)`, hoặc websocket Dialer |
| Tốc độ | Rất nhanh | Chậm hơn chút (vẫn tính bằng ms) |
| Dùng khi | Handler REST bình thường | Cần kết nối thật: WebSocket, streaming, test client HTTP |
| Tương đương | Laravel `$this->getJson('/api/...')`; ASP.NET `WebApplicationFactory` + `TestServer` (in-memory) | ASP.NET chạy Kestrel thật trên một cổng |

Vì sao WebSocket phải dùng `NewServer`? Handler `/ws` gọi `Upgrade`, hàm này cần **hijack** kết nối TCP (lấy socket ra khỏi tay `net/http` để tự đọc/ghi). `ResponseRecorder` không có socket nào để hijack, nên phải có server thật.

Chú ý: cả hai cách đều dùng **router thật** (`http.ServeMux` + `Register`). Như vậy ta test luôn cả pattern route (`GET /api/prices/{symbol}`), `r.PathValue`, query string, chứ không chỉ test hàm handler riêng lẻ.

## 5. Đi qua từng file

### 5.1. `price/handler_test.go`

**Fake lỗi:**

```go
type errStore struct{}

var errDB = errors.New("dial tcp 10.0.0.5:5432: connection refused")

func (errStore) History(context.Context, string, int) ([]Tick, error) { return nil, errDB }
func (errStore) All(context.Context) ([]Tick, error)                  { return nil, errDB }
func (errStore) Get(context.Context, string) (Tick, error)            { return Tick{}, errDB }
```

- `struct{}` rỗng: không cần dữ liệu, chỉ cần method.
- Ba method này giúp `errStore` thoả mãn cả `historyReader` (có `History`) và `latestReader` (có `All`, `Get`).
- Tham số không dùng thì bỏ tên, chỉ ghi kiểu (`context.Context`). Go cho phép điều này.
- Lỗi cố tình chứa IP nội bộ. Test sẽ kiểm tra chuỗi này **không** xuất hiện trong response, tức là `httpx.Internal` chỉ ghi log ở server và trả `{"error":"internal error"}` cho client. Khi chạy test bạn sẽ thấy dòng log `internal error: dial tcp 10.0.0.5...`: đó là log phía server, đúng như mong đợi.

**Helper dựng router:**

```go
func newTestMux(h historyReader, l latestReader) *http.ServeMux {
    mux := http.NewServeMux()
    NewHandler(NewService(h, l)).Register(mux)
    return mux
}
```

Giống hệt cách `cmd/api/main.go` nối các thành phần, chỉ khác là storage được thay bằng fake. Tương tự việc thay service trong DI container khi test ở ASP.NET/Laravel.

**Dữ liệu và bảng:**

```go
okMux := newTestMux(
    &fakeHistory{ticks: []Tick{{Symbol: "FPT", Price: 121, Ts: 2}, {Symbol: "FPT", Price: 120, Ts: 1}}},
    fakeLatest{ticks: []Tick{{Symbol: "VCB", Price: 90, Ts: 5}, {Symbol: "FPT", Price: 120, Ts: 5}}},
)
brokenMux := newTestMux(errStore{}, errStore{})
```

- `&fakeHistory{...}` là con trỏ vì method `History` của nó có receiver con trỏ (`func (f *fakeHistory)`), nó ghi lại `gotSymbol`.
- History fake trả "mới nhất trước" (Ts 2 rồi 1) giống `Store` thật. Service đảo lại, nên response phải là Ts 1 rồi 2.
- Latest fake cố tình để VCB đứng trước FPT để kiểm tra service có sắp xếp.

Mỗi dòng trong bảng có: tên, mux nào, path, status mong đợi, body mong đợi (chuỗi JSON).

**Thân vòng lặp:**

```go
req := httptest.NewRequest(http.MethodGet, tt.path, nil)
rec := httptest.NewRecorder()

tt.mux.ServeHTTP(rec, req)
```

- `NewRequest` tạo `*http.Request` trong bộ nhớ. `nil` là body rỗng (GET không có body).
- `NewRecorder` trả `*httptest.ResponseRecorder`, nó cài đặt `http.ResponseWriter`. Handler ghi vào đó và ta đọc lại qua `rec.Code`, `rec.Header()`, `rec.Body`.
- `ServeHTTP` là method mà mọi `http.Handler` đều có. `ServeMux` tự chọn handler theo method và path.

```go
if rec.Code != tt.wantStatus {
    t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body)
}
```

Sai status thì dừng luôn (`Fatalf`), vì kiểm tra body sau đó không còn ý nghĩa. In kèm body để dễ debug.

```go
if ct := rec.Header().Get("Content-Type"); ct != "application/json" { ... }
if strings.Contains(rec.Body.String(), "10.0.0.5") { ... }
assertJSON(t, rec.Body.Bytes(), tt.wantBody)
```

- `if x := ...; điều_kiện` khai báo biến chỉ sống trong khối `if`.
- Kiểm tra không lộ chi tiết lỗi nội bộ.
- `assertJSON` decode cả body thật và body mong đợi vào `any` rồi so bằng `reflect.DeepEqual`. Lý do: `json.Encoder` thêm `\n` ở cuối, và thứ tự key trong object không quan trọng. So sánh sau khi decode tránh fail vì khác biệt "trình bày".
- `t.Helper()` trong `assertJSON`: khi fail, Go báo số dòng ở chỗ *gọi* helper, dễ tìm case nào sai.

### 5.2. `watchlist/handler_test.go`

Điểm khác so với price: `memStore` **bị thay đổi** khi POST/DELETE. Nếu các case dùng chung một store, case "remove existing" chạy trước sẽ làm case sau thấy dữ liệu khác. Vì vậy mỗi case tự tạo store:

```go
var s store = &memStore{syms: slices.Clone(tt.seed)}
if tt.broken {
    s = errStore{}
}
mux := http.NewServeMux()
NewHandler(NewService(s)).Register(mux)
```

- `var s store` khai báo biến kiểu interface `store`, nên gán được cả `*memStore` lẫn `errStore`.
- `slices.Clone(tt.seed)` copy slice, để `memStore` sửa bản copy chứ không sửa dữ liệu trong bảng test.
- Field `seed` là dữ liệu có sẵn (giống seeder/factory trong Laravel, hay seed DB in-memory trong EF Core).

```go
req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
```

`strings.NewReader` biến chuỗi thành `io.Reader` để làm body. Body rỗng (`""`) vẫn hợp lệ cho GET/DELETE.

Các case đáng chú ý:

- `"add normalizes symbol"`: gửi `" vnm "`, mong đợi danh sách chứa `"VNM"`, tức service đã `TrimSpace` + `ToUpper`.
- `"add invalid JSON"`: body `{"symbol":` bị cụt, `Decode` lỗi, trả 400.
- `"add body too large"`: body hơn 2KB. Handler bọc body bằng `http.MaxBytesReader(w, r.Body, 1<<10)` (1KB). Đọc quá giới hạn thì `Decode` lỗi, trả 400. Đây là cách chống client gửi body khổng lồ làm tốn RAM.
- `"remove existing"`: trả 204 No Content, không có body. Code test xử lý riêng:

```go
if tt.wantBody == "" {
    if rec.Body.Len() != 0 { t.Errorf(...) }
    return
}
```

`return` trong hàm của `t.Run` chỉ kết thúc sub-test hiện tại, các case khác vẫn chạy.

### 5.3. `realtime/handler_test.go`

**Dựng server và kết nối:**

```go
func startServer(t *testing.T, hub *Hub, snap snapshotter) *websocket.Conn {
    t.Helper()
    mux := http.NewServeMux()
    NewHandler(hub, snap).Register(mux)
    srv := httptest.NewServer(mux)
    t.Cleanup(srv.Close)

    url := "ws://" + strings.TrimPrefix(srv.URL, "http://") + "/ws"
    conn, _, err := websocket.DefaultDialer.Dial(url, nil)
    if err != nil {
        t.Fatalf("dial %s: %v", url, err)
    }
    t.Cleanup(func() { conn.Close() })
    return conn
}
```

- `httptest.NewServer` mở server thật trên cổng ngẫu nhiên. `srv.URL` có dạng `http://127.0.0.1:54321`.
- `t.Cleanup(f)` đăng ký hàm dọn dẹp, chạy khi test kết thúc (kể cả khi fail). Giống `IDisposable`/`Dispose` trong xUnit hay `tearDown()` trong PHPUnit, nhưng khai báo ngay cạnh chỗ tạo tài nguyên. Cleanup chạy theo thứ tự ngược (LIFO): đóng `conn` trước, rồi tắt server.
- WebSocket dùng scheme `ws://`, nên đổi `http://` thành `ws://`.
- `Dial` trả 3 giá trị: kết nối, HTTP response của bước handshake (không cần, bỏ bằng `_`), và lỗi.

**Đọc có timeout:**

```go
conn.SetReadDeadline(time.Now().Add(testTimeout))
_, msg, err := conn.ReadMessage()
```

`ReadMessage` chặn (block) cho tới khi có message. Nếu server có bug không gửi gì, test sẽ treo mãi. Đặt read deadline 2 giây thì `ReadMessage` trả lỗi timeout và test fail rõ ràng.

**Chờ điều kiện xảy ra ở goroutine khác:**

```go
func waitFor(t *testing.T, what string, cond func() bool) {
    t.Helper()
    deadline := time.Now().Add(testTimeout)
    for !cond() {
        if time.Now().After(deadline) {
            t.Fatalf("timed out waiting for %s", what)
        }
        time.Sleep(5 * time.Millisecond)
    }
}
```

Server xử lý mỗi kết nối trong goroutine riêng. Khi `Dial` trả về, handshake đã xong nhưng server **có thể chưa** chạy tới `hub.register(c)`. Không có cách nào biết chính xác thời điểm đó từ phía test, nên ta *poll*: kiểm tra mỗi 5ms, tối đa 2 giây. Cách này tốt hơn `time.Sleep(500ms)` cố định: nhanh khi máy nhanh, vẫn đúng khi máy chậm (ví dụ CI).

**Test broadcast từng bước:**

```go
hub := NewHub()
conn := startServer(t, hub, fakeSnapshot{})
waitFor(t, "client registered", func() bool { return hub.Len() == 1 })
```

Snapshot rỗng để message đầu tiên client nhận được chắc chắn là message broadcast. Phải chờ register xong, nếu không `Broadcast` sẽ chạy khi hub chưa có client nào và message bị mất. Test sẽ thỉnh thoảng fail (flaky).

```go
want := price.Tick{Symbol: "HPG", Price: 27.5, Ts: 42}
msg, err := json.Marshal(want)
...
hub.Broadcast(msg)

if got := readTick(t, conn); got != want { ... }
```

`Broadcast` đưa message vào channel `send` của client. Goroutine `writePump` phía server lấy ra và ghi xuống socket, client đọc được. `got != want` dùng được vì `Tick` chỉ chứa string/float/int, nên struct so sánh được bằng `==`.

**Test client ngắt kết nối:**

```go
conn.Close()
waitFor(t, "client unregistered", func() bool { return hub.Len() == 0 })
```

Client đóng socket, `readPump` phía server nhận lỗi đọc và return, sau đó `serve` gọi `hub.unregister(c)`. Việc này cũng diễn ra ở goroutine server nên lại dùng `waitFor`. Gọi `conn.Close()` lần nữa trong Cleanup không sao: lần đóng thứ hai chỉ trả lỗi và lỗi đó bị bỏ qua.

## 6. Lệnh chạy test

Chạy từ thư mục `backend/`:

```bash
go test ./...                                   # toàn bộ package
go test ./internal/price -run TestHandler -v    # một test, in chi tiết từng sub-test
go test ./internal/price -run 'TestHandler/get_one_symbol' -v   # một sub-test
go test -count=5 ./internal/...                 # chạy 5 lần, bỏ cache, để bắt test flaky
go test -cover ./...                            # % dòng code được test chạy qua
go test -coverprofile=cover.out ./internal/price && go tool cover -html=cover.out
                                                # xem dòng nào chưa được test (mở trình duyệt)
go test -race ./...                             # bật race detector
```

- `-run` nhận regex, khớp với tên test.
- `-v` (verbose) in tên từng test và log, kể cả khi pass.
- Go cache kết quả test. Nếu code không đổi, lần chạy sau in `(cached)`. `-count=1` buộc chạy lại.
- `-race`: Go chèn thêm code kiểm tra để phát hiện hai goroutine đọc/ghi cùng một biến mà không có khoá (data race). Rất quan trọng với code như `Hub` (nhiều goroutine cùng truy cập map `clients`). Lưu ý: `-race` cần **cgo**, tức cần trình biên dịch C (gcc). Trên Windows không cài gcc sẽ báo `-race requires cgo`. Cách chạy: cài MinGW-w64 rồi đặt `CGO_ENABLED=1`, hoặc chạy trong Docker:

  ```bash
  docker run --rm -v "$(pwd -W):/src" -w /src golang:1.27.2 go test -race ./...
  ```

  (`pwd -W` cho đường dẫn kiểu Windows trong Git Bash. Trên Linux/macOS dùng `$(pwd)`.)

- Coverage chỉ cho biết dòng nào **đã chạy qua**, không cho biết đã kiểm tra đúng hay chưa. Đừng chạy theo 100%. `cmd/` và `platform/` 0% là bình thường, vì đó là code nối DB/Redis thật, nên test bằng integration test (Docker) thay vì unit test.

## 7. Câu hỏi phỏng vấn có thể gặp

**1. Bạn test HTTP handler trong Go thế nào?**
Dùng `net/http/httptest`. Với REST: tạo `ServeMux` thật, đăng ký handler với service dùng storage giả, tạo request bằng `httptest.NewRequest`, ghi response vào `httptest.NewRecorder`, gọi `mux.ServeHTTP`, rồi kiểm tra status, header và body. Không cần mở cổng mạng nên test chạy rất nhanh.

**2. `NewRecorder` khác `NewServer` thế nào? Khi nào dùng cái nào?**
`NewRecorder` không dùng mạng, gọi handler như gọi hàm, phù hợp cho đa số handler. `NewServer` mở server TCP thật trên localhost, dùng khi cần kết nối thật: WebSocket (cần hijack connection), streaming/SSE, hoặc khi test một HTTP client (dùng `NewServer` để giả lập API bên thứ ba).

**3. Table-driven test là gì, lợi ích?**
Gom các trường hợp vào một slice struct (input + kết quả mong đợi), lặp qua và chạy `t.Run` cho mỗi case. Lợi ích: thêm case dễ, logic kiểm tra viết một lần, mỗi case có tên riêng, fail riêng và chạy riêng được. Giống `[Theory]/[InlineData]` trong xUnit hay `@dataProvider` trong PHPUnit.

**4. Fake khác mock thế nào? Sao không dùng thư viện mock?**
Fake là cài đặt đơn giản nhưng hoạt động thật (map/slice trong bộ nhớ), test kiểm tra kết quả. Mock được cài kỳ vọng về việc method nào được gọi, test kiểm tra tương tác. Trong Go, interface được thoả mãn ngầm và nên nhỏ, nên tự viết fake chỉ vài dòng, dễ đọc, và không phụ thuộc chi tiết cài đặt. Vẫn có thể dùng `gomock`/`testify/mock` khi cần kiểm tra tương tác phức tạp.

**5. Làm sao test nhánh lỗi 500 mà không làm hỏng DB thật?**
Service nhận interface, nên truyền vào một fake luôn trả lỗi (`errStore`). Test kiểm tra status 500 và body chỉ có thông báo chung, không lộ chi tiết lỗi nội bộ (IP, câu SQL...).

**6. Test có goroutine thì làm sao tránh flaky và tránh treo?**
Không dựa vào `time.Sleep` cố định. Chờ bằng cách poll điều kiện với deadline (`waitFor`), hoặc dùng channel để đồng bộ. Mọi thao tác chặn (đọc socket, đọc channel) đều có timeout (`SetReadDeadline`, `select` với `time.After`). Chạy `-count=N` và `-race` để phát hiện vấn đề.

**7. `-race` làm gì? Có nhược điểm gì?**
Bật race detector, phát hiện truy cập đồng thời vào cùng vùng nhớ mà không đồng bộ. Chỉ phát hiện race trên những đường code thực sự chạy trong test. Chương trình chạy chậm hơn nhiều lần và tốn RAM hơn, nên thường bật trong CI chứ không bật ở production. Cần cgo.

**8. `t.Fatal` khác `t.Error`?**
`t.Error` ghi lỗi và chạy tiếp, dùng khi các kiểm tra sau vẫn có ý nghĩa. `t.Fatal` ghi lỗi và dừng test hiện tại ngay (gọi `runtime.Goexit`), dùng khi bước sau phụ thuộc bước trước (ví dụ sai status thì không cần kiểm tra body). `t.Fatal` phải gọi từ goroutine của test, không gọi từ goroutine khác.

**9. `t.Helper()` và `t.Cleanup()` để làm gì?**
`t.Helper()` đánh dấu hàm là helper, khi fail Go báo dòng ở nơi gọi helper. `t.Cleanup()` đăng ký hàm dọn dẹp chạy khi test kết thúc, theo thứ tự LIFO, giống `Dispose`/`tearDown`.

**10. Vì sao test dùng router thật thay vì gọi thẳng `h.latest(w, r)`?**
Để test cả phần định tuyến: pattern `GET /api/prices/{symbol}`, `r.PathValue("symbol")`, giới hạn method. Nếu gọi thẳng hàm handler, `PathValue` sẽ rỗng vì không đi qua mux, và lỗi đăng ký route sẽ không bị phát hiện.

**11. Test cùng package (`package price`) hay package riêng (`package price_test`)?**
Cùng package (white-box) truy cập được kiểu unexported, nên dùng lại fake và interface nội bộ. Package `_test` (black-box) chỉ thấy API public, giúp kiểm tra API từ góc nhìn người dùng package. Dự án này chọn cùng package vì interface storage là unexported.

**12. Coverage bao nhiêu là đủ?**
Không có con số tuyệt đối. Coverage cao không đảm bảo test tốt, vì nó chỉ đo dòng đã chạy, không đo assert có đúng không. Nên ưu tiên phủ logic nghiệp vụ và các nhánh lỗi (400/404/500). Code nối hạ tầng (DB, Redis) để cho integration test.
