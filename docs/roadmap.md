# Roadmap

## V1 已实现

- Docker 多容器部署：nginx、api、worker、postgres、redis。
- Go API 服务 HTTP，Go worker 执行扫描和媒体任务。
- PostgreSQL migration 自动初始化。
- 照片存储根只读扫描、删除标记、定时扫描、fsnotify 触发。
- 图片 thumb/preview、视频 poster/proxy 后台生成。
- Library、Albums、Search、Folders 四入口。
- Viewer 图片缩放/拖拽和视频播放。
- asset id 文件访问、Range 视频流、路径穿越防护。

## 后续可做

- 收藏：新增收藏表和筛选入口。
- 标签：手动标签表与标签筛选。
- AI 标签：新增异步 AI 任务和 `asset_tags`。
- 人脸识别：新增 `asset_faces` 和人脸聚类。
- CLIP 语义搜索：新增 `asset_embeddings` 和向量索引。
- 多图库：扩展 `library` 和 `file_instance` 的归属模型。
- 权限：增加用户、会话和图库授权。
- 更强视频转码队列：持久化任务、失败退避、进度上报。
