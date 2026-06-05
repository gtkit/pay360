//go:build livetest

// 真实环境联调测试，仅在 -tags livetest 时编译。凭据通过环境变量传入，不入库。
package pay360

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	appid := os.Getenv("PAY360_APPID")
	secret := os.Getenv("PAY360_APPSECRET")
	qid, err := strconv.ParseInt(os.Getenv("PAY360_QID"), 10, 64)
	if appid == "" || secret == "" || err != nil {
		t.Skip("缺少 PAY360_APPID / PAY360_QID / PAY360_APPSECRET 环境变量")
	}
	c, err := New(appid, qid, secret)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mask(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// TestLiveAuth 验证：签名规则、appsecret 位置、公共数字参数（qid/timestamp）以字符串发送是否被 360 接受。
func TestLiveAuth(t *testing.T) {
	c := liveClient(t)
	tok, err := c.token(context.Background())
	if err != nil {
		t.Fatalf("❌ 换 access_token 失败: %v", err)
	}
	t.Logf("✅ 换 access_token 成功: token=%s", mask(tok))
}

// TestLiveQueryOrder 用一个不存在的订单验证 GET 签名与鉴权链路（无副作用）。
// 预期返回“订单不存在”等业务错误，而非签名/鉴权错误。
func TestLiveQueryOrder(t *testing.T) {
	c := liveClient(t)
	o, err := c.QueryOrder(context.Background(), OrderQueryRequest{
		OrderID: "livetest-nonexistent-order",
		UserID:  "livetest-user",
	})
	t.Logf("订单查询: header_tid=%s err=%v", o.HeaderTid, err)
	switch {
	case errors.Is(err, ErrSign):
		t.Fatalf("❌ 签名被拒绝(10006)：签名规则需修正")
	case errors.Is(err, ErrAccessToken):
		t.Fatalf("❌ access_token 被拒绝(10012)：鉴权链路有问题")
	case errors.Is(err, ErrParam) || errors.Is(err, ErrParamType):
		t.Fatalf("❌ 参数错误(%v)：可能是数字字段类型或参数格式问题", err)
	default:
		t.Logf("✅ 签名与鉴权链路通畅（业务结果: %+v）", o)
	}
}
