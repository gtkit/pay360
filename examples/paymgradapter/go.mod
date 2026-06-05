// 独立 example 子模块：隔离 go-pay 的微信/支付宝 SDK 重依赖，使 pay360 主包保持轻量。
module github.com/gtkit/pay360/examples/paymgradapter

go 1.26

require (
	github.com/gtkit/go-pay v1.4.2
	github.com/gtkit/json/v2 v2.0.4
	github.com/gtkit/pay360 v0.0.0
)

require (
	github.com/bytedance/gopkg v0.1.4 // indirect
	github.com/bytedance/sonic v1.15.2 // indirect
	github.com/bytedance/sonic/loader v0.5.1 // indirect
	github.com/cloudwego/base64x v0.1.7 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/gtkit/httpc v1.2.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	golang.org/x/arch v0.27.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
)

replace github.com/gtkit/pay360 => ../..
