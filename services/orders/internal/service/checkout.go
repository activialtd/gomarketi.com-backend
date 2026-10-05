package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// abandonAfter is how long an unpaid order waits before it is treated as
// abandoned. Long enough to cover a buyer who steps away mid-payment and
// comes back, short enough that a vendor's abandoned list is about today.
const abandonAfter = 45 * time.Minute

// abandonSweepInterval is how often that check runs.
const abandonSweepInterval = 10 * time.Minute

// notifyPaidOrders fires the post-payment side effects: the vendor's dashboard
// event, the customer's receipt, the vendor's alert.
//
// These hang off payment rather than order creation now. An order that is
// never paid for should not put anything in a vendor's inbox, and a buyer
// should not receive a receipt for money that never left their account.
func (s *OrdersService) notifyPaidOrders(orders []dto.OrderResp) {
	for _, o := range orders {
		storeID, err := uuid.Parse(o.StoreID)
		if err != nil {
			continue
		}
		orderID, err := uuid.Parse(o.ID)
		if err != nil {
			continue
		}
		items := make([]dto.CreateOrderItem, 0, len(o.Items))
		for _, it := range o.Items {
			items = append(items, dto.CreateOrderItem{
				ProductID: it.ProductID,
				Name:      it.Name,
				ImageURL:  it.ImageURL,
				Quantity:  it.Quantity,
				PriceKobo: it.PriceKobo,
			})
		}
		slug, name := s.getStoreSlugName(context.Background(), storeID)
		s.notifyOrderCreated(storeID, orderID, o.TotalKobo, o.DeliveryFeeKobo, "",
			o.CustomerName, o.CustomerEmail, "", o.DeliveryAddress, slug, name, items)
	}
}

// StartAbandonedSweepLoop runs until ctx is cancelled, marking unpaid orders
// abandoned so they leave the live pipeline and show up where a vendor can
// act on them.
//
// They are not cancelled: the buyer picked items, filled in their details and
// chose a delivery option. That is a lead, and throwing it away was the reason
// the vendor's abandoned page had nothing in it.
func (s *OrdersService) StartAbandonedSweepLoop(ctx context.Context) {
	ticker := time.NewTicker(abandonSweepInterval)
	defer ticker.Stop()
	s.markAbandonedOrders(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.markAbandonedOrders(ctx)
		}
	}
}

func (s *OrdersService) markAbandonedOrders(ctx context.Context) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE orders
		SET status = 'abandoned', updated_at = NOW()
		WHERE status = 'awaiting_payment'
		  AND created_at < NOW() - $1::interval`,
		abandonAfter.String())
	if err != nil {
		s.log.Warn().Err(err).Msg("abandoned sweep: update failed")
		middleware.RecordBackgroundError(s.db, s.log, "orders", "abandoned sweep failed: "+err.Error(), nil)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.log.Info().Int64("count", n).Msg("marked unpaid orders abandoned")
	}
}
