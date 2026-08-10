package realtime

import "testing"

func TestChannels(t *testing.T) {
	if got := RoomStream(12); got != "room:12:stream" {
		t.Fatal(got)
	}
	if got := RoomStats(12); got != "room:12:stats" {
		t.Fatal(got)
	}
	if got := Personal(42); got != "personal:user#42" {
		t.Fatal(got)
	}
}
