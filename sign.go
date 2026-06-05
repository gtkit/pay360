package pay360

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"sort"
	"strings"
)

// buildSign 按 360 规则生成 sign:
//
//  1. 剔除 key 为 "sign" 的项；
//  2. 跳过值为空字符串的项；
//  3. 其余参数按 key 升序，以 "key=value" 用 "&" 拼接；
//  4. 末尾直接拼接 appsecret（无分隔符）；
//  5. 对结果取 md5，输出十六进制小写。
//
// 不修改传入的 params。
func buildSign(params map[string]string, appsecret string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	b.WriteString(appsecret)

	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// signEqual 以恒定时间比较两个签名，避免时序侧信道。
func signEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
