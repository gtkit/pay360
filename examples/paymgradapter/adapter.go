// Package paymgradapter 演示如何把 *pay360.Client 适配为 github.com/gtkit/go-pay
// 的 paymgr.Provider，使 360 联运能与微信/支付宝在同一套 paymgr 统一抽象下使用。
//
// 它刻意放在独立的 example 子模块（自带 go.mod）：go-pay 会拖入微信/支付宝官方 SDK
// 等较重依赖，本子模块隔离这些依赖，使 pay360 主包保持仅依赖 httpc/json。
//
// 360 联运的支付模型与微信/支付宝不同，部分 Provider 方法无对应服务端接口：
//   - 下单、取消支付由前端 JSSDK 完成 → UnifiedOrder / CloseOrder 返回 ErrNotSupported；
//   - 无独立退款查询接口 → QueryRefund 返回 ErrNotSupported；
//   - 订单查询/退款需要 user_id，而 paymgr 请求结构不含该字段 → 由 userIDOf 从业务侧解析。
package paymgradapter

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gtkit/go-pay/paymgr"
	"github.com/gtkit/json/v2"
	"github.com/gtkit/pay360"
)

// ChannelPay360 是 360 联运在 paymgr 中的渠道标识。
const ChannelPay360 paymgr.Channel = "pay360"

// Adapter 把 *pay360.Client 适配为 paymgr.Provider。
type Adapter struct {
	client *pay360.Client
	// userIDOf 根据商户订单号解析 360 所需的 user_id（业务自定义，如查库）。
	userIDOf func(outTradeNo string) (string, error)
}

// New 创建适配器。userIDOf 不能为 nil（QueryOrder/Refund 需要 user_id）。
func New(client *pay360.Client, userIDOf func(outTradeNo string) (string, error)) *Adapter {
	return &Adapter{client: client, userIDOf: userIDOf}
}

// 编译期断言：Adapter 必须满足 paymgr.Provider。
var _ paymgr.Provider = (*Adapter)(nil)

func (a *Adapter) Channel() paymgr.Channel { return ChannelPay360 }

// UnifiedOrder 360 下单在前端 JSSDK 完成，服务端无此接口。
func (a *Adapter) UnifiedOrder(context.Context, *paymgr.UnifiedOrderRequest) (*paymgr.UnifiedOrderResponse, error) {
	return nil, paymgr.ErrNotSupported
}

// CloseOrder 360 取消支付在前端 JSSDK 完成，服务端无此接口。
func (a *Adapter) CloseOrder(context.Context, *paymgr.CloseOrderRequest) error {
	return paymgr.ErrNotSupported
}

// QueryRefund 360 无独立退款查询接口，退款状态经订单查询或回调获取。
func (a *Adapter) QueryRefund(context.Context, *paymgr.QueryRefundRequest) (*paymgr.QueryRefundResponse, error) {
	return nil, paymgr.ErrNotSupported
}

// QueryOrder 通过商户订单号查询订单（user_id 由 userIDOf 解析）。
func (a *Adapter) QueryOrder(ctx context.Context, req *paymgr.QueryOrderRequest) (*paymgr.QueryOrderResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	uid, err := a.userIDOf(req.OutTradeNo)
	if err != nil {
		return nil, err
	}
	o, err := a.client.QueryOrder(ctx, pay360.OrderQueryRequest{OrderID: req.OutTradeNo, UserID: uid})
	if err != nil {
		return nil, mapErr(err)
	}
	return &paymgr.QueryOrderResponse{
		Channel:       ChannelPay360,
		OutTradeNo:    o.MfrOrderID,
		TransactionID: o.OrderCode,
		TradeStatus:   tradeStatus(o.OrderStatus),
		TotalAmount:   o.MfrOrderAmount,
	}, nil
}

// Refund 申请退款（user_id 由 userIDOf 解析；360 无退款单号概念，OutRefundNo 仅回传）。
func (a *Adapter) Refund(ctx context.Context, req *paymgr.RefundRequest) (*paymgr.RefundResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	uid, err := a.userIDOf(req.OutTradeNo)
	if err != nil {
		return nil, err
	}
	if _, err := a.client.Refund(ctx, pay360.RefundRequest{
		OrderID:      req.OutTradeNo,
		OrderAmount:  req.RefundAmount,
		UserID:       uid,
		RefundReason: req.Reason,
	}); err != nil {
		return nil, mapErr(err)
	}
	return &paymgr.RefundResponse{
		Channel:      ChannelPay360,
		OutRefundNo:  req.OutRefundNo,
		RefundAmount: req.RefundAmount,
	}, nil
}

// ParseNotify 验签并解析 360 订单推送回调，映射为统一通知结果。
// 签约通知（callback_type=3）无支付语义，appid/qid 不一致的回调不属于本应用，
// 二者均返回 ErrInvalidNotify 由上层忽略。
func (a *Adapter) ParseNotify(_ context.Context, r *http.Request) (*paymgr.NotifyResult, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	cb, err := a.client.VerifyCallback(body)
	if err != nil {
		if errors.Is(err, pay360.ErrCallbackSign) {
			return nil, paymgr.ErrInvalidSign
		}
		if errors.Is(err, pay360.ErrCallbackMismatch) {
			return nil, paymgr.ErrInvalidNotify
		}
		return nil, err
	}
	if cb.CallbackType == pay360.CallbackSign {
		return nil, paymgr.ErrInvalidNotify
	}
	return &paymgr.NotifyResult{
		Channel:       ChannelPay360,
		OutTradeNo:    cb.MfrOrderID,
		TransactionID: cb.OrderCode,
		TradeStatus:   tradeStatus(cb.OrderStatus),
		TotalAmount:   cb.MfrOrderAmount,
	}, nil
}

// ParseRefundNotify 360 退款经普通回调下发（callback_type=1 + order_status），无独立退款通知端点。
func (a *Adapter) ParseRefundNotify(context.Context, *http.Request) (*paymgr.RefundNotifyResult, error) {
	return nil, paymgr.ErrNotSupported
}

// ACKNotify 回写 360 要求的成功响应。
func (a *Adapter) ACKNotify(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	out, _ := json.Marshal(pay360.AckSuccess())
	_, _ = w.Write(out)
}

// tradeStatus 把 360 order_status 映射为 paymgr.TradeStatus。
func tradeStatus(s int) paymgr.TradeStatus {
	switch s {
	case 20, 30, 50: // 付款完成 / 待发权益 / 交易完成
		return paymgr.TradeStatusPaid
	case 40, 70: // 售后中 / 退款完成
		return paymgr.TradeStatusRefunded
	case 60: // 已取消
		return paymgr.TradeStatusClosed
	case 10: // 待付款
		return paymgr.TradeStatusPending
	default:
		return paymgr.TradeStatusError
	}
}

// mapErr 把 pay360 错误映射为 paymgr 哨兵错误。
func mapErr(err error) error {
	if errors.Is(err, pay360.ErrOrderNotFound) {
		return paymgr.ErrOrderNotFound
	}
	return err
}
