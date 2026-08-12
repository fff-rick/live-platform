package realtime

import "time"

type Event struct {
	EventID   string `json:"event_id"`
	Type      string `json:"type"`
	RoomID    int64  `json:"room_id"`
	Priority  string `json:"priority,omitempty"`
	Timestamp int64  `json:"timestamp"`
	Data      any    `json:"data"`
}

func NewEvent(eventID, typ string, roomID int64, data any) Event {
	return NewPriorityEvent(eventID, typ, roomID, "", data)
}

func NewPriorityEvent(eventID, typ string, roomID int64, priority string, data any) Event {
	return Event{EventID: eventID, Type: typ, RoomID: roomID, Priority: priority, Timestamp: time.Now().UnixMilli(), Data: data}
}
