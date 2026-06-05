// Package pay360 封装 360 联运（软件管家）开放平台 OPENAPI 的服务端接口。
//
// 它面向需要对接 360 联运支付/退款/订单/代扣/签约/发票的 Go 服务端程序，
// 统一处理签名生成、access_token 生命周期、Header-Tid 排障头与错误码语义。
//
// 快速开始:
//
//	c, err := pay360.New("your-appid", 123456, "your-appsecret")
//	if err != nil {
//	    // 处理构造错误
//	}
//	tid, err := c.Refund(ctx, pay360.RefundRequest{
//	    OrderID:      "order-1",
//	    OrderAmount:  100, // 单位：分
//	    UserID:       "user-1",
//	    RefundReason: "用户申请退款",
//	})
//
// access_token 由 [Client] 内部缓存并按需刷新；默认实现为无锁内存缓存，
// 多实例部署可通过 [WithTokenCache] 注入共享存储实现，详见 [TokenCache]。
//
// [Client] 构造后字段只读，可安全地在多个 goroutine 间共享。
package pay360
