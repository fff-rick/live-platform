package realtime

import "fmt"

func RoomStream(roomID int64) string { return fmt.Sprintf("room:%d:stream", roomID) }
func RoomStats(roomID int64) string  { return fmt.Sprintf("room:%d:stats", roomID) }
func Personal(userID int64) string   { return fmt.Sprintf("personal:user#%d", userID) }
