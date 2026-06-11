package pay360

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/gtkit/json/v2"
)

// 回调类型。
const (
	CallbackOrderStatus = 1 // 订单状态变更（普通下单扣款、退款）
	CallbackAutopay     = 2 // 代扣推送（自动扣款），需厂商据 order_extra 创建订单
	CallbackSign        = 3 // 签约/取消签约状态通知，无需下发或取消权益
)

// 签约状态（OrderExtra.AutoPayStatus / callback_type=3）。
const (
	AutoPayStatusOpen   = 1 // 开通签约
	AutoPayStatusCancel = 2 // 取消签约
)

// ErrCallbackSign 表示回调验签失败。
var ErrCallbackSign = errors.New("pay360: callback sign mismatch")

// ErrCallbackMismatch 表示回调验签通过但其中的 app_id/qid 与客户端凭据不一致，
// 调用方不应处理该回调（它属于其它应用）。
var ErrCallbackMismatch = errors.New("pay360: callback appid/qid mismatch")

// OrderExtra 为回调中 order_extra 字段（JSON 字符串）解析后的内容。
type OrderExtra struct {
	MfrOrderID      string `json:"mfr_order_id"`
	AgreementNumber string `json:"agreement_number"`
	AutoPayStatus   int    `json:"auto_pay_status"`
}

// Callback 为厂商订单推送回调的解析结果。
//
// AgreementNumber 与 AutoPayStatus 为文档 4.1.2.4 参数表列出的顶层签约字段；
// 推送示例中同名信息也可能出现在 order_extra 内（见 Extra），两处独立解析。
type Callback struct {
	AppID           string `json:"app_id"`
	AgreementNumber string `json:"agreement_number"` // 签约号
	AutoPayStatus   int    `json:"auto_pay_status"`  // 签约状态：1 开通签约，2 取消签约
	BankTradeCode   string `json:"bank_trade_code"`
	CallbackType    int    `json:"callback_type"`
	MfrOrderAmount  int64  `json:"mfr_order_amount"`
	MfrOrderID      string `json:"mfr_order_id"`
	MfrProductID    string `json:"mfr_product_id"`
	MfrProductName  string `json:"mfr_product_name"`
	OrderCode       string `json:"order_code"`
	OrderExtra      string `json:"order_extra"`
	OrderStatus     int    `json:"order_status"`
	PayChannel      int    `json:"pay_channel"`
	Qid             int64  `json:"qid"`
	Sign            string `json:"sign"`
	Timestamp       int64  `json:"timestamp"`
	TransTime       string `json:"trans_time"` // 支付/退款时间；签约、解约推送中不使用，为占位值 0001-01-01 00:00:00

	// Extra 为 OrderExtra 字段解析后的结构（callback_type 为 2/3 时有意义）。
	Extra OrderExtra `json:"-"`
}

// IsPaid 报告回调订单是否处于支付成功状态（order_status 为 20、30 或 50），
// 与 [OrderQuery.IsPaid] 语义一致。
func (cb Callback) IsPaid() bool {
	return isPaidStatus(cb.OrderStatus)
}

// ParseCallback 解析厂商订单推送回调的请求体，并解析内嵌的 order_extra。
// 它不做验签；如需验签请使用 [Client.VerifyCallback]。
func ParseCallback(body []byte) (*Callback, error) {
	var cb Callback
	if err := json.Unmarshal(body, &cb); err != nil {
		return nil, fmt.Errorf("pay360: parse callback: %w", err)
	}
	if cb.OrderExtra != "" {
		if err := json.Unmarshal([]byte(cb.OrderExtra), &cb.Extra); err != nil {
			return nil, fmt.Errorf("pay360: parse callback order_extra: %w", err)
		}
	}
	return &cb, nil
}

// VerifyCallback 验签并解析厂商订单推送回调。
//
// 验签规则与出站一致（空值不参与），签名盐为本客户端的 appsecret。
// 验签失败返回 [ErrCallbackSign]；验签通过但回调中的 app_id/qid 与客户端凭据
// 不一致时返回 [ErrCallbackMismatch]。金额与订单数据的一致性核对仍由调用方
// 在发放权益前完成（本包不持有订单数据）。
func (c *Client) VerifyCallback(body []byte) (*Callback, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("pay360: verify callback: %w", err)
	}

	got, _ := raw["sign"].(string)
	if got == "" {
		return nil, ErrCallbackSign
	}
	params, err := stringifyParams(raw)
	if err != nil {
		return nil, err
	}
	if !signEqual(buildSign(params, c.appsecret), got) {
		return nil, ErrCallbackSign
	}
	cb, err := ParseCallback(body)
	if err != nil {
		return nil, err
	}
	if cb.AppID != c.appid || cb.Qid != c.qid {
		return nil, ErrCallbackMismatch
	}
	return cb, nil
}

// stringifyParams 把验签所需的顶层参数转为字符串形式。
//
// 解码时启用 UseNumber，数字以原始字面量（json.Number，实现 fmt.Stringer）保留，
// 避免经 float64 中转丢失精度或改写原文，导致重建明文与服务端不一致。
// nil 值不参与签名；遇到对象/数组/布尔等非字符串、非数字类型直接报错，不臆测其字符串形态。
// sign 键由 buildSign 自行剔除。
func stringifyParams(raw map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			out[k] = val
		case fmt.Stringer: // json.Number 保留原始字面量
			out[k] = val.String()
		case nil:
			// 空值不参与签名
		default:
			return nil, fmt.Errorf("pay360: verify callback: 字段 %q 类型 %T 无法参与验签", k, v)
		}
	}
	return out, nil
}

// Ack 为厂商对订单推送回调的响应体。
type Ack struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

// AckSuccess 返回标准成功响应 {"code":200,"message":"success","data":""}。
// 360-Server 收到此响应后会将 order_status 由 30 变更为 50。
func AckSuccess() Ack {
	return Ack{Code: 200, Message: "success", Data: ""}
}

// AckResponse 返回自定义响应体（成功时 code 应为 200）。
func AckResponse(code int, message, data string) Ack {
	return Ack{Code: code, Message: message, Data: data}
}
