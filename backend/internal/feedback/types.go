// Package feedback implements the lightweight plain-text user feedback MVP.
// It is deliberately independent from payments, orders, security, messaging,
// and notifications.
package feedback

import (
	"errors"
	"time"
)

const (
	StatusNew       = "new"
	StatusProcessed = "processed"
	MaxContentRunes = 1000
	DefaultPageSize = 25
	MaxPageSize     = 200
	MaxJSONBodySize = 8 << 10
)

var (
	ErrInvalidContent = errors.New("反馈内容不能为空，且不能超过1000字")
	ErrInvalidStatus  = errors.New("反馈状态无效")
	ErrDuplicate      = errors.New("请勿重复提交相同反馈")
	ErrNotFound       = errors.New("未找到该反馈")
)

type Feedback struct {
	ID          string    `json:"id"`
	CNCode      string    `json:"cn_code,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	Content     string    `json:"content"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type ListFilter struct {
	Status   string
	Page     int
	PageSize int
}

type ListResponse struct {
	Items      []Feedback `json:"items"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	Total      int        `json:"total"`
	TotalPages int        `json:"total_pages"`
}
