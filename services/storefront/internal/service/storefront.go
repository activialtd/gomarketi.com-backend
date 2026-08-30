package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/email"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/vercel"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/planlimits"
)

// gatedTemplates maps a store template id to the plan slug required to use
// it. Templates not listed here are available on every plan. Kept in sync
// by hand with the frontend's PLAN_ORDER-driven template picker
// (apps/vendor-web/components/store-setup/store-customization/StoreCustomize.tsx)
// — no shared source of truth between Go and TS exists in this codebase.
var gatedTemplates = map[string]string{
	"lagos": "starter",
}

var planTier = map[string]int{"free": 0, "starter": 1, "growth": 2, "scale": 3}

func planAllowsTemplate(planSlug, template string) bool {
	required, gated := gatedTemplates[template]
	if !gated {
		return true
	}
	return planTier[planSlug] >= planTier[required]
}

type StorefrontService struct {
	db          *sqlx.DB
	log         zerolog.Logger
	emailer     email.WelcomeMailer
	domains     vercel.Registrar
	storeDomain string
}

func New(db *sqlx.DB, emailer email.WelcomeMailer, domains vercel.Registrar, storeDomain string, log zerolog.Logger) *StorefrontService {
	return &StorefrontService{db: db, emailer: emailer, domains: domains, storeDomain: storeDomain, log: log}
}

// ── Slug validation ─────────────────────────────────────────────────────────
// Slugs become live subdomains ({slug}.gomarketi.com) via Vercel domain
// registration, so they must be DNS-safe: lowercase letters/numbers/hyphens,
// no leading/trailing hyphen. Also blocks slugs that would collide with a
// platform subdomain already in use.

var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

var reservedSlugs = map[string]bool{
	"www": true, "api": true, "vendor": true, "admin": true, "app": true,
	"mail": true, "cdn": true, "ftp": true, "staging": true, "blog": true,
	"support": true, "help": true, "status": true, "docs": true,
	"staging-vendor": true, "api-staging": true,
}

func validateSlugFormat(slug string) error {
	if !slugPattern.MatchString(slug) {
		return apperrors.Wrap(http.StatusBadRequest,
			"slug must be lowercase letters, numbers, and hyphens only, and can't start or end with a hyphen", nil)
	}
	if reservedSlugs[slug] {
		return apperrors.Wrap(http.StatusBadRequest, "this slug is reserved", nil)
	}
	return nil
}

func (s *StorefrontService) DB() *sqlx.DB { return s.db }

// ── Store ─────────────────────────────────────────────────────────────────────

func (s *StorefrontService) CreateStore(ctx context.Context, userID uuid.UUID, req dto.CreateStoreReq) (dto.StoreResp, error) {
	// Idempotent: return existing store if vendor already has one.
	existing, err := s.getStoreByVendor(ctx, userID)
	if err == nil {
		return existing, nil
	}
	if !isNotFound(err) {
		return dto.StoreResp{}, fmt.Errorf("check existing store: %w", err)
	}

	if err := validateSlugFormat(req.Slug); err != nil {
		return dto.StoreResp{}, err
	}

	// Check slug is free.
	var taken bool
	_ = s.db.QueryRowContext(ctx, `SELECT TRUE FROM stores WHERE slug=$1`, req.Slug).Scan(&taken)
	if taken {
		return dto.StoreResp{}, apperrors.Wrap(http.StatusConflict, "slug already taken", nil)
	}

	limits, err := planlimits.ForVendorUserID(ctx, s.db, userID)
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("resolve plan limits: %w", err)
	}
	if !limits.AllowsCurrency(req.Currency) {
		return dto.StoreResp{}, apperrors.BadRequest("your plan does not support " + req.Currency + " — upgrade to unlock it")
	}
	if req.TeamSize != nil && !limits.AllowsTeamSize(*req.TeamSize) {
		return dto.StoreResp{}, apperrors.BadRequest("your plan does not support a team size of " + *req.TeamSize + " — upgrade to unlock it")
	}

	var row storeRow
	err = s.db.QueryRowxContext(ctx, `
		INSERT INTO stores (vendor_id, name, slug, category, currency, team_size, support_phone)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, vendor_id, name, slug, category, currency,
		          team_size, staff_range, tagline, logo_url, hero_image_url, site_description,
		          COALESCE(social_links, '{}') AS social_links,
		          support_phone, address, city, state, market_id,
		          (SELECT name FROM markets m WHERE m.id = market_id) AS market_name,
		          custom_domain, custom_domain_status,
		          COALESCE(theme_config, '{}') AS theme_config, is_active, created_at`,
		userID, req.Name, req.Slug, req.Category, req.Currency,
		req.TeamSize, req.SupportPhone,
	).StructScan(&row)
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("insert store: %w", err)
	}

	resp := rowToResp(row)

	// Send welcome email asynchronously — never block store creation on email delivery.
	var vendorEmail, vendorName string
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(email,''), COALESCE(full_name,'') FROM users WHERE id=$1`, userID,
	).Scan(&vendorEmail, &vendorName)

	if vendorEmail != "" {
		storeName := resp.Name
		storeSlug := resp.Slug
		storeDomain := s.storeDomain
		go func() {
			ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if emailErr := s.emailer.SendWelcome(ctx2,
				vendorEmail, vendorName, storeName, storeSlug, storeDomain,
			); emailErr != nil {
				s.log.Warn().Err(emailErr).Str("slug", storeSlug).Msg("welcome email failed")
			}
		}()
	}

	// Register {slug}.{storeDomain} on Vercel asynchronously — DNS routing
	// already works via the *.{storeDomain} wildcard, but Vercel only issues
	// a working SSL certificate for domains explicitly registered on the
	// project, so without this the store loads over plain HTTP until
	// someone adds it by hand.
	storeSlug := resp.Slug
	storeSubdomain := storeSlug + "." + s.storeDomain
	go func() {
		ctx3, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if domainErr := s.domains.AddDomain(ctx3, storeSubdomain); domainErr != nil {
			s.log.Warn().Err(domainErr).Str("domain", storeSubdomain).Msg("vercel domain registration failed")
		}
	}()

	return resp, nil
}

func (s *StorefrontService) GetMyStore(ctx context.Context, userID uuid.UUID) (dto.StoreResp, error) {
	return s.getStoreByVendor(ctx, userID)
}

func (s *StorefrontService) UpdateStore(ctx context.Context, userID uuid.UUID, storeID uuid.UUID, req dto.UpdateStoreReq) (dto.StoreResp, error) {
	if req.ThemeConfig != nil {
		var tc struct {
			Template string `json:"template"`
		}
		if err := json.Unmarshal(req.ThemeConfig, &tc); err == nil && tc.Template != "" {
			limits, err := planlimits.ForVendorUserID(ctx, s.db, userID)
			if err != nil {
				return dto.StoreResp{}, fmt.Errorf("resolve plan limits: %w", err)
			}
			if !planAllowsTemplate(limits.PlanSlug, tc.Template) {
				return dto.StoreResp{}, apperrors.BadRequest("your plan does not support the \"" + tc.Template + "\" template — upgrade to unlock it")
			}
		}
	}

	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE stores SET
			name             = COALESCE($1, name),
			tagline          = COALESCE($2, tagline),
			logo_url         = COALESCE($3, logo_url),
			hero_image_url   = COALESCE($4, hero_image_url),
			site_description = COALESCE($5, site_description),
			social_links     = CASE WHEN $6::jsonb IS NOT NULL THEN $6::jsonb ELSE COALESCE(social_links, '{}') END,
			support_phone    = COALESCE($7, support_phone),
			address          = COALESCE($8, address),
			city             = COALESCE($9, city),
			state            = COALESCE($10, state),
			market_id        = COALESCE($11::uuid, market_id),
			theme_config     = CASE WHEN $12::jsonb IS NOT NULL THEN $12::jsonb ELSE COALESCE(theme_config, '{}') END,
			updated_at       = NOW()
		WHERE id=$13 AND vendor_id=$14
		RETURNING id, vendor_id, name, slug, category, currency,
		          team_size, staff_range, tagline, logo_url, hero_image_url, site_description,
		          COALESCE(social_links, '{}') AS social_links,
		          support_phone, address, city, state, market_id,
		          (SELECT name FROM markets m WHERE m.id = market_id) AS market_name,
		          custom_domain, custom_domain_status,
		          COALESCE(theme_config, '{}') AS theme_config, is_active, created_at`,
		req.Name, req.Tagline, req.LogoURL, req.HeroImageURL, req.SiteDescription,
		nullJSON(req.SocialLinks), req.SupportPhone,
		req.Address, req.City, req.State, req.MarketID, nullJSON(req.ThemeConfig),
		storeID, userID,
	).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("update store: %w", err)
	}
	return rowToResp(row), nil
}

func (s *StorefrontService) CheckSlugAvailable(ctx context.Context, slug string) (dto.SlugCheckResp, error) {
	if validateSlugFormat(slug) != nil {
		return dto.SlugCheckResp{Slug: slug, Available: false}, nil
	}
	var taken bool
	_ = s.db.QueryRowContext(ctx, `SELECT TRUE FROM stores WHERE slug=$1`, slug).Scan(&taken)
	return dto.SlugCheckResp{Slug: slug, Available: !taken}, nil
}

func (s *StorefrontService) GetStoreBySlug(ctx context.Context, slug string) (dto.StoreResp, error) {
	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT id, vendor_id, name, slug, category, currency,
		       team_size, staff_range, tagline, logo_url, hero_image_url, site_description,
		       COALESCE(social_links, '{}') AS social_links,
		       support_phone, address, city, state, market_id,
		       (SELECT name FROM markets m WHERE m.id = market_id) AS market_name,
		       custom_domain, custom_domain_status,
		       COALESCE(theme_config, '{}') AS theme_config, is_active, created_at
		FROM stores WHERE slug=$1 AND is_active=TRUE`, slug).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("get store by slug: %w", err)
	}
	return rowToResp(row), nil
}

func (s *StorefrontService) GetStoreByDomain(ctx context.Context, domain string) (dto.StoreResp, error) {
	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT id, vendor_id, name, slug, category, currency,
		       team_size, staff_range, tagline, logo_url, hero_image_url, site_description,
		       COALESCE(social_links, '{}') AS social_links,
		       support_phone, address, city, state, market_id,
		       (SELECT name FROM markets m WHERE m.id = market_id) AS market_name,
		       custom_domain, custom_domain_status,
		       COALESCE(theme_config, '{}') AS theme_config, is_active, created_at
		FROM stores WHERE custom_domain=$1 AND custom_domain_status='active' AND is_active=TRUE`, domain).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("get store by domain: %w", err)
	}
	return rowToResp(row), nil
}

// ── Search ────────────────────────────────────────────────────────────────────

// categoryKeywords maps free-text search phrases to store categories, so a
// query like "men's wear" resolves to the "fashion" category. Matching is a
// simple case-insensitive substring check — no NLP/ML, just enough to cover
// how people actually phrase a shopping search.
var categoryKeywords = map[string][]string{
	"fashion":     {"cloth", "wear", "dress", "shirt", "trouser", "fashion", "outfit", "men's", "mens", "women's", "womens", "shoe", "sneaker", "kaftan", "ankara"},
	"thrift":      {"thrift", "okrika", "bend down", "bend-down", "second hand", "second-hand", "used cloth"},
	"jewelry":     {"jewel", "gold", "necklace", "ring", "bracelet", "earring", "accessor"},
	"electronics": {"gadget", "phone", "laptop", "electronic", "charger", "earpiece", "tech", "television", " tv ", "computer"},
	"food":        {"food", "restaurant", "eat", "meal", "kitchen", "suya", "amala", "jollof"},
	"groceries":   {"grocery", "groceries", "provision", "foodstuff", "food stuff", "rice", "beans", "cooking oil", "spaghetti"},
	"beauty":      {"beauty", "makeup", "make-up", "cosmetic", "skincare", "perfume", "wig"},
	"home":        {"furniture", "home decor", "kitchenware", "appliance"},
}

// matchCategories returns every store category whose keyword set appears in
// the query. Returns nil if nothing matched — the caller falls back to a
// free-text name search in that case.
func matchCategories(q string) []string {
	q = " " + strings.ToLower(q) + " "
	var matched []string
	for category, keywords := range categoryKeywords {
		for _, kw := range keywords {
			if strings.Contains(q, kw) {
				matched = append(matched, category)
				break
			}
		}
	}
	return matched
}

// cityAliases maps a spoken/typed area name to the exact stores.city value(s)
// it should match. Hand-curated (like categoryKeywords) rather than derived
// automatically from stores.city, so a generic word doesn't over-match.
// This is the "general area" tier — "men's wear in ikeja" — one level
// coarser than a specific named market like "balogun".
var cityAliases = map[string][]string{
	"ikeja":           {"Ikeja", "Ikeja City Mall Area"},
	"yaba":            {"Yaba"},
	"surulere":        {"Surulere"},
	"lekki":           {"Lekki"},
	"victoria island": {"Victoria Island"},
	"ajah":            {"Ajah"},
	"apapa":           {"Apapa"},
	"ikorodu":         {"Ikorodu"},
	"agege":           {"Agege"},
	"oshodi":          {"Oshodi"},
	"ojo":             {"Ojo"},
	"ebute metta":     {"Ebute-Metta"},
	"ebute-metta":     {"Ebute-Metta"},
	"ketu":            {"Ketu"},
	"kosofe":          {"Kosofe"},
	"okokomaiko":      {"Okokomaiko"},
	"lagos island":    {"Lagos Island"},
	"awka":            {"Awka"},
	"onitsha":         {"Onitsha"},
	"nnewi":           {"Nnewi"},
}

// matchCity returns the stores.city value(s) to filter by if the query
// names a general area, plus the matched keyword itself (for stripping it
// back out of the query to isolate the product term) — or ("", nil) if
// nothing matched.
func matchCity(q string) (keyword string, cities []string) {
	q = " " + strings.ToLower(q) + " "
	for kw, c := range cityAliases {
		if strings.Contains(q, kw) {
			return kw, c
		}
	}
	return "", nil
}

type marketRow struct {
	ID      uuid.UUID      `db:"id"`
	Name    string         `db:"name"`
	Aliases pq.StringArray `db:"aliases"`
}

// matchMarket finds the single named market (e.g. "Balogun Market") a query
// refers to, by checking the market's name and hand-seeded aliases against
// the query text. This is the "specific market" tier — one level more
// precise than a general area — so "men's wear in balogun" resolves to
// exactly Balogun Market, not every fashion store in Lagos Island.
func (s *StorefrontService) matchMarket(ctx context.Context, q string) (*marketRow, error) {
	var markets []marketRow
	if err := s.db.SelectContext(ctx, &markets, `SELECT id, name, aliases FROM markets`); err != nil {
		return nil, fmt.Errorf("load markets: %w", err)
	}

	padded := " " + strings.ToLower(q) + " "
	for i := range markets {
		m := &markets[i]
		if strings.Contains(padded, strings.ToLower(m.Name)) {
			return m, nil
		}
		for _, alias := range m.Aliases {
			if strings.Contains(padded, strings.ToLower(alias)) {
				return m, nil
			}
		}
	}
	return nil, nil
}

type vendorNameRow struct {
	ID   uuid.UUID `db:"id"`
	Name string    `db:"name"`
}

// vendorMatch pairs the matched store with the actual substring that
// matched it — the full name, or (see matchVendor's second pass) just its
// first word — so the caller strips exactly that text back out of the
// query, not the store's full name regardless of what was actually typed.
type vendorMatch struct {
	Store  vendorNameRow
	Phrase string
}

// matchVendor finds the single specific vendor store a query names. This is
// the highest-priority tier — more specific than a named market — so a
// query naming both a vendor and a market still resolves to just that
// vendor, and the cross-vendor carousel is suppressed entirely for it
// (that's the caller's job once it sees MatchType == "vendor").
//
// Two passes, most to least precise:
//  1. The query contains the store's full name verbatim ("...at TechHub
//     Store"). Word-boundary padded on BOTH the query and the candidate
//     name — matchMarket only pads the query side, which is a real gap: it
//     would treat a market literally named "co" as matching any query
//     containing "co" as a substring of a longer word (e.g. "coffee").
//  2. Nothing matched the full name — fall back to just the store's first
//     word ("...at techhub"). This is how people actually reference a
//     vendor in a short search query; requiring the full name would make
//     this tier fire on almost nothing in practice (confirmed by testing
//     "iphone at techhub" against a store literally named "TechHub Store"
//     — pass 1 alone misses it entirely). Riskier since a single word
//     matches more broadly, so it's a distinct, lower-priority pass rather
//     than loosening pass 1's own matching.
//
// Both passes skip candidate strings under 4 characters (blunt guard
// against short-name false positives) and prefer the longest match if
// several candidates match within the same pass.
func (s *StorefrontService) matchVendor(ctx context.Context, q string) (*vendorMatch, error) {
	var vendors []vendorNameRow
	if err := s.db.SelectContext(ctx, &vendors, `SELECT id, name FROM stores WHERE is_active = TRUE`); err != nil {
		return nil, fmt.Errorf("load vendors: %w", err)
	}

	padded := " " + strings.ToLower(q) + " "

	var best *vendorNameRow
	for i := range vendors {
		v := &vendors[i]
		name := strings.ToLower(v.Name)
		if len(name) < 4 {
			continue
		}
		if strings.Contains(padded, " "+name+" ") && (best == nil || len(name) > len(strings.ToLower(best.Name))) {
			best = v
		}
	}
	if best != nil {
		return &vendorMatch{Store: *best, Phrase: best.Name}, nil
	}

	var bestWord *vendorNameRow
	var bestWordText string
	for i := range vendors {
		v := &vendors[i]
		first, _, _ := strings.Cut(strings.ToLower(v.Name), " ")
		if len(first) < 4 {
			continue
		}
		if strings.Contains(padded, " "+first+" ") && len(first) > len(bestWordText) {
			bestWord = v
			bestWordText = first
		}
	}
	if bestWord != nil {
		return &vendorMatch{Store: *bestWord, Phrase: bestWordText}, nil
	}

	return nil, nil
}

// stripMatchedPhrase removes a matched vendor/market/city phrase from the
// query — plus one leading connector word, if present immediately before
// it — leaving the product term(s) to search catalogue with. Without this,
// a query like "iphone at techhub" would never ILIKE-match a product named
// "iPhone 14": "iphone at techhub" isn't a substring of any product name,
// but the stripped remainder "iphone" is.
func stripMatchedPhrase(q, phrase string) string {
	if phrase == "" {
		return strings.TrimSpace(q)
	}
	withConnector := regexp.MustCompile(`(?i)\b(in|at|near|from)\s+` + regexp.QuoteMeta(phrase) + `\b`)
	remaining := withConnector.ReplaceAllString(q, "")
	if remaining == q {
		bare := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(phrase) + `\b`)
		remaining = bare.ReplaceAllString(q, "")
	}
	remaining = regexp.MustCompile(`\s+`).ReplaceAllString(remaining, " ")
	return strings.TrimSpace(remaining)
}

type storeSearchRow struct {
	ID           uuid.UUID       `db:"id"`
	Name         string          `db:"name"`
	Slug         string          `db:"slug"`
	Category     string          `db:"category"`
	Tagline      sql.NullString  `db:"tagline"`
	LogoURL      sql.NullString  `db:"logo_url"`
	HeroImageURL sql.NullString  `db:"hero_image_url"`
	Address      sql.NullString  `db:"address"`
	City         sql.NullString  `db:"city"`
	State        sql.NullString  `db:"state"`
	MarketID     sql.NullString  `db:"market_id"`
	MarketName   sql.NullString  `db:"market_name"`
	DistanceKm   sql.NullFloat64 `db:"distance_km"`
}

// SearchStores finds stores by free-text query and/or explicit category,
// optionally sorted by distance from a buyer's location. Powers the
// consumer-app text and voice search, including resolving which stores are
// eligible for a cross-vendor product search (see MatchType/RemainingQuery
// on the response).
//
// Location precision, most to least specific:
//  0. a specific vendor named in the query ("...at TechHub Store") —
//     filters to exactly that store. Most specific tier: even if a market
//     is also named, the vendor wins.
//  1. explicit market_id, or a named market recognized in the query
//     ("...in balogun") — filters to exactly that market, no distance
//     fallback, even if that returns zero results.
//  2. a general area recognized in the query ("...in ikeja") — filters to
//     that city value, no distance fallback.
//  3. neither — falls back to distance-from-buyer (if lat/lng given) or a
//     plain name search.
func (s *StorefrontService) SearchStores(ctx context.Context, req dto.StoreSearchReq) (dto.StoreSearchListResp, error) {
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	var categoryList []string
	if req.Category != nil && *req.Category != "" {
		categoryList = []string{*req.Category}
	} else if req.Q != nil && *req.Q != "" {
		categoryList = matchCategories(*req.Q)
	}

	var matchedStoreID, matchedPhrase string
	if req.Q != nil && *req.Q != "" {
		v, err := s.matchVendor(ctx, *req.Q)
		if err != nil {
			return dto.StoreSearchListResp{}, err
		}
		if v != nil {
			matchedStoreID = v.Store.ID.String()
			matchedPhrase = v.Phrase
		}
	}

	var matchedMarketID string
	if matchedStoreID == "" {
		if req.MarketID != nil && *req.MarketID != "" {
			matchedMarketID = *req.MarketID
		} else if req.Q != nil && *req.Q != "" {
			m, err := s.matchMarket(ctx, *req.Q)
			if err != nil {
				return dto.StoreSearchListResp{}, err
			}
			if m != nil {
				matchedMarketID = m.ID.String()
				matchedPhrase = m.Name
				// The query may contain an alias rather than the market's
				// canonical name — prefer whichever string actually appears,
				// so stripMatchedPhrase removes the right text.
				lowerQ := strings.ToLower(*req.Q)
				if !strings.Contains(lowerQ, strings.ToLower(m.Name)) {
					for _, alias := range m.Aliases {
						if strings.Contains(lowerQ, strings.ToLower(alias)) {
							matchedPhrase = alias
							break
						}
					}
				}
			}
		}
	}

	var matchedCities []string
	var matchedCityKeyword string
	if matchedStoreID == "" && matchedMarketID == "" && req.Q != nil && *req.Q != "" {
		matchedCityKeyword, matchedCities = matchCity(*req.Q)
		if matchedCityKeyword != "" {
			matchedPhrase = matchedCityKeyword
		}
	}

	hasLoc := req.Lat != nil && req.Lng != nil
	radiusKm := 25.0
	if req.RadiusKm != nil && *req.RadiusKm > 0 {
		radiusKm = *req.RadiusKm
	}

	selectDistance := "NULL::float8 AS distance_km"
	orderBy := "created_at DESC"
	whereParts := []string{"is_active = TRUE"}
	var args []interface{}
	argN := 1
	matchType := "none"

	switch {
	case matchedStoreID != "":
		whereParts = append(whereParts, fmt.Sprintf("id = $%d::uuid", argN))
		args = append(args, matchedStoreID)
		argN++
		matchType = "vendor"
	case matchedMarketID != "":
		whereParts = append(whereParts, fmt.Sprintf("market_id = $%d::uuid", argN))
		args = append(args, matchedMarketID)
		argN++
		matchType = "market"
	case len(matchedCities) > 0:
		whereParts = append(whereParts, fmt.Sprintf("city = ANY($%d)", argN))
		args = append(args, matchedCities)
		argN++
		matchType = "city"
	case hasLoc:
		selectDistance = fmt.Sprintf(
			"ST_Distance(coordinates::geography, ST_SetSRID(ST_MakePoint($%d,$%d),4326)::geography) / 1000 AS distance_km",
			argN, argN+1)
		args = append(args, *req.Lng, *req.Lat)
		whereParts = append(whereParts, fmt.Sprintf(
			"(coordinates IS NULL OR ST_DWithin(coordinates::geography, ST_SetSRID(ST_MakePoint($%d,$%d),4326)::geography, $%d))",
			argN, argN+1, argN+2))
		args = append(args, radiusKm*1000)
		argN += 3
		orderBy = "(coordinates IS NULL) ASC, distance_km ASC NULLS LAST"
		matchType = "distance"
	}

	if len(categoryList) > 0 {
		whereParts = append(whereParts, fmt.Sprintf("category = ANY($%d)", argN))
		args = append(args, categoryList)
		argN++
	} else if matchedStoreID == "" && matchedMarketID == "" && len(matchedCities) == 0 && req.Q != nil && *req.Q != "" {
		whereParts = append(whereParts, fmt.Sprintf("(name ILIKE $%d OR tagline ILIKE $%d)", argN, argN))
		args = append(args, "%"+*req.Q+"%")
		argN++
	}

	// Fetch one extra row to know whether there's a next page, without a
	// separate COUNT(*) query.
	args = append(args, limit+1, offset)
	query := fmt.Sprintf(`
		SELECT id, name, slug, category, tagline, logo_url, hero_image_url, address, city, state,
		       market_id, (SELECT name FROM markets m WHERE m.id = stores.market_id) AS market_name,
		       %s
		FROM stores
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`,
		selectDistance, strings.Join(whereParts, " AND "), orderBy, argN, argN+1)

	rows, err := s.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return dto.StoreSearchListResp{}, fmt.Errorf("search stores: %w", err)
	}
	defer rows.Close()

	out := []dto.StoreSearchResp{}
	for rows.Next() {
		var r storeSearchRow
		if err := rows.StructScan(&r); err != nil {
			return dto.StoreSearchListResp{}, fmt.Errorf("scan store: %w", err)
		}
		item := dto.StoreSearchResp{
			ID:           r.ID.String(),
			Name:         r.Name,
			Slug:         r.Slug,
			Category:     r.Category,
			Tagline:      nullToPtr(r.Tagline),
			LogoURL:      nullToPtr(r.LogoURL),
			HeroImageURL: nullToPtr(r.HeroImageURL),
			Address:      nullToPtr(r.Address),
			City:         nullToPtr(r.City),
			State:        nullToPtr(r.State),
			MarketID:     nullToPtr(r.MarketID),
			MarketName:   nullToPtr(r.MarketName),
		}
		if r.DistanceKm.Valid {
			d := r.DistanceKm.Float64
			item.DistanceKm = &d
		}
		out = append(out, item)
	}

	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}

	resp := dto.StoreSearchListResp{
		Stores:    out,
		HasMore:   hasMore,
		MatchType: matchType,
	}
	if req.Q != nil {
		resp.RemainingQuery = stripMatchedPhrase(*req.Q, matchedPhrase)
	}
	if matchedStoreID != "" {
		resp.MatchedStoreID = &matchedStoreID
	}
	if matchedMarketID != "" {
		resp.MatchedMarketID = &matchedMarketID
	}
	return resp, nil
}

// ListMarkets returns major markets, optionally filtered by state and/or
// city — populates the vendor-web "which market is your store in?" dropdown.
func (s *StorefrontService) ListMarkets(ctx context.Context, req dto.MarketReq) ([]dto.MarketResp, error) {
	whereParts := []string{"1=1"}
	var args []interface{}
	argN := 1

	if req.State != nil && *req.State != "" {
		whereParts = append(whereParts, fmt.Sprintf("state ILIKE $%d", argN))
		args = append(args, *req.State)
		argN++
	}
	if req.City != nil && *req.City != "" {
		whereParts = append(whereParts, fmt.Sprintf("city ILIKE $%d", argN))
		args = append(args, *req.City)
		argN++
	}

	query := fmt.Sprintf(`SELECT id, name, city, state FROM markets WHERE %s ORDER BY name`, strings.Join(whereParts, " AND "))
	rows, err := s.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list markets: %w", err)
	}
	defer rows.Close()

	out := []dto.MarketResp{}
	for rows.Next() {
		var r struct {
			ID    uuid.UUID `db:"id"`
			Name  string    `db:"name"`
			City  string    `db:"city"`
			State string    `db:"state"`
		}
		if err := rows.StructScan(&r); err != nil {
			return nil, fmt.Errorf("scan market: %w", err)
		}
		out = append(out, dto.MarketResp{ID: r.ID.String(), Name: r.Name, City: r.City, State: r.State})
	}
	return out, nil
}

// ── Staff ─────────────────────────────────────────────────────────────────────

func (s *StorefrontService) ListStaff(ctx context.Context, userID uuid.UUID, storeID uuid.UUID) ([]dto.StaffMemberResp, error) {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryxContext(ctx,
		`SELECT id, user_id, full_name, email, role, invited_at FROM store_staff WHERE store_id=$1 ORDER BY invited_at`,
		storeID)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	defer rows.Close()

	var out []dto.StaffMemberResp
	for rows.Next() {
		var r staffRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		out = append(out, dto.StaffMemberResp{
			ID:        r.ID.String(),
			UserID:    r.UserID.String(),
			FullName:  r.FullName,
			Email:     r.Email,
			Role:      r.Role,
			InvitedAt: r.InvitedAt.UTC().Format(time.RFC3339),
		})
	}
	if out == nil {
		out = []dto.StaffMemberResp{}
	}
	return out, nil
}

func (s *StorefrontService) InviteStaff(ctx context.Context, userID uuid.UUID, storeID uuid.UUID, req dto.InviteStaffReq) (dto.StaffMemberResp, error) {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return dto.StaffMemberResp{}, err
	}
	var r staffRow
	err := s.db.QueryRowxContext(ctx, `
		INSERT INTO store_staff (store_id, user_id, email, role)
		VALUES ($1, gen_random_uuid(), $2, $3)
		ON CONFLICT (store_id, user_id) DO UPDATE SET role=EXCLUDED.role
		RETURNING id, user_id, full_name, email, role, invited_at`,
		storeID, req.Email, req.Role,
	).StructScan(&r)
	if err != nil {
		return dto.StaffMemberResp{}, fmt.Errorf("invite staff: %w", err)
	}
	return dto.StaffMemberResp{
		ID:        r.ID.String(),
		UserID:    r.UserID.String(),
		FullName:  r.FullName,
		Email:     r.Email,
		Role:      r.Role,
		InvitedAt: r.InvitedAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *StorefrontService) RemoveStaff(ctx context.Context, userID uuid.UUID, storeID uuid.UUID, staffID uuid.UUID) error {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM store_staff WHERE id=$1 AND store_id=$2`, staffID, storeID)
	if err != nil {
		return fmt.Errorf("remove staff: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperrors.NotFound("staff member not found")
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type storeRow struct {
	ID                 uuid.UUID      `db:"id"`
	VendorID           uuid.UUID      `db:"vendor_id"`
	Name               string         `db:"name"`
	Slug               string         `db:"slug"`
	Category           string         `db:"category"`
	Currency           string         `db:"currency"`
	TeamSize           sql.NullString `db:"team_size"`
	StaffRange         sql.NullString `db:"staff_range"`
	Tagline            sql.NullString `db:"tagline"`
	LogoURL            sql.NullString `db:"logo_url"`
	HeroImageURL       sql.NullString `db:"hero_image_url"`
	SiteDescription    sql.NullString `db:"site_description"`
	SocialLinks        []byte         `db:"social_links"`
	SupportPhone       sql.NullString `db:"support_phone"`
	Address            sql.NullString `db:"address"`
	City               sql.NullString `db:"city"`
	State              sql.NullString `db:"state"`
	MarketID           sql.NullString `db:"market_id"`
	MarketName         sql.NullString `db:"market_name"`
	CustomDomain       sql.NullString `db:"custom_domain"`
	CustomDomainStatus string         `db:"custom_domain_status"`
	ThemeConfig        []byte         `db:"theme_config"`
	IsActive           bool           `db:"is_active"`
	CreatedAt          time.Time      `db:"created_at"`
}

type staffRow struct {
	ID        uuid.UUID `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	FullName  string    `db:"full_name"`
	Email     string    `db:"email"`
	Role      string    `db:"role"`
	InvitedAt time.Time `db:"invited_at"`
}

func (s *StorefrontService) getStoreByVendor(ctx context.Context, userID uuid.UUID) (dto.StoreResp, error) {
	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT id, vendor_id, name, slug, category, currency,
		       team_size, staff_range, tagline, logo_url, hero_image_url, site_description,
		       COALESCE(social_links, '{}') AS social_links,
		       support_phone, address, city, state, market_id,
		       (SELECT name FROM markets m WHERE m.id = market_id) AS market_name,
		       custom_domain, custom_domain_status,
		       COALESCE(theme_config, '{}') AS theme_config, is_active, created_at
		FROM stores WHERE vendor_id=$1`, userID).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("get store: %w", err)
	}
	return rowToResp(row), nil
}

func (s *StorefrontService) assertOwner(ctx context.Context, userID, storeID uuid.UUID) error {
	var exists bool
	_ = s.db.QueryRowContext(ctx, `SELECT TRUE FROM stores WHERE id=$1 AND vendor_id=$2`, storeID, userID).Scan(&exists)
	if !exists {
		return apperrors.Wrap(http.StatusForbidden, "store not found or access denied", nil)
	}
	return nil
}

func rowToResp(r storeRow) dto.StoreResp {
	resp := dto.StoreResp{
		ID:                 r.ID.String(),
		VendorID:           r.VendorID.String(),
		Name:               r.Name,
		Slug:               r.Slug,
		Category:           r.Category,
		Currency:           r.Currency,
		TeamSize:           nullToPtr(r.TeamSize),
		StaffRange:         nullToPtr(r.StaffRange),
		Tagline:            nullToPtr(r.Tagline),
		LogoURL:            nullToPtr(r.LogoURL),
		HeroImageURL:       nullToPtr(r.HeroImageURL),
		SiteDescription:    nullToPtr(r.SiteDescription),
		SupportPhone:       nullToPtr(r.SupportPhone),
		Address:            nullToPtr(r.Address),
		City:               nullToPtr(r.City),
		State:              nullToPtr(r.State),
		MarketID:           nullToPtr(r.MarketID),
		MarketName:         nullToPtr(r.MarketName),
		CustomDomain:       nullToPtr(r.CustomDomain),
		CustomDomainStatus: r.CustomDomainStatus,
		IsActive:           r.IsActive,
		CreatedAt:          r.CreatedAt.UTC().Format(time.RFC3339),
	}
	// social_links and theme_config are JSONB — only set when non-empty.
	if len(r.SocialLinks) > 0 && string(r.SocialLinks) != "{}" && string(r.SocialLinks) != "null" {
		resp.SocialLinks = r.SocialLinks
	}
	if len(r.ThemeConfig) > 0 && string(r.ThemeConfig) != "{}" && string(r.ThemeConfig) != "null" {
		resp.ThemeConfig = r.ThemeConfig
	}
	return resp
}

func nullToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullJSON returns nil when the raw JSON is empty or a bare null/empty-object,
// so the SQL CASE expression leaves the existing DB value untouched.
func nullJSON(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	if s == "null" || s == "{}" || s == "[]" {
		return nil
	}
	return s
}

// LogView records a storefront page view. It silently ignores unknown slugs.
func (s *StorefrontService) LogView(ctx context.Context, req dto.LogViewReq, ipHash string) {
	var storeID uuid.UUID
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM stores WHERE slug=$1 AND is_active=TRUE`, req.StoreSlug).Scan(&storeID); err != nil {
		return // unknown slug — ignore
	}
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO storefront_views (store_id, slug, path, referrer, ip_hash)
		VALUES ($1,$2,$3,$4,$5)`,
		storeID, req.StoreSlug, req.Path,
		nullStr(req.Referrer), nullStr(ipHash),
	)
}

// GetStoreViews returns view counts for a store.
func (s *StorefrontService) GetStoreViews(ctx context.Context, storeID uuid.UUID) (dto.StoreViewsResp, error) {
	var r dto.StoreViewsResp
	r.StoreID = storeID.String()
	rows := []struct {
		label string
		field *int64
		where string
	}{
		{"30d", &r.Views30d, "NOW() - INTERVAL '30 days'"},
		{"7d", &r.Views7d, "NOW() - INTERVAL '7 days'"},
		{"all", &r.ViewsAll, "'epoch'"},
	}
	for _, row := range rows {
		q := fmt.Sprintf(`SELECT COUNT(*) FROM storefront_views WHERE store_id=$1 AND viewed_at > %s`, row.where)
		if err := s.db.QueryRowContext(ctx, q, storeID).Scan(row.field); err != nil {
			return r, err
		}
	}
	return r, nil
}

func isNotFound(err error) bool {
	return apperrors.IsNotFound(err) || errors.Is(err, sql.ErrNoRows)
}
