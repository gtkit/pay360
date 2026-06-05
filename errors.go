package pay360

import "fmt"

// APIError 表示 360 平台返回的业务错误（errno 非 0，或部分网关接口的 code 非 0）。
//
// 可用 [errors.Is] 与本包导出的错误码哨兵比较，例如:
//
//	if errors.Is(err, pay360.ErrAccessToken) { /* token 失效，触发重试 */ }
//
// HeaderTid 为本次响应头 Header-Tid 的值，便于向 360 反馈问题时快速定位。
type APIError struct {
	Code      int
	Msg       string
	HeaderTid string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("pay360: api error code=%d msg=%q header_tid=%s", e.Code, e.Msg, e.HeaderTid)
}

// Is 仅按错误码判等，忽略 Msg 与 HeaderTid，使哨兵值可用于 [errors.Is]。
func (e *APIError) Is(target error) bool {
	t, ok := target.(*APIError)
	return ok && t.Code == e.Code
}

// 文档《360联运JSSDK集成说明》第四章错误码表对应的哨兵值。
// 仅携带 Code，用于 errors.Is 判定。
var (
	ErrParam               = &APIError{Code: 10001, Msg: "参数错误"}
	ErrInternal            = &APIError{Code: 10002, Msg: "服务内部错误"}
	ErrSign                = &APIError{Code: 10006, Msg: "sign 错误"}
	ErrRequestExpired      = &APIError{Code: 10007, Msg: "请求过期"}
	ErrAccessToken         = &APIError{Code: 10012, Msg: "access_token 错误"}
	ErrIllegalAccess       = &APIError{Code: 10014, Msg: "非法访问"}
	ErrContentType         = &APIError{Code: 10015, Msg: "不支持的 content_type"}
	ErrParamType           = &APIError{Code: 10016, Msg: "参数类型错误"}
	ErrAppNotFound         = &APIError{Code: 100001, Msg: "应用信息不存在"}
	ErrAppOffline          = &APIError{Code: 100002, Msg: "应用信息已下线"}
	ErrVendorNotFound      = &APIError{Code: 100003, Msg: "厂商信息不存在"}
	ErrVendorOffline       = &APIError{Code: 100004, Msg: "厂商信息已下线"}
	ErrOrderNotFound       = &APIError{Code: 100005, Msg: "订单不存在"}
	ErrAppCategoryNotFound = &APIError{Code: 100006, Msg: "应用分类不存在"}
	ErrAuthInfo            = &APIError{Code: 100008, Msg: "认证信息错误"}
	ErrOrderStatusInvalid  = &APIError{Code: 100009, Msg: "订单状态有误，不允许进行此操作"}
)
