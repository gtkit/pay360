package pay360

import (
	"context"
	"fmt"
	"net/http"
	"unicode/utf8"
)

// RefundRequest 为订单退款申请参数。
type RefundRequest struct {
	OrderID      string // 厂商调用 SDK 传入的订单 ID
	OrderAmount  int64  // 订单金额，单位：分
	UserID       string // 用户 ID（厂商的用户唯一标识）
	RefundReason string // 申请退款说明，长度不超过 200 个字符
}

// Refund 申请订单退款。返回本次响应的 Header-Tid。
func (c *Client) Refund(ctx context.Context, req RefundRequest) (headerTid string, err error) {
	if req.OrderID == "" || req.UserID == "" || req.RefundReason == "" {
		return "", fmt.Errorf("pay360: refund: order_id/user_id/refund_reason 均为必填")
	}
	if req.OrderAmount <= 0 {
		return "", fmt.Errorf("pay360: refund: order_amount 必须为正（单位：分）")
	}
	if utf8.RuneCountInString(req.RefundReason) > 200 {
		return "", fmt.Errorf("pay360: refund: refund_reason 不能超过 200 个字符")
	}

	biz := map[string]any{
		"order_id":      req.OrderID,
		"order_amount":  req.OrderAmount,
		"user_id":       req.UserID,
		"refund_reason": req.RefundReason,
	}
	var resp errnoOnlyResp
	return c.call(ctx, http.MethodPost, pathOrderRefund, biz, &resp)
}

// OrderQueryRequest 为订单查询参数。
type OrderQueryRequest struct {
	OrderID string // 厂商调用 SDK 传入的订单 ID
	UserID  string // 用户 ID
}

// OrderQuery 为订单查询返回的订单详情。
type OrderQuery struct {
	MfrOrderID      string `json:"mfr_order_id"`      // 厂商订单编号
	MfrOrderAmount  int64  `json:"mfr_order_amount"`  // 厂商订单金额，单位：分
	MfrCreateTime   string `json:"mfr_create_time"`   // 厂商订单创建时间
	MfrProductID    string `json:"mfr_product_id"`    // 厂商商品 ID
	MfrProductName  string `json:"mfr_product_name"`  // 厂商商品名称
	OrderStatus     int    `json:"order_status"`      // 订单状态，见文档状态码
	PayChannel      int    `json:"pay_chanel"`        // 支付渠道 1-微信 2-支付宝（沿用文档查询响应字段名 pay_chanel）
	OrderCode       string `json:"order_code"`        // 360 订单编号
	BankTradeCode   string `json:"bank_trade_code"`   // 银行流水号
	OrderPayTime    string `json:"order_pay_time"`    // 付款时间
	OrderRefundTime string `json:"order_refund_time"` // 退款时间

	HeaderTid string `json:"-"` // 本次响应的 Header-Tid
}

// IsPaid 报告订单是否处于支付成功状态（order_status 为 20、30 或 50）。
func (o OrderQuery) IsPaid() bool {
	return o.OrderStatus == OrderStatusPaid ||
		o.OrderStatus == OrderStatusNotified ||
		o.OrderStatus == OrderStatusCompleted
}

// QueryOrder 查询订单详情。
//
// 注意：请勿通过客户端轮询此接口；SDK 本身会通过订单推送通知状态变更。
func (c *Client) QueryOrder(ctx context.Context, req OrderQueryRequest) (OrderQuery, error) {
	if req.OrderID == "" || req.UserID == "" {
		return OrderQuery{}, fmt.Errorf("pay360: query order: order_id/user_id 均为必填")
	}
	biz := map[string]any{
		"order_id": req.OrderID,
		"user_id":  req.UserID,
	}
	var resp struct {
		errnoEnvelope
		Data rawJSON `json:"data"`
	}
	tid, err := c.call(ctx, http.MethodGet, pathOrderQuery, biz, &resp)
	if err != nil {
		return OrderQuery{HeaderTid: tid}, err
	}
	var o OrderQuery
	if derr := decodeData(resp.Data, &o); derr != nil {
		return OrderQuery{HeaderTid: tid}, fmt.Errorf("pay360: query order: decode data: %w", derr)
	}
	o.HeaderTid = tid
	return o, nil
}
