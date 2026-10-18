# Frontend realtime: WebSocket → Context → bảng giá

Tài liệu này giải thích phần realtime của `web/` cho người mới với React, đặc biệt nếu bạn đến từ C#/.NET hoặc PHP/Laravel. Đọc xong bạn nên tự giải thích được từng dòng của bốn file:

| File | Vai trò |
|---|---|
| `web/src/lib/wsClient.ts` | Lõi WebSocket, **không dùng React**: mở kết nối, tự kết nối lại với backoff + jitter |
| `web/src/lib/useWebSocket.ts` | Custom hook bọc `wsClient` để dùng trong component React |
| `web/src/features/price-board/components/PriceStreamProvider.tsx` | Mở **một** kết nối cho cả app, giữ state giá, phát qua Context |
| `web/src/features/price-board/components/PriceTable.tsx` | Đọc giá từ Context, vẽ bảng, tô xanh/đỏ và nháy khi giá đổi |

---

## 1. Luồng dữ liệu tổng quát

```
Go api ──(WebSocket /ws, mỗi message là JSON {symbol, price, ts})──▶ trình duyệt
                                                                        │
  wsClient.connectWs            nhận chuỗi JSON thô, lo việc kết nối lại │
        ▲                                                               ▼
  useWebSocket("/ws", onMessage)   gọi onMessage(data), trả về { status }
        ▲
  PriceStreamProvider              JSON.parse → setPrices → value = { prices, status }
        │   <PriceStreamContext value=...>
        ▼
  usePriceStream()  ◀── PriceTable, RootLayout (badge trạng thái), SymbolPage
```

Mỗi tầng chỉ biết tầng ngay dưới nó:

- `wsClient` không biết React, không biết "giá" là gì, chỉ biết chuỗi.
- `useWebSocket` biết React (state, effect) nhưng vẫn không biết "giá".
- `PriceStreamProvider` là nơi duy nhất biết message là `Tick` và gom thành bảng giá.
- `PriceTable` chỉ đọc dữ liệu đã sẵn sàng, không hề biết có WebSocket.

Đây giống cách bạn tách trong .NET: một class `HubConnection` (thư viện), một service bọc nó (`PriceStreamService`), và view chỉ nhận ViewModel.

Vị trí file cũng theo quy tắc của repo: `lib/` là code dùng chung, không thuộc feature nào; `features/price-board` được import `lib`, còn `lib` không bao giờ import ngược `features`.

---

## 2. `wsClient.ts` từng khối

### Kiểu trạng thái và hằng số

```ts
export type WsStatus = "connecting" | "open" | "closed";
const MIN_DELAY = 500;
const MAX_DELAY = 10_000;
```

- `WsStatus` là *union type* của TypeScript: biến chỉ được nhận một trong ba chuỗi này. Tương tự `enum` trong C#, nhưng giá trị là chuỗi.
- `10_000` là cách viết số có dấu gạch cho dễ đọc (= 10000), C# cũng có `10_000`.

### `wsUrl`

```ts
export function wsUrl(path: string): string {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${location.host}${path}`;
}
```

WebSocket cần URL tuyệt đối (`ws://...`). Ta lấy host của chính trang hiện tại, nên dev (Vite proxy `/ws` sang `:8080`) và production (nginx proxy) đều chạy mà không cần cấu hình. Trang chạy HTTPS thì phải dùng `wss` (WebSocket mã hoá), nếu không trình duyệt chặn.

### `withJitter`

```ts
function withJitter(delay: number): number {
  return delay * (0.8 + Math.random() * 0.4);
}
```

`Math.random()` cho số trong [0, 1), nên hệ số nằm trong [0.8, 1.2). Delay 1000ms sẽ thành ngẫu nhiên khoảng 800–1200ms. Lý do ở mục 4.

### `connectWs`: các biến trạng thái

```ts
let ws: WebSocket | undefined;      // socket hiện tại
let retry: ReturnType<typeof setTimeout> | undefined; // hẹn giờ thử lại đang chờ
let delay = MIN_DELAY;              // thời gian chờ cho lần thử tới
let stopped = false;                // true khi người gọi đã đóng hẳn
```

Các biến này nằm trong *closure*: mỗi lần gọi `connectWs` có một bộ biến riêng, giống field private của một object. Thực ra `connectWs` là một "class" viết dạng hàm. Bản C# tương đương:

```csharp
class ReconnectingSocket : IDisposable {
    WebSocket? ws; Timer? retry; int delay = 500; bool stopped;
    public void Dispose() { ... }   // = hàm mà connectWs trả về
}
```

`ReturnType<typeof setTimeout>` là "kiểu mà setTimeout trả về". Viết vậy để chạy được cả trên trình duyệt (trả `number`) lẫn Node (trả object).

### `open`: mở một socket

```ts
const open = () => {
  clearTimeout(retry);
  retry = undefined;
```

Nếu đang có hẹn giờ thử lại thì huỷ nó. Nhờ vậy dù `open` được gọi từ đâu (hẹn giờ hay sự kiện `online`) thì cũng không bao giờ có hai lần thử chồng lên nhau. `retry = undefined` còn là tín hiệu "hiện không chờ thử lại", được `onOnline` dùng bên dưới.

```ts
  onStatus("connecting");
  const socket = new WebSocket(url);
  ws = socket;
```

Báo trạng thái cho người gọi rồi tạo socket. `new WebSocket` không chặn (non-blocking): nó trả về ngay, kết nối thật diễn ra nền và báo kết quả qua các sự kiện bên dưới. Biến cục bộ `socket` giúp mỗi handler biết chính xác nó thuộc socket nào.

```ts
  socket.onopen = () => {
    delay = MIN_DELAY;
    onStatus("open");
  };
```

Kết nối thành công thì reset delay về 0.5s, để lần rớt mạng tiếp theo bắt đầu thử lại nhanh.

```ts
  socket.onmessage = (e) => onMessage(e.data);
```

Chuyển nguyên chuỗi cho người gọi. `wsClient` không parse JSON, việc đó thuộc về tầng biết định dạng dữ liệu.

```ts
  socket.onclose = () => {
    if (stopped || socket !== ws) return;
    onStatus("closed");
    retry = setTimeout(open, withJitter(delay));
    delay = Math.min(delay * 2, MAX_DELAY);
  };
};
```

- `stopped`: người gọi chủ động đóng (ví dụ component unmount), không được kết nối lại.
- `socket !== ws`: sự kiện của một socket cũ không còn là socket hiện tại, bỏ qua. Đây là lớp phòng thủ để không bao giờ có hai chuỗi kết nối lại song song.
- Nếu không thì báo `closed`, hẹn giờ thử lại, rồi nhân đôi delay cho lần sau (tối đa 10s). Đây là *exponential backoff*, xem mục 4.

Lưu ý: khi kết nối thất bại (server tắt), trình duyệt gọi `onerror` rồi `onclose`, nên chỉ cần xử lý `onclose`.

### Sự kiện `online`

```ts
const onOnline = () => {
  if (retry === undefined) return;
  delay = MIN_DELAY;
  open();
};
window.addEventListener("online", onOnline);
```

Khi laptop mất Wi‑Fi, delay có thể đã tăng lên 10s. Lúc mạng có lại, trình duyệt phát sự kiện `online`; ta thử ngay thay vì bắt người dùng chờ hết 10s. Điều kiện `retry === undefined` nghĩa là không có hẹn giờ đang chờ, tức socket đang kết nối hoặc đang mở, thì không làm gì. `open()` tự huỷ hẹn giờ cũ nên không bị chồng.

### Hàm trả về: đóng hẳn

```ts
open();
return () => {
  stopped = true;
  clearTimeout(retry);
  window.removeEventListener("online", onOnline);
  ws?.close();
};
```

Mở kết nối đầu tiên, rồi trả về hàm dọn dẹp, giống `Dispose()`. Thứ tự quan trọng: đặt `stopped = true` **trước** `ws.close()`, vì `close()` sẽ kích hoạt `onclose`, và lúc đó ta muốn handler thấy `stopped` để không kết nối lại. `ws?.close()` là *optional chaining*: chỉ gọi nếu `ws` khác `undefined` (giống `ws?.Close()` trong C#).

---

## 3. `useWebSocket.ts` từng khối

Custom hook là một hàm tên bắt đầu bằng `use`, bên trong gọi các hook khác của React. Nó cho phép đóng gói logic có state để tái sử dụng, tương tự một service nhỏ gắn với vòng đời component.

```ts
export function useWebSocket(path: string, onMessage: (data: string) => void): { status: WsStatus } {
  const [status, setStatus] = useState<WsStatus>("connecting");
```

`useState` tạo một state. Mỗi lần `setStatus` được gọi với giá trị khác, component dùng hook sẽ render lại. Giá trị ban đầu là `"connecting"` để badge hiện "Đang kết nối" ngay frame đầu tiên.

```ts
  const onMessageRef = useRef(onMessage);
  useEffect(() => {
    onMessageRef.current = onMessage;
  });
```

`useRef` tạo một "hộp" `{ current: ... }` tồn tại suốt đời component. Khác `useState`, đổi `ref.current` **không** gây render lại. Effect không có mảng phụ thuộc chạy sau **mỗi** lần render, nên hộp luôn chứa hàm `onMessage` mới nhất. Vì sao cần, xem mục 5.

Vì sao gán trong `useEffect` mà không gán thẳng `onMessageRef.current = onMessage` trong thân hàm? Quy tắc của React là thân component phải "thuần" (chỉ tính toán, không có side effect), và lint `react-hooks` của repo báo lỗi khi ghi ref trong lúc render. Message luôn tới sau khi effect đã chạy, nên gán trong effect là đủ.

```ts
  useEffect(
    () =>
      connectWs(wsUrl(path), {
        onStatus: setStatus,
        onMessage: (data) => onMessageRef.current(data),
      }),
    [path],
  );
```

- `useEffect(fn, [path])`: chạy `fn` sau khi component xuất hiện trên màn hình, và chạy lại chỉ khi `path` đổi.
- Hàm `fn` **trả về** hàm đóng của `connectWs`. React giữ hàm đó làm *cleanup*: gọi khi unmount, hoặc trước khi chạy lại effect vì `path` đổi. Nhờ vậy không bao giờ rò socket.
- `onStatus: setStatus`: truyền thẳng setter của React. `setStatus` có định danh ổn định qua các lần render nên an toàn.
- `onMessage: (data) => onMessageRef.current(data)`: socket giữ một hàm cố định, mỗi lần có message nó mới đọc `ref.current`, tức luôn gọi bản callback mới nhất.

```ts
  return { status };
}
```

Trả về object để sau này thêm field (ví dụ `send`) mà không phá chỗ gọi.

---

## 4. Exponential backoff và jitter

**Backoff tăng dần:** thử lại sau 0.5s, 1s, 2s, 4s, 8s, 10s, 10s... Nếu server sập lâu, client không spam kết nối mỗi 0.5s mà thưa dần, giảm tải cho cả server lẫn máy người dùng. Kết nối thành công thì reset về 0.5s.

**Jitter (nhiễu ngẫu nhiên):** giả sử 10.000 người đang mở bảng giá và server deploy lại. Cả 10.000 socket rớt **cùng một khoảnh khắc**. Không có jitter, tất cả thử lại đúng sau 0.5s, rồi đúng sau 1s, 2s... Mỗi đợt là 10.000 request dội vào cùng lúc, server vừa khởi động có thể sập tiếp. Hiện tượng này gọi là *thundering herd* (đàn gia súc giẫm đạp). Nhân delay với hệ số ngẫu nhiên 0.8–1.2 làm các lần thử rải đều ra một khoảng thời gian thay vì dồn vào một điểm.

| Lần thử | delay gốc | delay thực tế (jitter ±20%) |
|---|---|---|
| 1 | 500ms | 400–600ms |
| 2 | 1s | 0.8–1.2s |
| 3 | 2s | 1.6–2.4s |
| 6+ | 10s | 8–12s |

So sánh: Polly trong .NET có `WaitAndRetry` với `DecorrelatedJitterBackoffV2`; queue worker Laravel có `$backoff = [1, 5, 10]`. Cùng ý tưởng.

---

## 5. Vì sao dùng `useRef` cho callback

Trong `PriceStreamProvider` ta viết:

```tsx
const { status } = useWebSocket("/ws", (data) => { ... });
```

Arrow function này được **tạo mới mỗi lần render**. Với JavaScript, hai hàm có code giống nhau vẫn là hai object khác nhau (`fnA !== fnB`), giống hai `new Action(...)` trong C#.

Nếu hook đưa `onMessage` vào mảng phụ thuộc, `useEffect(..., [path, onMessage])`, thì mỗi render, tức **mỗi tick giá** (2 lần/giây × 5 mã), React thấy `onMessage` đổi, chạy cleanup (đóng socket) rồi mở socket mới. Kết quả: socket bị đóng mở liên tục.

Có hai cách sửa:

1. Bắt người gọi bọc callback bằng `useCallback`. Dễ quên, và chỉ cần một dependency đổi là lại reconnect.
2. **Giữ callback trong ref** (cách ta dùng): socket chỉ phụ thuộc `path`; callback mới nhất luôn được đọc qua `ref.current`. Người gọi viết tự nhiên, không cần nghĩ.

React 19.2 có hook `useEffectEvent` làm đúng việc này một cách chính thức. Ta dùng `useRef` vì nó dễ hiểu, phổ biến và chạy với mọi phiên bản React.

---

## 6. `PriceStreamProvider.tsx` từng khối

```tsx
export function PriceStreamProvider({ children }: { children: ReactNode }) {
  const [prices, setPrices] = useState<Record<string, PriceRow>>({});
```

`prices` là một dictionary `symbol → PriceRow` (giống `Dictionary<string, PriceRow>`). `PriceRow` là `Tick` cộng thêm `prev?` (giá trước đó).

```tsx
  const { status } = useWebSocket("/ws", (data) => {
    const t: Tick = JSON.parse(data);
    setPrices((p) => ({ ...p, [t.symbol]: { ...t, prev: p[t.symbol]?.price } }));
  });
```

- `JSON.parse` biến chuỗi thành object. `: Tick` chỉ là khai báo kiểu cho TypeScript, lúc chạy không có kiểm tra (khác `JsonSerializer.Deserialize<Tick>` có validate một phần).
- `setPrices((p) => ...)`: dạng *functional update*, nhận state hiện tại `p` và trả state mới. Bắt buộc dùng dạng này vì message đến dồn dập; nếu đọc biến `prices` của closure thì có thể là giá trị cũ và mất tick.
- `{ ...p, [t.symbol]: ... }`: tạo object **mới** chép các mã cũ rồi ghi đè mã vừa đổi. React so sánh state bằng tham chiếu, nên phải tạo object mới thay vì sửa trực tiếp (immutable update).
- `prev: p[t.symbol]?.price`: giá cũ của mã đó (hoặc `undefined` nếu là tick đầu). Đây là dữ liệu để biết tăng hay giảm.

```tsx
  return <PriceStreamContext value={{ prices, status }}>{children}</PriceStreamContext>;
}
```

Đưa `{ prices, status }` vào Context. Mọi component con, ở bất kỳ độ sâu nào, đọc được qua `usePriceStream()` (file `hooks/usePriceStream.ts`, hook đó ném lỗi nếu quên bọc Provider). Ở React 19 có thể dùng thẳng `<Context value>` thay vì `<Context.Provider value>`.

### Vì sao một socket cho cả app (Context)

`PriceStreamProvider` được đặt một lần trong `app/providers.tsx`, bọc toàn bộ router. Lợi ích:

- **Một kết nối duy nhất** dù có bao nhiêu component cần giá (bảng giá, badge trạng thái trên header, trang chi tiết mã). Nếu mỗi component tự gọi `useWebSocket`, ta có N socket nhận cùng dữ liệu, tốn băng thông và tài nguyên server.
- **Chuyển trang không mất kết nối và không mất dữ liệu**: Provider nằm ngoài router nên không bị unmount khi đổi route.
- Giống đăng ký `HubConnection` dạng **singleton** trong DI của .NET, hoặc khởi tạo `window.Echo` một lần trong `bootstrap.js` của Laravel.

Đánh đổi: mỗi tick làm mọi component dùng `usePriceStream()` render lại. Với 5 mã thì không đáng kể; nếu có hàng nghìn mã thì cân nhắc store có selector (Zustand, `useSyncExternalStore`) hoặc gom tick theo frame.

---

## 7. `PriceTable.tsx`: tô xanh/đỏ và hiệu ứng nháy

```tsx
const arrow = { up: "▲", down: "▼", flat: "■" };
```

Ký hiệu đi kèm màu, để người mù màu đỏ‑xanh vẫn phân biệt được.

```tsx
const { prices } = usePriceStream();
const rows = Object.values(prices)
  .filter((r) => !symbols || symbols.includes(r.symbol))
  .sort((a, b) => a.symbol.localeCompare(b.symbol));
```

Lấy danh sách dòng từ dictionary, lọc theo `symbols` nếu có (trang Watchlist truyền vào), sắp theo mã. Giống `prices.Values.Where(...).OrderBy(...)` trong LINQ hay `collect($prices)->filter()->sortBy()` của Laravel.

```tsx
const dir = direction(r);
```

`direction` (trong `types.ts`) so `price` với `prev`: lớn hơn là `up`, nhỏ hơn là `down`, bằng hoặc chưa có `prev` là `flat`. Lưu ý: so với **tick ngay trước**, không phải giá tham chiếu đầu phiên như bảng giá chứng khoán thật.

```tsx
<td className={`num price ${dir}`}>
  <span key={r.ts} className={dir === "flat" ? undefined : `flash-${dir}`}>
    {arrow[dir]} {formatPrice(r.price)}
  </span>
</td>
```

- Class `up`/`down`/`flat` trên `<td>` tô **màu chữ** cố định: CSS `.price.up { color: var(--up) }` (xanh), `.price.down { color: var(--down) }` (đỏ), `.price.flat` (vàng). Màu lấy từ biến CSS trong `app/index.css`, có bộ riêng cho dark mode.
- Class `flash-up`/`flash-down` trên `<span>` chạy **animation nền** 0.6s: nền bắt đầu xanh/đỏ nhạt (`--up-bg`/`--down-bg`) rồi mờ dần về trong suốt.
- **Mẹo `key={r.ts}`**: animation CSS chỉ chạy khi phần tử xuất hiện. Nếu giá tăng hai lần liên tiếp, class vẫn là `flash-up`, trình duyệt không chạy lại animation. Đổi `key` theo thời điểm tick khiến React coi đây là phần tử **mới**, xoá span cũ, tạo span mới, nên animation chạy lại mỗi tick.
- Trong CSS có:

  ```css
  @media (prefers-reduced-motion: reduce) {
    .flash-up, .flash-down { animation: none; }
  }
  ```

  Người dùng bật "giảm chuyển động" trong hệ điều hành sẽ không thấy nháy, nhưng vẫn thấy màu chữ và mũi tên.

---

## 8. React Query hay WebSocket: khi nào dùng cái nào

| | React Query (REST) | WebSocket |
|---|---|---|
| Ai chủ động | Client hỏi (pull) | Server đẩy (push) |
| Hợp với | Dữ liệu đọc theo yêu cầu, thay đổi không liên tục: lịch sử giá, watchlist | Dữ liệu đổi liên tục, cần độ trễ thấp: giá realtime |
| Có sẵn | Cache, loading/error, retry, refetch, invalidate sau mutation | Chỉ một đường ống; cache, reconnect phải tự làm (chính là `wsClient`) |
| Trong repo | `useHistory` (`refetchInterval: 5s`), API watchlist | `/ws` → `PriceStreamProvider` |

Quy tắc ngắn: **"trạng thái server mà mình hỏi"** thì React Query; **"sự kiện server đẩy tới"** thì WebSocket. Hai cái bổ sung nhau, ví dụ trang chi tiết mã lấy lịch sử bằng React Query và giá hiện tại bằng stream. Một pattern nâng cao: nhận tick qua WebSocket rồi `queryClient.setQueryData(...)` để cập nhật cache React Query.

React Query giống `HttpClient` cộng `IMemoryCache` cộng Polly gộp lại; với Laravel, nó tương ứng phía client của các route API thông thường.

---

## 9. StrictMode chạy effect hai lần

`app/main.tsx` bọc app trong `<StrictMode>`. Ở **chế độ dev**, React cố tình mount → unmount → mount lại mỗi component một lần để phát hiện effect quên dọn dẹp. Với hook của ta:

1. Mount: `connectWs` mở socket A.
2. React giả lập unmount: gọi cleanup → `stopped = true`, đóng A. Handler `onclose` của A thấy `stopped` nên không kết nối lại.
3. Mount lại: `connectWs` mới mở socket B, với bộ biến closure hoàn toàn mới.

Kết quả: chỉ còn một socket sống. Trong DevTools tab Network có thể thấy socket đầu bị huỷ, console có thể có cảnh báo "WebSocket is closed before the connection is established". Đây là bình thường và **không xảy ra ở production**. Nếu cleanup bị thiếu, bạn sẽ thấy hai socket và mỗi tick bị xử lý hai lần: đó đúng là lỗi StrictMode muốn bạn phát hiện.

---

## 10. So sánh với SignalR client và Laravel Echo

| | Repo này | SignalR (JS/.NET client) | Laravel Echo |
|---|---|---|---|
| Giao thức | WebSocket thuần, message là JSON tự định nghĩa | Giao thức SignalR trên WebSocket (fallback SSE/long polling) | Pusher protocol (Pusher, Reverb, Soketi) |
| Kết nối lại | Tự viết: backoff + jitter + sự kiện `online` | `.withAutomaticReconnect([0, 2000, 10000, 30000])` | pusher-js tự kết nối lại |
| Nhận message | `onMessage(data)` rồi tự `JSON.parse` | `connection.on("ReceivePrice", (sym, price) => ...)` | `Echo.channel('prices').listen('PriceUpdated', e => ...)` |
| Trạng thái | `status` từ hook | `onreconnecting`, `onreconnected`, `onclose` | `Echo.connector.pusher.connection.bind('state_change', ...)` |
| Dùng chung | Một Provider/Context cho cả app | Thường là singleton `HubConnection` | `window.Echo` toàn cục |

Ta tự viết vì server Go gửi WebSocket thuần, và vì code ngắn đủ để hiểu hết. Ở dự án lớn có thể chọn thư viện, ví dụ `react-use-websocket`.

---

## 11. Câu hỏi phỏng vấn có thể gặp

**1. Tại sao tách `wsClient.ts` khỏi `useWebSocket.ts`?**
`wsClient` là logic thuần, không phụ thuộc React: dễ test, dùng lại được ở nơi khác (Vue, web worker). Hook chỉ lo nối logic đó vào vòng đời component. Đây là tách "cơ chế" khỏi "framework".

**2. Exponential backoff là gì, tại sao cần jitter?**
Mỗi lần thất bại thì chờ lâu gấp đôi (có giới hạn trên) để không spam server đang sập. Jitter thêm ngẫu nhiên để nhiều client không thử lại cùng một thời điểm sau khi server restart (thundering herd).

**3. Tại sao reset delay khi `onopen`?**
Để lần rớt mạng sau bắt đầu lại từ thử nhanh. Nếu không reset, sau một lần sự cố mọi lần rớt sau đều phải chờ 10s.

**4. Nếu bỏ `useRef` và đưa `onMessage` vào dependency của `useEffect` thì sao?**
Callback là hàm mới mỗi render, mà mỗi tick gây render, nên socket bị đóng và mở lại sau mỗi tick. Ref giúp socket ổn định và vẫn gọi callback mới nhất.

**5. Tại sao `setPrices((p) => ...)` mà không phải `setPrices({ ...prices, ... })`?**
Callback của socket có thể giữ `prices` cũ (stale closure). Dạng hàm luôn nhận state mới nhất, không mất tick khi nhiều message đến trước một lần render.

**6. Làm sao bảo đảm không rò kết nối khi component unmount?**
Effect trả về hàm đóng; hàm đó đặt `stopped = true`, huỷ hẹn giờ, gỡ listener `online`, đóng socket. `onclose` thấy `stopped` nên không mở lại.

**7. StrictMode làm effect chạy hai lần, có tạo hai socket không?**
Ở dev có tạo, nhưng cái đầu bị đóng ngay bởi cleanup, chỉ còn một. Production chỉ chạy một lần.

**8. Tại sao đặt WebSocket trong Context thay vì mỗi component tự kết nối?**
Một kết nối cho cả app, dữ liệu giữ nguyên khi chuyển trang, mọi nơi thấy cùng một trạng thái. Nhược điểm là mọi consumer render lại mỗi tick; khi quy mô lớn dùng store có selector.

**9. Hiệu ứng nháy chạy lại mỗi tick bằng cách nào?**
`key={r.ts}` làm React tạo span mới mỗi tick, animation CSS chạy lại. Tôn trọng `prefers-reduced-motion` bằng cách tắt animation; màu và mũi tên vẫn còn.

**10. Khi nào dùng React Query, khi nào WebSocket?**
React Query cho dữ liệu client chủ động hỏi (lịch sử, watchlist) với cache/retry sẵn có; WebSocket cho dữ liệu server đẩy liên tục (giá). Có thể kết hợp bằng `setQueryData` khi nhận message.

**11. Khi mất kết nối thì giao diện xử lý ra sao?**
Badge trên header chuyển "Mất kết nối" (đỏ), bảng vẫn hiện giá cuối cùng. Khi kết nối lại, server gửi ngay snapshot giá mới nhất nên bảng tự cập nhật. Có mạng lại (sự kiện `online`) thì thử ngay không chờ backoff.

**12. Nếu server gửi message không phải JSON hợp lệ thì sao?**
Hiện tại `JSON.parse` sẽ ném lỗi trong handler (lỗi hiện ở console, socket vẫn sống, các tick sau vẫn xử lý bình thường). Cải tiến: bọc `try/catch` và validate bằng thư viện như `zod`.

**13. Có cần gửi heartbeat/ping không?**
Một số proxy cắt kết nối im lặng lâu. Ở đây server đẩy tick mỗi 500ms nên kết nối không bao giờ im lặng; nếu dữ liệu thưa thì nên có ping/pong (phía Go) hoặc timeout phía client để phát hiện "kết nối chết mà không có `onclose`".
