package pay360

import (
	"context"
	"fmt"
	"net/http"
)

// PlainInvoiceRequest 为普票开具参数。OrderID、InvoiceTitle、UserEmail 必填，其余可选。
type PlainInvoiceRequest struct {
	OrderID       string // 厂商调用 SDK 传入的订单 ID
	InvoiceTitle  string // 发票抬头
	UserEmail     string // 用户邮箱
	TaxRegisterNo string // 纳税人识别号
	Address       string // 地址
	Phone         string // 电话
	BankName      string // 银行名称
	BankAccount   string // 银行账号
	Remarks       string // 备注
}

// PlainInvoiceResult 为普票开具返回结果。
type PlainInvoiceResult struct {
	DownloadURL string `json:"download_url"` // PDF 下载地址
	InvoiceCode string `json:"invoice_code"` // 发票代码
	InvoiceNo   string `json:"invoice_no"`   // 发票号码
	ReceiptURL  string `json:"receipt_url"`  // 收票地址，可据此生成二维码
	SuccessTime string `json:"success_time"` // 开票日期
	VerifyCode  string `json:"verify_code"`  // 校验码

	HeaderTid string `json:"-"`
}

// PlainInvoice 开具普通发票。
func (c *Client) PlainInvoice(ctx context.Context, req PlainInvoiceRequest) (PlainInvoiceResult, error) {
	if req.OrderID == "" || req.InvoiceTitle == "" || req.UserEmail == "" {
		return PlainInvoiceResult{}, fmt.Errorf("pay360: plain invoice: order_id/invoice_title/user_email 均为必填")
	}
	biz := map[string]any{
		"order_id":        req.OrderID,
		"invoice_title":   req.InvoiceTitle,
		"user_email":      req.UserEmail,
		"tax_register_no": req.TaxRegisterNo,
		"address":         req.Address,
		"phone":           req.Phone,
		"bank_name":       req.BankName,
		"bank_account":    req.BankAccount,
		"remarks":         req.Remarks,
	}
	var resp struct {
		errnoEnvelope
		Data rawJSON `json:"data"`
	}
	tid, err := c.call(ctx, http.MethodPost, pathInvoicePlain, biz, &resp)
	if err != nil {
		return PlainInvoiceResult{HeaderTid: tid}, err
	}
	var out PlainInvoiceResult
	if derr := decodeData(resp.Data, &out); derr != nil {
		return PlainInvoiceResult{HeaderTid: tid}, fmt.Errorf("pay360: plain invoice: decode data: %w", derr)
	}
	out.HeaderTid = tid
	return out, nil
}

// PlainInvoiceCancel 普票红冲（红冲对应订单的普通发票）。返回本次响应的 Header-Tid。
//
// 此接口响应采用 {code,msg} 信封（code 0 表示成功），与多数接口的 errno 信封不同。
func (c *Client) PlainInvoiceCancel(ctx context.Context, orderID string) (headerTid string, err error) {
	if orderID == "" {
		return "", fmt.Errorf("pay360: plain invoice cancel: order_id 必填")
	}
	biz := map[string]any{"order_id": orderID}
	var resp struct {
		codeEnvelope
	}
	return c.call(ctx, http.MethodPost, pathInvoicePlainCancel, biz, &resp)
}

// SpecialInvoiceRequest 为专票开具参数。除 Remarks 外均为必填。
// 专票申请后需人工审核（一般 5 个工作日内），可通过 [Client.QuerySpecialInvoice] 查询进度。
type SpecialInvoiceRequest struct {
	OrderID       string // 厂商调用 SDK 传入的订单 ID
	InvoiceTitle  string // 发票抬头（不支持个人抬头）
	UserEmail     string // 用户邮箱
	TaxRegisterNo string // 纳税人识别号
	Address       string // 地址
	Phone         string // 电话
	BankName      string // 银行名称
	BankAccount   string // 银行账号
	CustomType    string // 商户类型，1 企业
	Remarks       string // 备注
}

// SpecialInvoiceResult 为专票开具返回结果。
type SpecialInvoiceResult struct {
	SourceID string `json:"source_id"` // 蓝票申请单来源 ID，红冲时需要

	HeaderTid string `json:"-"`
}

// SpecialInvoice 申请开具专用发票，返回 source_id。建议保存返回信息以便后续查询/红冲。
func (c *Client) SpecialInvoice(ctx context.Context, req SpecialInvoiceRequest) (SpecialInvoiceResult, error) {
	if req.OrderID == "" || req.InvoiceTitle == "" || req.UserEmail == "" ||
		req.TaxRegisterNo == "" || req.Address == "" || req.Phone == "" ||
		req.BankName == "" || req.BankAccount == "" || req.CustomType == "" {
		return SpecialInvoiceResult{}, fmt.Errorf("pay360: special invoice: 除 remarks 外均为必填")
	}
	biz := map[string]any{
		"order_id":        req.OrderID,
		"invoice_title":   req.InvoiceTitle,
		"user_email":      req.UserEmail,
		"tax_register_no": req.TaxRegisterNo,
		"address":         req.Address,
		"phone":           req.Phone,
		"bank_name":       req.BankName,
		"bank_account":    req.BankAccount,
		"custom_type":     req.CustomType,
		"remarks":         req.Remarks,
	}
	var resp struct {
		errnoEnvelope
		Data rawJSON `json:"data"`
	}
	tid, err := c.call(ctx, http.MethodPost, pathInvoiceSpecial, biz, &resp)
	if err != nil {
		return SpecialInvoiceResult{HeaderTid: tid}, err
	}
	var out SpecialInvoiceResult
	if derr := decodeData(resp.Data, &out); derr != nil {
		return SpecialInvoiceResult{HeaderTid: tid}, fmt.Errorf("pay360: special invoice: decode data: %w", derr)
	}
	out.HeaderTid = tid
	return out, nil
}

// SpecialInvoiceQueryResult 为专票查询返回结果（开票或红冲）。
type SpecialInvoiceQueryResult struct {
	DownloadURL string `json:"download_url"` // PDF 下载地址
	InvoiceCode string `json:"invoice_code"` // 发票代码
	InvoiceNum  string `json:"invoice_num"`  // 发票号码
	ReceiptURL  string `json:"receipt_url"`  // 收票地址
	Status      string `json:"status"`       // 状态，如 SUCCESS_END
	SuccessTime string `json:"success_time"` // 开票/红冲日期

	HeaderTid string `json:"-"`
}

// 专票查询请求类型。
const (
	SpecialInvoiceQueryIssue  = "1" // 开票查询
	SpecialInvoiceQueryCancel = "2" // 红冲查询
)

// QuerySpecialInvoice 查询专票开票或红冲进度。
// requestType 取 [SpecialInvoiceQueryIssue] 或 [SpecialInvoiceQueryCancel]。
func (c *Client) QuerySpecialInvoice(ctx context.Context, requestType, sourceID string) (SpecialInvoiceQueryResult, error) {
	if requestType == "" || sourceID == "" {
		return SpecialInvoiceQueryResult{}, fmt.Errorf("pay360: query special invoice: request_type/source_id 均为必填")
	}
	if requestType != SpecialInvoiceQueryIssue && requestType != SpecialInvoiceQueryCancel {
		return SpecialInvoiceQueryResult{}, fmt.Errorf("pay360: query special invoice: request_type 必须为 1(开票查询) 或 2(红冲查询)")
	}
	biz := map[string]any{
		"request_type": requestType,
		"source_id":    sourceID,
	}
	var resp struct {
		errnoEnvelope
		Data rawJSON `json:"data"`
	}
	tid, err := c.call(ctx, http.MethodPost, pathInvoiceSpecialQuery, biz, &resp)
	if err != nil {
		return SpecialInvoiceQueryResult{HeaderTid: tid}, err
	}
	var out SpecialInvoiceQueryResult
	if derr := decodeData(resp.Data, &out); derr != nil {
		return SpecialInvoiceQueryResult{HeaderTid: tid}, fmt.Errorf("pay360: query special invoice: decode data: %w", derr)
	}
	out.HeaderTid = tid
	return out, nil
}

// 文档明确定义的发票取值常量。仅供组装请求时复用，
// 请求方法不据此封闭取值集合（文档未声明集合封闭）。
const (
	InvoiceRedCategorySeller    = "1"               // 专票红冲类别：销方红冲
	InvoiceRedReasonMistake     = "INVOICE_MISTAKE" // 专票红冲原因（销方红冲必填）
	InvoiceCustomTypeEnterprise = "1"               // 专票商户类型：企业
)

// SpecialInvoiceCancelRequest 为专票红冲参数（销方红冲）。
type SpecialInvoiceCancelRequest struct {
	Category   string // 红冲类别，1 销方红冲
	InvoiceNum string // 发票号码
	RedReason  string // 红冲原因，如 INVOICE_MISTAKE
	SourceID   string // 蓝票申请单来源 ID
	OrderID    string // 厂商调用 SDK 传入的订单 ID
}

// SpecialInvoiceCancel 申请专票红冲。返回本次响应的 Header-Tid。
func (c *Client) SpecialInvoiceCancel(ctx context.Context, req SpecialInvoiceCancelRequest) (headerTid string, err error) {
	if req.Category == "" || req.InvoiceNum == "" || req.RedReason == "" || req.SourceID == "" || req.OrderID == "" {
		return "", fmt.Errorf("pay360: special invoice cancel: category/invoice_num/red_reason/source_id/order_id 均为必填")
	}
	biz := map[string]any{
		"category":    req.Category,
		"invoice_num": req.InvoiceNum,
		"red_reason":  req.RedReason,
		"source_id":   req.SourceID,
		"order_id":    req.OrderID,
	}
	var resp errnoOnlyResp
	return c.call(ctx, http.MethodPost, pathInvoiceSpecialCancel, biz, &resp)
}
