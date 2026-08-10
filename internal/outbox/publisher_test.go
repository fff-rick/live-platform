package outbox

import (
	"testing"
	"time"
)

func TestRetryDelayCaps(t *testing.T) {
	if got := retryDelay(1); got != time.Second {
		t.Fatalf("attempt 1=%s", got)
	}
	if got := retryDelay(3); got != 4*time.Second {
		t.Fatalf("attempt 3=%s", got)
	}
	if got := retryDelay(20); got != 5*time.Minute {
		t.Fatalf("cap=%s", got)
	}
}
