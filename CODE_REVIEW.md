# LPicto 代码审查 —— 改进意见

> 只列问题与建议。

---

## 架构设计

### `events.Bus` 无背压 — 慢消费者丢事件

`backend/internal/events/events.go` Publish 中：
```go
select {
case ch <- event:
default:  // 丢弃
}
```
SSE 客户端跟不上时静默丢事件。改为环形缓冲区，或丢事件时打日志 + 暴露丢弃计数。

### `jobs.Manager.Start` 未启动 VideoPoster worker

`backend/internal/jobs/jobs.go`:
```go
for i := 0; i < cfg.Image; i++       { go m.worker(..., m.imageQueue, ...) }
for i := 0; i < cfg.VideoProxy; i++  { go m.worker(..., m.videoProxyQueue, ...) }
// VideoPoster worker 未启动 — videoPosterQueue 定义但无消费者
```
补充 VideoPoster worker 启动，或移除 videoPosterQueue。

### `usePagedLoader` deps 触发多余 reset

`frontend/src/hooks/usePagedLoader.ts`: `useEffect(() => reset(), [reset, ...deps])` — query 从空到空也会触发 reset + UI 闪烁。用 ref 存最新 loadPage，避免链条依赖。

---

## 安全性

### `cacheThumb` 绕过 asset 存在性校验

`backend/internal/api/server.go`: 直接 `/api/cache/thumbs/{cacheKey}.webp` 读文件，不查 asset 是否存在。虽 cacheKey 为 20 位 hex 碰撞概率极低，仍建议加 rate limit。

### `findStaticDir` 包含 `..` 路径穿越

```go
filepath.Join("..", "frontend", "dist"),
```
生产容器中意外走此路径可能访问宿主机。改为环境变量 `STATIC_DIR` 显式指定，移除隐式 `..`。

### SSE `/api/events` 无认证/限流

长连接 + 无任何鉴权。内网问题不大，公网部署需加 token 或 rate limit。

---

## 性能

### `anchorsForFilter` 全量加载锚点行

`backend/internal/db/assets.go`: 对数十万资源做 `SELECT ... FROM assets WHERE ... ORDER BY ...` 全量加载到内存算锚点。应：
- uniform anchors 用 SQL `MIN/MAX` + `NTILE` 分桶
- date anchors 用 `GROUP BY strftime`
- 加硬上限（如采样 50000 行）

### `RefreshFolders` 相关子查询 O(n)

`backend/internal/db/folders.go`: 每个 folder 三次相关子查询（asset_count、recursive、cover）。10000 个文件夹 = 30000 次子查询。改用 CTE + 一次 JOIN 批量计算。

### `computeCacheStats` 全量 WalkDir

`backend/internal/api/settings.go`: 每次刷新遍历 `/data/cache` 所有文件。大缓存时秒级耗时。TTL 从 5s 提到 60s，或维护运行中增量计数器。

### 前端 SSE 不可用时 2s 轮询

`frontend/src/pages/LibraryPage.tsx`: `setInterval(..., 2000)` 拉全量第一页。改为 5-10s，或按 `?since=` 增量拉取。

---

## 代码质量

### SQL 字符串拼接分散

`db/assets.go` 中 `"WHERE " + where + " ORDER BY " + order` 的拼接逻辑分散在 `listAssets`、`anchorsForFilter`、`Neighbors` 等处。集中到一个 `buildAssetQuery()` 函数。

### `publicError` 重复定义

`backend/internal/thumb/thumb.go` 和 `video/video.go` 各有相同实现。提取到 `util` 包。

### `fileExists` 重复定义

同上，两个包各有一份。提取到 `util`。

### `main.go` struct 字面量膨胀

`thumb.Processor{...}` 和 `video.Processor{...}` 字段超 10 个后应用 Functional Options 或 `NewProcessor(cfg)` 构造。

### `db/assets.go` 中 `nfoColumnsEqual` 和 `assetFilterSQL` 未在可见文件中

确认这两个函数在独立文件中有定义且有测试覆盖。

---

## 错误处理

### `scanner.handleRemovedPath` 用 `context.Background()`

`backend/internal/scanner/watcher.go`: 服务关闭后删除操作继续执行，可能 panic。传入 watcher 的 ctx。

### `cacheThumb` 中 Stat → Open 存在 TOCTOU

先 `os.Stat` 后 `os.Open`，中间文件可能被删。直接 `Open` 再从 `*os.File` 取 `Stat()`。

### `thumb.processAsset` 中 asset 存在性检查与 rename 存在 TOCTOU

check 完 `GetAsset` 到 `os.Rename` 之间 asset 可能被删，留下孤立缓存。添加定期清理孤儿缓存机制。

---

## 数据库

### `app_state` 存储 JSON 无版本

`backend/internal/db/settings.go`: `ScanLibrary` 结构体变化后旧 JSON 解析失败。给 value 加版本字段或 migration 中升级数据。

### `timeline.go` 的 `sqlNullInt64` 自定义类型可简化

标准库 `sql.NullInt64` 可替代，减少自定义代码。

### `assets` 表 `rotation` 字段

`model.Asset.Rotation` 来自 `asset_preferences` 表而非 `assets` 表，加注释说明。

---

## 前端

### `LibraryPage` > 500 行

拆分出 `useLibraryFilters`、`useLibraryAnchors`、`useScrollRestore` hooks。

### `AssetGrid` > 500 行

拆分出 `usePressPreview`、`useGridLayout`、`useGridScroll` hooks。

### 无 Error Boundary

任何组件渲染异常 → 白屏。在 `Layout` 和 `ViewerPage` 包 `ErrorBoundary`。

### `browserPlayable` 图片直接加载原图

50MB PNG 会卡很久。始终 thumb → preview → original 渐进加载。

### 动画 GIF 通过切换 src 控制播放

每次切换重新加载。用 Canvas API 控制帧播放。

### `ImageViewer` 按下即触发 zoom

应按住一定时间（如 200ms）再触发，与 click 区分更明确。

---

## 测试

### 无集成测试

缺 HTTP handler → db → 响应的端到端测试。加 `httptest.NewServer` 测试。

### 前端零测试

加 vitest + `@testing-library/react` 覆盖关键 hook 和 `api/client.ts`。

### 部分测试无超时

`context.Background()` 可能卡死。统一加 `context.WithTimeout(..., 30s)`。

---

## 逐文件速查

| 文件 | 问题 |
|------|------|
| `cmd/server/main.go` | 缺 version flag、缺 pprof 开关 |
| `config/config.go` | `ThumbLongEdge` 等无范围校验；`hwAccelEnv` 白名单不全 |
| `api/server.go` | `eventStream` 中 `fmt.Fprintf` 错误被忽略；`findStaticDir` 每次都调用 |
| `db/assets.go` | `UpsertAssetDetailed` ~100 行，拆分 insert/update；硬删无清理策略 |
| `scanner/scanner.go` | `run` ~130 行，拆分 phase；`countDir` 无深度限制 |
| `scanner/watcher.go` | `watchLogger` 函数定义但未被调用 |
| `jobs/jobs.go` | channel 容量巨大（131072），实际 2 worker 用不满 |
| `jobs/resource.go` | `readLoadAvg1` 仅 Linux，其他 OS 无资源限制但无文档说明 |
| `storage/storage.go` | `samePath` 用 `EqualFold`，Linux 上应区分大小写 |
| `storage/source_manifest.go` | `LoadSourceFolderManifest` 兼容两种 JSON 格式，历史遗留 |
| `thumb/thumb.go` | 视频缩略图硬编码 `-ss 1` 可能抓到黑帧 |
| `video/video.go` | `proxyTimeout` 固定 2h，4K 视频可能不够；`ffmpegOutputContains` 无缓存 |
| `media/metadata.go` | `intPtrValue` switch 只覆盖 `float64`，其他数字类型被忽略 |
| `util/command.go` | `RunLowPriorityCommand` Windows 上不降优先级，文档应注明 |
| `util/fs.go` | `ReadDirPartial` 批大小 128 硬编码 |
| `frontend/api/client.ts` | 无请求超时、无重试 |
| `frontend/hooks/usePagedLoader.ts` | `mutateItems` 后不更新 `hasMore` |
| `frontend/components/AssetGrid.tsx` | 9 个 ref 用于回调引用，职责过重 |

---

## 优先级

### 🔴 立即修

1. `videoPosterQueue` 无消费者 (`jobs.go`)
2. `fetch` 无超时 (`client.ts`)
3. `samePath` Linux 上 `EqualFold` (`storage.go`)

### 🟡 下次迭代

4. `anchorsForFilter` 全量加载 (`assets.go`)
5. `RefreshFolders` O(n) 子查询 (`folders.go`)
6. `LibraryPage` / `AssetGrid` 拆分
7. `ffmpegOutputContains` 无缓存 (`video.go`)
8. `computeCacheStats` 全量遍历 (`settings.go`)
9. `publicError` / `fileExists` 重复代码

### 🟢 后续

10. SSE 无背压
11. 前端缺 Error Boundary
12. 缺集成测试
13. 大函数拆分
14. 前端零测试
