package wallet

import "time"

type Balance struct {
	UserID  int64 `json:"user_id"`
	Balance int64 `json:"balance"`
}

type Transaction struct {
	TransactionNo string    `json:"transaction_no"`
	UserID        int64     `json:"user_id"`
	BizType       string    `json:"biz_type"`
	BizID         string    `json:"biz_id"`
	Amount        int64     `json:"amount"`
	BalanceBefore int64     `json:"balance_before"`
	BalanceAfter  int64     `json:"balance_after"`
	CreatedAt     time.Time `json:"created_at"`
}
