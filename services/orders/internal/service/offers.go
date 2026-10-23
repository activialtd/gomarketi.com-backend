package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/email"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/sse"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Price offers: a buyer haggling over a negotiable product, Jiji-style.
//
// The vendor sets a range when they mark a product negotiable — the list
// price buyers see, and a private floor. An offer at or above the floor is a
// deal on the spot; anything lower goes to the vendor, who can accept,
// counter or decline, and a counter goes back to the buyer. Once agreed, the
// buyer checks out at that price: checkout reads the deal from here rather
// than trusting the price the client sends (see priceItems).

const (
	// offerWindow is how long an open offer waits for the other side, and how
	// long an agreed price is held for the buyer. Long enough to cover a
	// vendor who checks their phone once a day; short enough that a deal
	// does not outlive the vendor's stock or mood.
	offerWindow = 48 * time.Hour

	// maxBuyerOffers caps how many offers a buyer makes in one negotiation —
	// the opening one and two comebacks. Past that, it is a price, not a
	// conversation.
	maxBuyerOffers = 3

	// minOfferShare is the lowest offer worth sending, as a share of the list
	// price. Below half, the vendor is only being asked to say no.
	minOfferShare = 0.5

	// dailyOffersPerProduct limits how often one buyer can start over on one
	// product. Without it a buyer could withdraw and re-offer upward until one
	// is accepted instantly, and read the floor off the result.
	dailyOffersPerProduct = 3

	offerSweepInterval = 10 * time.Minute
)

// offerColumns is the shared SELECT list for every query that scans into
// offerRow. The store and product joins supply the buyer-facing store name
// and the vendor's current floor.
const offerColumns = `o.id, o.store_id, o.product_id, o.product_name, o.product_image,
	o.list_price_kobo, o.offer_kobo, o.counter_kobo, o.agreed_kobo, o.quantity, o.status,
	o.auto_accepted, o.buyer_offers, o.customer_name, o.customer_email, o.customer_phone,
	o.message, o.expires_at, o.created_at, o.updated_at,
	COALESCE(st.slug, '') AS store_slug, COALESCE(st.name, '') AS store_name,
	p.min_price_kobo`

const offerFrom = ` FROM price_offers o
	LEFT JOIN stores st ON st.id = o.store_id
	LEFT JOIN products p ON p.id = o.product_id`

type offerRow struct {
	ID            uuid.UUID      `db:"id"`
	StoreID       uuid.UUID      `db:"store_id"`
	ProductID     uuid.UUID      `db:"product_id"`
	ProductName   string         `db:"product_name"`
	ProductImage  sql.NullString `db:"product_image"`
	ListPriceKobo int64          `db:"list_price_kobo"`
	OfferKobo     int64          `db:"offer_kobo"`
	CounterKobo   sql.NullInt64  `db:"counter_kobo"`
	AgreedKobo    sql.NullInt64  `db:"agreed_kobo"`
	Quantity      int32          `db:"quantity"`
	Status        string         `db:"status"`
	AutoAccepted  bool           `db:"auto_accepted"`
	BuyerOffers   int            `db:"buyer_offers"`
	CustomerName  string         `db:"customer_name"`
	CustomerEmail string         `db:"customer_email"`
	CustomerPhone sql.NullString `db:"customer_phone"`
	Message       sql.NullString `db:"message"`
	ExpiresAt     time.Time      `db:"expires_at"`
	CreatedAt     time.Time      `db:"created_at"`
	UpdatedAt     time.Time      `db:"updated_at"`
	StoreSlug     string         `db:"store_slug"`
	StoreName     string         `db:"store_name"`
	MinPriceKobo  sql.NullInt64  `db:"min_price_kobo"`
}

// open reports whether the negotiation is still waiting on someone.
func (r offerRow) open() bool {
	return r.Status == string(dto.OfferPending) || r.Status == string(dto.OfferCountered)
}

// lapsed is true for an open offer or a deal whose window has passed but the
// sweep has not reached yet. Treated exactly as expired, so the outcome never
// depends on when the sweep last ran.
func (r offerRow) lapsed(now time.Time) bool {
	return (r.open() || r.Status == string(dto.OfferAccepted)) && now.After(r.ExpiresAt)
}

// rowToOffer is the buyer's view. forVendor adds the floor.
func rowToOffer(r offerRow, forVendor bool) dto.OfferResp {
	o := dto.OfferResp{
		ID:              r.ID.String(),
		StoreID:         r.StoreID.String(),
		StoreSlug:       r.StoreSlug,
		StoreName:       r.StoreName,
		ProductID:       r.ProductID.String(),
		ProductName:     r.ProductName,
		ProductImage:    nullStrPtr(r.ProductImage),
		ListPriceKobo:   r.ListPriceKobo,
		OfferKobo:       r.OfferKobo,
		Quantity:        r.Quantity,
		Status:          dto.OfferStatus(r.Status),
		AutoAccepted:    r.AutoAccepted,
		BuyerOffersLeft: max(0, maxBuyerOffers-r.BuyerOffers),
		CustomerName:    r.CustomerName,
		CustomerEmail:   r.CustomerEmail,
		CustomerPhone:   nullStrPtr(r.CustomerPhone),
		Message:         nullStrPtr(r.Message),
		ExpiresAt:       r.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt:       r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       r.UpdatedAt.UTC().Format(time.RFC3339),
		Events:          []dto.OfferEvent{},
	}
	if r.CounterKobo.Valid {
		o.CounterKobo = &r.CounterKobo.Int64
	}
	if r.AgreedKobo.Valid {
		o.AgreedKobo = &r.AgreedKobo.Int64
	}
	// Shown as expired straight away rather than waiting on the sweep.
	if r.lapsed(time.Now()) {
		o.Status = dto.OfferExpired
	}
	if forVendor && r.MinPriceKobo.Valid {
		floor := r.MinPriceKobo.Int64
		below := r.OfferKobo < floor
		o.MinPriceKobo = &floor
		o.BelowFloor = &below
	}
	return o
}

// ── Buyer token ───────────────────────────────────────────────────────────────

// offerTokenKey signs buyer tokens. INTERNAL_API_KEY is already a shared
// secret on every deployed service, so offers need no new configuration;
// OFFER_TOKEN_SECRET overrides it when set.
func offerTokenKey() []byte {
	if k := os.Getenv("OFFER_TOKEN_SECRET"); k != "" {
		return []byte(k)
	}
	if k := os.Getenv("INTERNAL_API_KEY"); k != "" {
		return []byte(k)
	}
	return []byte("gomarketi-dev-offer-token")
}

// OfferToken is the guest buyer's key to one negotiation. Derived rather than
// stored, so every email about the offer can carry a working link.
func OfferToken(offerID uuid.UUID) string {
	mac := hmac.New(sha256.New, offerTokenKey())
	mac.Write([]byte("offer:" + offerID.String()))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

func validOfferToken(offerID uuid.UUID, token string) bool {
	return token != "" && hmac.Equal([]byte(OfferToken(offerID)), []byte(token))
}

// ── Reads ─────────────────────────────────────────────────────────────────────

func (s *OrdersService) getOfferRow(ctx context.Context, q sqlx.QueryerContext, offerID uuid.UUID, lock bool) (offerRow, error) {
	query := `SELECT ` + offerColumns + offerFrom + ` WHERE o.id = $1`
	if lock {
		query += ` FOR UPDATE OF o`
	}
	var r offerRow
	err := sqlx.GetContext(ctx, q, &r, query, offerID)
	if errors.Is(err, sql.ErrNoRows) {
		return offerRow{}, apperrors.NotFound("offer not found")
	}
	if err != nil {
		return offerRow{}, fmt.Errorf("get offer: %w", err)
	}
	return r, nil
}

// withEvents fills each offer's conversation in one query.
func (s *OrdersService) withEvents(ctx context.Context, offers []dto.OfferResp) error {
	if len(offers) == 0 {
		return nil
	}
	ids := make([]string, len(offers))
	byID := make(map[string]int, len(offers))
	for i, o := range offers {
		ids[i] = o.ID
		byID[o.ID] = i
	}
	rows, err := s.db.QueryxContext(ctx, `
		SELECT offer_id, actor, action, amount_kobo, message, created_at
		FROM price_offer_events
		WHERE offer_id = ANY($1::uuid[])
		ORDER BY created_at, id`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("load offer events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			offerID   string
			actor     string
			action    string
			amount    sql.NullInt64
			message   sql.NullString
			createdAt time.Time
		)
		if err := rows.Scan(&offerID, &actor, &action, &amount, &message, &createdAt); err != nil {
			return err
		}
		ev := dto.OfferEvent{
			Actor:     actor,
			Action:    action,
			Message:   nullStrPtr(message),
			CreatedAt: createdAt.UTC().Format(time.RFC3339),
		}
		if amount.Valid {
			ev.AmountKobo = &amount.Int64
		}
		i := byID[offerID]
		offers[i].Events = append(offers[i].Events, ev)
	}
	return rows.Err()
}

func (s *OrdersService) loadOffer(ctx context.Context, offerID uuid.UUID, forVendor bool) (dto.OfferResp, error) {
	r, err := s.getOfferRow(ctx, s.db, offerID, false)
	if err != nil {
		return dto.OfferResp{}, err
	}
	out := []dto.OfferResp{rowToOffer(r, forVendor)}
	if err := s.withEvents(ctx, out); err != nil {
		return dto.OfferResp{}, err
	}
	return out[0], nil
}

// GetOfferForBuyer is a guest's read of their negotiation, by token.
func (s *OrdersService) GetOfferForBuyer(ctx context.Context, offerID uuid.UUID, token string) (dto.OfferResp, error) {
	if !validOfferToken(offerID, token) {
		// Not found rather than forbidden: a wrong token should not confirm
		// that the offer exists.
		return dto.OfferResp{}, apperrors.NotFound("offer not found")
	}
	return s.loadOffer(ctx, offerID, false)
}

// ListMyOffers is a signed-in buyer's negotiations across every store,
// matched on their account email for the same reason GetMyOrders is.
func (s *OrdersService) ListMyOffers(ctx context.Context, userID uuid.UUID) (dto.MyOffersResp, error) {
	userEmail, err := s.resolveUserEmail(ctx, userID)
	if err != nil {
		return dto.MyOffersResp{}, err
	}
	var rows []offerRow
	if err := sqlx.SelectContext(ctx, s.db, &rows, `SELECT `+offerColumns+offerFrom+`
		WHERE LOWER(o.customer_email) = LOWER($1)
		ORDER BY o.updated_at DESC
		LIMIT 100`, userEmail); err != nil {
		return dto.MyOffersResp{}, fmt.Errorf("list my offers: %w", err)
	}
	out := make([]dto.OfferResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToOffer(r, false))
	}
	if err := s.withEvents(ctx, out); err != nil {
		return dto.MyOffersResp{}, err
	}
	return dto.MyOffersResp{Offers: out}, nil
}

// offerStatusFilter maps an inbox tab to the statuses behind it.
var offerStatusFilter = map[string][]string{
	"pending":   {"pending"},
	"countered": {"countered"},
	"accepted":  {"accepted"},
	"closed":    {"declined", "expired", "withdrawn", "redeemed"},
}

// ListStoreOffers is the vendor's offers inbox, one tab at a time, with the
// count behind every tab so the inbox can badge them all.
func (s *OrdersService) ListStoreOffers(ctx context.Context, storeID uuid.UUID, tab string, page, perPage int) (dto.OfferListResp, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	// An offer the sweep has not reached yet still reads as open in the
	// table. Expire it now so the tab counts agree with what the cards say.
	s.expireOffers(ctx, &storeID)

	where := ` WHERE o.store_id = $1`
	args := []any{storeID}
	if statuses, ok := offerStatusFilter[tab]; ok {
		where += ` AND o.status = ANY($2::text[])`
		args = append(args, pq.Array(statuses))
	} else if tab != "" && tab != "all" {
		return dto.OfferListResp{}, apperrors.BadRequest("status must be pending, countered, accepted, closed or all")
	}

	var total int64
	if err := s.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM price_offers o`+where, args...); err != nil {
		return dto.OfferListResp{}, fmt.Errorf("count offers: %w", err)
	}

	var rows []offerRow
	n := len(args)
	if err := sqlx.SelectContext(ctx, s.db, &rows, `SELECT `+offerColumns+offerFrom+where+
		fmt.Sprintf(` ORDER BY o.updated_at DESC LIMIT $%d OFFSET $%d`, n+1, n+2),
		append(args, perPage, (page-1)*perPage)...); err != nil {
		return dto.OfferListResp{}, fmt.Errorf("list offers: %w", err)
	}
	offers := make([]dto.OfferResp, 0, len(rows))
	for _, r := range rows {
		offers = append(offers, rowToOffer(r, true))
	}
	if err := s.withEvents(ctx, offers); err != nil {
		return dto.OfferListResp{}, err
	}

	var counts dto.OfferCounts
	if err := s.db.GetContext(ctx, &counts, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending')   AS pending,
			COUNT(*) FILTER (WHERE status = 'countered') AS countered,
			COUNT(*) FILTER (WHERE status = 'accepted')  AS accepted,
			COUNT(*) FILTER (WHERE status IN ('declined','expired','withdrawn','redeemed')) AS closed
		FROM price_offers WHERE store_id = $1`, storeID); err != nil {
		return dto.OfferListResp{}, fmt.Errorf("count offers by status: %w", err)
	}

	return dto.OfferListResp{Offers: offers, Total: total, Page: page, PerPage: perPage, Counts: counts}, nil
}

// ── Making an offer ───────────────────────────────────────────────────────────

type negotiableProduct struct {
	StoreID      uuid.UUID      `db:"store_id"`
	Name         string         `db:"name"`
	Image        sql.NullString `db:"image"`
	PriceKobo    int64          `db:"price_kobo"`
	IsNegotiable bool           `db:"is_negotiable"`
	IsPublished  bool           `db:"is_published"`
	MinPriceKobo sql.NullInt64  `db:"min_price_kobo"`
}

// clearsFloor is the instant-deal rule. A product with no floor (one marked
// negotiable before floors were required) never auto-accepts.
func (p negotiableProduct) clearsFloor(amount int64) bool {
	return p.MinPriceKobo.Valid && amount >= p.MinPriceKobo.Int64
}

func (s *OrdersService) getNegotiableProduct(ctx context.Context, q sqlx.QueryerContext, productID uuid.UUID) (negotiableProduct, error) {
	var p negotiableProduct
	err := sqlx.GetContext(ctx, q, &p, `
		SELECT store_id, name, images[1] AS image, price_kobo, is_negotiable, is_published, min_price_kobo
		FROM products WHERE id = $1`, productID)
	if errors.Is(err, sql.ErrNoRows) {
		return negotiableProduct{}, apperrors.NotFound("product not found")
	}
	if err != nil {
		return negotiableProduct{}, fmt.Errorf("get product: %w", err)
	}
	return p, nil
}

// CreateOffer opens a negotiation. An offer at or above the vendor's floor is
// accepted on the spot — the vendor already said yes to that price when they
// set the range — so the buyer gets an answer while still on the page.
func (s *OrdersService) CreateOffer(ctx context.Context, req dto.CreateOfferReq) (dto.CreateOfferResp, error) {
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		return dto.CreateOfferResp{}, apperrors.BadRequest("invalid product_id")
	}
	product, err := s.getNegotiableProduct(ctx, s.db, productID)
	if err != nil {
		return dto.CreateOfferResp{}, err
	}
	if !product.IsPublished {
		return dto.CreateOfferResp{}, apperrors.NotFound("product not found")
	}
	if !product.IsNegotiable {
		return dto.CreateOfferResp{}, apperrors.BadRequest("this item has a fixed price")
	}
	if req.AmountKobo >= product.PriceKobo {
		return dto.CreateOfferResp{}, apperrors.BadRequest("that's the full price or more — add it to your cart instead")
	}
	if float64(req.AmountKobo) < float64(product.PriceKobo)*minOfferShare {
		return dto.CreateOfferResp{}, apperrors.BadRequest(fmt.Sprintf(
			"offers under half the price aren't sent — try %s or more", fmtNairaPlain(int64(float64(product.PriceKobo)*minOfferShare))))
	}
	quantity := req.Quantity
	if quantity == 0 {
		quantity = 1
	}
	customerEmail := strings.TrimSpace(req.CustomerEmail)

	var recent int
	if err := s.db.GetContext(ctx, &recent, `
		SELECT COUNT(*) FROM price_offers
		WHERE product_id = $1 AND LOWER(customer_email) = LOWER($2)
		  AND created_at > NOW() - INTERVAL '24 hours'`, productID, customerEmail); err != nil {
		return dto.CreateOfferResp{}, fmt.Errorf("count recent offers: %w", err)
	}
	if recent >= dailyOffersPerProduct {
		return dto.CreateOfferResp{}, apperrors.TooManyRequests("you've made several offers on this item today — try again tomorrow")
	}

	instant := product.clearsFloor(req.AmountKobo)
	status := dto.OfferPending
	var agreed *int64
	if instant {
		status = dto.OfferAccepted
		agreed = &req.AmountKobo
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return dto.CreateOfferResp{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var offerID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		INSERT INTO price_offers (store_id, product_id, product_name, product_image, list_price_kobo,
			offer_kobo, agreed_kobo, quantity, status, auto_accepted,
			customer_name, customer_email, customer_phone, message, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),NULLIF($14,''),$15)
		RETURNING id`,
		product.StoreID, productID, product.Name, product.Image, product.PriceKobo,
		req.AmountKobo, agreed, quantity, status, instant,
		strings.TrimSpace(req.CustomerName), customerEmail, strings.TrimSpace(req.CustomerPhone),
		strings.TrimSpace(req.Message), time.Now().Add(offerWindow),
	).Scan(&offerID)
	if isUniqueViolation(err) {
		return dto.CreateOfferResp{}, apperrors.Conflict("you already have an open offer on this item — see My offers")
	}
	if err != nil {
		return dto.CreateOfferResp{}, fmt.Errorf("insert offer: %w", err)
	}
	if err := addOfferEvent(ctx, tx, offerID, "buyer", "offer", &req.AmountKobo, strings.TrimSpace(req.Message)); err != nil {
		return dto.CreateOfferResp{}, err
	}
	if instant {
		if err := addOfferEvent(ctx, tx, offerID, "system", "accept", &req.AmountKobo, ""); err != nil {
			return dto.CreateOfferResp{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return dto.CreateOfferResp{}, fmt.Errorf("commit: %w", err)
	}

	offer, err := s.loadOffer(ctx, offerID, false)
	if err != nil {
		return dto.CreateOfferResp{}, err
	}

	kind := email.VendorOfferNew
	if instant {
		kind = email.VendorOfferInstant
	}
	s.publishOffer(offer, "offer_created")
	s.mailVendorAboutOffer(offer, offer.OfferKobo, kind, strings.TrimSpace(req.Message))

	return dto.CreateOfferResp{Offer: offer, BuyerToken: OfferToken(offerID)}, nil
}

func addOfferEvent(ctx context.Context, tx *sqlx.Tx, offerID uuid.UUID, actor, action string, amount *int64, message string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO price_offer_events (offer_id, actor, action, amount_kobo, message)
		VALUES ($1,$2,$3,$4,NULLIF($5,''))`,
		offerID, actor, action, amount, message); err != nil {
		return fmt.Errorf("record offer event: %w", err)
	}
	return nil
}

// ── Moves ─────────────────────────────────────────────────────────────────────

// RespondAsBuyer is a guest buyer's move, authorised by token.
func (s *OrdersService) RespondAsBuyer(ctx context.Context, offerID uuid.UUID, token string, req dto.RespondOfferReq) (dto.OfferResp, error) {
	if !validOfferToken(offerID, token) {
		return dto.OfferResp{}, apperrors.NotFound("offer not found")
	}
	return s.buyerMove(ctx, offerID, "", req)
}

// RespondAsSignedInBuyer is the same move from the consumer app, authorised
// by the account's email matching the offer's.
func (s *OrdersService) RespondAsSignedInBuyer(ctx context.Context, userID, offerID uuid.UUID, req dto.RespondOfferReq) (dto.OfferResp, error) {
	userEmail, err := s.resolveUserEmail(ctx, userID)
	if err != nil {
		return dto.OfferResp{}, err
	}
	return s.buyerMove(ctx, offerID, userEmail, req)
}

// buyerMove answers the vendor's counter, comes back with a new offer, or
// walks away. requireEmail, when set, must match the offer's buyer.
func (s *OrdersService) buyerMove(ctx context.Context, offerID uuid.UUID, requireEmail string, req dto.RespondOfferReq) (dto.OfferResp, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return dto.OfferResp{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	r, err := s.getOfferRow(ctx, tx, offerID, true)
	if err != nil {
		return dto.OfferResp{}, err
	}
	if requireEmail != "" && !strings.EqualFold(requireEmail, r.CustomerEmail) {
		return dto.OfferResp{}, apperrors.NotFound("offer not found")
	}
	if r.lapsed(time.Now()) {
		return dto.OfferResp{}, apperrors.BadRequest("this offer has expired")
	}

	countered := r.Status == string(dto.OfferCountered)
	var (
		vendorMail string // which email the vendor gets, if any
		mailAmount int64
	)
	switch req.Action {
	case "accept":
		if !countered || !r.CounterKobo.Valid {
			return dto.OfferResp{}, apperrors.BadRequest("there's no counter-offer to accept")
		}
		agreed := r.CounterKobo.Int64
		if err := settleOffer(ctx, tx, offerID, dto.OfferAccepted, &agreed); err != nil {
			return dto.OfferResp{}, err
		}
		if err := addOfferEvent(ctx, tx, offerID, "buyer", "accept", &agreed, req.Message); err != nil {
			return dto.OfferResp{}, err
		}
		vendorMail, mailAmount = email.VendorOfferAccepted, agreed

	case "decline":
		if !countered {
			return dto.OfferResp{}, apperrors.BadRequest("there's no counter-offer to decline — withdraw your offer instead")
		}
		if err := settleOffer(ctx, tx, offerID, dto.OfferDeclined, nil); err != nil {
			return dto.OfferResp{}, err
		}
		if err := addOfferEvent(ctx, tx, offerID, "buyer", "decline", nil, req.Message); err != nil {
			return dto.OfferResp{}, err
		}

	case "counter":
		if !countered || !r.CounterKobo.Valid {
			return dto.OfferResp{}, apperrors.BadRequest("wait for the seller to reply before making another offer")
		}
		if r.BuyerOffers >= maxBuyerOffers {
			return dto.OfferResp{}, apperrors.BadRequest("you've used all your offers on this item — accept or decline the seller's price")
		}
		if req.AmountKobo <= r.OfferKobo || req.AmountKobo >= r.CounterKobo.Int64 {
			return dto.OfferResp{}, apperrors.BadRequest(fmt.Sprintf(
				"a new offer has to be above your last one (%s) and below the seller's %s",
				fmtNairaPlain(r.OfferKobo), fmtNairaPlain(r.CounterKobo.Int64)))
		}
		product, err := s.getNegotiableProduct(ctx, tx, r.ProductID)
		if err != nil {
			return dto.OfferResp{}, err
		}
		instant := product.clearsFloor(req.AmountKobo)
		status, agreed := dto.OfferPending, (*int64)(nil)
		if instant {
			status, agreed = dto.OfferAccepted, &req.AmountKobo
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE price_offers
			SET offer_kobo = $2, buyer_offers = buyer_offers + 1, status = $3, agreed_kobo = $4,
			    auto_accepted = $5, expires_at = $6, updated_at = NOW()
			WHERE id = $1`,
			offerID, req.AmountKobo, status, agreed, instant, time.Now().Add(offerWindow)); err != nil {
			return dto.OfferResp{}, fmt.Errorf("counter offer: %w", err)
		}
		if err := addOfferEvent(ctx, tx, offerID, "buyer", "offer", &req.AmountKobo, req.Message); err != nil {
			return dto.OfferResp{}, err
		}
		if instant {
			if err := addOfferEvent(ctx, tx, offerID, "system", "accept", &req.AmountKobo, ""); err != nil {
				return dto.OfferResp{}, err
			}
			vendorMail = email.VendorOfferInstant
		} else {
			vendorMail = email.VendorOfferNew
		}
		mailAmount = req.AmountKobo

	case "withdraw":
		if !r.open() {
			return dto.OfferResp{}, apperrors.BadRequest("this offer is already closed")
		}
		if err := settleOffer(ctx, tx, offerID, dto.OfferWithdrawn, nil); err != nil {
			return dto.OfferResp{}, err
		}
		if err := addOfferEvent(ctx, tx, offerID, "buyer", "withdraw", nil, req.Message); err != nil {
			return dto.OfferResp{}, err
		}

	default:
		return dto.OfferResp{}, apperrors.BadRequest("action must be accept, counter, decline or withdraw")
	}

	if err := tx.Commit(); err != nil {
		return dto.OfferResp{}, fmt.Errorf("commit: %w", err)
	}

	offer, err := s.loadOffer(ctx, offerID, false)
	if err != nil {
		return dto.OfferResp{}, err
	}
	s.publishOffer(offer, "offer_updated")
	if vendorMail != "" {
		s.mailVendorAboutOffer(offer, mailAmount, vendorMail, strings.TrimSpace(req.Message))
	}
	return offer, nil
}

// RespondAsVendor is the vendor's move on a buyer's offer: take it, counter,
// or turn it down.
func (s *OrdersService) RespondAsVendor(ctx context.Context, storeID, offerID uuid.UUID, req dto.RespondOfferReq) (dto.OfferResp, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return dto.OfferResp{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	r, err := s.getOfferRow(ctx, tx, offerID, true)
	if err != nil {
		return dto.OfferResp{}, err
	}
	if r.StoreID != storeID {
		return dto.OfferResp{}, apperrors.NotFound("offer not found")
	}
	if r.lapsed(time.Now()) {
		return dto.OfferResp{}, apperrors.BadRequest("this offer has expired")
	}
	pending := r.Status == string(dto.OfferPending)

	var mailAmount int64
	switch req.Action {
	case "accept":
		if !pending {
			return dto.OfferResp{}, apperrors.BadRequest("this offer isn't waiting on you")
		}
		agreed := r.OfferKobo
		if err := settleOffer(ctx, tx, offerID, dto.OfferAccepted, &agreed); err != nil {
			return dto.OfferResp{}, err
		}
		if err := addOfferEvent(ctx, tx, offerID, "vendor", "accept", &agreed, req.Message); err != nil {
			return dto.OfferResp{}, err
		}
		mailAmount = agreed

	case "counter":
		if !pending {
			return dto.OfferResp{}, apperrors.BadRequest("this offer isn't waiting on you")
		}
		if req.AmountKobo <= r.OfferKobo || req.AmountKobo >= r.ListPriceKobo {
			return dto.OfferResp{}, apperrors.BadRequest(fmt.Sprintf(
				"a counter-offer has to be above the buyer's %s and below your price of %s",
				fmtNairaPlain(r.OfferKobo), fmtNairaPlain(r.ListPriceKobo)))
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE price_offers
			SET counter_kobo = $2, status = 'countered', expires_at = $3, updated_at = NOW()
			WHERE id = $1`, offerID, req.AmountKobo, time.Now().Add(offerWindow)); err != nil {
			return dto.OfferResp{}, fmt.Errorf("counter offer: %w", err)
		}
		if err := addOfferEvent(ctx, tx, offerID, "vendor", "counter", &req.AmountKobo, req.Message); err != nil {
			return dto.OfferResp{}, err
		}
		mailAmount = req.AmountKobo

	case "decline":
		if !r.open() {
			return dto.OfferResp{}, apperrors.BadRequest("this offer is already closed")
		}
		if err := settleOffer(ctx, tx, offerID, dto.OfferDeclined, nil); err != nil {
			return dto.OfferResp{}, err
		}
		if err := addOfferEvent(ctx, tx, offerID, "vendor", "decline", nil, req.Message); err != nil {
			return dto.OfferResp{}, err
		}
		mailAmount = r.OfferKobo

	default:
		return dto.OfferResp{}, apperrors.BadRequest("action must be accept, counter or decline")
	}

	if err := tx.Commit(); err != nil {
		return dto.OfferResp{}, fmt.Errorf("commit: %w", err)
	}

	offer, err := s.loadOffer(ctx, offerID, true)
	if err != nil {
		return dto.OfferResp{}, err
	}
	s.publishOffer(offer, "offer_updated")
	s.mailBuyerAboutOffer(offer, req.Action, mailAmount, strings.TrimSpace(req.Message))
	return offer, nil
}

// settleOffer moves an offer to accepted or a closed status. An accepted deal
// gets a fresh window: the buyer has that long to check out at the price.
func settleOffer(ctx context.Context, tx *sqlx.Tx, offerID uuid.UUID, status dto.OfferStatus, agreed *int64) error {
	expires := sql.NullTime{}
	if status == dto.OfferAccepted {
		expires = sql.NullTime{Time: time.Now().Add(offerWindow), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE price_offers
		SET status = $2,
		    agreed_kobo = COALESCE($3, agreed_kobo),
		    expires_at = COALESCE($4, expires_at),
		    updated_at = NOW()
		WHERE id = $1`, offerID, status, agreed, expires); err != nil {
		return fmt.Errorf("settle offer: %w", err)
	}
	return nil
}

// ── Expiry ────────────────────────────────────────────────────────────────────

// StartOfferExpiryLoop closes negotiations nobody answered and deals nobody
// bought, so they leave the vendor's live tabs.
func (s *OrdersService) StartOfferExpiryLoop(ctx context.Context) {
	ticker := time.NewTicker(offerSweepInterval)
	defer ticker.Stop()
	s.expireOffers(ctx, nil)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.expireOffers(ctx, nil)
		}
	}
}

// expireOffers marks lapsed offers expired, for one store or all of them.
func (s *OrdersService) expireOffers(ctx context.Context, storeID *uuid.UUID) {
	rows, err := s.db.QueryxContext(ctx, `
		WITH lapsed AS (
			UPDATE price_offers
			SET status = 'expired', updated_at = NOW()
			WHERE status IN ('pending','countered','accepted')
			  AND expires_at < NOW()
			  AND ($1::uuid IS NULL OR store_id = $1)
			RETURNING id, store_id
		), logged AS (
			INSERT INTO price_offer_events (offer_id, actor, action)
			SELECT id, 'system', 'expire' FROM lapsed
		)
		SELECT id, store_id FROM lapsed`, storeID)
	if err != nil {
		s.log.Warn().Err(err).Msg("offer expiry sweep failed")
		middleware.RecordBackgroundError(s.db, s.log, "orders", "offer expiry sweep failed: "+err.Error(), nil)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, store string
		if rows.Scan(&id, &store) == nil {
			go s.broker.Publish(store, sse.Event{
				Type: "offer_updated",
				Data: fmt.Sprintf(`{"offer_id":%q,"status":"expired"}`, id),
			})
		}
	}
}

// ── Checkout ──────────────────────────────────────────────────────────────────

// priceItems checks every line of a store's order against the price the
// server agrees to: the product's current price, or an accepted deal's price
// when the line names one.
//
// Line prices used to be taken from the client as sent, so a tampered request
// could buy anything for a kobo — and negotiation would have been pointless
// on top of that, since anyone could name their own price. A line that
// disagrees is refused with the real price, rather than silently repriced:
// the buyer is about to be charged, and should see the number change.
func (s *OrdersService) priceItems(ctx context.Context, storeID uuid.UUID, customerEmail string, items []dto.CreateOrderItem) error {
	dealUnits := map[string]int32{}
	for _, it := range items {
		productID, err := uuid.Parse(it.ProductID)
		if err != nil {
			return apperrors.BadRequest("invalid product_id in items")
		}
		var (
			productStore uuid.UUID
			priceKobo    int64
			published    bool
		)
		err = s.db.QueryRowContext(ctx,
			`SELECT store_id, price_kobo, is_published FROM products WHERE id = $1`, productID,
		).Scan(&productStore, &priceKobo, &published)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !published) {
			return apperrors.BadRequest(fmt.Sprintf("%s is no longer available — remove it from your cart", it.Name))
		}
		if err != nil {
			return fmt.Errorf("price item: %w", err)
		}
		if productStore != storeID {
			return apperrors.BadRequest(fmt.Sprintf("%s isn't sold by this store", it.Name))
		}

		if it.OfferID == "" {
			if it.PriceKobo != priceKobo {
				return apperrors.BadRequest(fmt.Sprintf(
					"the price of %s is now %s — refresh your cart and try again", it.Name, fmtNairaPlain(priceKobo)))
			}
			continue
		}

		offerID, err := uuid.Parse(it.OfferID)
		if err != nil {
			return apperrors.BadRequest("invalid offer_id in items")
		}
		var deal struct {
			ProductID     uuid.UUID     `db:"product_id"`
			Status        string        `db:"status"`
			AgreedKobo    sql.NullInt64 `db:"agreed_kobo"`
			Quantity      int32         `db:"quantity"`
			CustomerEmail string        `db:"customer_email"`
			ExpiresAt     time.Time     `db:"expires_at"`
		}
		err = s.db.GetContext(ctx, &deal, `
			SELECT product_id, status, agreed_kobo, quantity, customer_email, expires_at
			FROM price_offers WHERE id = $1`, offerID)
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.BadRequest(fmt.Sprintf("we couldn't find the deal on %s — add it again at the listed price", it.Name))
		}
		if err != nil {
			return fmt.Errorf("load deal: %w", err)
		}
		switch {
		case deal.ProductID != productID || !strings.EqualFold(deal.CustomerEmail, strings.TrimSpace(customerEmail)):
			return apperrors.BadRequest(fmt.Sprintf(
				"the deal on %s was made with a different email — check out with that email, or buy at the listed price", it.Name))
		case deal.Status == string(dto.OfferRedeemed):
			return apperrors.BadRequest(fmt.Sprintf("the deal on %s has already been used", it.Name))
		case deal.Status != string(dto.OfferAccepted) || time.Now().After(deal.ExpiresAt) || !deal.AgreedKobo.Valid:
			return apperrors.BadRequest(fmt.Sprintf("the deal on %s has expired — it's %s now, or make a new offer", it.Name, fmtNairaPlain(priceKobo)))
		case it.PriceKobo != deal.AgreedKobo.Int64:
			return apperrors.BadRequest(fmt.Sprintf("the agreed price for %s is %s — refresh your cart and try again", it.Name, fmtNairaPlain(deal.AgreedKobo.Int64)))
		}
		dealUnits[it.OfferID] += it.Quantity
		if dealUnits[it.OfferID] > deal.Quantity {
			return apperrors.BadRequest(fmt.Sprintf("your deal on %s covers %d — buy any more at the listed price", it.Name, deal.Quantity))
		}
	}
	return nil
}

// redeemOffersTx closes the deals an order was bought with, once it is paid.
// 'expired' is included: the deal was valid when the order was placed, and a
// payment that lands after the window must not leave it looking unused.
func redeemOffersTx(ctx context.Context, tx *sqlx.Tx, orderID uuid.UUID) ([]string, error) {
	rows, err := tx.QueryxContext(ctx, `
		WITH redeemed AS (
			UPDATE price_offers
			SET status = 'redeemed', redeemed_order_id = $1, updated_at = NOW()
			WHERE id IN (SELECT offer_id FROM order_items WHERE order_id = $1 AND offer_id IS NOT NULL)
			  AND status IN ('accepted','expired')
			RETURNING id, agreed_kobo
		), logged AS (
			INSERT INTO price_offer_events (offer_id, actor, action, amount_kobo)
			SELECT id, 'system', 'redeem', agreed_kobo FROM redeemed
		)
		SELECT id FROM redeemed`, orderID)
	if err != nil {
		return nil, fmt.Errorf("redeem offers: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ── Notifications ─────────────────────────────────────────────────────────────

// publishOffer pushes the change to the vendor's open dashboards.
func (s *OrdersService) publishOffer(o dto.OfferResp, eventType string) {
	payload, _ := json.Marshal(map[string]any{
		"offer_id":     o.ID,
		"product_name": o.ProductName,
		"amount_kobo":  o.OfferKobo,
		"status":       o.Status,
	})
	go s.broker.Publish(o.StoreID, sse.Event{Type: eventType, Data: string(payload)})
}

func offerMail(o dto.OfferResp, amount int64, message string) email.OfferMail {
	m := email.OfferMail{
		OfferID:       o.ID,
		StoreSlug:     o.StoreSlug,
		StoreName:     o.StoreName,
		ProductName:   o.ProductName,
		CustomerName:  o.CustomerName,
		CustomerEmail: o.CustomerEmail,
		ListPriceKobo: o.ListPriceKobo,
		AmountKobo:    amount,
		Message:       message,
	}
	if id, err := uuid.Parse(o.ID); err == nil {
		m.BuyerToken = OfferToken(id)
	}
	if o.CustomerPhone != nil {
		m.CustomerPhone = *o.CustomerPhone
	}
	return m
}

func (s *OrdersService) mailVendorAboutOffer(o dto.OfferResp, amount int64, kind, message string) {
	storeID, err := uuid.Parse(o.StoreID)
	if err != nil {
		return
	}
	go func() {
		ctx := context.Background()
		vendorEmail, err := s.getVendorEmail(ctx, storeID)
		if err != nil || vendorEmail == "" {
			s.log.Warn().Err(err).Str("offer_id", o.ID).Msg("offer email: vendor email lookup failed")
			return
		}
		if err := email.SendOfferToVendor(ctx, vendorEmail, offerMail(o, amount, message), kind); err != nil {
			s.log.Warn().Err(err).Str("offer_id", o.ID).Msg("vendor offer email failed")
			middleware.RecordBackgroundError(s.db, s.log, "orders", "vendor offer email failed: "+err.Error(),
				map[string]any{"offer_id": o.ID})
		}
	}()
}

func (s *OrdersService) mailBuyerAboutOffer(o dto.OfferResp, action string, amount int64, message string) {
	go func() {
		if err := email.SendOfferUpdateToBuyer(context.Background(), action, offerMail(o, amount, message)); err != nil {
			s.log.Warn().Err(err).Str("offer_id", o.ID).Msg("buyer offer email failed")
			middleware.RecordBackgroundError(s.db, s.log, "orders", "buyer offer email failed: "+err.Error(),
				map[string]any{"offer_id": o.ID})
		}
	}()
}

// fmtNairaPlain formats kobo for an error message a buyer reads.
func fmtNairaPlain(kobo int64) string {
	naira := kobo / 100
	s := fmt.Sprintf("%d", naira)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return "₦" + string(out)
}
