package service

import (
	"testing"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// TestVendorMaySet pins who may move an order where. The two rules that cost
// real money if they slip: a vendor on a GoMarketi-delivered basket must not
// be able to claim a dispatch they do not perform (it starts the escrow
// auto-release clock), and delivered must be unreachable either way (it
// releases escrow immediately).
func TestVendorMaySet(t *testing.T) {
	cases := []struct {
		name    string
		f       dto.Fulfilment
		status  dto.OrderStatus
		allowed bool
	}{
		{"vendor-delivered may confirm", dto.FulfilmentVendor, dto.OrderStatusConfirmed, true},
		{"vendor-delivered may ship themselves", dto.FulfilmentVendor, dto.OrderStatusShipped, true},
		{"vendor-delivered may cancel", dto.FulfilmentVendor, dto.OrderStatusCancelled, true},
		{"vendor-delivered has no hub step", dto.FulfilmentVendor, dto.OrderStatusAtHub, false},

		{"gomarketi-delivered may confirm", dto.FulfilmentGoMarketi, dto.OrderStatusConfirmed, true},
		{"gomarketi-delivered may hand to hub", dto.FulfilmentGoMarketi, dto.OrderStatusAtHub, true},
		{"gomarketi-delivered may cancel", dto.FulfilmentGoMarketi, dto.OrderStatusCancelled, true},
		{"gomarketi-delivered may NOT self-dispatch", dto.FulfilmentGoMarketi, dto.OrderStatusShipped, false},

		{"delivered is never vendor-settable", dto.FulfilmentVendor, dto.OrderStatusDelivered, false},
		{"delivered is never settable on hub orders", dto.FulfilmentGoMarketi, dto.OrderStatusDelivered, false},
		{"pending is not settable", dto.FulfilmentVendor, dto.OrderStatusPending, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := vendorMaySet(tc.f, tc.status)
			if tc.allowed && err != nil {
				t.Errorf("%s/%s should be allowed, got: %v", tc.f, tc.status, err)
			}
			if !tc.allowed && err == nil {
				t.Errorf("%s/%s must be refused, but was allowed", tc.f, tc.status)
			}
		})
	}
}
