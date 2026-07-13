package export

import "testing"

func TestPaymentMethodLabel(t *testing.T) {
	cases := map[string]string{
		"alipay":  "支付宝",
		"wechat":  "微信",
		"bank":    "银行转账",
		"cash":    "现金",
		"other":   "其他",
		"":        "-",
		"unknown": "unknown",
	}
	for input, want := range cases {
		if got := paymentMethodLabel(input); got != want {
			t.Fatalf("paymentMethodLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPaymentStatusLabel(t *testing.T) {
	cases := map[string]string{
		"approved":  "已交肾",
		"voided":    "已撤销",
		"submitted": "待处理",
		"rejected":  "已驳回",
		"cancelled": "已取消",
		"unknown":   "unknown",
	}
	for input, want := range cases {
		if got := paymentStatusLabel(input); got != want {
			t.Fatalf("paymentStatusLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUserStatusLabel(t *testing.T) {
	cases := map[string]string{
		"active":   "正常",
		"disabled": "已停用",
		"merged":   "已合并",
		"unknown":  "unknown",
	}
	for input, want := range cases {
		if got := userStatusLabel(input); got != want {
			t.Fatalf("userStatusLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestItemPaymentStatusLabel(t *testing.T) {
	cases := map[string]string{
		"unpaid":  "未付款",
		"partial": "部分付款",
		"paid":    "已付款",
		"unknown": "unknown",
	}
	for input, want := range cases {
		if got := itemPaymentStatusLabel(input); got != want {
			t.Fatalf("itemPaymentStatusLabel(%q) = %q, want %q", input, got, want)
		}
	}
}
