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

- 使用网关的一次性代理接口，每个健康 IP 最多发送 10 次真实业务请求，成功和失败都计入使用次数。
- 新 IP 先发送 2 次探测请求；两次都是连接、代理鉴权、HTTP 403 或 HTTP 429 时立即废弃，探测正常后再使用剩余 8 次。
- 当前 IP 未满 10 次时会跨设备批次、主单、补单和下一任务继续使用；不会因为一笔任务结束而提前丢弃。
- 代理只在设备已准备好且即将发送请求时提取，不提前预取。
- 主轮完成后按实际缺口补单，最多 3 轮；个位数缺口最多尝试 10 台设备，达到缺口即停止。
- 设备池组模式按任务、业务和目标 ID 分配设备并回报逐设备结果。
- 最终向任务平台回传实际成功数量，不把 HTTP 请求数当作成功数。

`PROXY_REUSE_LIMIT` 默认为 `10`，正常部署不应调大。`GROUP_SIZE` 默认为 `10`，单次实际执行量还会受到当前 IP 剩余次数限制。

## 本地验证

```bash
go test ./...
go vet ./...
```
