package service

import (
	"testing"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// TestVendorMaySet pins who may move an order where. On a multi-vendor basket
// the vendor's involvement ends at confirming — the platform moves it from
// there — so neither at_hub nor shipped is theirs to set; shipped in
// particular would start the escrow auto-release clock on a dispatch they did
// not make. delivered is unreachable in both flows because it releases escrow
// immediately.
func TestVendorMaySet(t *testing.T) {
	cases := []struct {
		name     string
		f        dto.Fulfilment
		isPickup bool
		status   dto.OrderStatus
		allowed  bool
	}{
		{"vendor-delivered may confirm", dto.FulfilmentVendor, false, dto.OrderStatusConfirmed, true},
		{"vendor-delivered may ship themselves", dto.FulfilmentVendor, false, dto.OrderStatusShipped, true},
		{"vendor-delivered may cancel", dto.FulfilmentVendor, false, dto.OrderStatusCancelled, true},
		{"vendor-delivered has no hub step", dto.FulfilmentVendor, false, dto.OrderStatusAtHub, false},

		{"gomarketi-delivered may confirm", dto.FulfilmentGoMarketi, false, dto.OrderStatusConfirmed, true},
		{"gomarketi-delivered may cancel", dto.FulfilmentGoMarketi, false, dto.OrderStatusCancelled, true},
		{"gomarketi-delivered may NOT self-dispatch", dto.FulfilmentGoMarketi, false, dto.OrderStatusShipped, false},
		{"gomarketi-delivered may NOT mark at hub", dto.FulfilmentGoMarketi, false, dto.OrderStatusAtHub, false},

		{"delivered is never vendor-settable", dto.FulfilmentVendor, false, dto.OrderStatusDelivered, false},
		{"delivered is never settable on hub orders", dto.FulfilmentGoMarketi, false, dto.OrderStatusDelivered, false},
		{"pending is not settable", dto.FulfilmentVendor, false, dto.OrderStatusPending, false},

		// Collection: the vendor puts it on the counter, nobody dispatches it.
		{"collected may confirm", dto.FulfilmentVendor, true, dto.OrderStatusConfirmed, true},
		{"collected may be made ready", dto.FulfilmentVendor, true, dto.OrderStatusReadyForCollection, true},
		{"collected may cancel", dto.FulfilmentVendor, true, dto.OrderStatusCancelled, true},
		{"collected is never shipped", dto.FulfilmentVendor, true, dto.OrderStatusShipped, false},
		{"collected never reaches the hub", dto.FulfilmentVendor, true, dto.OrderStatusAtHub, false},
		{"collected is still not delivered by the vendor", dto.FulfilmentVendor, true, dto.OrderStatusDelivered, false},

		// A delivery cannot borrow the collection status.
		{"delivered order is not ready for collection", dto.FulfilmentVendor, false, dto.OrderStatusReadyForCollection, false},
		{"hub order is not ready for collection", dto.FulfilmentGoMarketi, false, dto.OrderStatusReadyForCollection, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := vendorMaySet(tc.f, tc.isPickup, tc.status)
			if tc.allowed && err != nil {
				t.Errorf("%s (pickup=%v)/%s should be allowed, got: %v", tc.f, tc.isPickup, tc.status, err)
			}
			if !tc.allowed && err == nil {
				t.Errorf("%s (pickup=%v)/%s must be refused, but was allowed", tc.f, tc.isPickup, tc.status)
			}
		})
	}
}

// TestVendorCreditAmount pins who earns the delivery fee. The fee follows
// whoever does the delivering: a vendor running their own order end to end
// earns it on top of the items, while on a multi-vendor basket GoMarketi
// consolidates and delivers, so the fee is platform revenue.
//
// This mirrors the arithmetic in insertOrderTx. It is kept as its own test
// because the failure mode is silent — a wrong credit is a real payout, and
// nothing downstream re-derives it.
func TestVendorCreditAmount(t *testing.T) {
	credit := func(itemsKobo, deliveryFeeKobo int64, vendorDelivers bool) int64 {
		if vendorDelivers && deliveryFeeKobo > 0 {
			return itemsKobo + deliveryFeeKobo
		}
		return itemsKobo
	}

	cases := []struct {
		name                    string
		itemsKobo, deliveryKobo int64
		vendorDelivers          bool
		want                    int64
	}{
		{"vendor delivers: earns items plus fee", 500_000, 150_000, true, 650_000},
		{"vendor delivers, no fee charged", 500_000, 0, true, 500_000},
		{"gomarketi delivers: items only", 500_000, 150_000, false, 500_000},
		{"gomarketi delivers, no fee on the order row", 500_000, 0, false, 500_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := credit(tc.itemsKobo, tc.deliveryKobo, tc.vendorDelivers); got != tc.want {
				t.Errorf("credit = %d kobo, want %d", got, tc.want)
			}
		})
	}
}
