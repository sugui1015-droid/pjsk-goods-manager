package export

import (
	"strings"
	"time"
)

// These label mappers translate internal enum values into Chinese text for
// human-facing CSV/Excel exports. Database values and JSON API fields are
// never changed — only the exported display text.

// chinaLocation is a fixed UTC+8 offset, matching the project's existing
// Asia/Shanghai convention (see .env.example TZ=Asia/Shanghai) and the
// frontend's toLocaleString('zh-CN') rendering, which assumes a China-based
// browser clock. A fixed offset is used instead of time.LoadLocation because
// Shanghai has no DST and Windows hosts do not reliably ship IANA tzdata.
var chinaLocation = time.FixedZone("CST", 8*60*60)

// formatDisplayTime converts a backend timestamp (stored and transmitted as
// UTC, formatted like "2026-07-12T15:39:00Z") into a human-readable
// Asia/Shanghai local time string for exports. If the value cannot be
// parsed as RFC3339, it is returned unchanged rather than dropped.
func formatDisplayTime(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return trimmed
	}
	return parsed.In(chinaLocation).Format("2006-01-02 15:04:05")
}

func paymentMethodLabel(value string) string {
	switch value {
	case "alipay":
		return "支付宝"
	case "wechat":
		return "微信"
	case "bank":
		return "银行转账"
	case "cash":
		return "现金"
	case "other":
		return "其他"
	case "":
		return "-"
	default:
		return value
	}
}

func paymentStatusLabel(value string) string {
	switch value {
	case "approved":
		return "已交肾"
	case "voided":
		return "已撤销"
	case "submitted":
		return "待处理"
	case "rejected":
		return "已驳回"
	case "cancelled":
		return "已取消"
	default:
		return value
	}
}

func userStatusLabel(value string) string {
	switch value {
	case "active":
		return "正常"
	case "disabled":
		return "已停用"
	case "merged":
		return "已合并"
	default:
		return value
	}
}

func itemPaymentStatusLabel(value string) string {
	switch value {
	case "unpaid":
		return "未付款"
	case "partial":
		return "部分付款"
	case "paid":
		return "已付款"
	default:
		return value
	}
}
