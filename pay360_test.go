package pay360

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gtkit/json/v2"
)

// --- 测试辅助 ---

const testTid = "tid-test-001"

// fakeClock 是可控时间源。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

type fakeRefreshLock struct {
	mu    sync.Mutex
	calls atomic.Int64
	err   error
}

func (l *fakeRefreshLock) Lock(ctx context.Context, fn func(context.Context) error) error {
	l.calls.Add(1)
	if l.err != nil {
		return l.err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return fn(ctx)
}

// writeResp 写出带 Header-Tid 的 JSON 响应。
func writeResp(w http.ResponseWriter, body string) {
	w.Header().Set("Header-Tid", testTid)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// muxServer 启动一个带默认 auth 处理的 httptest 服务，biz 为各业务路径处理器。
func muxServer(t *testing.T, biz map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(pathAuth, func(w http.ResponseWriter, _ *http.Request) {
		writeResp(w, `{"errno":0,"errmsg":"","data":{"access_token":"tok-abc","expire_time":"2026-01-01 03:00:00"},"time":0}`)
	})
	for p, h := range biz {
		mux.HandleFunc(p, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New("appid1", 123456, "secret1", WithBaseURL(srv.URL), WithClock(newFakeClock().now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// --- 签名 ---

func TestBuildSign(t *testing.T) {
	// 明文应为 "appid=a&b=2&c=1" + 盐 "s"，再 md5 小写
	got := buildSign(map[string]string{"appid": "a", "c": "1", "b": "2"}, "s")
	if got != md5hex("appid=a&b=2&c=1s") {
		t.Fatalf("sign=%s", got)
	}
}

func TestBuildSignSkipsEmptyAndSign(t *testing.T) {
	// 空值与 sign 键不参与：明文应为 "a=1" + 盐 "s"
	got := buildSign(map[string]string{"a": "1", "b": "", "sign": "x"}, "s")
	if got != md5hex("a=1s") {
		t.Fatalf("got %s", got)
	}
}

// md5hex 独立计算 md5 小写，用于核对 buildSign 结果。
func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// --- 错误码 ---

func TestAPIErrorIs(t *testing.T) {
	err := &APIError{Code: 10012, Msg: "x", HeaderTid: "t"}
	if !errors.Is(err, ErrAccessToken) {
		t.Fatal("应匹配 ErrAccessToken")
	}
	if errors.Is(err, ErrSign) {
		t.Fatal("不应匹配 ErrSign")
	}
}

// --- memCache 并发读 ---

func TestMemCacheConcurrent(t *testing.T) {
	m := newMemCache()
	_ = m.Store(context.Background(), "tok", time.Now().Add(time.Hour))
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			if tok, _, ok, _ := m.Load(context.Background()); !ok || tok != "tok" {
				t.Errorf("unexpected load")
			}
		})
	}
	wg.Wait()
}

// --- token 单飞 + 提前刷新 ---

func TestTokenSingleFlight(t *testing.T) {
	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc(pathAuth, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond) // 拉长窗口以制造并发
		writeResp(w, `{"errno":0,"data":{"access_token":"tok","expire_time":""}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newClient(t, srv)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := c.token(context.Background()); err != nil {
				t.Errorf("token: %v", err)
			}
		})
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("auth 调用次数=%d, 期望 1", got)
	}
}

func TestTokenRefreshAhead(t *testing.T) {
	var calls atomic.Int64
	clk := newFakeClock()
	mux := http.NewServeMux()
	mux.HandleFunc(pathAuth, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeResp(w, `{"errno":0,"data":{"access_token":"tok","expire_time":""}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, _ := New("a", 1, "s", WithBaseURL(srv.URL), WithClock(clk.now), WithTokenRefreshAhead(5*time.Minute))

	ctx := context.Background()
	mustToken(t, c, ctx)
	clk.advance(2 * time.Hour) // 仍在安全期（expire=+3h，refreshAhead=5m）
	mustToken(t, c, ctx)
	if calls.Load() != 1 {
		t.Fatalf("安全期内不应刷新, calls=%d", calls.Load())
	}
	clk.advance(time.Hour) // 推进到 +3h，越过 expire-5m
	mustToken(t, c, ctx)
	if calls.Load() != 2 {
		t.Fatalf("越过安全边界应刷新, calls=%d", calls.Load())
	}
}

func TestTokenRefreshLockSingleFlightAcrossClients(t *testing.T) {
	var calls atomic.Int64
	sharedCache := newMemCache()
	sharedLock := &fakeRefreshLock{}
	mux := http.NewServeMux()
	mux.HandleFunc(pathAuth, func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		writeResp(w, fmt.Sprintf(`{"errno":0,"data":{"access_token":"tok-%d","expire_time":""}}`, call))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	newLockedClient := func(t *testing.T) *Client {
		t.Helper()
		c, err := New(
			"appid1", 123456, "secret1",
			WithBaseURL(srv.URL),
			WithClock(newFakeClock().now),
			WithTokenCache(sharedCache),
			WithTokenRefreshLock(sharedLock),
		)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c1 := newLockedClient(t)
	c2 := newLockedClient(t)

	var wg sync.WaitGroup
	for _, c := range []*Client{c1, c2} {
		wg.Go(func() {
			if _, err := c.token(context.Background()); err != nil {
				t.Errorf("token: %v", err)
			}
		})
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("auth 调用次数=%d, 期望 1", got)
	}
	if got := sharedLock.calls.Load(); got != 2 {
		t.Fatalf("lock 调用次数=%d, 期望 2", got)
	}
}

func TestTokenRefreshLockError(t *testing.T) {
	c, err := New("a", 1, "s", WithTokenRefreshLock(&fakeRefreshLock{err: errors.New("lock busy")}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pay360: lock token refresh") {
		t.Fatalf("应返回锁错误包装, got %v", err)
	}
}

func mustToken(t *testing.T, c *Client, ctx context.Context) {
	t.Helper()
	if _, err := c.token(ctx); err != nil {
		t.Fatalf("token: %v", err)
	}
}

// --- New 校验与零副作用 ---

func TestNewValidation(t *testing.T) {
	if _, err := New("", 1, "s"); err == nil {
		t.Fatal("空 appid 应报错")
	}
	if _, err := New("a", 0, "s"); err == nil {
		t.Fatal("非法 qid 应报错")
	}
	if _, err := New("a", 1, ""); err == nil {
		t.Fatal("空 appsecret 应报错")
	}
}

func TestNewNoSideEffect(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit.Store(true)
	}))
	t.Cleanup(srv.Close)
	if _, err := New("a", 1, "s", WithBaseURL(srv.URL)); err != nil {
		t.Fatal(err)
	}
	if hit.Load() {
		t.Fatal("构造期不应发起请求")
	}
}

// --- 请求执行：Header-Tid 与错误码 ---

func TestCallHeaderTidAndError(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathOrderRefund: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"errno":10012,"errmsg":"token 错误"}`)
		},
	})
	c := newClient(t, srv)
	tid, err := c.Refund(context.Background(), RefundRequest{
		OrderID: "o1", OrderAmount: 100, UserID: "u1", RefundReason: "test",
	})
	if tid != testTid {
		t.Fatalf("header tid=%q", tid)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 10012 || apiErr.HeaderTid != testTid {
		t.Fatalf("err=%v", err)
	}
	if !errors.Is(err, ErrAccessToken) {
		t.Fatal("应匹配 ErrAccessToken")
	}
}

func TestCallRefreshesTokenOnAccessTokenError(t *testing.T) {
	var authCalls atomic.Int64
	var refundCalls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc(pathAuth, func(w http.ResponseWriter, _ *http.Request) {
		call := authCalls.Add(1)
		writeResp(w, fmt.Sprintf(`{"errno":0,"data":{"access_token":"tok-%d","expire_time":""}}`, call))
	})
	mux.HandleFunc(pathOrderRefund, func(w http.ResponseWriter, r *http.Request) {
		call := refundCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if call == 1 {
			if got["access_token"] != "tok-1" {
				t.Errorf("首次请求 token=%v, 期望 tok-1", got["access_token"])
			}
			writeResp(w, `{"errno":10012,"errmsg":"access_token 错误"}`)
			return
		}
		if got["access_token"] != "tok-2" {
			t.Errorf("重试请求 token=%v, 期望 tok-2", got["access_token"])
		}
		writeResp(w, `{"errno":0,"errmsg":"","data":{}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newClient(t, srv)

	tid, err := c.Refund(context.Background(), RefundRequest{
		OrderID: "o1", OrderAmount: 100, UserID: "u1", RefundReason: "test",
	})
	if err != nil || tid != testTid {
		t.Fatalf("tid=%q err=%v", tid, err)
	}
	if authCalls.Load() != 2 || refundCalls.Load() != 2 {
		t.Fatalf("authCalls=%d refundCalls=%d, 期望均为 2", authCalls.Load(), refundCalls.Load())
	}
}

func TestCallReusesTokenRefreshedByPeerOnAccessTokenError(t *testing.T) {
	var authCalls atomic.Int64
	var refundCalls atomic.Int64
	sharedCache := newMemCache()
	sharedLock := &fakeRefreshLock{}
	mux := http.NewServeMux()
	mux.HandleFunc(pathAuth, func(w http.ResponseWriter, _ *http.Request) {
		call := authCalls.Add(1)
		writeResp(w, fmt.Sprintf(`{"errno":0,"data":{"access_token":"tok-%d","expire_time":""}}`, call))
	})
	mux.HandleFunc(pathOrderRefund, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		call := refundCalls.Add(1)
		if call <= 2 {
			if got["access_token"] != "tok-1" {
				t.Errorf("失效请求 token=%v, 期望 tok-1", got["access_token"])
			}
			writeResp(w, `{"errno":10012,"errmsg":"access_token 错误"}`)
			return
		}
		if got["access_token"] != "tok-2" {
			t.Errorf("重试请求 token=%v, 期望 tok-2", got["access_token"])
		}
		writeResp(w, `{"errno":0,"errmsg":"","data":{}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	newLockedClient := func(t *testing.T) *Client {
		t.Helper()
		c, err := New(
			"appid1", 123456, "secret1",
			WithBaseURL(srv.URL),
			WithClock(newFakeClock().now),
			WithTokenCache(sharedCache),
			WithTokenRefreshLock(sharedLock),
		)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c1 := newLockedClient(t)
	c2 := newLockedClient(t)
	if _, err := c1.token(context.Background()); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for _, c := range []*Client{c1, c2} {
		wg.Go(func() {
			_, err := c.Refund(context.Background(), RefundRequest{
				OrderID: "o1", OrderAmount: 100, UserID: "u1", RefundReason: "test",
			})
			if err != nil {
				t.Errorf("refund: %v", err)
			}
		})
	}
	wg.Wait()
	if got := authCalls.Load(); got != 2 {
		t.Fatalf("auth 调用次数=%d, 期望 2（初始 token + 一次刷新）", got)
	}
	if got := refundCalls.Load(); got != 4 {
		t.Fatalf("refund 调用次数=%d, 期望 4", got)
	}
}

// --- 退款 ---

func TestRefundReasonTooLong(t *testing.T) {
	c, _ := New("a", 1, "s")
	long := make([]rune, 201)
	for i := range long {
		long[i] = '字'
	}
	if _, err := c.Refund(context.Background(), RefundRequest{
		OrderID: "o", UserID: "u", RefundReason: string(long),
	}); err == nil {
		t.Fatal("超长 refund_reason 应报错且不发请求")
	}
}

func TestRefundRejectsNonPositiveAmount(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit.Store(true)
	}))
	t.Cleanup(srv.Close)
	c, err := New("a", 1, "s", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Refund(context.Background(), RefundRequest{
		OrderID: "o", OrderAmount: 0, UserID: "u", RefundReason: "r",
	}); err == nil {
		t.Fatal("非正 order_amount 应报错")
	}
	if hit.Load() {
		t.Fatal("非法参数不应发起请求")
	}
}

func TestRefundSuccessAndSign(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathOrderRefund: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.UseNumber()
			var got map[string]any
			_ = dec.Decode(&got)
			// 服务端按字符串重算签名（与回调验签同逻辑）
			sp, _ := stringifyParams(got)
			sign, _ := got["sign"].(string)
			if sign != buildSign(sp, "secret1") {
				t.Errorf("sign 不一致")
			}
			if got["access_token"] != "tok-abc" {
				t.Errorf("缺少 access_token: %v", got)
			}
			// 数字字段必须以 JSON number 发送，而非字符串
			if _, isStr := got["qid"].(string); isStr {
				t.Errorf("qid 不应以字符串发送")
			}
			if _, isStr := got["order_amount"].(string); isStr {
				t.Errorf("order_amount 不应以字符串发送")
			}
			writeResp(w, `{"errno":0,"errmsg":"","data":{}}`)
		},
	})
	c := newClient(t, srv)
	tid, err := c.Refund(context.Background(), RefundRequest{
		OrderID: "o1", OrderAmount: 100, UserID: "u1", RefundReason: "test",
	})
	if err != nil || tid != testTid {
		t.Fatalf("tid=%q err=%v", tid, err)
	}
}

// --- 订单查询 ---

func TestQueryOrder(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathOrderQuery: func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			params := map[string]string{}
			for k := range q {
				params[k] = q.Get(k)
			}
			if buildSign(params, "secret1") != q.Get("sign") {
				t.Errorf("GET sign 不一致")
			}
			writeResp(w, `{"errno":0,"data":{"mfr_order_id":"m1","order_status":30,"pay_chanel":1}}`)
		},
	})
	c := newClient(t, srv)
	o, err := c.QueryOrder(context.Background(), OrderQueryRequest{OrderID: "o1", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if o.MfrOrderID != "m1" || !o.IsPaid() || o.HeaderTid != testTid {
		t.Fatalf("order=%+v", o)
	}
}

func TestQueryOrderNotFound(t *testing.T) {
	// 真实场景：业务错误时 data 为空字符串，错误码不应被解码错误掩盖。
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathOrderQuery: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"errno":100005,"errmsg":"订单不存在","data":"","time":1,"type":0}`)
		},
	})
	c := newClient(t, srv)
	_, err := c.QueryOrder(context.Background(), OrderQueryRequest{OrderID: "x", UserID: "y"})
	if !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("应返回 ErrOrderNotFound（而非解码错误），err=%v", err)
	}
}

func TestIsPaid(t *testing.T) {
	for _, tc := range []struct {
		status int
		paid   bool
	}{{10, false}, {20, true}, {30, true}, {40, false}, {50, true}, {60, false}, {70, false}} {
		if (OrderQuery{OrderStatus: tc.status}).IsPaid() != tc.paid {
			t.Errorf("OrderQuery status %d: 期望 paid=%v", tc.status, tc.paid)
		}
		if (Callback{OrderStatus: tc.status}).IsPaid() != tc.paid {
			t.Errorf("Callback status %d: 期望 paid=%v", tc.status, tc.paid)
		}
	}
}

// --- 代扣 / 取消签约 ---

func TestCancelSignStatusError(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathAutopayCancelSign: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"errno":100009,"errmsg":"状态有误"}`)
		},
	})
	c := newClient(t, srv)
	_, err := c.CancelSign(context.Background(), CancelSignRequest{OrderID: "o", AgreementNumber: "a"})
	if !errors.Is(err, ErrOrderStatusInvalid) {
		t.Fatalf("应匹配 ErrOrderStatusInvalid, err=%v", err)
	}
}

func TestDoPostSuccess(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathAutopayDoPost: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"errno":0,"data":{}}`)
		},
	})
	c := newClient(t, srv)
	tid, err := c.DoPost(context.Background(), DoPostRequest{
		OrderID: "o", AgreementNumber: "a", AutopayAmount: 100, AutopayOrderID: "ap1",
	})
	if err != nil || tid != testTid {
		t.Fatalf("tid=%q err=%v", tid, err)
	}
}

func TestDoPostRejectsNonPositiveAmount(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit.Store(true)
	}))
	t.Cleanup(srv.Close)
	c, err := New("a", 1, "s", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DoPost(context.Background(), DoPostRequest{
		OrderID: "o", AgreementNumber: "a", AutopayAmount: 0, AutopayOrderID: "ap1",
	}); err == nil {
		t.Fatal("非正 autopay_amount 应报错")
	}
	if hit.Load() {
		t.Fatal("非法参数不应发起请求")
	}
}

// --- 发票 ---

func TestPlainInvoice(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathInvoicePlain: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"errno":0,"data":{"download_url":"u","invoice_no":"123","verify_code":"v"}}`)
		},
	})
	c := newClient(t, srv)
	r, err := c.PlainInvoice(context.Background(), PlainInvoiceRequest{
		OrderID: "o", InvoiceTitle: "t", UserEmail: "e@x.com",
	})
	if err != nil || r.InvoiceNo != "123" || r.DownloadURL != "u" || r.HeaderTid != testTid {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestPlainInvoiceCancelCodeEnvelope(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathInvoicePlainCancel: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"code":0,"msg":"操作成功","data":{}}`)
		},
	})
	c := newClient(t, srv)
	tid, err := c.PlainInvoiceCancel(context.Background(), "o1")
	if err != nil || tid != testTid {
		t.Fatalf("tid=%q err=%v", tid, err)
	}
}

func TestPlainInvoiceCancelCodeError(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathInvoicePlainCancel: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"code":500,"msg":"失败"}`)
		},
	})
	c := newClient(t, srv)
	_, err := c.PlainInvoiceCancel(context.Background(), "o1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 500 {
		t.Fatalf("err=%v", err)
	}
}

func TestSpecialInvoiceFlow(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathInvoiceSpecial: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Errorf("decode special invoice body: %v", err)
			}
			if m["custom_type"] != "1" {
				t.Errorf("custom_type 与文档取值不一致: %v", m["custom_type"])
			}
			writeResp(w, `{"errno":0,"data":{"source_id":"src-1"}}`)
		},
		pathInvoiceSpecialQuery: func(w http.ResponseWriter, _ *http.Request) {
			writeResp(w, `{"errno":0,"data":{"status":"SUCCESS_END","invoice_num":"n1"}}`)
		},
		pathInvoiceSpecialCancel: func(w http.ResponseWriter, r *http.Request) {
			// 以常量组装的请求，线上参数值应与文档取值一致
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Errorf("decode cancel body: %v", err)
			}
			if m["category"] != "1" || m["red_reason"] != "INVOICE_MISTAKE" {
				t.Errorf("红冲参数与文档取值不一致: category=%v red_reason=%v", m["category"], m["red_reason"])
			}
			writeResp(w, `{"errno":0,"data":{}}`)
		},
	})
	c := newClient(t, srv)
	ctx := context.Background()

	r, err := c.SpecialInvoice(ctx, SpecialInvoiceRequest{
		OrderID: "o", InvoiceTitle: "t", UserEmail: "e@x.com", TaxRegisterNo: "TAX123456789012",
		Address: "addr", Phone: "010", BankName: "bank", BankAccount: "acc",
		CustomType: InvoiceCustomTypeEnterprise,
	})
	if err != nil || r.SourceID != "src-1" {
		t.Fatalf("special invoice: r=%+v err=%v", r, err)
	}

	q, err := c.QuerySpecialInvoice(ctx, SpecialInvoiceQueryIssue, "src-1")
	if err != nil || q.Status != "SUCCESS_END" || q.InvoiceNum != "n1" {
		t.Fatalf("query: q=%+v err=%v", q, err)
	}

	if _, err := c.SpecialInvoiceCancel(ctx, SpecialInvoiceCancelRequest{
		Category: InvoiceRedCategorySeller, InvoiceNum: "n1", RedReason: InvoiceRedReasonMistake,
		SourceID: "src-1", OrderID: "o",
	}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
}

func TestQuerySpecialInvoiceRejectsInvalidRequestType(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit.Store(true)
	}))
	t.Cleanup(srv.Close)
	c, err := New("a", 1, "s", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.QuerySpecialInvoice(context.Background(), "3", "src"); err == nil {
		t.Fatal("非法 request_type 应报错")
	}
	if hit.Load() {
		t.Fatal("非法参数不应发起请求")
	}
}

// --- 回调 ---

func TestVerifyCallback(t *testing.T) {
	c, _ := New("a", 1, "secret1")
	params := map[string]string{
		"app_id": "a", "callback_type": "1", "mfr_order_amount": "9900",
		"mfr_order_id": "mo1", "order_status": "30", "qid": "1", "timestamp": "1709266143",
		"order_extra": "",
	}
	sign := buildSign(params, "secret1")
	body := fmt.Sprintf(`{"app_id":"a","callback_type":1,"mfr_order_amount":9900,"mfr_order_id":"mo1","order_status":30,"qid":1,"timestamp":1709266143,"order_extra":"","sign":%q}`, sign)

	cb, err := c.VerifyCallback([]byte(body))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if cb.CallbackType != 1 || cb.MfrOrderID != "mo1" || cb.OrderStatus != 30 {
		t.Fatalf("cb=%+v", cb)
	}
	if !cb.IsPaid() {
		t.Fatal("order_status=30 应判定为支付成功")
	}

	// 篡改后应失败
	bad := fmt.Sprintf(`{"app_id":"y","callback_type":1,"sign":%q}`, sign)
	if _, err := c.VerifyCallback([]byte(bad)); !errors.Is(err, ErrCallbackSign) {
		t.Fatalf("篡改应验签失败, err=%v", err)
	}
}

func TestVerifyCallbackMismatch(t *testing.T) {
	c, _ := New("a", 1, "secret1")

	// 验签通过但 app_id 不一致
	params := map[string]string{"app_id": "other", "callback_type": "1", "qid": "1", "timestamp": "1709266143"}
	body := fmt.Sprintf(`{"app_id":"other","callback_type":1,"qid":1,"timestamp":1709266143,"sign":%q}`, buildSign(params, "secret1"))
	if _, err := c.VerifyCallback([]byte(body)); !errors.Is(err, ErrCallbackMismatch) {
		t.Fatalf("app_id 不一致应返回 ErrCallbackMismatch, err=%v", err)
	}

	// 验签通过但 qid 不一致
	params = map[string]string{"app_id": "a", "callback_type": "1", "qid": "2", "timestamp": "1709266143"}
	body = fmt.Sprintf(`{"app_id":"a","callback_type":1,"qid":2,"timestamp":1709266143,"sign":%q}`, buildSign(params, "secret1"))
	if _, err := c.VerifyCallback([]byte(body)); !errors.Is(err, ErrCallbackMismatch) {
		t.Fatalf("qid 不一致应返回 ErrCallbackMismatch, err=%v", err)
	}

	// 验签失败优先于一致性校验：sign 错误且 app_id 也不一致时报验签失败
	bad := `{"app_id":"other","callback_type":1,"qid":1,"timestamp":1709266143,"sign":"deadbeef"}`
	if _, err := c.VerifyCallback([]byte(bad)); !errors.Is(err, ErrCallbackSign) {
		t.Fatalf("sign 错误应优先返回 ErrCallbackSign, err=%v", err)
	}
}

func TestParseCallbackOrderExtra(t *testing.T) {
	body := `{"callback_type":3,"mfr_order_id":"mo1","order_extra":"{\"mfr_order_id\":\"child1\",\"agreement_number\":\"ag1\",\"auto_pay_status\":2}"}`
	cb, err := ParseCallback([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if cb.CallbackType != CallbackSign || cb.Extra.AgreementNumber != "ag1" || cb.Extra.AutoPayStatus != AutoPayStatusCancel {
		t.Fatalf("cb=%+v", cb)
	}
}

func TestParseCallbackTopLevelSignFields(t *testing.T) {
	// 顶层签约字段与 order_extra 内同名字段独立解析、互不影响。
	body := `{"callback_type":3,"agreement_number":"top-ag","auto_pay_status":1,"order_extra":"{\"agreement_number\":\"ag1\",\"auto_pay_status\":2}"}`
	cb, err := ParseCallback([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if cb.AgreementNumber != "top-ag" || cb.AutoPayStatus != AutoPayStatusOpen {
		t.Fatalf("顶层签约字段解析错误: cb=%+v", cb)
	}
	if cb.Extra.AgreementNumber != "ag1" || cb.Extra.AutoPayStatus != AutoPayStatusCancel {
		t.Fatalf("order_extra 内字段不应被顶层覆盖: extra=%+v", cb.Extra)
	}
}

func TestVerifyCallbackLargeInteger(t *testing.T) {
	// 超过 2^53 的整数字段：若经 float64 中转会被改写导致验签失败。
	c, _ := New("a", 1, "secret1")
	params := map[string]string{
		"app_id":           "a",
		"callback_type":    "1",
		"mfr_order_amount": "9007199254740993",
		"qid":              "1",
		"timestamp":        "1709266143",
	}
	sign := buildSign(params, "secret1")
	body := fmt.Sprintf(`{"app_id":"a","callback_type":1,"mfr_order_amount":9007199254740993,"qid":1,"timestamp":1709266143,"sign":%q}`, sign)
	if _, err := c.VerifyCallback([]byte(body)); err != nil {
		t.Fatalf("大整数应验签通过（保留原始字面量）, err=%v", err)
	}
}

func TestVerifyCallbackRejectsNonScalar(t *testing.T) {
	c, _ := New("a", 1, "secret1")
	// 顶层出现数组类型，无法参与验签，应报错而非臆测
	body := `{"callback_type":1,"weird":[1,2],"sign":"x"}`
	if _, err := c.VerifyCallback([]byte(body)); err == nil {
		t.Fatal("非标量字段应导致验签报错")
	}
}

func TestCallRejectsNon2xx(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathOrderRefund: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Header-Tid", testTid)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{}`) // 零值信封若被解析会误判成功
		},
	})
	c := newClient(t, srv)
	tid, err := c.Refund(context.Background(), RefundRequest{
		OrderID: "o", OrderAmount: 1, UserID: "u", RefundReason: "r",
	})
	if err == nil {
		t.Fatal("非 2xx 应报错而非误判成功")
	}
	if tid != testTid {
		t.Fatalf("应仍返回 header tid, got %q", tid)
	}
}

func TestCallRejectsEmptyBody(t *testing.T) {
	srv := muxServer(t, map[string]http.HandlerFunc{
		pathOrderRefund: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Header-Tid", testTid)
			// 200 但空 body
		},
	})
	c := newClient(t, srv)
	if _, err := c.Refund(context.Background(), RefundRequest{
		OrderID: "o", OrderAmount: 1, UserID: "u", RefundReason: "r",
	}); err == nil {
		t.Fatal("空 body 应报错")
	}
}

// --- Benchmark ---

func BenchmarkBuildSign(b *testing.B) {
	params := map[string]string{
		"appid": "appid1", "timestamp": "1709266143", "qid": "123456",
		"access_token": "tok-abc", "order_id": "o1", "order_amount": "100", "user_id": "u1",
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = buildSign(params, "secret1")
	}
}

// --- Example ---

func ExampleAckSuccess() {
	out, _ := json.Marshal(AckSuccess())
	fmt.Println(string(out))
	// Output: {"code":200,"message":"success","data":""}
}

func ExampleAckResponse() {
	out, _ := json.Marshal(AckResponse(500, "retry later", "job-1"))
	fmt.Println(string(out))
	// Output: {"code":500,"message":"retry later","data":"job-1"}
}

func ExampleParseCallback() {
	body := `{"callback_type":2,"mfr_order_id":"parent1","order_extra":"{\"mfr_order_id\":\"child1\"}"}`
	cb, _ := ParseCallback([]byte(body))
	fmt.Println(cb.CallbackType, cb.Extra.MfrOrderID)
	// Output: 2 child1
}
