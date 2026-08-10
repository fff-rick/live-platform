package room

import "time"

type Status string

const (
	StatusPreparing Status = "PREPARING"
	StatusLiving    Status = "LIVING"
	StatusClosed    Status = "CLOSED"
)

type Room struct {
	ID        int64      `json:"room_id"`
	AnchorID  int64      `json:"anchor_id"`
	Title     string     `json:"title"`
	Status    Status     `json:"status"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
