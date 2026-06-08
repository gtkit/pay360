package pay360

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// tokenTTL 为 access_token 的有效期（文档规定 3 小时）。
//
// 过期时间按“收到 token 的本地时刻 + tokenTTL”计算，而非解析响应中的
// expire_time 绝对时间串——后者时区语义文档未明确，本地推算可彻底规避时区偏差，
// 并配合刷新安全边界（[WithTokenRefreshAhead]）保证不会使用临界过期的 token。
const tokenTTL = 3 * time.Hour

// TokenCache 抽象 access_token 的持久化，供多实例部署共享 token。
//
// 默认实现为进程内无锁内存缓存，适用于单实例。多实例（多进程）场景下，
// 由于“新申请 token 会使旧 token 失效”，应注入基于共享存储（如 Redis）的实现，
// 并在刷新处自行加分布式锁或采用单点刷新，避免实例间互相作废 token。
type TokenCache interface {
	// Load 返回当前缓存的 token 及其过期时间；ok 为 false 表示无缓存。
	Load(ctx context.Context) (token string, expireAt time.Time, ok bool, err error)
	// Store 写入新的 token 及其过期时间。
	Store(ctx context.Context, token string, expireAt time.Time) error
}

// tokenSnapshot 为不可变快照，通过原子指针整体替换，保证读取无锁且一致。
type tokenSnapshot struct {
	token    string
	expireAt time.Time
}

// memCache 是 [TokenCache] 的默认进程内实现。
// 读取为无锁原子加载，写入为原子指针替换，可安全并发使用。
type memCache struct {
	p atomic.Pointer[tokenSnapshot]
}

func newMemCache() *memCache { return &memCache{} }

func (m *memCache) Load(context.Context) (string, time.Time, bool, error) {
	s := m.p.Load()
	if s == nil {
		return "", time.Time{}, false, nil
	}
	return s.token, s.expireAt, true, nil
}

func (m *memCache) Store(_ context.Context, token string, expireAt time.Time) error {
	m.p.Store(&tokenSnapshot{token: token, expireAt: expireAt})
	return nil
}

// token 返回有效的 access_token：命中缓存的安全期则无锁直接返回，
// 否则在锁内双重检查后单飞刷新，保证同进程同一时刻最多一次换 token。
func (c *Client) token(ctx context.Context) (string, error) {
	if tok, ok := c.cachedToken(ctx); ok {
		return tok, nil
	}

	return c.refreshToken(ctx, false)
}

// refreshToken 刷新 access_token，并在同进程内单飞去重。
// force 为 false 时会先双重检查缓存；force 为 true 时跳过缓存，直接换新 token。
func (c *Client) refreshToken(ctx context.Context, force bool) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if !force {
		// 双重检查：可能已有其它 goroutine 在等待锁期间完成刷新。
		if tok, ok := c.cachedToken(ctx); ok {
			return tok, nil
		}
	}

	tok, expireAt, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	if err := c.cache.Store(ctx, tok, expireAt); err != nil {
		return "", fmt.Errorf("pay360: store token: %w", err)
	}
	return tok, nil
}

// cachedToken 返回仍处于安全期（剩余有效期大于 refreshAhead）的缓存 token。
func (c *Client) cachedToken(ctx context.Context) (string, bool) {
	tok, expireAt, ok, err := c.cache.Load(ctx)
	if err != nil || !ok {
		return "", false
	}
	if !c.clock().Before(expireAt.Add(-c.refreshAhead)) {
		return "", false
	}
	return tok, true
}

// authResp 为 access_token 接口的响应结构。
//
// 响应中的 expire_time 不参与解析：过期时间统一按本地时刻 + tokenTTL 推算（见 [tokenTTL]）。
type authResp struct {
	errnoEnvelope
	Data struct {
		AccessToken string `json:"access_token"`
	} `json:"data"`
}

// fetchToken 调用 access_token 接口换取新 token。
//
// 鉴权接口不携带 access_token，但需携带 appsecret（既作为请求参数，
// 也作为 sign 的盐值）。过期时间按本地时刻 + tokenTTL 推算。
func (c *Client) fetchToken(ctx context.Context) (string, time.Time, error) {
	now := c.clock()
	params := map[string]any{
		"appid":     c.appid,
		"timestamp": now.Unix(),
		"qid":       c.qid,
		"appsecret": c.appsecret,
	}
	params["sign"] = buildSign(stringifyForSign(params), c.appsecret)

	var resp authResp
	tid, err := c.doRequest(ctx, http.MethodPost, pathAuth, c.baseURL+pathAuth, params, &resp)
	if err != nil {
		return "", time.Time{}, err
	}
	if e := resp.toError(tid); e != nil {
		return "", time.Time{}, e
	}
	if resp.Data.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("pay360: auth response missing access_token (header_tid=%s)", tid)
	}
	return resp.Data.AccessToken, now.Add(tokenTTL), nil
}
