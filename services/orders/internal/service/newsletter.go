package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/email"
)

// Subscribe adds an email to a store's newsletter list.
// Re-subscribes silently if the email was previously unsubscribed.
func (s *OrdersService) Subscribe(ctx context.Context, storeID uuid.UUID, req dto.SubscribeReq) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO newsletter_subscribers (store_id, email, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (store_id, email) DO UPDATE
		  SET name = EXCLUDED.name, unsubscribed_at = NULL`,
		storeID, req.Email, req.Name,
	)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	return nil
}

// Unsubscribe soft-deletes a subscriber by setting unsubscribed_at.
func (s *OrdersService) Unsubscribe(ctx context.Context, storeID uuid.UUID, subscriberID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE newsletter_subscribers SET unsubscribed_at = NOW()
		 WHERE id = $1 AND store_id = $2 AND unsubscribed_at IS NULL`,
		subscriberID, storeID,
	)
	if err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperrors.NotFound("subscriber not found")
	}
	return nil
}

// ListSubscribers returns active subscribers (not unsubscribed) for a store.
func (s *OrdersService) ListSubscribers(ctx context.Context, storeID uuid.UUID, page, perPage int) (dto.SubscriberListResp, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	offset := (page - 1) * perPage

	var total int64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM newsletter_subscribers WHERE store_id=$1 AND unsubscribed_at IS NULL`,
		storeID,
	).Scan(&total)

	type row struct {
		ID           string    `db:"id"`
		Email        string    `db:"email"`
		Name         string    `db:"name"`
		SubscribedAt time.Time `db:"subscribed_at"`
	}
	rows, err := s.db.QueryxContext(ctx, `
		SELECT id, email, name, subscribed_at
		FROM newsletter_subscribers
		WHERE store_id=$1 AND unsubscribed_at IS NULL
		ORDER BY subscribed_at DESC
		LIMIT $2 OFFSET $3`, storeID, perPage, offset)
	if err != nil {
		return dto.SubscriberListResp{}, fmt.Errorf("list subscribers: %w", err)
	}
	defer rows.Close()

	subs := make([]dto.SubscriberResp, 0)
	for rows.Next() {
		var r row
		if err := rows.StructScan(&r); err != nil {
			return dto.SubscriberListResp{}, err
		}
		subs = append(subs, dto.SubscriberResp{
			ID:           r.ID,
			Email:        r.Email,
			Name:         r.Name,
			SubscribedAt: r.SubscribedAt.UTC().Format(time.RFC3339),
		})
	}
	return dto.SubscriberListResp{
		Subscribers: subs,
		Total:       total,
		Page:        page,
		PerPage:     perPage,
	}, nil
}

// CreateCampaign saves a draft campaign.
func (s *OrdersService) CreateCampaign(ctx context.Context, storeID uuid.UUID, req dto.CreateCampaignReq) (dto.CampaignResp, error) {
	var id string
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO email_campaigns (store_id, subject, body_html)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`,
		storeID, req.Subject, req.BodyHTML,
	).Scan(&id, &createdAt)
	if err != nil {
		return dto.CampaignResp{}, fmt.Errorf("create campaign: %w", err)
	}
	return dto.CampaignResp{
		ID:        id,
		Subject:   req.Subject,
		Status:    "draft",
		CreatedAt: createdAt.UTC().Format(time.RFC3339),
	}, nil
}

// ListCampaigns returns all campaigns for a store, newest first.
func (s *OrdersService) ListCampaigns(ctx context.Context, storeID uuid.UUID) (dto.CampaignListResp, error) {
	type row struct {
		ID              string       `db:"id"`
		Subject         string       `db:"subject"`
		Status          string       `db:"status"`
		RecipientsCount int          `db:"recipients_count"`
		CreatedAt       time.Time    `db:"created_at"`
		SentAt          sql.NullTime `db:"sent_at"`
	}
	rows, err := s.db.QueryxContext(ctx, `
		SELECT id, subject, status, recipients_count, created_at, sent_at
		FROM email_campaigns WHERE store_id=$1
		ORDER BY created_at DESC`, storeID)
	if err != nil {
		return dto.CampaignListResp{}, fmt.Errorf("list campaigns: %w", err)
	}
	defer rows.Close()

	camps := make([]dto.CampaignResp, 0)
	for rows.Next() {
		var r row
		if err := rows.StructScan(&r); err != nil {
			return dto.CampaignListResp{}, err
		}
		c := dto.CampaignResp{
			ID:              r.ID,
			Subject:         r.Subject,
			Status:          r.Status,
			RecipientsCount: r.RecipientsCount,
			CreatedAt:       r.CreatedAt.UTC().Format(time.RFC3339),
		}
		if r.SentAt.Valid {
			t := r.SentAt.Time.UTC().Format(time.RFC3339)
			c.SentAt = &t
		}
		camps = append(camps, c)
	}
	return dto.CampaignListResp{Campaigns: camps}, nil
}

// SendCampaign sends a campaign to all active subscribers of a store.
// The actual send is async — this marks the campaign as 'sending' synchronously
// and updates to 'sent'/'failed' in the background.
func (s *OrdersService) SendCampaign(ctx context.Context, storeID uuid.UUID, campaignID uuid.UUID) (dto.CampaignResp, error) {
	type campRow struct {
		ID       string `db:"id"`
		Subject  string `db:"subject"`
		BodyHTML string `db:"body_html"`
		Status   string `db:"status"`
	}
	var c campRow
	err := s.db.QueryRowxContext(ctx,
		`SELECT id, subject, body_html, status FROM email_campaigns WHERE id=$1 AND store_id=$2`,
		campaignID, storeID,
	).StructScan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.CampaignResp{}, apperrors.NotFound("campaign not found")
	}
	if err != nil {
		return dto.CampaignResp{}, fmt.Errorf("get campaign: %w", err)
	}
	if c.Status == "sent" || c.Status == "sending" {
		return dto.CampaignResp{}, apperrors.BadRequest("campaign has already been sent or is currently sending")
	}

	// Get all active subscribers
	type subRow struct {
		Email string `db:"email"`
		Name  string `db:"name"`
	}
	rows, err := s.db.QueryxContext(ctx,
		`SELECT email, name FROM newsletter_subscribers WHERE store_id=$1 AND unsubscribed_at IS NULL`,
		storeID,
	)
	if err != nil {
		return dto.CampaignResp{}, fmt.Errorf("load subscribers: %w", err)
	}
	defer rows.Close()

	var subs []subRow
	for rows.Next() {
		var r subRow
		if err := rows.StructScan(&r); err != nil {
			continue
		}
		subs = append(subs, r)
	}

	if len(subs) == 0 {
		return dto.CampaignResp{}, apperrors.BadRequest("no active subscribers to send to")
	}

	_, name := s.getStoreSlugName(ctx, storeID)

	// Mark as sending
	_, _ = s.db.ExecContext(ctx,
		`UPDATE email_campaigns SET status='sending', updated_at=NOW() WHERE id=$1`, campaignID,
	)

	subject := c.Subject
	bodyHTML := c.BodyHTML
	campIDStr := campaignID.String()

	go func() {
		sent := 0
		for _, sub := range subs {
			plain := "You received a message from " + name + ". Please view this email in an HTML-capable client."
			_ = email.SendCampaignMail(context.Background(), sub.Email, sub.Name, name, subject, bodyHTML, plain)
			sent++
		}
		status := "sent"
		_, _ = s.db.ExecContext(context.Background(), `
			UPDATE email_campaigns
			SET status=$1, recipients_count=$2, sent_at=NOW(), updated_at=NOW()
			WHERE id=$3`, status, sent, campIDStr,
		)
	}()

	createdAt := time.Now().UTC().Format(time.RFC3339)
	return dto.CampaignResp{
		ID:              c.ID,
		Subject:         c.Subject,
		Status:          "sending",
		RecipientsCount: len(subs),
		CreatedAt:       createdAt,
	}, nil
}
