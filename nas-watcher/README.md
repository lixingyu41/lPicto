# lPicto NAS 实时监听器

该容器运行在媒体文件实际落盘的 NAS 上，通过 Linux 文件事件通知 lPicto 精确扫描新增、修改、移动或删除的路径。媒体目录仅以只读方式挂载；启动时递归读取一次目录结构以建立监听，之后不轮询、不读取媒体内容。

复制 `.env.example` 为 `.env`，确保 `LPICTO_WATCHER_TOKEN` 与 lPicto 的 `NAS_WATCHER_TOKEN` 相同，然后运行：

```sh
docker compose up -d --build
```

监听器每 30 秒发送一次心跳。监听器停止或网络中断不会影响 lPicto，手动扫描和计划扫描仍可使用。
