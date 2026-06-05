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

// TestLiveAuth 验证：签名规则、appsecret 位置、公共数字参数以 number 发送是否被接受。
func TestLiveAuth(t *testing.T) {
	c := liveClient(t)
	tok, err := c.token(context.Background())
	if err != nil {
		t.Fatalf("❌ 换 access_token 失败: %v", err)
	}
	t.Logf("✅ 换 access_token 成功: token=%s", mask(tok))
}

// TestLiveQueryOrder 用不存在的订单验证 GET 签名与鉴权链路。
func TestLiveQueryOrder(t *testing.T) {
	c := liveClient(t)
	o, err := c.QueryOrder(context.Background(), OrderQueryRequest{
		OrderID: "livetest-nonexistent-order",
		UserID:  "livetest-user",
	})
	t.Logf("订单查询: header_tid=%s err=%v", o.HeaderTid, err)
	classify(t, "query_order", err)
}

// classify 判定探测结果：签名/参数/鉴权错误视为缺陷，业务错误视为“格式已被接受”。
func classify(t *testing.T, name string, err error) {
	t.Helper()
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case 10006:
			t.Errorf("❌ %s: 签名错误(10006) %q —— 签名规则不被接受", name, apiErr.Msg)
		case 10012:
			t.Errorf("❌ %s: access_token 错误(10012) —— 鉴权链路问题", name)
		case 10014, 10015:
			t.Errorf("❌ %s: 访问/Content-Type 错误(%d) %q —— 与数据值无关，需修", name, apiErr.Code, apiErr.Msg)
		case 10001, 10016:
			// 参数错误可能是字段值校验（如 bank_account 须纯数字）而非格式/类型问题，
			// 探测假数据本身可能触发，需看 msg 人工判断。
			t.Logf("⚠️ %s: 参数错误(%d) %q —— 请看 msg 区分“类型问题”还是“字段值校验”", name, apiErr.Code, apiErr.Msg)
		default:
			t.Logf("✅ %s: 业务错误 code=%d msg=%q（格式/签名/鉴权已被接受）", name, apiErr.Code, apiErr.Msg)
		}
		return
	}
	if err == nil {
		t.Logf("⚠️ %s: 无错误返回（无效参数却成功，需留意）", name)
		return
	}
	t.Logf("⚠️ %s: 非业务错误（响应格式可能与假设不同）: %v", name, err)
}

// TestLiveProbe 对所有出站接口用无效/不存在参数做格式+签名探测（无副作用）。
func TestLiveProbe(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	const fake = "pay360-probe-nonexistent"

	tid, err := c.Refund(ctx, RefundRequest{OrderID: fake, OrderAmount: 1, UserID: "probe", RefundReason: "probe"})
	t.Logf("[refund] tid=%s", tid)
	classify(t, "refund", err)

	tid, err = c.DoPost(ctx, DoPostRequest{OrderID: fake, AgreementNumber: fake, AutopayAmount: 1, AutopayOrderID: fake})
	t.Logf("[dopost] tid=%s", tid)
	classify(t, "dopost", err)

	tid, err = c.CancelSign(ctx, CancelSignRequest{OrderID: fake, AgreementNumber: fake})
	t.Logf("[cancel_sign] tid=%s", tid)
	classify(t, "cancel_sign", err)

	pi, err := c.PlainInvoice(ctx, PlainInvoiceRequest{OrderID: fake, InvoiceTitle: "probe", UserEmail: "probe@example.com"})
	t.Logf("[plain_invoice] tid=%s", pi.HeaderTid)
	classify(t, "plain_invoice", err)

	tid, err = c.PlainInvoiceCancel(ctx, fake)
	t.Logf("[plain_invoice_cancel] tid=%s", tid)
	classify(t, "plain_invoice_cancel", err)

	si, err := c.SpecialInvoice(ctx, SpecialInvoiceRequest{
		OrderID: fake, InvoiceTitle: "probe", UserEmail: "probe@example.com",
		TaxRegisterNo: "91110000123456789X", Address: "addr", Phone: "13800000000",
		BankName: "bank", BankAccount: "6222021234567890", CustomType: "1",
	})
	t.Logf("[special_invoice] tid=%s", si.HeaderTid)
	classify(t, "special_invoice", err)

	sq, err := c.QuerySpecialInvoice(ctx, SpecialInvoiceQueryIssue, fake)
	t.Logf("[query_special] tid=%s", sq.HeaderTid)
	classify(t, "query_special", err)

	tid, err = c.SpecialInvoiceCancel(ctx, SpecialInvoiceCancelRequest{
		Category: "1", InvoiceNum: fake, RedReason: "INVOICE_MISTAKE", SourceID: fake, OrderID: fake,
	})
	t.Logf("[special_invoice_cancel] tid=%s", tid)
	classify(t, "special_invoice_cancel", err)
}
