package pay360

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gtkit/json/v2"
)

// 投诉状态（complain_info.currStatus）。
const (
	ComplainStatusPending    = 1 // 待处理
	ComplainStatusProcessing = 2 // 处理中
	ComplainStatusFinished   = 3 // 已完结
	ComplainStatusUnfinished = 4 // 未完结/兼容值
)

// 投诉来源平台（complain_info.platForm）。
const (
	ComplainPlatformOther         = 1 // 其他/未知
	ComplainPlatformAlipay        = 2 // 支付宝
	ComplainPlatformWechat        = 3 // 微信
	ComplainPlatformAlipaySpecial = 4 // 支付宝特殊版
)

// 投诉回复来源（ComplainReplyRequest.Source）。
const (
	ComplainSourceNormal = 1 // 普通回复
	ComplainSourceRefund = 2 // 退款相关回复
)

// 投诉 webhook 事件类型（ComplaintWebhook.EventType）。
const (
	ComplaintEventCreated       = "COMPLAINT_CREATED" // 新投诉创建
	ComplaintEventReplyAdded    = "REPLY_ADDED"       // 投诉新增回复
	ComplaintEventStatusChanged = "STATUS_CHANGED"    // 投诉状态发生变化
)

// ErrComplaintWebhookSign 表示投诉 webhook 推送验签失败。
var ErrComplaintWebhookSign = errors.New("pay360: complaint webhook sign mismatch")

// RefundOrdersRequest 为多笔订单退款（v2）参数。
type RefundOrdersRequest struct {
	OrderIDs     []string // 厂商订单 ID 列表，发送时以英文逗号拼接，拼接后总长 1-500 字符
	UserID       string   // 用户 ID（厂商的用户唯一标识），长度 1-50 字符
	OrderAmount  int64    // 订单金额，单位：分，必须大于 0；建议与原订单金额一致
	RefundReason string   // 退款原因，长度 1-200 字符
}

// RefundOrders 申请订单退款（v2 接口），支持单笔与多笔。返回本次响应的 Header-Tid。
//
// 多笔时平台按逗号拆分去重逐笔处理，任一订单退款失败则本次请求返回失败。
// 通过任务系统完成的订单（任务单）不允许退款。退款成功后，如订单关联投诉且
// 关联订单均已关闭，平台会自动完结投诉。与 v1 [Client.Refund] 并存。
func (c *Client) RefundOrders(ctx context.Context, req RefundOrdersRequest) (headerTid string, err error) {
	if len(req.OrderIDs) == 0 {
		return "", fmt.Errorf("pay360: refund orders: order_ids 不能为空")
	}
	if slices.Contains(req.OrderIDs, "") {
		return "", fmt.Errorf("pay360: refund orders: order_ids 含空元素")
	}
	orderID := strings.Join(req.OrderIDs, ",")
	if utf8.RuneCountInString(orderID) > 500 {
		return "", fmt.Errorf("pay360: refund orders: order_id 拼接后不能超过 500 个字符")
	}
	if req.UserID == "" || utf8.RuneCountInString(req.UserID) > 50 {
		return "", fmt.Errorf("pay360: refund orders: user_id 长度须为 1-50 个字符")
	}
	if req.OrderAmount <= 0 {
		return "", fmt.Errorf("pay360: refund orders: order_amount 必须为正（单位：分）")
	}
	if req.RefundReason == "" || utf8.RuneCountInString(req.RefundReason) > 200 {
		return "", fmt.Errorf("pay360: refund orders: refund_reason 长度须为 1-200 个字符")
	}

	biz := map[string]any{
		"order_id":      orderID,
		"user_id":       req.UserID,
		"order_amount":  req.OrderAmount,
		"refund_reason": req.RefundReason,
	}
	var resp errnoOnlyResp
	return c.call(ctx, http.MethodPost, pathRefundOrders, biz, &resp)
}

// ComplainReplyRequest 为投诉回复参数。
type ComplainReplyRequest struct {
	ComplainNo string // 投诉编号，即 webhook 推送中的 complainNo
	Content    string // 回复内容，长度 1-100 字符
	Source     int    // 回复来源，见 ComplainSourceNormal / ComplainSourceRefund
}

// ComplainReply 回复用户投诉，回复内容会同步至对应投诉渠道。返回本次响应的 Header-Tid。
func (c *Client) ComplainReply(ctx context.Context, req ComplainReplyRequest) (headerTid string, err error) {
	if req.ComplainNo == "" {
		return "", fmt.Errorf("pay360: complain reply: complain_no 必填")
	}
	if req.Content == "" || utf8.RuneCountInString(req.Content) > 100 {
		return "", fmt.Errorf("pay360: complain reply: content 长度须为 1-100 个字符")
	}
	if req.Source != ComplainSourceNormal && req.Source != ComplainSourceRefund {
		return "", fmt.Errorf("pay360: complain reply: source 必须为 1(普通回复) 或 2(退款相关回复)")
	}

	biz := map[string]any{
		"complain_no": req.ComplainNo,
		"content":     req.Content,
		"source":      req.Source,
	}
	var resp errnoOnlyResp
	return c.call(ctx, http.MethodPost, pathComplainReply, biz, &resp)
}

// ComplainFinishRequest 为投诉完结参数。完结处理码 code 由本包固定填充 "05"。
type ComplainFinishRequest struct {
	ComplainNo string // 投诉编号
	Content    string // 完结说明，长度 1-100 字符
}

// ComplainFinish 完结投诉（厂商确认投诉已处理完成后调用）。返回本次响应的 Header-Tid。
func (c *Client) ComplainFinish(ctx context.Context, req ComplainFinishRequest) (headerTid string, err error) {
	if req.ComplainNo == "" {
		return "", fmt.Errorf("pay360: complain finish: complain_no 必填")
	}
	if req.Content == "" || utf8.RuneCountInString(req.Content) > 100 {
		return "", fmt.Errorf("pay360: complain finish: content 长度须为 1-100 个字符")
	}

	biz := map[string]any{
		"complain_no": req.ComplainNo,
		"content":     req.Content,
		// 文档规定完结处理码固定传 "05"，其他值返回参数错误
		"code": "05",
	}
	var resp errnoOnlyResp
	return c.call(ctx, http.MethodPost, pathComplainFinish, biz, &resp)
}

// ComplainReplyInfo 为投诉回复信息（webhook 的 reply_info 及 complain_info.replies 元素）。
type ComplainReplyInfo struct {
	ComplainNo  string   `json:"complainNo"`  // 投诉编号
	OperateTime string   `json:"operateTime"` // 回复时间
	Operator    string   `json:"operator"`    // 回复操作人
	Content     string   `json:"content"`     // 回复内容
	Images      []string `json:"images"`      // 回复图片，base64 编码，不带 data:image/... 前缀
}

// ComplainInfo 为投诉信息（webhook 的 complain_info）。
//
// 服务端以 omitempty 序列化：字段值为空、0、空数组时可能不出现在推送中，
// 解析后表现为对应零值，不代表平台显式推送了空值。
type ComplainInfo struct {
	Qid                      int64               `json:"qid"`                      // 厂商 ID
	AppID                    string              `json:"appId"`                    // 应用 ID
	ComplainNo               string              `json:"complainNo"`               // 投诉编号
	ComplainTime             string              `json:"complainTime"`             // 投诉时间，YYYY-MM-DD HH:MM:SS
	BankTradeCode            string              `json:"bankTradeCode"`            // 第三方支付流水号
	CurrStatus               int                 `json:"currStatus"`               // 投诉状态，见 ComplainStatus*
	PhoneNo                  string              `json:"phoneNo"`                  // 用户联系方式
	ComplainContent          string              `json:"complainContent"`          // 投诉内容
	PlatForm                 int                 `json:"platForm"`                 // 来源平台，见 ComplainPlatform*
	BankTradeCodes           []string            `json:"bankTradeCodes"`           // 关联的第三方支付流水号列表
	LatestReplyContentDigest string              `json:"latestReplyContentDigest"` // 最新回复摘要
	HasNewMsg                int                 `json:"hasNewMsg"`                // 是否存在新的待处理消息：1 是，0 否
	Images                   []string            `json:"images"`                   // 投诉图片，base64 编码，不带 data:image/... 前缀
	Replies                  []ComplainReplyInfo `json:"replies"`                  // 投诉历史回复列表
}

// ComplaintWebhook 为投诉 webhook 推送的解析结果。
//
// ComplainInfo 与 ReplyInfo 按事件类型出现：COMPLAINT_CREATED、REPLY_ADDED、
// STATUS_CHANGED 均携带 ComplainInfo；仅 REPLY_ADDED 携带 ReplyInfo。未携带时为 nil。
type ComplaintWebhook struct {
	MessageID      string             `json:"message_id"`      // 推送消息 ID，与请求头 X-Message-Id 一致，请据此做幂等
	EventType      string             `json:"event_type"`      // 事件类型，见 ComplaintEvent*
	PayloadVersion string             `json:"payload_version"` // 推送协议版本，当前固定 v1
	ComplainInfo   *ComplainInfo      `json:"complain_info"`   // 投诉信息
	ReplyInfo      *ComplainReplyInfo `json:"reply_info"`      // 本次新增回复信息

	Timestamp int64 `json:"-"` // 外层推送时间戳，单位秒
}

// complaintEnvelope 为投诉 webhook 推送体外层。data 以原始字节承接，供原文验签。
type complaintEnvelope struct {
	Data      rawJSON `json:"data"`
	Timestamp int64   `json:"timestamp"`
	Sign      string  `json:"sign"`
}

// ParseComplaintWebhook 解析投诉 webhook 推送体。
// 它不做验签；如需验签请使用 [Client.VerifyComplaintWebhook]。
func ParseComplaintWebhook(body []byte) (*ComplaintWebhook, error) {
	env, err := decodeComplaintEnvelope(body)
	if err != nil {
		return nil, err
	}
	return decodeComplaintData(env)
}

// VerifyComplaintWebhook 验签并解析投诉 webhook 推送。
//
// 验签规则与 OPENAPI 一致，但参与签名的字段仅有 data 与 timestamp，且 data 取
// 请求原文中的 JSON 字符串（不重新序列化）。签名密钥优先使用 [WithVendorKey]
// 配置的厂商密钥，未配置时使用 appsecret。验签失败返回 [ErrComplaintWebhookSign]。
//
// 平台按 HTTP 状态码判定推送结果：处理成功请返回任意 2xx；失败最多重试 3 次，
// 请按 MessageID 做幂等。
func (c *Client) VerifyComplaintWebhook(body []byte) (*ComplaintWebhook, error) {
	env, err := decodeComplaintEnvelope(body)
	if err != nil {
		return nil, err
	}
	if env.Sign == "" {
		return nil, ErrComplaintWebhookSign
	}
	key := c.vendorKey
	if key == "" {
		key = c.appsecret
	}
	params := map[string]string{
		"data":      string(env.Data),
		"timestamp": strconv.FormatInt(env.Timestamp, 10),
	}
	if !signEqual(buildSign(params, key), env.Sign) {
		return nil, ErrComplaintWebhookSign
	}
	return decodeComplaintData(env)
}

func decodeComplaintEnvelope(body []byte) (complaintEnvelope, error) {
	var env complaintEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return env, fmt.Errorf("pay360: parse complaint webhook: %w", err)
	}
	return env, nil
}

func decodeComplaintData(env complaintEnvelope) (*ComplaintWebhook, error) {
	var w ComplaintWebhook
	if err := decodeData(env.Data, &w); err != nil {
		return nil, fmt.Errorf("pay360: parse complaint webhook data: %w", err)
	}
	w.Timestamp = env.Timestamp
	return &w, nil
}
