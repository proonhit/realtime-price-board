# CI với GitHub Actions — giải thích cho người mới

Tài liệu này giải thích file [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) và [`backend/Dockerfile`](../../backend/Dockerfile) gần như từng dòng, để bạn tự tin trình bày khi phỏng vấn.

---

## 1. CI là gì?

**CI (Continuous Integration — tích hợp liên tục)** là thói quen: mỗi lần ai đó đẩy code lên (push) hoặc mở pull request, một máy chủ **tự động** lấy code về và chạy các bước kiểm tra (format, lint, test, build). Nếu có bước nào lỗi, PR bị đánh dấu đỏ và ta biết ngay, trước khi code lỗi vào nhánh `main`.

Lợi ích:

- **Phát hiện lỗi sớm**: lỗi được bắt ở PR chứ không phải lúc deploy.
- **"Chạy được trên máy tôi" không còn là cái cớ**: CI chạy trên một máy sạch, giống nhau mọi lần.
- **Tiêu chuẩn chung**: ai cũng phải qua cùng một bộ kiểm tra (format, lint, test).
- **Bằng chứng cho người review**: badge xanh trên README cho thấy dự án đang build được.

**CD (Continuous Delivery/Deployment)** là bước tiếp theo: tự động deploy sau khi CI pass. Dự án này mới chỉ có CI.

### Các khái niệm của GitHub Actions

| Khái niệm | Ý nghĩa | Tương đương Azure DevOps |
|---|---|---|
| **Workflow** | Một file YAML trong `.github/workflows/`. | Pipeline (`azure-pipelines.yml`) |
| **Event / trigger** (`on:`) | Sự kiện kích hoạt workflow (push, pull_request…). | `trigger:` / `pr:` |
| **Job** | Một nhóm bước chạy trên **một máy ảo riêng**. Các job mặc định chạy **song song**. | `jobs:` / `job:` |
| **Runner** (`runs-on`) | Máy ảo chạy job, ví dụ `ubuntu-latest`. | Agent pool (`vmImage: ubuntu-latest`) |
| **Step** | Một bước trong job: chạy lệnh shell (`run`) hoặc dùng action có sẵn (`uses`). | `steps:` / `script:` / `task:` |
| **Action** | "Gói" step viết sẵn, tái sử dụng được, ví dụ `actions/checkout@v4`. | Task, ví dụ `UseDotNet@2` |

---

## 2. Giải thích `ci.yml` từng khối

### 2.1. Tên workflow

```yaml
name: CI
```

Tên hiển thị trên tab **Actions** của GitHub. Badge trên README trỏ tới file `ci.yml`.

### 2.2. Trigger: khi nào chạy

```yaml
on:
  push:
    branches:
      - main
      - "dev/**"
  pull_request:
    branches:
      - main
```

- `push` lên `main` hoặc bất kỳ nhánh nào bắt đầu bằng `dev/` (ví dụ `dev/add_login`). `**` nghĩa là "khớp mọi thứ, kể cả dấu `/`", nên `dev/a/b` cũng khớp.
- `"dev/**"` phải để trong ngoặc kép vì trong YAML, `*` ở đầu giá trị có nghĩa đặc biệt (alias). Ở đây không bắt buộc nhưng để ngoặc cho an toàn.
- `pull_request` với `branches: [main]` nghĩa là PR **nhắm vào** `main` (nhánh đích), bất kể nhánh nguồn là gì. Với PR, GitHub chạy trên một **merge commit tạm** (code của PR đã được gộp thử vào main), nên ta kiểm tra đúng cái sẽ xảy ra sau khi merge.

> Vì sao không chạy cho mọi nhánh? Để tiết kiệm phút chạy CI. Nhánh khác vẫn được kiểm tra khi mở PR vào `main`.

### 2.3. Concurrency: huỷ lần chạy cũ

```yaml
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true
```

- `group`: các lần chạy có cùng tên group sẽ "xếp hàng" với nhau. `github.ref` là nhánh hoặc PR, ví dụ `refs/heads/dev/add_login` hoặc `refs/pull/12/merge`. Vậy mỗi nhánh/PR là một group.
- `cancel-in-progress: true`: nếu bạn push 3 commit liên tiếp, lần chạy của commit 1 và 2 sẽ bị **huỷ**, chỉ commit mới nhất được kiểm tra. Kết quả của commit cũ không còn ý nghĩa, không cần tốn tài nguyên chờ nó.
- `${{ ... }}` là **expression** của GitHub Actions, được thay giá trị trước khi chạy (giống `$(Build.SourceBranch)` trong Azure DevOps).

### 2.4. Permissions: quyền tối thiểu

```yaml
permissions:
  contents: read
```

Mỗi workflow có một token tự động (`GITHUB_TOKEN`). Mặc định token này có thể có quyền ghi. Ta khai báo rõ **chỉ đọc code**: theo nguyên tắc **least privilege (quyền tối thiểu)**. Nếu một action bên thứ ba bị chiếm quyền, nó cũng không thể push code hay sửa release.

### 2.5. Job `backend`

```yaml
jobs:
  backend:
    name: Backend (Go)
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: backend
```

- `backend:` là ID của job (dùng trong `needs`). `name:` là tên hiển thị.
- `runs-on: ubuntu-latest`: chạy trên máy ảo Ubuntu do GitHub cấp, miễn phí cho repo public. Máy này đã có sẵn Docker, git, gcc…
- `defaults.run.working-directory: backend`: mọi step `run:` sẽ `cd backend` trước khi chạy. Không cần viết `cd backend && ...` ở từng bước. Lưu ý: nó **chỉ áp dụng cho `run`**, không áp dụng cho `uses`. Vì vậy ở `setup-go` ta vẫn ghi đường dẫn đầy đủ `backend/go.mod`.

```yaml
      - uses: actions/checkout@v4
```

Máy ảo lúc đầu trống trơn. Action này `git clone` repo và checkout đúng commit đang được kiểm tra. `@v4` là **ghim major version**: nhận bản vá lỗi của v4 nhưng không tự nhảy lên v5 (có thể có breaking change).

```yaml
      - uses: actions/setup-go@v5
        with:
          go-version-file: backend/go.mod
          cache-dependency-path: backend/go.sum
```

- Cài Go. `go-version-file` đọc dòng `go 1.27.2` trong `go.mod`, nên CI luôn dùng **cùng phiên bản** với khi dev, không phải sửa hai nơi.
- `cache-dependency-path`: setup-go tự cache thư mục module (`~/go/pkg/mod`) và build cache. **Key** của cache được tính từ hash của `go.sum`. `go.sum` không đổi thì dùng lại cache, khỏi tải lại dependency. Ta phải chỉ rõ `backend/go.sum` vì `go.sum` không nằm ở gốc repo.

```yaml
      - name: Check formatting (gofmt)
        run: |
          unformatted=$(gofmt -l .)
          if [ -n "$unformatted" ]; then
            echo "Các file sau chưa chạy gofmt:"
            echo "$unformatted"
            exit 1
          fi
```

- `run: |`: dấu `|` trong YAML nghĩa là chuỗi nhiều dòng, giữ nguyên xuống dòng. Đây là một đoạn script bash.
- `gofmt -l .` **liệt kê** (`-l` = list) các file có format khác chuẩn, nhưng **không sửa** và **luôn trả exit code 0**. Vì vậy ta phải tự kiểm tra output: `[ -n "$unformatted" ]` nghĩa là "chuỗi không rỗng", tức có file chưa format. Khi đó in danh sách ra rồi `exit 1` để step (và job) fail.
- Vì sao cần? Go có **một** chuẩn format chính thức. Ai cũng format giống nhau thì diff trong PR chỉ chứa thay đổi logic, không lẫn thay đổi khoảng trắng. Sửa lỗi này rất dễ: chạy `gofmt -w .` hoặc `go fmt ./...`.

> **Lưu ý trên Windows:** nếu git đặt `core.autocrlf=true`, file trong thư mục làm việc dùng xuống dòng CRLF, và `gofmt -l .` chạy ở máy bạn sẽ liệt kê **mọi** file. Trong repo (và trên CI Linux), file vẫn là LF nên CI vẫn pass. Muốn kiểm tra local chính xác, có thể thêm file `.gitattributes` với dòng `*.go text eol=lf`.

```yaml
      - name: Vet
        run: go vet ./...
```

- `./...` nghĩa là "package hiện tại và **mọi** package con".
- `go vet` là **phân tích tĩnh** có sẵn của Go: tìm code compile được nhưng gần như chắc chắn sai. Ví dụ `fmt.Printf("%d", "chuỗi")`, copy một `sync.Mutex` theo giá trị, struct tag sai cú pháp, code không bao giờ chạy tới. Tương tự Roslyn analyzers / warnings trong .NET.

```yaml
      - name: Test
        run: go test -race -count=1 ./...
```

- `go test ./...` chạy mọi file `*_test.go`.
- `-race` bật **race detector**. Dự án này có nhiều goroutine chạy đồng thời (hub WebSocket broadcast tới nhiều client, subscriber Redis…). **Data race** xảy ra khi hai goroutine cùng truy cập một biến, ít nhất một bên ghi, mà không có đồng bộ (mutex/channel). Lỗi này thường **không** lộ ra khi test bình thường mà chỉ thỉnh thoảng làm sập production. `-race` gắn thêm code theo dõi mọi lần đọc/ghi bộ nhớ và báo lỗi ngay khi thấy race. Cái giá là test chậm hơn khoảng 2–10 lần và tốn RAM hơn. Đó là lý do ta bật nó trong CI chứ không bật trong build production.
- `-race` cần **cgo** (trình biên dịch C). Runner Ubuntu có sẵn gcc nên chạy được. Trên Windows không có gcc thì sẽ báo `-race requires cgo`.
- `-count=1` **tắt test cache**. Bình thường Go nhớ kết quả test, code không đổi thì in `(cached)` mà không chạy lại. Trên CI ta muốn chắc chắn test thật sự chạy (cache của setup-go có thể khôi phục cả test cache).

```yaml
      - name: Build
        run: go build ./...
```

Compile mọi package, kể cả `cmd/api` và `cmd/producer` (không có test nên `go test` không "đụng" kỹ tới). Với nhiều package, `go build ./...` chỉ kiểm tra compile được và không ghi file binary ra, nên rất nhanh.

### 2.6. Job `web`

```yaml
  web:
    name: Web (React)
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: web
```

Giống job backend nhưng làm việc trong thư mục `web/`. Job này chạy **song song** với `backend` trên một máy ảo khác.

```yaml
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: web/package-lock.json
```

- Cài Node 22, trùng với `FROM node:22-alpine` trong `web/Dockerfile`, để CI và Docker build dùng cùng phiên bản.
- `cache: npm` cache thư mục cache tải về của npm (`~/.npm`), **không phải** `node_modules`. Key cache là hash của `package-lock.json`. Lần sau `npm ci` lấy package từ cache local thay vì tải lại từ internet.

```yaml
      - name: Install dependencies
        run: npm ci
```

**`npm ci` khác `npm install` thế nào?**

| | `npm install` | `npm ci` |
|---|---|---|
| Đọc | `package.json` (có thể cập nhật lock file) | **Chỉ** `package-lock.json` |
| Version | Có thể cài bản mới hơn nếu `^` cho phép | Cài **đúng** version trong lock |
| Lock file không khớp `package.json` | Tự sửa lock | **Báo lỗi và dừng** |
| `node_modules` cũ | Giữ lại, cập nhật thêm | **Xoá** rồi cài lại từ đầu |
| Dùng khi | Dev thêm/sửa package | CI, Docker build |

Tóm lại, `npm ci` cho kết quả **tái lập được (reproducible)**: CI cài đúng những gì đã được commit. Tương tự `dotnet restore --locked-mode` với `packages.lock.json`, hoặc `composer install` (theo `composer.lock`) so với `composer update` bên PHP.

```yaml
      - name: Lint
        run: npm run lint
```

Chạy script `"lint": "eslint ."` trong `package.json`. ESLint đọc `eslint.config.js`: rule JS chuẩn, rule TypeScript, rule **React Hooks** (ví dụ thiếu dependency trong `useEffect`), và rule cho Vite fast refresh. ESLint trả exit code khác 0 khi có **error** nên step fail.

```yaml
      - name: Build
        run: npm run build
```

Chạy `"build": "tsc -b && vite build"`:

1. `tsc -b` **type-check** toàn bộ TypeScript theo `tsconfig*.json`. Vite tự nó **không** kiểm tra kiểu (chỉ xoá kiểu đi cho nhanh), nên lỗi kiểu chỉ được bắt ở bước này.
2. `vite build` đóng gói (bundle) và minify ra thư mục `dist/`. Đây cũng là thứ `web/Dockerfile` copy vào nginx.

### 2.7. Job `docker`

```yaml
  docker:
    name: Docker images
    runs-on: ubuntu-latest
    needs: [backend, web]
    steps:
      - uses: actions/checkout@v4
      - name: Build images
        run: docker compose build
```

- `needs: [backend, web]`: job này **đợi** hai job kia pass mới chạy. Nếu test fail thì không tốn thời gian build image. Đây là cách tạo **thứ tự phụ thuộc** giữa các job (giống `dependsOn` trong Azure DevOps).
- `docker compose build` đọc `docker-compose.yml` và build mọi service có khai báo `build:`: `api` và `producer` (từ `backend/Dockerfile` với build arg `CMD` khác nhau) và `web`. Các service chỉ dùng `image:` (postgres, redis…) thì bỏ qua.
- Chỉ **build** để chứng minh Dockerfile đúng (multi-stage build compile được). Không `up`, không push image lên registry.
- Không cần `defaults.run.working-directory` vì `docker-compose.yml` nằm ở gốc repo.

---

## 3. So sánh với Azure DevOps / .NET

Nếu viết pipeline tương tự cho một dự án .NET trên Azure DevOps:

```yaml
# azure-pipelines.yml
trigger:
  branches:
    include: [main, dev/*]
pr:
  branches:
    include: [main]

pool:
  vmImage: ubuntu-latest

steps:
  - task: UseDotNet@2
    inputs:
      useGlobalJson: true              # ~ go-version-file: go.mod
  - script: dotnet format --verify-no-changes   # ~ gofmt -l
  - script: dotnet restore --locked-mode        # ~ npm ci / go mod download
  - script: dotnet build --no-restore -warnaserror  # ~ go build + go vet
  - script: dotnet test --no-build              # ~ go test
```

Và trên GitHub Actions cho .NET:

```yaml
- uses: actions/setup-dotnet@v4
  with:
    global-json-file: global.json
- run: dotnet restore
- run: dotnet build --no-restore
- run: dotnet test --no-build
```

| Go / Node (dự án này) | .NET | PHP/Laravel |
|---|---|---|
| `gofmt -l` | `dotnet format --verify-no-changes` | `vendor/bin/pint --test` |
| `go vet` | Roslyn analyzers, `-warnaserror` | `phpstan` / `larastan` |
| `go test -race` | `dotnet test` (không có race detector tích hợp) | `php artisan test` |
| `npm ci` | `dotnet restore --locked-mode` | `composer install` |
| `go.mod` (version Go) | `global.json` (version SDK) | `composer.json` → `require.php` |

Khác biệt chính giữa GitHub Actions và Azure DevOps:

- Azure DevOps mặc định có **một job**, các step nối tiếp nhau. GitHub Actions khuyến khích tách **nhiều job song song**, mỗi job một máy ảo riêng. Vì vậy mỗi job phải `checkout` lại.
- "Task" của Azure tương ứng với "action" (`uses:`) của GitHub. Action lấy từ repo GitHub khác (`owner/repo@version`), nên cần ghim version.

---

## 4. Caching tóm tắt

- **Vì sao cache?** Mỗi job chạy trên máy ảo mới tinh. Không có cache thì lần nào cũng tải lại toàn bộ Go modules và npm packages, tốn thêm vài chục giây đến vài phút.
- **Cơ chế**: cache lưu theo **key**. Key gồm hệ điều hành + hash file lock (`go.sum`, `package-lock.json`). File lock đổi thì key đổi, cache cũ không dùng nữa và cache mới được tạo khi job kết thúc thành công.
- **An toàn**: cache chỉ giúp tăng tốc, không ảnh hưởng đúng/sai. `npm ci` vẫn kiểm tra integrity hash của từng package. Cache hỏng hoặc thiếu thì chỉ chậm hơn chứ không sai.
- `setup-go` và `setup-node` đều có cache tích hợp, nên ta không cần viết `actions/cache` thủ công.
- Docker cũng có cache layer, nhưng trên runner mới thì cache trống. Có thể tối ưu thêm bằng `docker/build-push-action` với `cache-from: type=gha`, nhưng hiện chưa cần.

---

## 5. Giải thích `backend/Dockerfile` từng dòng

```dockerfile
# Một Dockerfile cho mọi command trong cmd/: chọn bằng build arg CMD (api | producer).
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG CMD=api
RUN CGO_ENABLED=0 go build -o /app ./cmd/${CMD}

FROM alpine:3.20
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]
```

Đây là **multi-stage build**: có hai lệnh `FROM`, tức hai "giai đoạn". Giai đoạn 1 có đủ công cụ để **build**. Giai đoạn 2 chỉ chứa thứ cần để **chạy**. Image cuối cùng chỉ là giai đoạn cuối.

**Giai đoạn 1: build**

- `FROM golang:1.27-alpine AS build`: image chứa trình biên dịch Go (bản Alpine, nhỏ hơn bản Debian). `AS build` đặt tên cho giai đoạn để lát nữa tham chiếu.
- `WORKDIR /src`: tạo và `cd` vào thư mục `/src` trong container.
- `COPY go.mod go.sum ./`: **chỉ** copy hai file khai báo dependency trước.
- `RUN go mod download`: tải dependency. Đây là mẹo **cache layer**: Docker cache từng lệnh. Khi bạn chỉ sửa code `.go` mà không đổi `go.mod`/`go.sum`, hai layer này dùng lại từ cache, không phải tải lại dependency. Nếu `COPY . .` trước thì sửa một dòng code cũng làm mất cache tải dependency.
- `COPY . .`: copy toàn bộ code của `backend/` (build context) vào `/src`.
- `ARG CMD=api`: khai báo **build argument** tên `CMD`, mặc định `api`. Trong `docker-compose.yml`, service `producer` truyền `args: { CMD: producer }` và service `api` truyền `CMD: api`. Nhờ vậy **một** Dockerfile build được **hai** chương trình (`cmd/api` và `cmd/producer`), không phải viết hai file gần giống nhau. Lưu ý: `ARG` chỉ tồn tại lúc build, không có trong container lúc chạy (khác `ENV`).
- `RUN CGO_ENABLED=0 go build -o /app ./cmd/${CMD}`: compile ra một file binary `/app`.
  - `CGO_ENABLED=0` tắt cgo, tức không liên kết với thư viện C của hệ điều hành. Kết quả là binary **tĩnh hoàn toàn (static)**, không phụ thuộc libc. Nó chạy được trên bất kỳ Linux nào, kể cả image tối giản như `alpine` (dùng musl thay glibc) hay `scratch` (rỗng hoàn toàn). Nếu bật cgo, binary có thể cần đúng bản libc lúc build và sẽ lỗi "not found" khó hiểu khi chạy ở image khác.
  - Đây cũng là lý do `-race` (cần cgo) chỉ dùng trong CI test, không dùng ở build production.

**Giai đoạn 2: runtime**

- `FROM alpine:3.20`: bắt đầu lại từ một image Alpine sạch, chỉ khoảng 8 MB. Toàn bộ trình biên dịch Go, mã nguồn và cache module của giai đoạn 1 bị **bỏ lại**.
- `COPY --from=build /app /app`: lấy **duy nhất** file binary từ giai đoạn `build`.
- `EXPOSE 8080`: ghi chú (metadata) rằng app lắng nghe cổng 8080. Lệnh này **không** tự mở cổng. Việc map cổng ra ngoài do `ports: ["8080:8080"]` trong compose đảm nhiệm.
- `ENTRYPOINT ["/app"]`: lệnh chạy khi container khởi động. Dạng mảng JSON (exec form) chạy trực tiếp binary, không qua shell, nên app nhận được tín hiệu `SIGTERM` khi `docker stop` và có thể tắt êm (graceful shutdown).

**Vì sao image nhỏ quan trọng?**

- Image `golang:1.27-alpine` nặng vài trăm MB. Image cuối chỉ khoảng 8 MB Alpine cộng vài chục MB binary.
- **Pull/deploy nhanh hơn**, tốn ít dung lượng registry hơn.
- **Bảo mật**: ít phần mềm hơn thì ít lỗ hổng (CVE) hơn, và kẻ tấn công không có trình biên dịch hay mã nguồn trong container.
- Tương tự .NET: build bằng image `mcr.microsoft.com/dotnet/sdk`, chạy bằng image `mcr.microsoft.com/dotnet/aspnet` (nhỏ hơn nhiều). Go còn đi xa hơn vì không cần runtime, chỉ cần một file binary.

`web/Dockerfile` cũng theo cùng ý tưởng: giai đoạn 1 dùng `node:22-alpine` để `npm ci` + `npm run build`, giai đoạn 2 chỉ là `nginx:alpine` phục vụ thư mục `dist/` tĩnh. Node.js không có trong image cuối.

---

## 6. Câu hỏi phỏng vấn có thể gặp

**1. CI là gì, khác CD thế nào?**
CI tự động build và test mỗi lần có thay đổi để phát hiện lỗi sớm. CD tự động đưa bản đã qua CI lên môi trường (staging/production). Dự án này có CI. CD có thể thêm sau, ví dụ build và push image lên GHCR khi merge vào `main`.

**2. Workflow chạy khi nào?**
Khi push lên `main` hoặc nhánh `dev/**`, và khi mở/cập nhật PR nhắm vào `main`. Với PR, GitHub test trên merge commit giữa nhánh PR và `main`.

**3. Vì sao tách ba job thay vì một?**
`backend` và `web` độc lập nên chạy **song song**, nhanh hơn. Lỗi cũng được khoanh vùng rõ: nhìn là biết hỏng ở Go hay React. `docker` dùng `needs` để chỉ chạy khi hai job kia pass, tránh tốn tài nguyên build image từ code đang lỗi.

**4. `-race` để làm gì? Sao không bật luôn trong production?**
Nó phát hiện data race, tức hai goroutine truy cập cùng biến mà không đồng bộ. Điều này quan trọng vì dự án có WebSocket hub broadcast tới nhiều client đồng thời. Không bật trong production vì nó làm chương trình chậm hơn vài lần, tốn RAM, và cần cgo (trong khi image production build với `CGO_ENABLED=0`). Lưu ý: race detector chỉ bắt race ở những đoạn code **thực sự được chạy** trong test, nên test cần có kịch bản đồng thời.

**5. `npm ci` khác `npm install`?**
`npm ci` cài chính xác theo `package-lock.json`, xoá `node_modules` cũ, và báo lỗi nếu lock file lệch với `package.json`. Kết quả tái lập được, phù hợp cho CI/Docker. `npm install` có thể cập nhật lock file và dùng cho lúc dev.

**6. Vì sao cần kiểm tra gofmt trong CI?**
Để code luôn theo một chuẩn format duy nhất, review PR không bị nhiễu bởi thay đổi khoảng trắng. `gofmt -l` không trả exit code lỗi nên phải tự kiểm tra output rỗng hay không.

**7. `go vet` khác `go build` thế nào?**
`go build` chỉ kiểm tra code có compile được không. `go vet` tìm code compile được nhưng nhiều khả năng sai (format string sai kiểu, copy mutex, struct tag lỗi…).

**8. Cache hoạt động thế nào? Có thể làm CI sai không?**
Cache được lưu theo key là hash của `go.sum`/`package-lock.json`. Lock file đổi thì cache mới được tạo. Cache chỉ tăng tốc: thiếu cache thì chậm hơn chứ không sai, vì package vẫn được kiểm tra checksum. Riêng test cache của Go thì ta tắt bằng `-count=1` để test luôn thật sự chạy.

**9. `concurrency` với `cancel-in-progress` giải quyết vấn đề gì?**
Khi push nhiều commit liên tiếp lên cùng nhánh/PR, chỉ commit mới nhất cần được kiểm tra. Các lần chạy cũ bị huỷ để tiết kiệm phút CI và có kết quả nhanh hơn.

**10. Vì sao đặt `permissions: contents: read`?**
Nguyên tắc quyền tối thiểu. `GITHUB_TOKEN` chỉ đọc được code, nên nếu một action bên thứ ba bị chiếm quyền cũng không thể push code, sửa release hay ghi vào repo.

**11. Vì sao ghim `@v4`, `@v5`? Có cách an toàn hơn không?**
Ghim major version để không bất ngờ dính breaking change mà vẫn nhận bản vá. An toàn hơn nữa là ghim theo **commit SHA đầy đủ** (ví dụ `actions/checkout@<sha>`), vì tag có thể bị di chuyển. Kết hợp với Dependabot để tự cập nhật.

**12. Multi-stage build là gì, lợi ích?**
Dùng nhiều `FROM` trong một Dockerfile: một stage có đủ công cụ để build, stage cuối chỉ copy kết quả (binary hoặc `dist/`) sang image tối giản. Image nhỏ, deploy nhanh, ít lỗ hổng hơn, không chứa mã nguồn hay trình biên dịch.

**13. `CGO_ENABLED=0` để làm gì?**
Tạo binary Go tĩnh, không phụ thuộc thư viện C (libc) của hệ điều hành. Nhờ vậy nó chạy được trên Alpine/`scratch`, và build ổn định không cần gcc.

**14. Vì sao `COPY go.mod go.sum` rồi `go mod download` trước `COPY . .`?**
Tận dụng cache layer của Docker. Dependency ít thay đổi hơn code, nên tách ra thì sửa code không làm tải lại toàn bộ dependency.

**15. Build arg `CMD` dùng để làm gì?**
Để một Dockerfile build được cả `cmd/api` và `cmd/producer`. Compose truyền `args: { CMD: producer }` cho service producer. Tránh lặp lại hai Dockerfile gần như giống nhau.

**16. Nếu CI fail ở PR thì sao?**
PR hiện dấu, xem log của step bị lỗi trên tab Actions, sửa rồi push lại (CI tự chạy lại). Có thể bật **branch protection** trên `main` để bắt buộc CI pass mới được merge.

**17. Muốn mở rộng CI thì làm gì tiếp?**
Một số hướng:
- Integration test với Postgres/Redis thật bằng `services:` trong job.
- `golangci-lint` để có nhiều linter hơn.
- Đo test coverage.
- Build và push image lên GitHub Container Registry khi merge vào `main` (bước đầu của CD).
- Dependabot để cập nhật dependency và action.
