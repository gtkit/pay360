package pay360

import (
	"context"
	"fmt"
	"net/http"
)

// DoPostRequest 为厂商侧发起代扣的参数。
//
// 本接口仅用于厂商服务端主动发起代扣，代扣发起日期与周期由厂商自行设定。
// 注意：若上一笔代扣失败需重新发起，必须使用一个全新的 AutopayOrderID，
// 复用旧值可能出现“代扣失败但显示成功”的异常。
type DoPostRequest struct {
	OrderID         string // 签约的订单 ID
	AgreementNumber string // 签约的协议号
	AutopayAmount   int64  // 代扣金额，单位：分
	AutopayOrderID  string // 本次代扣的订单 ID，须保证唯一
}

// DoPost 厂商侧发起一次代扣。返回本次响应的 Header-Tid。
func (c *Client) DoPost(ctx context.Context, req DoPostRequest) (headerTid string, err error) {
	if req.OrderID == "" || req.AgreementNumber == "" || req.AutopayOrderID == "" {
		return "", fmt.Errorf("pay360: dopost: order_id/agreement_number/autopay_orderid 均为必填")
	}
	biz := map[string]any{
		"order_id":         req.OrderID,
		"agreement_number": req.AgreementNumber,
		"autopay_amount":   req.AutopayAmount,
		"autopay_orderid":  req.AutopayOrderID,
	}
	var resp errnoOnlyResp
	return c.call(ctx, http.MethodPost, pathAutopayDoPost, biz, &resp)
}

// CancelSignRequest 为厂商侧取消签约的参数。
type CancelSignRequest struct {
	OrderID         string // 签约的订单 ID
	AgreementNumber string // 签约的协议号
}

// CancelSign 厂商侧取消用户代扣签约。返回本次响应的 Header-Tid。
func (c *Client) CancelSign(ctx context.Context, req CancelSignRequest) (headerTid string, err error) {
	if req.OrderID == "" || req.AgreementNumber == "" {
		return "", fmt.Errorf("pay360: cancel_sign: order_id/agreement_number 均为必填")
	}
	biz := map[string]any{
		"order_id":         req.OrderID,
		"agreement_number": req.AgreementNumber,
	}
	var resp errnoOnlyResp
	return c.call(ctx, http.MethodPost, pathAutopayCancelSign, biz, &resp)
}
