package gift

import (
	"strings"
	"time"
)

// messageID 返回礼物在历史接口和实时消息中共享的业务消息 ID。
// Outbox event_id 只标识一次投递事件，不能替代礼物订单本身的身份。
func messageID(orderNo string) string {
	return "gift:" + strings.TrimSpace(orderNo)
}

type Gift struct {
	ID     int64  `json:"gift_id"`
	Name   string `json:"name"`
	Price  int64  `json:"price"`
	Status int8   `json:"status"`
}

type Order struct {
	OrderNo     string    `json:"order_no"`
	RequestID   string    `json:"request_id,omitempty"`
	UserID      int64     `json:"user_id"`
	AnchorID    int64     `json:"anchor_id"`
	RoomID      int64     `json:"room_id"`
	GiftID      int64     `json:"gift_id"`
	GiftName    string    `json:"gift_name"`
	GiftCount   int64     `json:"gift_count"`
	UnitPrice   int64     `json:"unit_price"`
	TotalAmount int64     `json:"total_amount"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type SendResult struct {
	Order            Order `json:"order"`
	IdempotentReplay bool  `json:"idempotent_replay"`
	EventQueued      bool  `json:"event_queued"`
}
