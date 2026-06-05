# Changelog

本项目遵循 [Keep a Changelog 1.1.0](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [SemVer 2.0.0](https://semver.org/lang/zh-CN/)。

## [Unreleased]

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
