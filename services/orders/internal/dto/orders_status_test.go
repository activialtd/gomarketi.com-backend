package dto

import (
	"testing"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/validator"
)

// TestUpdateOrderStatusReqAllowedValues pins the vendor trust boundary.
//
// A vendor may walk their own order forward to dispatch, but "delivered" is
// the status that releases escrow immediately rather than after the
// seven-day window — it belongs to the buyer's confirm-delivery call and the
// auto-release sweep, never to a vendor's own PATCH. Widening the oneof tag
// by accident would let a vendor pay themselves out the moment they ship.
func TestUpdateOrderStatusReqAllowedValues(t *testing.T) {
	v := validator.New()

	allowed := []OrderStatus{
		OrderStatusConfirmed,
		OrderStatusAtHub,
		OrderStatusShipped,
		OrderStatusCancelled,
	}
	for _, s := range allowed {
		t.Run("allows "+string(s), func(t *testing.T) {
			if err := v.Validate(UpdateOrderStatusReq{Status: s}); err != nil {
				t.Errorf("status %q should be settable by a vendor, got: %v", s, err)
			}
		})
	}

	rejected := []OrderStatus{
		OrderStatusDelivered, // releases escrow — buyer-confirmed only
		OrderStatusPending,   // orders are never moved backwards
		OrderStatus("bogus"),
		OrderStatus(""),
	}
	for _, s := range rejected {
		t.Run("rejects "+string(s), func(t *testing.T) {
			if err := v.Validate(UpdateOrderStatusReq{Status: s}); err == nil {
				t.Errorf("status %q must not be settable by a vendor, but validation passed", s)
			}
		})
	}
}
