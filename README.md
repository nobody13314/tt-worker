# TT Worker

今日头条点赞任务 Worker。任务由任务平台分发，设备从 MSSDK 设备池或设备池组获取，请求通过代理网关发送，并将实际成功数量回传任务平台。

核心请求、链接跳转和 ID 提取逻辑迁移自原 `tt-dz` 程序。默认任务类型 ID 为 `38`。

## 部署

复制 `.env.example` 并填写以下必需配置：

- `TASK_PLATFORM_URL`：任务平台地址。
- `DEVICE_API_URL`、`DEVICE_API_KEY`：MSSDK API 地址与 API Key。
- `DEVICE_POOL_GROUP_ID`：推荐使用的设备池组；也可改用旧的 `DEVICE_POOL_ID`。
- `SIGNER_URL`：签名服务的 `/api/sign` 地址。
- `PROXY_GATEWAY_URL`、`GATEWAY_API_KEY`：代理网关地址与 API Key。

启动：

```bash
docker compose pull
docker compose up -d
docker compose logs -f tt-worker
```

## 处理模型

- 每组默认处理 30 台设备，并发数默认 30。
- 一组失败率达到 80% 时切换代理，初始代理加两次切换，总共最多尝试 3 个代理。
- 主轮完成后按实际缺口补单，最多 3 轮；个位数缺口最多尝试 10 台设备，达到缺口即停止。
- 设备池组模式按任务、业务和目标 ID 分配设备并回报逐设备结果。
- 最终向任务平台回传实际成功数量，不把 HTTP 请求数当作成功数。

## 本地验证

```bash
go test ./...
go vet ./...
```
