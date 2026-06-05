# paymgradapter

把 `*pay360.Client` 适配为 [`go-pay`](https://github.com/gtkit/go-pay) 的 `paymgr.Provider`，让 360 联运能与微信/支付宝在同一套 `paymgr` 抽象下统一调度。

这是一个**独立 example 子模块**（自带 `go.mod`）：`go-pay` 会引入微信/支付宝官方 SDK 等较重依赖，独立子模块把这些依赖隔离在示例里，使 `pay360` 主包保持仅依赖 `httpc`/`json`。可直接复制本文件到你的业务项目按需修改。

## 支持范围

360 联运的支付模型与微信/支付宝不同，并非所有 `Provider` 方法都有对应服务端接口：

| Provider 方法 | 360 适配 |
|--------------|---------|
| `QueryOrder` / `Refund` | ✅ 支持（`user_id` 由 `userIDOf` 从业务侧解析） |
| `ParseNotify` / `ACKNotify` | ✅ 支持（基于 `VerifyCallback` / `AckSuccess`） |
| `Channel` | ✅ 返回 `"pay360"` |
| `UnifiedOrder`（下单） | ❌ `ErrNotSupported`：360 下单在前端 JSSDK |
| `CloseOrder`（关单） | ❌ `ErrNotSupported`：360 取消在前端 JSSDK |
| `QueryRefund`（退款查询） | ❌ `ErrNotSupported`：360 无独立接口 |
| `ParseRefundNotify` | ❌ `ErrNotSupported`：退款经普通回调下发 |

> 360 特有能力（代扣、签约、发票、任务）不在 `Provider` 抽象内，请直接使用 `pay360.Client` 的对应方法。

## 用法

```go
client, _ := pay360.New(appid, qid, appsecret)

// userIDOf：根据商户订单号查出 360 所需的 user_id（如查库）
adapter := paymgradapter.New(client, func(outTradeNo string) (string, error) {
    return lookupUserID(outTradeNo)
})

mgr := paymgr.NewManager()
mgr.Register(adapter) // 与微信/支付宝 Provider 一同注册，按 Channel 调度
```
