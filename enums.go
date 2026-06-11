package pay360

// 订单状态码（order_status，用于订单查询响应与订单推送回调）。
//
// 其中 20、30、50 均视为支付成功状态（见 [OrderQuery.IsPaid]）。
const (
	OrderStatusPending   = 10 // 待付款（初始状态）
	OrderStatusPaid      = 20 // 付款完成（待通知厂商）
	OrderStatusNotified  = 30 // 待厂商发权益（已通知厂商）
	OrderStatusAfterSale = 40 // 售后中（厂商发起退款）
	OrderStatusCompleted = 50 // 交易完成（厂商已发放）
	OrderStatusCanceled  = 60 // 已取消（支付超时、过期等）
	OrderStatusClosed    = 70 // 交易关闭（退款完成）
)

// isPaidStatus 报告 order_status 是否属于文档定义的支付成功状态（20、30、50）。
func isPaidStatus(status int) bool {
	return status == OrderStatusPaid ||
		status == OrderStatusNotified ||
		status == OrderStatusCompleted
}

// 支付渠道（pay_channel / pay_chanel）。
const (
	PayChannelTask          = -1 // 任务单
	PayChannelWechat        = 1  // 微信
	PayChannelAlipay        = 2  // 支付宝
	PayChannelAlipayAutopay = 3  // 支付宝代扣单
)

// 代扣周期类型（period_type）。
const (
	PeriodTypeDay   = 0 // 扣款周期按天计
	PeriodTypeMonth = 1 // 扣款周期按自然月计
)

// 订单类型（order_pay_type）。
const (
	OrderPayTypeNormal = 0 // 付费单
	OrderPayTypeTask   = 3 // 任务单
)

// 是否开启代扣（createOrder 的 auto_pay_status）。
//
// 注意：与回调 order_extra 中的 auto_pay_status（[AutoPayStatusOpen]/[AutoPayStatusCancel]，
// 表示签约/取消签约）语义不同。
const (
	AutoPayDisabled = 0 // 不开启代扣
	AutoPayEnabled  = 1 // 开启代扣
)

// 代扣发起方（ext.autopay_mode）。
const (
	AutopayModeManager = 0 // 管家侧发起代扣续费订单（需固定扣款周期）
	AutopayModeVendor  = 1 // 厂商侧发起代扣续费订单（厂商完全可控，经服务端代扣接口发起）
)
