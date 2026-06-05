package pay360

import (
	"strings"
	"time"

	"github.com/gtkit/httpc"
)

// Option 配置 [Client]。
type Option func(*Client)

// WithHTTPClient 注入自定义的 httpc 客户端（用于自定义超时、传输等）。
// 如需请求日志，可在构造该客户端时通过 httpc.WithTransport 注入带日志的 RoundTripper。
func WithHTTPClient(h *httpc.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithTokenCache 注入自定义的 access_token 缓存实现（如基于 Redis 的多实例共享）。
func WithTokenCache(tc TokenCache) Option {
	return func(c *Client) {
		if tc != nil {
			c.cache = tc
		}
	}
}

// WithBaseURL 覆盖接口域名（默认 https://api.openstore.360.cn）。主要用于测试。
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = strings.TrimRight(u, "/")
		}
	}
}

// WithTokenRefreshAhead 设置 token 提前刷新的安全边界（默认 5 分钟）。
// 剩余有效期不足该值时，下一次取 token 会触发刷新。
func WithTokenRefreshAhead(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.refreshAhead = d
		}
	}
}

// WithClock 注入时间源，主要用于测试中控制 token 过期与 timestamp。
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.clock = now
		}
	}
}
