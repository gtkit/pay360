# pay360

`github.com/gtkit/pay360` 封装 360 联运（软件管家）开放平台 OPENAPI 的**服务端接口**，面向需要对接 360 联运支付、退款、订单、代扣、签约、发票的 Go 服务端程序。

它统一处理：签名生成、`access_token` 生命周期（3 小时有效，且“新申请使旧失效”）、`Header-Tid` 排障头、错误码语义，让调用方专注业务。

## 特性

- 覆盖文档第四章全部服务端接口（出站 + 入站回调验签）。
- `access_token` 两层缓存：默认进程内**无锁内存缓存 + 单飞刷新**；可注入自定义 `TokenCache`（如 Redis）支持多实例共享。
- 每次调用均可获取 `Header-Tid`（成功与错误路径）。
- 类型化错误码，支持 `errors.Is` 判定。
- 并发安全：`Client` 构造后只读，可跨 goroutine 共享。
- JSON 使用 `github.com/gtkit/json/v2`，HTTP 使用 `github.com/gtkit/httpc`。

## 安装

```bash
go get github.com/gtkit/pay360
```

要求 Go 1.26+。

## 初始化

```go
c, err := pay360.New("your-appid", 123456, "your-appsecret")
if err != nil {
    log.Fatal(err)
}
```

`qid` 必须为正数。凭据 `appid`、`qid`、`appsecret` 请勿硬编码，从配置或密钥管理服务读取。

### 可选项

| Option | 说明 |
|--------|------|
| `WithBaseURL(u)` | 覆盖接口域名（默认 `https://api.openstore.360.cn`），主要用于测试 |
| `WithHTTPClient(h)` | 注入自定义 `*httpc.Client`（超时、传输等） |
| `WithTokenCache(tc)` | 注入自定义 `access_token` 缓存（多实例共享） |
| `WithTokenRefreshAhead(d)` | token 提前刷新安全边界（默认 5 分钟） |
| `WithClock(now)` | 注入时间源，主要用于测试 |

> httpc 自身不提供日志。如需请求日志，构造 `*httpc.Client` 时用 `httpc.WithTransport` 注入带日志的 `http.RoundTripper`，再经 `WithHTTPClient` 注入本客户端。

## 前端订单参数（createOrder）

前端 `SDK360.createOrder` 的下单由 JSSDK 直连完成，但订单数据应由服务端生成唯一 `order_id`、组装并留存后下发给前端。`CreateOrderParams` 提供类型化构造、条件校验与序列化（不发请求）：

```go
p := pay360.CreateOrderParams{
    OrderID:     genOrderID(),                                  // 业务生成，需唯一
    OrderAmount: 1,                                             // 单位：分
    CreateTime:  strconv.FormatInt(time.Now().Unix(), 10),     // 10 位时间戳
    UserID:      "user-1",
    ProductID:   "vip-1",
    ProductName: "会员月卡",
}
data, err := p.MarshalForSDK() // 下发给前端，前端传入 SDK360.createOrder
```

开启代扣时设 `AutoPayStatus = pay360.AutoPayEnabled` 并填写一组代扣字段，`Validate`/`MarshalForSDK` 会强制「代扣必填」校验并自动组装 `ext`（`autopay_mode`）。包内提供 `OrderStatus*`、`PayChannel*`、`PeriodType*`、`OrderPayType*`、`AutoPayEnabled/Disabled`、`AutopayMode*` 等枚举常量，供服务端解析回调/查询及与前端约定时共用。

## 出站接口

| 方法 | 说明 |
|------|------|
| `Refund(ctx, RefundRequest)` | 订单退款申请 |
| `QueryOrder(ctx, OrderQueryRequest)` | 订单查询（返回 `OrderQuery`，`IsPaid()` 判定支付成功） |
| `DoPost(ctx, DoPostRequest)` | 厂商侧发起代扣 |
| `CancelSign(ctx, CancelSignRequest)` | 厂商侧取消签约 |
| `PlainInvoice(ctx, PlainInvoiceRequest)` | 普票开具 |
| `PlainInvoiceCancel(ctx, orderID)` | 普票红冲 |
| `SpecialInvoice(ctx, SpecialInvoiceRequest)` | 专票开具（返回 `source_id`） |
| `QuerySpecialInvoice(ctx, requestType, sourceID)` | 专票查询（开票/红冲进度） |
| `SpecialInvoiceCancel(ctx, SpecialInvoiceCancelRequest)` | 专票红冲 |

多数方法返回 `(headerTid string, err error)`；返回数据的方法（查询、开具）返回带 `HeaderTid` 字段的结果结构。

```go
tid, err := c.Refund(ctx, pay360.RefundRequest{
    OrderID:      "order-1",
    OrderAmount:  100, // 单位：分
    UserID:       "user-1",
    RefundReason: "用户申请退款",
})

o, err := c.QueryOrder(ctx, pay360.OrderQueryRequest{OrderID: "order-1", UserID: "user-1"})
if err == nil && o.IsPaid() {
    // 发放权益
}
```

## 入站回调（厂商订单推送）

在你的 HTTP handler 中验签、解析并响应：

```go
func handle360Callback(c *pay360.Client) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)

        cb, err := c.VerifyCallback(body) // 验签失败返回 pay360.ErrCallbackSign
        if err != nil {
            http.Error(w, "invalid sign", http.StatusBadRequest)
            return
        }

        switch cb.CallbackType {
        case pay360.CallbackOrderStatus: // 1 普通支付/退款，需下发权益
        case pay360.CallbackAutopay:     // 2 代扣推送，据 cb.Extra.MfrOrderID 创建订单
        case pay360.CallbackSign:        // 3 签约/取消签约通知
        }

        // 权益处理完成后返回标准成功响应，请确保 10s 内响应，否则会被重复推送
        out, _ := json.Marshal(pay360.AckSuccess())
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write(out)
    }
}
```

> **务必校验一致性**：`VerifyCallback` 只保证请求确实来自 360（签名正确）。发放权益前，你仍需核对回调中的 `cb.AppID` / `cb.Qid` 是否与本应用一致，并核对 `cb.MfrOrderAmount`、`cb.MfrOrderID` 与本地订单是否相符，再决定是否发放。

## 错误处理

业务错误为 `*pay360.APIError`，可用哨兵判定：

```go
if errors.Is(err, pay360.ErrAccessToken) { /* 10012 */ }
if errors.Is(err, pay360.ErrOrderNotFound) { /* 100005 */ }

var apiErr *pay360.APIError
if errors.As(err, &apiErr) {
    log.Printf("code=%d msg=%s header_tid=%s", apiErr.Code, apiErr.Msg, apiErr.HeaderTid)
}
```

## access_token 缓存与多实例

默认情况下，token 缓存在进程内：读取无锁，刷新单飞，到期前 5 分钟自动刷新。**单实例无需任何配置。**

多实例（多进程）部署时，由于“新申请 token 会使旧 token 失效”，各实例必须共享同一份 token，否则会互相作废。此时注入自定义 `TokenCache`：

```go
type TokenCache interface {
    Load(ctx context.Context) (token string, expireAt time.Time, ok bool, err error)
    Store(ctx context.Context, token string, expireAt time.Time) error
}
```

**重要**：即使共享存储，若多个实例同时刷新仍会互相作废。请在 `Store`/刷新处使用分布式锁（如 Redis `SETNX`），或采用单点刷新服务，业务实例只读缓存。

当业务接口返回 `errno=10012`（`ErrAccessToken`）时，本包会强制刷新一次 `access_token` 并用新 token 重试该业务请求一次。该重试仅覆盖鉴权失败场景；业务错误、网络错误和其它平台错误不会自动重试。

Redis 实现范式（伪代码）：

```go
type redisCache struct{ rdb *redis.Client }

func (r *redisCache) Load(ctx context.Context) (string, time.Time, bool, error) {
    // GET token 与 expireAt；未命中返回 ok=false
}
func (r *redisCache) Store(ctx context.Context, token string, expireAt time.Time) error {
    // 先 SETNX 分布式锁，再写入 token（带 TTL），最后释放锁
}

c, _ := pay360.New(appid, qid, secret, pay360.WithTokenCache(&redisCache{rdb: rdb}))
```

## 限流与韧性

本包是无状态的薄封装，**不内置限流、熔断、重试**——这些有状态、依赖部署拓扑的韧性策略应由调用方在服务层处理（如 `golang.org/x/time/rate`、`sony/gobreaker`）。本包只负责签名、鉴权与请求执行，职责单一。

- **限频**（文档规定，请自行控制）：换 token 与退款 3 次/秒、订单查询 5 次/秒、发票相关 3 次/秒。换 token 已被内部缓存+单飞天然限频；业务接口的频率由调用方保证。
- **`access_token` 失效重试**：单实例下 token 不会被外部作废，几乎不会遇到 `errno=10012`。多实例共享缓存时，若极窄竞态窗口内 token 被其它实例作废，本包会强制刷新并自动重试一次。仍然需要共享缓存与分布式锁，避免实例间长期互相作废 token。
- **回调 body 大小**：`VerifyCallback`/`ParseCallback` 解析调用方传入的 body，请在 HTTP handler 用 `http.MaxBytesReader` 限制大小，防止超大请求耗内存。
- **出站响应大小**：默认 HTTP 客户端限制响应体 ≤10 MiB，可经 `WithHTTPClient` 调整。

## 注意事项

- 请勿通过客户端轮询订单查询接口；SDK 本身会推送订单状态变更。
- 代扣失败重发须使用全新的 `AutopayOrderID`，复用旧值可能“失败但显示成功”。
- 回调响应须在 10s 内返回，否则会被重复推送（最多补推 30 次）。
- 专票申请后需人工审核（一般 5 个工作日内），通过 `QuerySpecialInvoice` 查询进度。
- 订单查询为 GET 接口，`access_token` 会出现在 URL query 中，请注意网关/代理访问日志的脱敏。

## 许可

MIT，详见 [LICENSE](LICENSE)。
