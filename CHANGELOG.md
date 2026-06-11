# Changelog

本项目遵循 [Keep a Changelog 1.1.0](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [SemVer 2.0.0](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Added

- 新增客诉接口支持（《软件管家客诉接口》V1.0）：多笔订单退款 `RefundOrders`（v2 接口，与 v1 `Refund` 并存）、投诉回复 `ComplainReply`、投诉完结 `ComplainFinish`。
- 新增投诉 webhook 验签与解析：`VerifyComplaintWebhook`/`ParseComplaintWebhook`，按文档专用规则以请求原文 `data` 与 `timestamp` 验签；新增 `WithVendorKey` 配置厂商密钥（未配置回落 appsecret）。
- 新增错误码哨兵 `ErrRateLimited`(10018)、`ErrComplainNotFound`(10034)、`ErrComplainReplyLimit`(10035) 与验签哨兵 `ErrComplaintWebhookSign`。
- 新增投诉枚举常量：投诉状态 `ComplainStatus*`、来源平台 `ComplainPlatform*`、回复来源 `ComplainSource*`、事件类型 `ComplaintEvent*`。

## [0.2.0] - 2026-06-11

### Added

- 新增 `TokenRefreshLock` 与 `WithTokenRefreshLock`，支持多副本部署时对 `access_token` 刷新做跨副本单飞。
- `CreateOrderParams` 支持纯任务单：`OrderPayType` 为任务单时无须开启代扣即可构造与序列化（输出 `order_pay_type` 与 `task_id`），且任务单允许订单金额为 0（依据文档任务示例）。
- 回调 `Callback` 新增文档参数表中的顶层字段 `AgreementNumber`（签约号）、`AutoPayStatus`（签约状态）。
- 回调 `Callback` 新增 `IsPaid()`，按「20、30、50 视为支付成功」判定，与 `OrderQuery.IsPaid()` 语义一致。
- 新增发票文档取值常量：`InvoiceRedCategorySeller`（红冲类别 1 销方红冲）、`InvoiceRedReasonMistake`（红冲原因 `INVOICE_MISTAKE`）、`InvoiceCustomTypeEnterprise`（商户类型 1 企业）。
- 新增错误哨兵 `ErrCallbackMismatch`，表示回调凭据与客户端不一致。

### Changed

- `VerifyCallback` 验签通过后会校验回调中的 `app_id`/`qid` 是否与客户端凭据一致，不一致返回 `ErrCallbackMismatch`，避免误处理其它应用的回调（校验收紧：此前此类回调可通过验签）。
- token 刷新流程会在刷新锁内二次读取共享缓存，避免等待锁期间其它副本已刷新后仍重复换 token。

## [0.1.1] - 2026-06-08

### Added

- 新增真实成功链路 `livetest` 框架，可通过环境变量验证已付订单查询、退款、普票、专票开具/查询/红冲。

### Changed

- `New` 现在会拒绝小于等于 0 的 `qid`，避免构造出无效客户端。
- 业务接口收到 `errno=10012`（`ErrAccessToken`）时会强制刷新 `access_token` 并自动重试一次。
- `QuerySpecialInvoice` 现在会拒绝非 `1`/`2` 的 `requestType`，避免无效专票查询请求发送到 360 平台。
- `live_test.go` 对退款和发票类真实联调用例增加显式开关，避免误触发副作用。

### Fixed

- `Refund` 和 `DoPost` 现在会在本地拒绝非正金额，避免无效请求发送到 360 平台。

## [0.1.0] - 2026-06-05

### Added

- 新增 `Client` 与 `New`，封装 360 联运开放平台服务端接口。
- 出站接口：订单退款 `Refund`、订单查询 `QueryOrder`、厂商侧代扣 `DoPost`、取消签约 `CancelSign`、普票开具 `PlainInvoice`、普票红冲 `PlainInvoiceCancel`、专票开具 `SpecialInvoice`、专票查询 `QuerySpecialInvoice`、专票红冲 `SpecialInvoiceCancel`。
- 入站回调：`VerifyCallback` 验签并解析、`ParseCallback` 解析、`AckSuccess`/`AckResponse` 构造标准响应。
- `access_token` 两层缓存：默认进程内无锁内存缓存 + 单飞刷新；`TokenCache` 接口支持多实例共享存储，`WithTokenCache` 注入。
- 类型化错误 `APIError` 及文档错误码哨兵，支持 `errors.Is`。
- 每次调用可获取响应头 `Header-Tid`。
- 配置项：`WithBaseURL`、`WithHTTPClient`、`WithTokenCache`、`WithTokenRefreshAhead`、`WithClock`。
- 前端订单参数 `CreateOrderParams`：前端 `createOrder` 数据的类型化构造、「代扣必填」条件校验与序列化（`MarshalForSDK`，含 `ext` 组装），不发起请求。
- 协议枚举常量：`OrderStatus*`、`PayChannel*`、`PeriodType*`、`OrderPayType*`、`AutoPayEnabled/Disabled`、`AutopayMode*`。

### Fixed

- 修复上游返回非 2xx 状态或空响应体时可能被误判为成功的问题：解码前先校验 HTTP 状态码与响应体非空。
- 数字字段（`qid`、`order_amount`、`autopay_amount` 等）以 JSON number 发送：360 要求数字类型，此前以字符串发送会被拒为“类型错误”。签名仍按字符串拼接。
- 业务错误时 `data` 返回空字符串不再掩盖错误码：订单查询、发票等接口的 `data` 延迟解析，确保如“订单不存在”能正确返回 `ErrOrderNotFound` 而非解码错误。
- 回调验签改用 `UseNumber` 保留数字原始字面量，修复大整数经 float64 中转丢失精度导致合法回调被误拒的问题；遇到非标量字段时报错而非臆测其字符串形态。
