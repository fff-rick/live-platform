package room

import "time"

type Status string

const (
	StatusPreparing Status = "PREPARING"
	StatusLiving    Status = "LIVING"
	StatusClosed    Status = "CLOSED"
)

type Room struct {
	ID             int64      `json:"room_id"`
	AnchorID       int64      `json:"anchor_id"`
	AnchorNickname string     `json:"anchor_nickname,omitempty"`
	Title          string     `json:"title"`
	Status         Status     `json:"status"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// RoomAccess 汇总房间状态与用户治理状态，供互动请求一次完成准入判断。
type RoomAccess struct {
	Room   Room `json:"room"`
	Banned bool `json:"banned"`
	Muted  bool `json:"muted"`
}

type Mute struct {
	UserID     int64      `json:"user_id"`
	Nickname   string     `json:"nickname"`
	MutedUntil *time.Time `json:"muted_until,omitempty"`
	Reason     string     `json:"reason"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type Ban struct {
	UserID    int64     `json:"user_id"`
	Nickname  string    `json:"nickname"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
