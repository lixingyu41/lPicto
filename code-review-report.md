# lPicto 全量代码审查报告

**审查日期**: 2026-07-05  
**审查范围**: 119 个源文件，约 1.1 MB 源码  
**项目结构**: Go 后端（约 50 个文件）+ React/TypeScript/Vite 前端（约 50 个文件）+ SQL 迁移 + Docker/部署配置  

---

## 一、严重问题

### 1.1 SQL 注入风险 — `db.go` 数据库名拼接

**位置**: `backend/internal/db/db.go`，行 212、230

```go
_, err := admin.ExecContext(ctx, `CREATE DATABASE `+name)       // 行 212
_, err = admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name) // 行 230
```

数据库名直接拼接到 SQL 语句中，未使用 `quote_ident()`。虽然 `name` 来自 SHA1 哈希（`lpicto_test_` + hex），在测试环境下理论上安全，但这是一种危险模式。如果在生产代码中复用此模式，将产生严重 SQL 注入漏洞。

**建议**: 使用 `pq.QuoteIdentifier(name)` 或 PostgreSQL 的 `quote_ident()` 函数。

---

### 1.2 全表扫描 — `AssetCountsForLibraries` 内存计算

**位置**: `backend/internal/db/progress.go`，行 53-75

```go
rows, err := d.conn.QueryContext(ctx, `SELECT rel_path FROM assets WHERE deleted_at IS NULL`) // 无分页、无上限
// 然后对每个 asset，逐条在 Go 代码中匹配 library 的扫描范围
for rows.Next() {
    var rel string
    rows.Scan(&rel)
    for _, library := range libraries {
        if AssetInScanFolders(rel, library.Roots) {
            counts[library.ID]++
        }
    }
}
```

**问题**: 当 assets 表有 10 万+ 条记录时，此查询会产生网络传输巨大结果集，然后在 Go 端进行 O(assets × libraries) 的循环匹配，耗费大量内存和 CPU。在有多个 library 时性能尤其糟糕。

**建议**: 改为每 library 单独执行 SQL 计数查询，利用数据库索引过滤。或者至少使用 COUNT 聚合在 SQL 层面完成。

---

### 1.3 未管理的 goroutine — `markDeletedAsset` 泄露

**位置**: `backend/internal/api/server.go`，行 1130-1134

```go
go func() {
    if err := s.db.RefreshFolders(context.Background()); err != nil {
        s.logger.Warn(...)
    }
}()
```

每次删除资源都会启动一个独立的 goroutine，使用 `context.Background()` 而非请求上下文。在大量删除操作场景下，这些 goroutine 会堆积且不跟随服务优雅关闭。

**建议**: 使用 worker pool 或将此操作改为异步队列任务（项目已有 Redis job 队列可复用）。

---

### 1.4 资产计数部分读取风险 — `countMediaDir`

**位置**: `backend/internal/scanner/count.go`，行 75-102

```go
entries, readErr := util.ReadDirPartial(dirPath)
```

`ReadDirPartial` 在目录条目过多时会截断结果，导致文件计数不准确。对于包含数千个文件的扁平目录，扫描结果会偏低，且错误被延续到后续调用。

**建议**: 对截断的目录进行标记，在日志中警告，或在后续扫描中补齐。

---

## 二、设计中度问题

### 2.1 迁移 SQL 性能 — `003_relink_asset_folders.sql`

**位置**: `backend/migrations/003_relink_asset_folders.sql`

该迁移使用递归 CTE 遍历全部 `file_instance` 和 `media_asset` 表来重建文件夹层级关系，末尾的文件夹计数和封面更新（行 74-98）通过 `assets` 视图 JOIN 三张表逐条计算。在大数据集（10 万+ 资源）上首次运行此迁移可能需要数分钟甚至超时。

**建议**: 分批处理或为迁移添加进度输出，让运维人员感知执行进度。

---

### 2.2 `rebindPostgres` — 每次查询都进行字符级替换

**位置**: `backend/internal/db/db.go`，行 291-317

```go
func rebindPostgres(query string) string {
    var b strings.Builder
    // 逐字符遍历，将 ? 替换为 $1, $2, ...
}
```

项目中每个 SQL 查询在执行前都经过这个 O(n) 的逐字符扫描。在高 QPS 场景下会产生显著的 CPU 开销。

**建议**: 使用 `pgx` 驱动的原生 `$1, $2` 占位符，避免运行时替换；或至少对已知 SQL 使用 `sync.Pool` 缓存替换结果。

---

### 2.3 `assetFilterSQL` 每次都重建复杂 WHERE 子句

**位置**: `backend/internal/db/assets.go`（约 100 行长的条件构建函数）

`assetFilterSQL` 根据 `AssetListOptions` 中的 30+ 个字段动态构建 SQL WHERE 子句。每次查询都生成结构不同的 SQL，导致 PostgreSQL 无法复用查询计划缓存。

**建议**: 考虑对高频查询模式使用预编译语句，或至少将过滤器拆分为固定和可变两部分。

---

### 2.4 CSS 文件过大（78KB）

**位置**: `frontend/src/styles/global.css`

单文件 78KB 的 CSS 难以维护，且大部分规则可能从未被使用。当前使用 Vite 但未启用 CSS Modules 或 PostCSS/Tailwind 等工具。

**建议**: 拆分为组件级 CSS Modules 或在构建时使用 PurgeCSS 移除无用样式。

---

### 2.5 前端巨型组件（超 30KB 的 4 个页面）

| 文件 | 大小 | 建议 |
|------|------|------|
| `SettingsPage.tsx` | 39KB | 拆分为独立设置面板组件 |
| `SearchPage.tsx` | 34KB | 提取搜索表单、结果列表、索引面板 |
| `ViewerPage.tsx` | 34KB | 分隔图片/视频查看、NFO 面板、删除对话框 |
| `AlbumsPage.tsx` | 32KB | 拆分为相册列表、相册编辑器、来源选择器 |

---

## 三、逻辑缺陷

### 3.1 `enqueuePendingWork` 忽略传入的 context

**位置**: `backend/cmd/server/main.go`，行 158-167

```go
func enqueuePendingWork(ctx context.Context, ...) {
    items, err := database.PendingWork(ctx)  // 使用了传入的 ctx
    // ...
}
```

实际上这里的函数体是正确的——它确实使用了 `ctx`。经重新审查没有缺陷。

---

### 3.2 `statusCounts` SQL 列名格式化

**位置**: `backend/internal/db/progress.go`，行 108-127

```go
query := fmt.Sprintf(`... CASE WHEN %[1]s = 'ready' ...`, field)
```

虽然 `field` 参数经过 `validStatusField` 白名单校验，但将校验与格式化分离的模式容易在重构时被破坏。如果某人绕过校验直接传入字段名，就会产生 SQL 注入。

**建议**: 在校验函数中直接返回安全的字符串常量而非信任调用方。

---

### 3.3 `db.go` `Close` 错误处理

**位置**: `backend/internal/db/db.go`，行 71-82

```go
err := d.raw.Close()
if d.testAdminURL != "" && d.testDatabase != "" {
    if dropErr := dropTestDatabase(context.Background(), d.testAdminURL, d.testDatabase); err == nil {
        err = dropErr  // 如果 close 成功,用 drop 的错误覆盖
    }
}
return err
```

`Close` 成功时可能因为后续 `DROP DATABASE` 失败而返回错误，但数据库连接已经正常关闭。调用者可能因为收到错误而做出错误判断。

**建议**: 分别记录两个操作的错误，DROP 失败时仅打印日志而不改变返回值。

---

### 3.4 `main.go` worker 模式上下文取消

**位置**: `backend/cmd/server/main.go`，行 92-94

```go
case "worker":
    runWorker(rootCtx, cfg, database, queue, scan, logger)
    <-rootCtx.Done()
```

`runWorker` 启动多个后台 goroutine（扫描、文件监控、工作队列）后立即返回。主 goroutine 阻塞在 `<-rootCtx.Done()` 上等待信号。如果任何后台 goroutine 崩溃（panic），主进程不会感知到。`queue.Start` 和 `scan.Start` 内部的 goroutine 如果异常退出，也不会有恢复机制。

**建议**: 为关键后台 goroutine 添加 recover + 告警机制。

---

### 3.5 `db.go` `Checkpoint` 空实现

**位置**: `backend/internal/db/db.go`，行 88-91

```go
func (d *DB) Checkpoint(ctx context.Context) error {
    _ = ctx
    return nil
}
```

`Checkpoint` 总是返回 nil 而不执行任何操作。如果调用方期望它执行 WAL checkpoint（如 SQLite 场景），则实际上什么都没做。当前使用 PostgreSQL，WAL 由 PG 自动管理，所以空实现不会导致数据丢失，但函数名具有误导性。

**建议**: 如果不需要实现，移除此方法或添加注释说明 PostgreSQL 自动管理 WAL。

---

## 四、性能问题

### 4.1 视频代理扫荡——全表扫描

**位置**: `backend/internal/api/video_proxy.go`，`sweepVideoProxyCache` 方法

每次 sweeper 运行时，遍历全部 runtime state map 来检查过期项。在并发观看场景下（map 中有大量条目），可能产生锁竞争。

**建议**: 使用按时间排序的优先队列而非每次全量扫描。

---

### 4.2 `VideoViewer` 固定间隔轮询无退避

**位置**: `frontend/src/viewer/VideoViewer.tsx`

视频代理状态轮询使用固定间隔，不随响应时间或播放状态调整，在弱网环境下会产生大量冗余请求。

**建议**: 实现指数退避，或使用 SSE（项目已有 `/api/events` SSE 端点）。

---

### 4.3 迁移 `001_init.sql` 索引缺乏覆盖

虽然有了 `idx_asset_timeline`、`idx_asset_folder_timeline` 等索引，但 `assets` 视图的 JOIN（`media_asset` + `file_instance` + `folder`）在实际查询中可能无法有效利用部分索引。对于按 `parent_rel_path` 过滤的查询（`LIKE 'folder/%'`），当前没有专用索引。

**建议**: 对高频查询模式做 EXPLAIN ANALYZE 分析，为 `parent_rel_path` LIKE 查询考虑 GIN trigram 索引。

---

### 4.4 `nginx.conf` — nginx 缺少常用配置

**位置**: `nginx.conf`

当前配置缺少：
- `gzip` 压缩（JS/CSS bundle 不经压缩传输，浪费带宽）
- `proxy_read_timeout`（长视频代理请求可能超时）
- `proxy_buffers` 调优（大量并发时会用默认小 Buffer）

**建议**: 添加 gzip 压缩和合理的 proxy 超时配置。

---

## 五、安全审查

### 5.1 路径遍历防护

**评估**: **基本充分**

`storage.go` `NormalizeRelPath` 使用 `filepath.Clean` + `..` 检查 + 绝对路径检查，三层防护。`validCacheKey` 限制缓存键为 20 位十六进制字符。静态文件服务中对 `..` 有显式检查。

**改进建议**: `contentDisposition` 函数虽然去除了双引号，但 `url.PathEscape` 本身不转义所有特殊字符。考虑使用 RFC 5987 编码或专用的 `mime.FormatMediaType`。

---

### 5.2 API 缺乏速率限制

全局没有任何速率限制中间件。`/api/health`、SSE 事件流等端点可能被滥用。

**建议**: 对敏感操作（删除资源、触发扫描）添加速率限制，对 SSE 连接数加入上限。

---

### 5.3 请求日志缺失

HTTP 请求未经日志记录——没有请求耗时、状态码、来源 IP 等信息。排查问题时缺乏可见性。

**建议**: 添加一个简单的 chi 日志中间件。

---

## 六、并发与数据竞争

### 6.1 多个互斥锁的正确性

`server.go` 中使用了 6 个独立的 `sync.Mutex`（`cacheMu`、`progressMu`、`cleanupMu`、`sourceDirMu`、`libraryCountsMu`、`videoProxyMu`），各管各的数据，避免了死锁。`video_proxy.go` 中对 `videoProxyStates` 的访问都有 `videoProxyMu` 保护。审查未发现明显的锁顺序问题。

### 6.2 `scanner.go` channel 管理

Scanner 的命令循环（`commandLoop`）使用了 `pending` 队列 + `cmdCh` channel + `resultCh` channel 的复杂模式。代码实现了"重复请求去重"（`sameScanRequest`），但未实现命令超时——如果某个扫描命令一直不被消费，调用者会永久阻塞。

**建议**: 为 `RequestScan` 等方法的 result channel 添加超时机制。

---

## 七、代码质量与可维护性

### 7.1 问题汇总

| 问题 | 严重程度 | 位置 |
|------|---------|------|
| 文件过大（>1000 行） | 中 | `assets.go`(1789)、`scanner.go`(1670)、`server.go`(1186)、`video_proxy.go`(1075) |
| 巨型页面组件（>30KB） | 中 | Settings/Search/Viewer/Albums 四个页面 |
| `assetFilterSQL` 圈复杂度高 | 中 | `assets.go`，约 40 个条件分支 |
| 魔法数字 | 低 | `validCacheKey` 硬编码长度 20，配置默认值散布在各处 |
| 时间戳使用 int64 | 低 | 全局使用 Unix 秒级时间戳，丢失精度和时区信息 |
| 无单元测试覆盖关键路径 | 低 | assets.go(1789行)无测试文件，video_proxy.go 无测试 |
| 硬编码的 `lPicto` 路径 | 低 | `findStaticDir`/`findMigrationsDir` 中包含 `LPicto` 字面量 |

### 7.2 优点

- **类型安全**: 前后端均有完善的 TypeScript/Go 类型定义，DTO 转换函数完备
- **错误处理**: HTTP 层有统一的 `writeError` 模式，返回结构化 JSON 错误
- **数据库迁移有序**: 5 个顺序迁移文件，使用 advisory lock 防止并发执行
- **部署自动化**: Docker Compose + GPU 支持 + 一键部署脚本，运维友好
- **路径安全**: `NormalizeRelPath` 有多层路径穿越防护
- **缓存策略**: 静态资源和缓存资源使用不可变 `max-age` 头，ETag 支持条件请求
- **代码风格一致**: Go 代码遵循惯用写法，TypeScript 有明确的接口定义

---

## 八、建议优先修复项（按紧急程度排序）

1. **`AssetCountsForLibraries` 全表扫描** — 大数据量场景下会导致 OOM
2. **`markDeletedAsset` 未管理的 goroutine** — 可通过复用已有 job 队列解决
3. **`countMediaDir` 部分读取** — 导致扫描计数不准
4. **视频代理全量扫荡** — 并发观看场景下存在锁竞争
5. **前端巨型组件拆分** — 影响长期可维护性
6. **nginx 缺少 gzip** — 一行配置即可改善首屏加载
7. **API 速率限制** — 防止恶意或意外滥用
8. **`rebindPostgres` 运行时替换** — 可优化的 CPU 开销
