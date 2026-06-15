# 内网部署指南

`slow-sql-analyzer` 编译为**单个完全静态的二进制**，部署到内网**目标机零依赖**：
不需要 Go、不需要 Python、不需要 `libpq`、不需要 `psql`，拷一个文件过去就能跑。

> 关键点：实时连库（`analyze` / `/v1/analyze`）用的是 **pgx 驱动，已编译进二进制**。
> 因此目标机连 PostgreSQL 客户端库都不用装，只需网络能连通 PG 服务端口。

## 一、在一台能联网（或已配好 Go）的机器上构建

```bash
git clone https://github.com/Klein4062/slow-sql-analyzer.git
cd slow-sql-analyzer

# 一次性交叉编译所有目标平台（CGO 关闭，纯静态）
make build-all
```

产物在 `dist/` 下，按 `OS-架构` 命名：

| 文件 | 目标环境 |
|---|---|
| `slow-sql-analyzer-linux-amd64` | Linux x86_64（最常见的服务器） |
| `slow-sql-analyzer-linux-arm64` | Linux ARM64 |
| `slow-sql-analyzer-darwin-arm64` | Apple Silicon macOS |
| `slow-sql-analyzer-darwin-amd64` | Intel macOS |
| `slow-sql-analyzer-windows-amd64.exe` | Windows x64 |

每个约 11 MB，`CGO_ENABLED=0` 编译，验证为 `statically linked`（`file` 可查）。

只编当前平台用 `make build`。

### 构建机无法访问公网（air-gapped）？

先在一台能联网的机器上把依赖打进仓库，之后构建机离线即可：

```bash
make vendor          # 生成 vendor/ 目录（依赖源码随仓库走）
# 把整个目录拷到离线构建机，然后：
CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags "-s -w" \
    -o slow-sql-analyzer ./cmd/slow-sql-analyzer
```

`go mod vendor` 一次性把 cobra/pgx/chi 等都拉到 `vendor/`，之后 `go build -mod=vendor` 不再需要网络。

## 二、部署到内网目标机

```bash
# 1. 拷贝（任选其一）
scp dist/slow-sql-analyzer-linux-amd64 user@intranet-host:~/
# 或用 U 盘 / 文件中转

# 2. 落地（目标机上）
mv slow-sql-analyzer-linux-amd64 slow-sql-analyzer
chmod +x slow-sql-analyzer

# 3. 验证
./slow-sql-analyzer version
./slow-sql-analyzer plan -f explain.json        # 离线分析，无需数据库
```

确认目标架构：`uname -m`（`x86_64` → amd64，`aarch64` → arm64）。

## 三、三种用法（都不需要额外安装任何东西）

### 1) 离线分析（无需数据库）

```bash
# 在有 PG 的机器上导出计划，拷到分析机
psql -d mydb -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT ..." -t -A > plan.json

./slow-sql-analyzer plan -f plan.json
./slow-sql-analyzer plan -f plan.json --format json
```

### 2) 实时分析（直连 PG，pgx 已内置，无需 psql/libpq）

```bash
./slow-sql-analyzer analyze \
    --dsn "host=10.0.0.5 port=5432 user=app dbname=prod sslmode=disable" \
    --query "SELECT * FROM orders WHERE status='pending'"
```

> 写语句需 `--allow-writes`（在回滚事务内执行）；不允许执行查询的环境用 `--no-analyze`。

### 3) HTTP API + 可视化网页（`serve`）

```bash
./slow-sql-analyzer serve --addr 0.0.0.0:8080 --dsn "host=10.0.0.5 ..."
```

浏览器打开 `http://<内网IP>:8080/` 即用可视化界面。JSON API：`POST /v1/plan`、`POST /v1/analyze`、`GET /healthz`。

## 四、把 `serve` 作为系统服务（systemd 示例）

`/etc/systemd/system/slow-sql-analyzer.service`：

```ini
[Unit]
Description=Slow SQL Analyzer
After=network.target

[Service]
Type=simple
ExecStart=/opt/slow-sql-analyzer/slow-sql-analyzer serve \
    --addr 0.0.0.0:8080 \
    --dsn "host=10.0.0.5 port=5432 user=app dbname=prod sslmode=disable"
Restart=on-failure
User=nobody
# 可选：限制资源
MemoryMax=256M

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now slow-sql-analyzer
curl -s http://localhost:8080/healthz   # {"status":"ok"}
```

## 五、安全提示

- `serve` 默认监听 `0.0.0.0`，内网部署务必确认端口仅在内网可达，或加反代+鉴权。
- 实时 `analyze` / `/v1/analyze` 默认走**只读事务** + `statement_timeout`；写语句默认拒绝。
- 避免把高权限 DB 账号配给 `serve`，建议为分析用途建只读账号。

## 附：为什么 Go 静态二进制适合内网

- **部署零依赖**：一个文件，目标机什么都不用装。
- **跨平台一次编译**：Mac 上 `make build-all` 直接产出 Linux/Windows 二进制。
- **驱动内置**：pgx 编译进二进制，连库不依赖目标机的 PostgreSQL 客户端库。
- 相比 Python：无需保证目标机的 Python 版本与 pip 包，也无需 shell 调用 psql。
