package pay360

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gtkit/json/v2"
)

// --- 出站：多笔退款 / 投诉回复 / 投诉完结 ---

func TestRefundOrdersSuccess(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathRefundOrders: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if m["order_id"] != "o1,o2" {
				t.Errorf("order_id 应为逗号拼接串, got %v", m["order_id"])
			}
			writeResp(w, `{"errno":0,"errmsg":"","data":{}}`)
		},
	})
	c := newClient(t, srv)
	tid, err := c.RefundOrders(context.Background(), RefundOrdersRequest{
		OrderIDs: []string{"o1", "o2"}, UserID: "u1", OrderAmount: 9900, RefundReason: "用户申请退款",
	})
	if err != nil || tid != testTid {
		t.Fatalf("tid=%q err=%v", tid, err)
	}
}

func TestRefundOrdersValidation(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit.Store(true)
	}))
	t.Cleanup(srv.Close)
	c, err := New("a", 1, "s", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	valid := RefundOrdersRequest{
		OrderIDs: []string{"o1"}, UserID: "u1", OrderAmount: 1, RefundReason: "r",
	}
	tests := []struct {
		name   string
		mutate func(*RefundOrdersRequest)
	}{
		{"列表为空", func(r *RefundOrdersRequest) { r.OrderIDs = nil }},
		{"含空元素", func(r *RefundOrdersRequest) { r.OrderIDs = []string{"o1", ""} }},
		{"拼接超 500 字符", func(r *RefundOrdersRequest) {
			r.OrderIDs = []string{strings.Repeat("x", 501)}
		}},
		{"user_id 为空", func(r *RefundOrdersRequest) { r.UserID = "" }},
		{"user_id 超 50 字符", func(r *RefundOrdersRequest) { r.UserID = strings.Repeat("u", 51) }},
		{"金额非正", func(r *RefundOrdersRequest) { r.OrderAmount = 0 }},
		{"refund_reason 为空", func(r *RefundOrdersRequest) { r.RefundReason = "" }},
		{"refund_reason 超 200 字符", func(r *RefundOrdersRequest) { r.RefundReason = strings.Repeat("字", 201) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := valid
			tc.mutate(&req)
			if _, err := c.RefundOrders(context.Background(), req); err == nil {
				t.Fatal("应在本地校验阶段报错")
			}
		})
	}
	if hit.Load() {
		t.Fatal("非法参数不应发起请求")
	}
}

func TestComplainReply(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathComplainReply: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if m["complain_no"] != "C1" || m["source"] != float64(ComplainSourceRefund) {
				t.Errorf("body=%v", m)
			}
			writeResp(w, `{"errno":0,"errmsg":"","data":{}}`)
		},
	})
	c := newClient(t, srv)
	tid, err := c.ComplainReply(context.Background(), ComplainReplyRequest{
		ComplainNo: "C1", Content: "已处理", Source: ComplainSourceRefund,
	})
	if err != nil || tid != testTid {
		t.Fatalf("tid=%q err=%v", tid, err)
	}
}

func TestComplainReplyValidation(t *testing.T) {
	c, _ := New("a", 1, "s")
	for name, req := range map[string]ComplainReplyRequest{
		"缺 complain_no":   {Content: "x", Source: 1},
		"content 为空":      {ComplainNo: "C1", Source: 1},
		"content 超 100 字": {ComplainNo: "C1", Content: strings.Repeat("字", 101), Source: 1},
		"source 非法":       {ComplainNo: "C1", Content: "x", Source: 3},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := c.ComplainReply(context.Background(), req); err == nil {
				t.Fatal("应在本地校验阶段报错")
			}
		})
	}
}

func TestComplainReplyLimitSentinel(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathComplainReply: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"errno":10035,"errmsg":"投诉回复次数超限"}`)
		},
	})
	c := newClient(t, srv)
	_, err := c.ComplainReply(context.Background(), ComplainReplyRequest{
		ComplainNo: "C1", Content: "x", Source: ComplainSourceNormal,
	})
	if !errors.Is(err, ErrComplainReplyLimit) {
		t.Fatalf("应返回 ErrComplainReplyLimit, err=%v", err)
	}
}

func TestComplainFinishFixedCode(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathComplainFinish: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if m["code"] != "05" {
				t.Errorf("code 应固定为 \"05\", got %v", m["code"])
			}
			writeResp(w, `{"errno":0,"errmsg":"","data":{}}`)
		},
	})
	c := newClient(t, srv)
	tid, err := c.ComplainFinish(context.Background(), ComplainFinishRequest{
		ComplainNo: "C1", Content: "已完成售后服务",
	})
	if err != nil || tid != testTid {
		t.Fatalf("tid=%q err=%v", tid, err)
	}
}

func TestComplainNotFoundSentinel(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathComplainFinish: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"errno":10034,"errmsg":"投诉不存在"}`)
		},
	})
	c := newClient(t, srv)
	_, err := c.ComplainFinish(context.Background(), ComplainFinishRequest{ComplainNo: "C1", Content: "x"})
	if !errors.Is(err, ErrComplainNotFound) {
		t.Fatalf("应返回 ErrComplainNotFound, err=%v", err)
	}
}

// --- 入站：投诉 webhook ---

// signComplaintBody 按文档规则构造推送体：仅 data 原文与 timestamp 参与签名。
func signComplaintBody(data string, ts int64, key string) string {
	sign := buildSign(map[string]string{
		"data":      data,
		"timestamp": fmt.Sprintf("%d", ts),
	}, key)
	return fmt.Sprintf(`{"data":%s,"timestamp":%d,"sign":%q}`, data, ts, sign)
}

func TestVerifyComplaintWebhook(t *testing.T) {
	c, _ := New("a", 1, "secret1")
	// data 内故意保留空格（非紧凑），验签必须用原文而非重新序列化
	data := `{"message_id":"msg_001", "event_type":"COMPLAINT_CREATED","payload_version":"v1","complain_info":{"qid":10001,"appId":"app_001","complainNo":"C1","currStatus":1,"platForm":3}}`
	body := signComplaintBody(data, 1778745600, "secret1")

	w, err := c.VerifyComplaintWebhook([]byte(body))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if w.MessageID != "msg_001" || w.EventType != ComplaintEventCreated || w.Timestamp != 1778745600 {
		t.Fatalf("w=%+v", w)
	}
	if w.ComplainInfo == nil || w.ComplainInfo.ComplainNo != "C1" ||
		w.ComplainInfo.CurrStatus != ComplainStatusPending || w.ComplainInfo.PlatForm != ComplainPlatformWechat {
		t.Fatalf("complain_info=%+v", w.ComplainInfo)
	}
	if w.ReplyInfo != nil {
		t.Fatal("COMPLAINT_CREATED 不应携带 reply_info")
	}

	// 篡改 data 后应验签失败
	tampered := strings.Replace(body, "msg_001", "msg_002", 1)
	if _, err := c.VerifyComplaintWebhook([]byte(tampered)); !errors.Is(err, ErrComplaintWebhookSign) {
		t.Fatalf("篡改应验签失败, err=%v", err)
	}

	// 缺失 sign 应验签失败
	noSign := fmt.Sprintf(`{"data":%s,"timestamp":1778745600}`, data)
	if _, err := c.VerifyComplaintWebhook([]byte(noSign)); !errors.Is(err, ErrComplaintWebhookSign) {
		t.Fatalf("缺 sign 应验签失败, err=%v", err)
	}
}

func TestVerifyComplaintWebhookVendorKey(t *testing.T) {
	data := `{"message_id":"m1","event_type":"STATUS_CHANGED","payload_version":"v1"}`
	body := signComplaintBody(data, 1778745600, "vendor-key")

	// 配置厂商密钥：以厂商密钥签名的推送验签通过
	withKey, _ := New("a", 1, "secret1", WithVendorKey("vendor-key"))
	if _, err := withKey.VerifyComplaintWebhook([]byte(body)); err != nil {
		t.Fatalf("厂商密钥验签应通过, err=%v", err)
	}
	// 同一推送在未配置厂商密钥（回落 appsecret）的客户端上应验签失败
	noKey, _ := New("a", 1, "secret1")
	if _, err := noKey.VerifyComplaintWebhook([]byte(body)); !errors.Is(err, ErrComplaintWebhookSign) {
		t.Fatalf("appsecret 验签厂商密钥签名应失败, err=%v", err)
	}
	// 未配置厂商密钥时，以 appsecret 签名的推送验签通过
	bySecret := signComplaintBody(data, 1778745600, "secret1")
	if _, err := noKey.VerifyComplaintWebhook([]byte(bySecret)); err != nil {
		t.Fatalf("appsecret 回落验签应通过, err=%v", err)
	}
}

func TestParseComplaintWebhookReplyAdded(t *testing.T) {
	body := `{"data":{"message_id":"m2","event_type":"REPLY_ADDED","payload_version":"v1",
		"complain_info":{"complainNo":"C1","currStatus":2,"replies":[{"complainNo":"C1","operator":"用户","content":"历史回复"}]},
		"reply_info":{"complainNo":"C1","operator":"用户","content":"本次新增回复"}},
		"timestamp":1778745600,"sign":"x"}`
	w, err := ParseComplaintWebhook([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if w.EventType != ComplaintEventReplyAdded || w.ReplyInfo == nil || w.ReplyInfo.Content != "本次新增回复" {
		t.Fatalf("w=%+v reply=%+v", w, w.ReplyInfo)
	}
	if len(w.ComplainInfo.Replies) != 1 || w.ComplainInfo.Replies[0].Content != "历史回复" {
		t.Fatalf("replies=%+v", w.ComplainInfo.Replies)
	}
}

func ExampleClient_VerifyComplaintWebhook() {
	c, _ := New("your-appid", 123456, "your-appsecret")
	http.HandleFunc("/union/complain/callback", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		wh, err := c.VerifyComplaintWebhook(body)
		if err != nil {
			http.Error(w, "invalid sign", http.StatusBadRequest)
			return
		}
		// 按 wh.MessageID 做幂等；建议先落库后返回 2xx，再异步处理
		switch wh.EventType {
		case ComplaintEventCreated, ComplaintEventReplyAdded, ComplaintEventStatusChanged:
		}
		w.WriteHeader(http.StatusOK)
	})
	fmt.Println("registered")
	// Output: registered
}
