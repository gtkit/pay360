package pay360

import (
	"fmt"
	"testing"

	"github.com/gtkit/json/v2"
)

func validBase() CreateOrderParams {
	return CreateOrderParams{
		OrderID: "o1", OrderAmount: 100, CreateTime: "1700000000",
		UserID: "u1", ProductID: "p1", ProductName: "vip",
	}
}

func TestCreateOrderValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*CreateOrderParams)
		wantErr bool
	}{
		{"基础合法", func(*CreateOrderParams) {}, false},
		{"缺 order_id", func(p *CreateOrderParams) { p.OrderID = "" }, true},
		{"金额非正", func(p *CreateOrderParams) { p.OrderAmount = 0 }, true},
		{"缺 product_name", func(p *CreateOrderParams) { p.ProductName = "" }, true},
		{"开启代扣-合法", func(p *CreateOrderParams) {
			p.AutoPayStatus = AutoPayEnabled
			p.PeriodType = PeriodTypeMonth
			p.Period = 1
			p.ExecuteTime = "2026-07-01"
			p.AutoPayAmount = 100
			p.OrderPayType = OrderPayTypeNormal
			p.AutopayMode = AutopayModeVendor
		}, false},
		{"开启代扣-缺 execute_time", func(p *CreateOrderParams) {
			p.AutoPayStatus = AutoPayEnabled
			p.PeriodType = PeriodTypeMonth
			p.Period = 1
			p.AutoPayAmount = 100
			p.AutopayMode = AutopayModeVendor
		}, true},
		{"开启代扣-period 非正", func(p *CreateOrderParams) {
			p.AutoPayStatus = AutoPayEnabled
			p.PeriodType = PeriodTypeDay
			p.Period = 0
			p.ExecuteTime = "2026-07-01"
			p.AutoPayAmount = 100
			p.AutopayMode = AutopayModeManager
		}, true},
		{"任务单-缺 task_id", func(p *CreateOrderParams) {
			p.AutoPayStatus = AutoPayEnabled
			p.PeriodType = PeriodTypeDay
			p.Period = 30
			p.ExecuteTime = "2026-07-01"
			p.AutoPayAmount = 100
			p.OrderPayType = OrderPayTypeTask
			p.AutopayMode = AutopayModeVendor
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validBase()
			tc.mutate(&p)
			err := p.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestMarshalForSDKNormal(t *testing.T) {
	p := validBase()
	data, err := p.MarshalForSDK()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	// 非代扣不应输出代扣字段
	for _, k := range []string{"auto_pay_status", "period_type", "ext", "order_pay_type", "task_id"} {
		if _, ok := m[k]; ok {
			t.Errorf("非代扣订单不应含字段 %q", k)
		}
	}
	// order_amount 必须是 number 而非字符串
	if _, isStr := m["order_amount"].(string); isStr {
		t.Error("order_amount 不应为字符串")
	}
}

func TestMarshalForSDKAutopay(t *testing.T) {
	p := validBase()
	p.AutoPayStatus = AutoPayEnabled
	p.PeriodType = PeriodTypeMonth
	p.Period = 1
	p.ExecuteTime = "2026-07-01"
	p.AutoPayAmount = 990
	p.OrderPayType = OrderPayTypeTask
	p.TaskID = "task9"
	p.AutopayMode = AutopayModeVendor

	data, err := p.MarshalForSDK()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	ext, ok := m["ext"].(string)
	if !ok || ext != `{"autopay_mode":1}` {
		t.Fatalf("ext 应为 JSON 字符串 {\"autopay_mode\":1}, got %v", m["ext"])
	}
	if m["task_id"] != "task9" {
		t.Errorf("任务单应含 task_id, got %v", m["task_id"])
	}
}

func ExampleCreateOrderParams_MarshalForSDK() {
	p := CreateOrderParams{
		OrderID: "order-1", OrderAmount: 1, CreateTime: "1700000000",
		UserID: "user-1", ProductID: "vip-1", ProductName: "会员月卡",
	}
	data, _ := p.MarshalForSDK()
	fmt.Println(string(data))
	// Output: {"create_time":"1700000000","order_amount":1,"order_id":"order-1","product_id":"vip-1","product_name":"会员月卡","user_id":"user-1"}
}
