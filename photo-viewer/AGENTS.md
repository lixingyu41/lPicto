# AGENTS.md

## 项目目标

本项目是 LPicto，一个 Docker 化 NAS 图片/视频相册。目标是只读浏览 `/photos`，在 `/data` 维护 SQLite 数据库和缓存，并提供时间线、图库、文件夹、Viewer 四类核心体验。

## 目录职责

- `backend/cmd/server`：进程入口和依赖装配。
- `backend/internal/api`：HTTP 路由、参数解析、DTO、静态前端服务。
- `backend/internal/config`：环境变量、默认值、启动前路径准备。
- `backend/internal/db`：SQLite 连接、migration、查询、事务边界，SQL 集中在这里。
- `backend/internal/model`：核心数据结构。
- `backend/internal/storage`：路径归一化、PHOTO_ROOT 映射、防路径穿越、缓存路径。
- `backend/internal/media`：媒体类型识别、EXIF/ffprobe/ExifTool 元数据、浏览器可播放判断。
- `backend/internal/scanner`：扫描、删除检测、定时扫描、fsnotify。
- `backend/internal/thumb`：libvips 图片 thumb/preview。
- `backend/internal/video`：FFmpeg poster/proxy。
- `backend/internal/jobs`：内存队列和 worker pool。
- `frontend/src/api`：所有 fetch 调用。
- `frontend/src/components`：通用 UI。
- `frontend/src/pages`：页面级组件。
- `frontend/src/viewer`：图片/视频查看器。

## 编码风格

Go 使用 gofmt，错误必须处理，handler 只做解析和返回，耗时任务不得放在 HTTP handler 中。TypeScript 使用 strict，fetch 不写进页面组件之外，复杂交互拆 hook。

## 禁止过度抽象

不要为每个函数建 interface；只有存在真实替换需求时才定义 interface。不要新增“未来可能用到”的空包、空 service、空 AI API。

## DB migration 规则

migration 文件编号递增；已发布 migration 不随意修改；新结构使用新 migration；数据库启动时自动执行未应用 migration。

## API 兼容规则

DTO 字段使用 camelCase。V1 内可以调整接口；README 标注稳定后，优先新增字段和新增接口，避免破坏已有字段语义。

## 测试要求

提交前至少运行：

```bash
make check
```

后端关键测试范围：路径安全、symlink 逃逸检测、媒体识别、分页上限、cache key、timeline fallback、migration 初始化、文件夹父路径。

## 未来 AI 扩展边界

V1 不创建 AI 表、不实现 AI API、不在 assets 表塞 AI 字段。未来通过新增 `asset_tags`、`asset_faces`、`asset_embeddings` 和独立任务队列扩展。

## 提交前检查命令

```bash
cd backend && gofmt -w . && go test ./... && go vet ./...
cd ../frontend && npm install && npm run build
```
