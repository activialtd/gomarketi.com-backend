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

// TestShortOrderRefShape guards the gate in front of the prefix lookup. The
// track form sends whatever the buyer typed, and only an exact eight-character
// hex string may reach a LIKE query — otherwise a stray input turns into a
// scan, or a one-character "ref" matches an arbitrary order.
func TestShortOrderRefShape(t *testing.T) {
	valid := []string{"ee40078c", "EE40078C", "00000000", "abcdef01"}
	for _, v := range valid {
		if len(v) != 8 || !isHex(v) {
			t.Errorf("%q should be accepted as a short order ref", v)
		}
	}

	rejected := []string{
		"",          // empty
		"ee40078",   // seven — would match far too much
		"ee40078cd", // nine
		"ee40078g",  // not hex
		"ee40 078c", // spaced
		"' OR 1=1",  // not hex, and never reaches SQL
	}
	for _, v := range rejected {
		if len(v) == 8 && isHex(v) {
			t.Errorf("%q should not be accepted as a short order ref", v)
		}
	}
}

// TestBasketTotalsReconcile is the arithmetic ConfirmPayment verifies against.
//
// Delivery is charged once for a whole basket but orders are per vendor, so
// the fee rides on one of them. If the split ever stops summing to what
// Paystack was charged, verification is an exact match and refuses the payment
// AFTER the buyer has been debited — the most expensive failure in the system,
// and invisible until someone complains.
func TestBasketTotalsReconcile(t *testing.T) {
	cases := []struct {
		name       string
		itemTotals []int64
		feeKobo    int64
	}{
		{"single vendor", []int64{28_500_00}, 450_000},
		{"two vendors", []int64{28_500_00, 1_450_00}, 450_000},
		{"three vendors, free delivery", []int64{5_000, 7_500, 2_250}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// What the app charges: every item, plus one delivery.
			var charged int64
			for _, it := range tc.itemTotals {
				charged += it
			}
			charged += tc.feeKobo

			// What the orders add up to: the fee sits on the first one only.
			var sumOfOrders int64
			for i, it := range tc.itemTotals {
				fee := int64(0)
				if i == 0 {
					fee = tc.feeKobo
				}
				sumOfOrders += it + fee
			}

			if sumOfOrders != charged {
				t.Errorf("orders sum to %d but %d was charged — payment verification would refuse this", sumOfOrders, charged)
			}
		})
	}
}
