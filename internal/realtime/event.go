package realtime

import "time"

type Event struct {
	EventID   string `json:"event_id"`
	Type      string `json:"type"`
	RoomID    int64  `json:"room_id"`
	Timestamp int64  `json:"timestamp"`
	Data      any    `json:"data"`
}

func NewEvent(eventID, typ string, roomID int64, data any) Event {
	return Event{EventID: eventID, Type: typ, RoomID: roomID, Timestamp: time.Now().UnixMilli(), Data: data}
}
