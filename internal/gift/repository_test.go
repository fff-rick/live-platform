package gift

import (
	"errors"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestIsDuplicateRequest(t *testing.T) {
	if !isDuplicateRequest(&mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'gift_orders.uk_gift_orders_request_id'"}) {
		t.Fatal("expected request id duplicate")
	}
	if isDuplicateRequest(&mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'gift_orders.uk_gift_orders_order_no'"}) {
		t.Fatal("order number collision must not be treated as idempotent replay")
	}
	if isDuplicateRequest(errors.New("other")) {
		t.Fatal("unexpected duplicate")
	}
}
