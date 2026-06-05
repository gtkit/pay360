package pay360

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gtkit/httpc"
	"github.com/gtkit/json/v2"
)

const defaultBaseURL = "https://api.openstore.360.cn"

// headerTidKey 是 360 响应头中用于排障定位的字段名。
const headerTidKey = "Header-Tid"

// 接口路径常量。
const (
	pathAuth                 = "/main/open/v1/auth/access_token"
	pathOrderRefund          = "/main/open/v1/order/refund"
	pathOrderQuery           = "/main/open/v1/order/query"
	pathAutopayDoPost        = "/main/open/v1/autopay/dopost"
	pathAutopayCancelSign    = "/main/open/v1/autopay/cancel_sign"
	pathInvoicePlain         = "/main/gateway/v1/order/invoicing"
	pathInvoicePlainCancel   = "/main/gateway/v1/invoice/cancel"
	pathInvoiceSpecial       = "/main/gateway/v1/invoice/dospecial"
	pathInvoiceSpecialQuery  = "/main/gateway/v1/invoice/queryspecial"
	pathInvoiceSpecialCancel = "/main/gateway/v1/invoice/cancelspecial"
)

const defaultRefreshAhead = 5 * time.Minute

// Client 是 360 联运 OPENAPI 的服务端客户端。
//
// 构造后所有字段只读（token 缓存内部自带并发安全），可在多个 goroutine 间共享。
type Client struct {
	appid     string
	qid       int64
	appsecret string
	baseURL   string

	http  *httpc.Client
	cache TokenCache
	clock func() time.Time

	refreshAhead time.Duration

	// tokenMu 仅在刷新 token 时短暂持有，用于单飞去重；读取 token 不经此锁。
	tokenMu sync.Mutex
}

// New 创建客户端。appid 与 appsecret 必填，qid 为厂商 ID。
//
// 默认使用官方域名、进程内内存 token 缓存与带连接池的默认 HTTP 客户端。
// 构造期不发起任何网络请求，也不启动后台 goroutine。
func New(appid string, qid int64, appsecret string, opts ...Option) (*Client, error) {
	if appid == "" {
		return nil, errors.New("pay360: appid 不能为空")
	}
	if appsecret == "" {
		return nil, errors.New("pay360: appsecret 不能为空")
	}

	c := &Client{
		appid:        appid,
		qid:          qid,
		appsecret:    appsecret,
		baseURL:      defaultBaseURL,
		clock:        time.Now,
		refreshAhead: defaultRefreshAhead,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.http == nil {
		c.http = httpc.New()
	}
	if c.cache == nil {
		c.cache = newMemCache()
	}
	return c, nil
}

// respEnvelope 由各接口响应结构实现，用于把平台错误码转为 *APIError。
type respEnvelope interface {
	toError(headerTid string) error
}

// errnoEnvelope 是多数接口的 {errno,errmsg} 信封。
type errnoEnvelope struct {
	Errno  int    `json:"errno"`
	Errmsg string `json:"errmsg"`
}

func (e errnoEnvelope) toError(tid string) error {
	if e.Errno == 0 {
		return nil
	}
	return &APIError{Code: e.Errno, Msg: e.Errmsg, HeaderTid: tid}
}

// codeEnvelope 是普票红冲等网关接口的 {code,msg} 信封（code 0 表示成功）。
type codeEnvelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e codeEnvelope) toError(tid string) error {
	if e.Code == 0 {
		return nil
	}
	return &APIError{Code: e.Code, Msg: e.Msg, HeaderTid: tid}
}

// errnoOnlyResp 用于无业务数据、仅判定 errno 的接口（退款、代扣、取消签约等）。
type errnoOnlyResp struct {
	errnoEnvelope
}

// rawJSON 暂存 data 字段的原始字节，延迟解析。
//
// 360 在业务错误或无数据时把 data 返回为空字符串 ""（而非对象），用它先吞下任意
// data 形态，可避免业务错误码被 JSON 解码错误掩盖（如“订单不存在”被误报为解码失败）。
type rawJSON []byte

func (r *rawJSON) UnmarshalJSON(b []byte) error {
	*r = append((*r)[:0], b...)
	return nil
}

// decodeData 把 data 原始字节解析到 dst；data 为空字符串/null/空白时视为无数据，dst 保持零值。
func decodeData(raw rawJSON, dst any) error {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" || string(t) == `""` {
		return nil
	}
	return json.Unmarshal(t, dst)
}

// call 执行一次带公共参数与签名的业务请求，解码到 out 并返回 Header-Tid。
//
// 公共参数 appid/timestamp/qid/access_token 由本方法注入，biz 仅含业务参数；
// 值为空的业务参数会被忽略（既不发送也不参与签名）。
func (c *Client) call(ctx context.Context, method, path string, biz map[string]any, out respEnvelope) (string, error) {
	token, err := c.token(ctx)
	if err != nil {
		return "", err
	}

	params := map[string]any{
		"appid":        c.appid,
		"timestamp":    c.clock().Unix(),
		"qid":          c.qid,
		"access_token": token,
	}
	for k, v := range biz {
		// 可选的空字符串字段不发送、不参与签名
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		params[k] = v
	}

	// 签名按字符串拼接（数字转十进制），但请求体保留各字段的 JSON 类型——
	// 360 要求 qid、order_amount 等数字字段以 number 发送，否则报“类型错误”。
	signParams := stringifyForSign(params)
	sign := buildSign(signParams, c.appsecret)

	var tid string
	if method == http.MethodGet {
		signParams["sign"] = sign
		tid, err = c.doRequest(ctx, method, path, c.baseURL+path+"?"+encodeQuery(signParams), nil, out)
	} else {
		params["sign"] = sign
		tid, err = c.doRequest(ctx, method, path, c.baseURL+path, params, out)
	}
	if err != nil {
		return tid, err
	}
	return tid, out.toError(tid)
}

// stringifyForSign 把参数转为签名用的字符串形式：数字按十进制，空字符串跳过。
func stringifyForSign(params map[string]any) map[string]string {
	out := make(map[string]string, len(params))
	for k, v := range params {
		if s := signValue(v); s != "" {
			out[k] = s
		}
	}
	return out
}

// signValue 返回参数值参与签名时的字符串形式。
func signValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	default:
		return fmt.Sprint(x)
	}
}

// doRequest 执行单次 HTTP 请求并返回 Header-Tid。
//
// 它走 httpc 的 [httpc.Client.RequestRawWithHeader]（复用连接池、日志、响应体大小
// 上限），拿到响应头、原始 body 与状态码后自行处理：先判 HTTP 状态——非 2xx 直接
// 报错（避免上游 4xx/5xx 恰好返回零值信封时被误判为成功）；再判空 body；最后用
// gtkit/json 解码。pay360 存在 errno 与 code 两种信封，故解码由本包掌控。
//
// logPath 仅用于错误信息（不含 query），避免把 GET 请求 query 中的 access_token
// 写入日志。
func (c *Client) doRequest(ctx context.Context, method, logPath, fullURL string, body, out any) (string, error) {
	hdr, data, status, err := c.http.RequestRawWithHeader(ctx, method, fullURL, nil, body)
	var tid string
	if hdr != nil {
		tid = hdr.Get(headerTidKey)
	}
	if err != nil {
		return tid, fmt.Errorf("pay360: %s %s: %w", method, logPath, err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return tid, fmt.Errorf("pay360: %s %s: http status %d (header_tid=%s)", method, logPath, status, tid)
	}
	if out != nil {
		if len(bytes.TrimSpace(data)) == 0 {
			return tid, fmt.Errorf("pay360: %s %s: empty response body (status=%d header_tid=%s)", method, logPath, status, tid)
		}
		if err := json.Unmarshal(data, out); err != nil {
			return tid, fmt.Errorf("pay360: %s %s: decode response: %w (header_tid=%s)", method, logPath, err, tid)
		}
	}
	return tid, nil
}

// encodeQuery 把参数编码为 URL query 串。签名在原始值上计算，此处仅做传输编码，
// 服务端解码后重算签名仍一致。
func encodeQuery(params map[string]string) string {
	v := make(url.Values, len(params))
	for k, val := range params {
		v.Set(k, val)
	}
	return v.Encode()
}
