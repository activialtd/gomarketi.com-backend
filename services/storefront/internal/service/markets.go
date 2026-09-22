package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

// ── Markets ───────────────────────────────────────────────────────────────────

// ListMarkets returns the active markets, optionally narrowed to a state
// and/or city. Both filters are case-insensitive exact matches, which is what
// the store-setup form sends after the vendor picks their location.
func (s *StorefrontService) ListMarkets(ctx context.Context, state, city string) ([]dto.MarketResp, error) {
	rows, err := s.db.QueryxContext(ctx, `
		SELECT id, name, city, state
		FROM markets
		WHERE is_active = TRUE
		  AND ($1 = '' OR LOWER(state) = LOWER($1) OR LOWER(state) = 'any')
		  AND ($2 = '' OR LOWER(city)  = LOWER($2) OR LOWER(city)  = 'any')
		ORDER BY
		  -- General Market first so the fallback option is always reachable.
		  CASE WHEN LOWER(state) = 'any' THEN 0 ELSE 1 END,
		  state, city, name`,
		strings.TrimSpace(state), strings.TrimSpace(city),
	)
	if err != nil {
		return nil, fmt.Errorf("list markets: %w", err)
	}
	defer rows.Close()

	out := []dto.MarketResp{}
	for rows.Next() {
		var m dto.MarketResp
		if err := rows.Scan(&m.ID, &m.Name, &m.City, &m.State); err != nil {
			return nil, fmt.Errorf("scan market: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ── Store search ──────────────────────────────────────────────────────────────

// StoreSearchParams carries the query string parameters of
// GET /v1/storefront/public/stores/search.
type StoreSearchParams struct {
	Query    string
	Category string
	MarketID string
	Limit    int
	Offset   int
}

// SearchStores finds active stores by market, category and/or free text.
//
// Before filtering it resolves what the query actually named — a vendor, a
// market, or a city — so the client can word its results header and search the
// leftover terms as products. Distance/radius search is not implemented; lat,
// lng and radius_km are accepted and ignored.
func (s *StorefrontService) SearchStores(ctx context.Context, p StoreSearchParams) (dto.StoreSearchResp, error) {
	if p.Limit <= 0 || p.Limit > 50 {
		p.Limit = 20
	}
	if p.Offset < 0 {
		p.Offset = 0
	}

	q := strings.TrimSpace(p.Query)
	resp := dto.StoreSearchResp{
		Stores:         []dto.StoreSearchResult{},
		MatchType:      "none",
		RemainingQuery: q,
	}

	marketID := strings.TrimSpace(p.MarketID)
	if marketID != "" {
		if _, err := uuid.Parse(marketID); err != nil {
			return dto.StoreSearchResp{}, apperrors.BadRequest("invalid market_id")
		}
		resp.MatchType = "market"
		resp.MatchedMarketID = &marketID
	}

	// Resolve the free-text query against vendor, market and city names, most
	// specific first. Only done when the caller has not already scoped to a
	// market — an explicit market_id wins.
	if q != "" && marketID == "" {
		if id, name, ok := s.matchStoreName(ctx, q); ok {
			resp.MatchType = "vendor"
			resp.MatchedStoreID = &id
			resp.RemainingQuery = stripPhrase(q, name)
		} else if id, name, ok := s.matchMarketName(ctx, q); ok {
			resp.MatchType = "market"
			resp.MatchedMarketID = &id
			marketID = id
			resp.RemainingQuery = stripPhrase(q, name)
		} else if name, ok := s.matchCityName(ctx, q); ok {
			resp.MatchType = "city"
			resp.RemainingQuery = stripPhrase(q, name)
		}
	}

	// A named vendor filters to that store; anything else filters by the
	// remaining text so "ogba rice" still searches rice.
	textFilter := q
	if resp.MatchType == "vendor" || resp.MatchType == "market" || resp.MatchType == "city" {
		textFilter = resp.RemainingQuery
	}

	// Fetch one extra row to answer has_more without a second COUNT query.
	rows, err := s.db.QueryxContext(ctx, `
		SELECT s.id, s.name, s.slug, s.category, s.tagline, s.logo_url, s.hero_image_url,
		       s.address, s.city, s.state, s.market_id, m.name AS market_name
		FROM stores s
		LEFT JOIN markets m ON m.id = s.market_id
		WHERE s.is_active = TRUE
		  AND ($1 = '' OR s.market_id = $1::uuid)
		  AND ($2 = '' OR LOWER(s.category) = LOWER($2))
		  AND ($3 = '' OR LOWER(s.name) % LOWER($3)
		               OR s.name ILIKE '%' || $3 || '%'
		               OR COALESCE(s.tagline, '') ILIKE '%' || $3 || '%'
		               OR s.category ILIKE '%' || $3 || '%')
		ORDER BY
		  -- Strongest name match first; recency only breaks ties.
		  CASE WHEN $3 = '' THEN 0
		       ELSE (CASE WHEN LOWER(s.name) LIKE LOWER($3) || '%' THEN 3.0 ELSE 0 END
		             + CASE WHEN LOWER(s.name) LIKE '%' || LOWER($3) || '%' THEN 2.0 ELSE 0 END
		             + similarity(LOWER(s.name), LOWER($3)) * 2.0)
		  END DESC,
		  s.created_at DESC
		LIMIT $4 OFFSET $5`,
		marketID, strings.TrimSpace(p.Category), strings.TrimSpace(textFilter),
		p.Limit+1, p.Offset,
	)
	if err != nil {
		return dto.StoreSearchResp{}, fmt.Errorf("search stores: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			r          dto.StoreSearchResult
			tagline    sql.NullString
			logoURL    sql.NullString
			heroImage  sql.NullString
			address    sql.NullString
			city       sql.NullString
			state      sql.NullString
			mktID      uuid.NullUUID
			marketName sql.NullString
		)
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Slug, &r.Category, &tagline, &logoURL, &heroImage,
			&address, &city, &state, &mktID, &marketName,
		); err != nil {
			return dto.StoreSearchResp{}, fmt.Errorf("scan store: %w", err)
		}
		r.Tagline = nullToPtr(tagline)
		r.LogoURL = nullToPtr(logoURL)
		r.HeroImage = nullToPtr(heroImage)
		r.Address = nullToPtr(address)
		r.City = nullToPtr(city)
		r.State = nullToPtr(state)
		r.MarketName = nullToPtr(marketName)
		if mktID.Valid {
			id := mktID.UUID.String()
			r.MarketID = &id
		}
		resp.Stores = append(resp.Stores, r)
	}
	if err := rows.Err(); err != nil {
		return dto.StoreSearchResp{}, fmt.Errorf("scan stores: %w", err)
	}

	if len(resp.Stores) > p.Limit {
		resp.Stores = resp.Stores[:p.Limit]
		resp.HasMore = true
	}
	return resp, nil
}

// matchStoreName returns the store whose name appears in the query.
func (s *StorefrontService) matchStoreName(ctx context.Context, q string) (id, name string, ok bool) {
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name FROM stores
		WHERE is_active = TRUE
		  AND (LOWER($1) LIKE '%' || LOWER(name) || '%'
		       OR word_similarity(LOWER(name), LOWER($1)) >= 0.7)
		ORDER BY word_similarity(LOWER(name), LOWER($1)) DESC, LENGTH(name) DESC
		LIMIT 1`, q).Scan(&id, &name)
	return id, name, err == nil
}

// matchMarketName returns the market whose name appears in the query.
func (s *StorefrontService) matchMarketName(ctx context.Context, q string) (id, name string, ok bool) {
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name FROM markets
		WHERE is_active = TRUE AND LOWER(state) <> 'any'
		  AND (LOWER($1) LIKE '%' || LOWER(name) || '%'
		       OR word_similarity(LOWER(name), LOWER($1)) >= 0.7)
		ORDER BY word_similarity(LOWER(name), LOWER($1)) DESC, LENGTH(name) DESC
		LIMIT 1`, q).Scan(&id, &name)
	return id, name, err == nil
}

// matchCityName returns the city named in the query, from the cities stores
// and markets actually sit in.
func (s *StorefrontService) matchCityName(ctx context.Context, q string) (name string, ok bool) {
	err := s.db.QueryRowContext(ctx, `
		SELECT city FROM (
			SELECT city FROM markets WHERE is_active = TRUE AND LOWER(city) <> 'any'
			UNION
			SELECT city FROM stores WHERE is_active = TRUE AND city IS NOT NULL AND city <> ''
		) c
		WHERE LOWER($1) LIKE '%' || LOWER(city) || '%'
		ORDER BY LENGTH(city) DESC
		LIMIT 1`, q).Scan(&name)
	return name, err == nil
}

// stripPhrase removes the matched phrase from the query, case-insensitively,
// and tidies the leftover whitespace.
func stripPhrase(q, phrase string) string {
	if phrase == "" {
		return q
	}
	lowerQ, lowerP := strings.ToLower(q), strings.ToLower(phrase)
	i := strings.Index(lowerQ, lowerP)
	if i < 0 {
		return q
	}
	return strings.Join(strings.Fields(q[:i]+" "+q[i+len(phrase):]), " ")
}
