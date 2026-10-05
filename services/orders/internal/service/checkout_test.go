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

// TestAbandonWindow guards the two ends of the abandoned window. Too short and
// a vendor is chasing someone who is still on the Paystack screen; too long
// and the lead has gone cold by the time they see it. Being marked abandoned
// is not destructive — paying flips the order back — so the window leans short.
func TestAbandonWindow(t *testing.T) {
	if abandonAfter < 5*time.Minute {
		t.Errorf("abandonAfter %s would chase buyers who are still paying", abandonAfter)
	}
	if abandonAfter > time.Hour {
		t.Errorf("abandonAfter %s lets the lead go cold", abandonAfter)
	}
	// The sweep is what makes the window real; checking less often than the
	// window itself would make a 10-minute rule behave like a 20-minute one.
	if abandonSweepInterval > abandonAfter/2 {
		t.Errorf("sweeping every %s cannot honour a %s window", abandonSweepInterval, abandonAfter)
	}
}
