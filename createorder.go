package pay360

import (
	"fmt"

	"github.com/gtkit/json/v2"
)

// CreateOrderParams 是前端 SDK360.createOrder 所需的订单参数。
//
// 360 联运的下单由前端 JSSDK 直连完成，但订单数据应由厂商服务端生成唯一 order_id、
// 组装并留存后下发给前端。本结构提供类型化构造、条件校验与序列化，不发起任何请求。
//
// 代扣字段（AutoPayStatus 起的一组）仅当开启代扣（AutoPayStatus == [AutoPayEnabled]）时
// 生效且必填，校验由 [CreateOrderParams.Validate] 强制。
//
// 任务单（OrderPayType == [OrderPayTypeTask]）独立于代扣：不开启代扣亦可构造，
// 此时 TaskID 必填，且 OrderAmount 允许为 0（依据文档任务示例 task_amount 为 0）。
type CreateOrderParams struct {
	OrderID     string // 厂商订单号，需保证应用内唯一
	OrderAmount int64  // 订单金额，单位：分
	CreateTime  string // 厂商创建订单的时间戳（10 位，秒级）
	UserID      string // 用户 ID
	ProductID   string // 商品 ID
	ProductName string // 商品描述

	AutoPayStatus int    // 是否开启代扣，见 AutoPayDisabled / AutoPayEnabled
	OrderPayType  int    // 订单类型，见 OrderPayTypeNormal / OrderPayTypeTask
	PeriodType    int    // 代扣周期类型，见 PeriodTypeDay / PeriodTypeMonth
	Period        int    // 代扣周期值（与 PeriodType 组合，如 PeriodTypeDay+90 表示 90 天）
	ExecuteTime   string // 首次扣款时间，格式 yyyy-MM-dd
	AutoPayAmount int64  // 代扣金额，单位：分
	AutopayMode   int    // 代扣发起方，见 AutopayModeManager / AutopayModeVendor
	TaskID        string // 任务 ID，OrderPayType == OrderPayTypeTask 时必填（不要求开启代扣）
}

// Validate 校验参数。基础字段与订单类型恒校验；任务单强制 task_id 且允许金额为 0；
// 开启代扣时强制校验代扣相关字段。
func (p CreateOrderParams) Validate() error {
	if p.OrderID == "" || p.CreateTime == "" || p.UserID == "" || p.ProductID == "" || p.ProductName == "" {
		return fmt.Errorf("pay360: create order: order_id/create_time/user_id/product_id/product_name 均为必填")
	}
	if p.OrderPayType != OrderPayTypeNormal && p.OrderPayType != OrderPayTypeTask {
		return fmt.Errorf("pay360: create order: order_pay_type 必须为 0(付费单) 或 3(任务单)")
	}
	if p.OrderPayType == OrderPayTypeTask {
		if p.TaskID == "" {
			return fmt.Errorf("pay360: create order: 任务单（order_pay_type=3）必须提供 task_id")
		}
		if p.OrderAmount < 0 {
			return fmt.Errorf("pay360: create order: 任务单 order_amount 不能为负（单位：分）")
		}
	} else if p.OrderAmount <= 0 {
		return fmt.Errorf("pay360: create order: order_amount 必须为正（单位：分）")
	}
	if p.AutoPayStatus != AutoPayEnabled {
		return nil
	}
	switch {
	case p.PeriodType != PeriodTypeDay && p.PeriodType != PeriodTypeMonth:
		return fmt.Errorf("pay360: create order: 开启代扣时 period_type 必须为 0(日) 或 1(月)")
	case p.Period <= 0:
		return fmt.Errorf("pay360: create order: 开启代扣时 period 必须为正")
	case p.ExecuteTime == "":
		return fmt.Errorf("pay360: create order: 开启代扣时 execute_time 必填（yyyy-MM-dd）")
	case p.AutoPayAmount <= 0:
		return fmt.Errorf("pay360: create order: 开启代扣时 auto_pay_amount 必须为正")
	case p.AutopayMode != AutopayModeManager && p.AutopayMode != AutopayModeVendor:
		return fmt.Errorf("pay360: create order: autopay_mode 必须为 0(管家侧) 或 1(厂商侧)")
	}
	return nil
}

// MarshalForSDK 校验并序列化为前端 SDK360.createOrder 所需的 JSON。
//
// 非代扣付费单只输出基础字段；任务单无论是否开启代扣均附带 order_pay_type 与 task_id；
// 开启代扣时附带代扣字段及 ext（autopay_mode 的 JSON 字符串）。
// 数字字段以 JSON number 输出，与 360 前端约定一致。
func (p CreateOrderParams) MarshalForSDK() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	m := map[string]any{
		"order_id":     p.OrderID,
		"order_amount": p.OrderAmount,
		"create_time":  p.CreateTime,
		"user_id":      p.UserID,
		"product_id":   p.ProductID,
		"product_name": p.ProductName,
	}
	if p.AutoPayStatus == AutoPayEnabled {
		ext, err := json.Marshal(map[string]int{"autopay_mode": p.AutopayMode})
		if err != nil {
			return nil, fmt.Errorf("pay360: create order: marshal ext: %w", err)
		}
		m["auto_pay_status"] = p.AutoPayStatus
		m["period_type"] = p.PeriodType
		m["period"] = p.Period
		m["execute_time"] = p.ExecuteTime
		m["auto_pay_amount"] = p.AutoPayAmount
		m["order_pay_type"] = p.OrderPayType
		m["ext"] = string(ext)
	}
	if p.OrderPayType == OrderPayTypeTask {
		m["order_pay_type"] = p.OrderPayType
		m["task_id"] = p.TaskID
	}
	return json.Marshal(m)
}
