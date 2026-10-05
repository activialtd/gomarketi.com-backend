package service

import (
	"testing"
	"time"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// TestCheckoutStatusValues pins the two statuses the new flow turns on. They
// travel as plain strings through SQL, the API and both frontends, so a
// rename would compile cleanly here and break at runtime — in the one flow
// where failure means money with no order behind it.
func TestCheckoutStatusValues(t *testing.T) {
	if got := string(dto.OrderStatusAwaitingPayment); got != "awaiting_payment" {
		t.Errorf("awaiting-payment status = %q, want %q", got, "awaiting_payment")
	}
	if got := string(dto.OrderStatusAbandoned); got != "abandoned" {
		t.Errorf("abandoned status = %q, want %q", got, "abandoned")
	}
}

// TestAbandonWindow guards the gap between a buyer stepping away mid-payment
// and the order leaving the live pipeline. Too short and someone who takes a
// phone call finds their order abandoned; too long and the vendor's abandoned
// page is about last week.
func TestAbandonWindow(t *testing.T) {
	if abandonAfter < 15*time.Minute {
		t.Errorf("abandonAfter %s is short enough to catch buyers mid-payment", abandonAfter)
	}
	if abandonAfter > 4*time.Hour {
		t.Errorf("abandonAfter %s leaves unpaid orders in the pipeline too long", abandonAfter)
	}
	if abandonSweepInterval > abandonAfter {
		t.Errorf("sweep every %s cannot keep up with a %s window", abandonSweepInterval, abandonAfter)
	}
}
