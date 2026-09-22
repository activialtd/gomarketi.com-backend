package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/activialtd/gomarketi.com-backend/services/catalogue/internal/dto"
)

// Search returns products and vendors for one query.
//
// Matching is deliberately forgiving: a product is a hit when its name is
// trigram-similar to the query (which covers misspellings and partial words),
// when the query appears as a substring, or when full-text matches across
// name, tags and description. Results are ranked by how strong the match is,
// not by recency, so "rice" puts a product called Rice above one whose
// description merely mentions rice.
//
// Vendors are searched the same way over store name, tagline and category, so
// a shopper can look for a seller directly instead of only reaching one
// through a product.

// SearchParams carries the query string parameters of the search endpoints.
type SearchParams struct {
	Query      string
	CategoryID string
	StoreIDs   []uuid.UUID // optional scope; empty means the whole platform
	Limit      int
	Offset     int
	// Include decides which sections are populated: "all", "products" or
	// "vendors". The client uses "vendors" for an explicit vendor search.
	Include string
}

// Fuzzy matching pairs two clauses. `%` is the only trigram comparison a GIN
// index can answer, so it carries the query; its threshold is a per-database
// setting (lowered to 0.2 in catalogue migration 0004). similarity() >= the
// floor below repeats that test explicitly, so results stay identical on
// connections that opened before the setting existed and on managed Postgres
// where altering the database is not permitted.

// trigramFloor is how similar a name must be to count as a fuzzy match.
// Low enough that "rise" still reaches "rice", high enough to keep unrelated
// words out.
const trigramFloor = 0.2

// shortQueryLen is the point below which trigram similarity stops being
// meaningful and prefix matching does the work instead.
const shortQueryLen = 3

func (s *CatalogueService) Search(ctx context.Context, p SearchParams) (dto.SearchResp, error) {
	if p.Limit <= 0 || p.Limit > 50 {
		p.Limit = 24
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	include := p.Include
	if include == "" {
		include = "all"
	}

	q := strings.TrimSpace(p.Query)
	resp := dto.SearchResp{
		Query:           q,
		Products:        []dto.ProductResp{},
		Vendors:         []dto.VendorResult{},
		RelatedProducts: []dto.ProductResp{},
		Suggestions:     []string{},
	}

	if include == "all" || include == "products" {
		products, hasMore, err := s.searchProducts(ctx, q, p)
		if err != nil {
			return dto.SearchResp{}, err
		}
		resp.Products = products
		resp.ProductsHasMore = hasMore
	}

	if include == "all" || include == "vendors" {
		vendors, hasMore, err := s.searchVendors(ctx, q, p.Limit, p.Offset)
		if err != nil {
			return dto.SearchResp{}, err
		}
		resp.Vendors = vendors
		resp.VendorsHasMore = hasMore
	}

	// Related products and suggestions only make sense alongside product
	// results, and only for the first page — they don't paginate.
	if (include == "all" || include == "products") && p.Offset == 0 && len(resp.Products) > 0 {
		related, err := s.relatedProducts(ctx, resp.Products, p)
		if err != nil {
			return dto.SearchResp{}, err
		}
		resp.RelatedProducts = related
	}
	if q != "" && p.Offset == 0 {
		suggestions, err := s.suggestions(ctx, q)
		if err != nil {
			return dto.SearchResp{}, err
		}
		resp.Suggestions = suggestions
	}

	return resp, nil
}

// searchProducts ranks published products against the query. An empty query
// returns the newest products, which is what an empty search box should show.
func (s *CatalogueService) searchProducts(ctx context.Context, q string, p SearchParams) ([]dto.ProductResp, bool, error) {
	var (
		where []string
		args  []any
		i     = 1
	)

	where = append(where, "p.is_published = TRUE")

	if len(p.StoreIDs) > 0 {
		where = append(where, fmt.Sprintf("p.store_id = ANY($%d)", i))
		ids := make([]string, len(p.StoreIDs))
		for n, id := range p.StoreIDs {
			ids[n] = id.String()
		}
		args = append(args, pq.Array(ids))
		i++
	}
	if p.CategoryID != "" {
		where = append(where, fmt.Sprintf("p.category_id = $%d::uuid", i))
		args = append(args, p.CategoryID)
		i++
	}

	rank := "0::float"
	if q != "" {
		term := strings.ToLower(q)
		// $i is the query term, reused across every match clause.
		where = append(where, fmt.Sprintf(`(
			LOWER(p.name) %% $%[1]d
			OR similarity(LOWER(p.name), $%[1]d) >= %[2]f
			OR LOWER(p.name) LIKE '%%' || $%[1]d || '%%'
			OR LOWER(COALESCE(p.description, '')) LIKE '%%' || $%[1]d || '%%'
			OR p.search_doc @@ websearch_to_tsquery('simple', $%[1]d)
			OR EXISTS (
				SELECT 1 FROM unnest(p.tags) t
				WHERE LOWER(t) %% $%[1]d
				   OR similarity(LOWER(t), $%[1]d) >= %[2]f
				   OR LOWER(t) LIKE '%%' || $%[1]d || '%%'
			)
		)`, i, trigramFloor))

		// Ranking, highest first:
		//   3.0  name starts with the query        ("rice" → "Rice, Basmati")
		//   2.0  name contains it
		//   0–2  trigram similarity on the name    (typo tolerance)
		//   0–1  full-text rank across all fields
		rank = fmt.Sprintf(`(
			CASE WHEN LOWER(p.name) LIKE $%[1]d || '%%' THEN 3.0 ELSE 0 END
			+ CASE WHEN LOWER(p.name) LIKE '%%' || $%[1]d || '%%' THEN 2.0 ELSE 0 END
			+ similarity(LOWER(p.name), $%[1]d) * 2.0
			+ ts_rank(p.search_doc, websearch_to_tsquery('simple', $%[1]d))
		)`, i)

		args = append(args, term)
		i++
	}

	// One extra row answers has_more without a second COUNT.
	args = append(args, p.Limit+1, p.Offset)

	query := fmt.Sprintf(`
		SELECT p.id, p.store_id, p.name, p.description, p.category_id, p.price_kobo,
		       p.stock, p.sku, p.images, p.tags, p.is_digital, p.is_published,
		       p.created_at, p.updated_at
		FROM products p
		WHERE %s
		ORDER BY %s DESC, p.created_at DESC
		LIMIT $%d OFFSET $%d`,
		strings.Join(where, " AND "), rank, i, i+1)

	rows, err := s.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("search products: %w", err)
	}
	defer rows.Close()

	out := make([]dto.ProductResp, 0)
	for rows.Next() {
		var r productRow
		if err := rows.StructScan(&r); err != nil {
			return nil, false, fmt.Errorf("scan product: %w", err)
		}
		out = append(out, rowToProduct(r))
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("scan products: %w", err)
	}

	hasMore := len(out) > p.Limit
	if hasMore {
		out = out[:p.Limit]
	}
	return out, hasMore, nil
}

// searchVendors ranks stores against the query over name, tagline, category
// and the market they sit in.
func (s *CatalogueService) searchVendors(ctx context.Context, q string, limit, offset int) ([]dto.VendorResult, bool, error) {
	var (
		where = []string{"s.is_active = TRUE"}
		args  []any
		rank  = "0::float"
		i     = 1
	)

	if q != "" {
		term := strings.ToLower(q)
		where = append(where, fmt.Sprintf(`(
			LOWER(s.name) %% $%[1]d
			OR similarity(LOWER(s.name), $%[1]d) >= %[2]f
			OR LOWER(s.name) LIKE '%%' || $%[1]d || '%%'
			OR LOWER(COALESCE(s.tagline, '')) LIKE '%%' || $%[1]d || '%%'
			OR LOWER(s.category) LIKE '%%' || $%[1]d || '%%'
			OR LOWER(COALESCE(m.name, '')) LIKE '%%' || $%[1]d || '%%'
		)`, i, trigramFloor))
		rank = fmt.Sprintf(`(
			CASE WHEN LOWER(s.name) LIKE $%[1]d || '%%' THEN 3.0 ELSE 0 END
			+ CASE WHEN LOWER(s.name) LIKE '%%' || $%[1]d || '%%' THEN 2.0 ELSE 0 END
			+ similarity(LOWER(s.name), $%[1]d) * 2.0
			+ CASE WHEN LOWER(s.category) LIKE '%%' || $%[1]d || '%%' THEN 0.5 ELSE 0 END
		)`, i)
		args = append(args, term)
		i++
	}

	args = append(args, limit+1, offset)

	query := fmt.Sprintf(`
		SELECT s.id, s.name, s.slug, s.category, s.tagline, s.logo_url,
		       s.city, s.state, s.market_id, m.name AS market_name,
		       (SELECT COUNT(*) FROM products pr
		         WHERE pr.store_id = s.id AND pr.is_published = TRUE) AS product_count
		FROM stores s
		LEFT JOIN markets m ON m.id = s.market_id
		WHERE %s
		ORDER BY %s DESC, product_count DESC, s.created_at DESC
		LIMIT $%d OFFSET $%d`,
		strings.Join(where, " AND "), rank, i, i+1)

	rows, err := s.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("search vendors: %w", err)
	}
	defer rows.Close()

	out := make([]dto.VendorResult, 0)
	for rows.Next() {
		var (
			v          dto.VendorResult
			tagline    sql.NullString
			logoURL    sql.NullString
			city       sql.NullString
			state      sql.NullString
			marketID   uuid.NullUUID
			marketName sql.NullString
		)
		if err := rows.Scan(
			&v.ID, &v.Name, &v.Slug, &v.Category, &tagline, &logoURL,
			&city, &state, &marketID, &marketName, &v.ProductCount,
		); err != nil {
			return nil, false, fmt.Errorf("scan vendor: %w", err)
		}
		v.Tagline = nullStrPtr(tagline)
		v.LogoURL = nullStrPtr(logoURL)
		v.City = nullStrPtr(city)
		v.State = nullStrPtr(state)
		v.MarketName = nullStrPtr(marketName)
		if marketID.Valid {
			id := marketID.UUID.String()
			v.MarketID = &id
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("scan vendors: %w", err)
	}

	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

// relatedProducts finds published products sharing a category or tag with the
// matches, excluding the matches themselves — the "you might also like" rail.
func (s *CatalogueService) relatedProducts(ctx context.Context, matches []dto.ProductResp, p SearchParams) ([]dto.ProductResp, error) {
	matchedIDs := make([]string, 0, len(matches))
	categories := make([]string, 0, len(matches))
	tags := make([]string, 0)
	seenTag := map[string]bool{}
	for _, m := range matches {
		matchedIDs = append(matchedIDs, m.ID)
		if m.CategoryID != nil {
			categories = append(categories, *m.CategoryID)
		}
		for _, t := range m.Tags {
			if !seenTag[t] {
				seenTag[t] = true
				tags = append(tags, t)
			}
		}
	}
	if len(categories) == 0 && len(tags) == 0 {
		return []dto.ProductResp{}, nil
	}

	rows, err := s.db.QueryxContext(ctx, `
		SELECT p.id, p.store_id, p.name, p.description, p.category_id, p.price_kobo,
		       p.stock, p.sku, p.images, p.tags, p.is_digital, p.is_published,
		       p.created_at, p.updated_at
		FROM products p
		WHERE p.is_published = TRUE
		  AND NOT (p.id::text = ANY($1))
		  AND (p.category_id::text = ANY($2) OR p.tags && $3)
		ORDER BY p.created_at DESC
		LIMIT 12`,
		pq.Array(matchedIDs), pq.Array(categories), pq.Array(tags),
	)
	if err != nil {
		return nil, fmt.Errorf("related products: %w", err)
	}
	defer rows.Close()

	out := make([]dto.ProductResp, 0)
	for rows.Next() {
		var r productRow
		if err := rows.StructScan(&r); err != nil {
			return nil, fmt.Errorf("scan related product: %w", err)
		}
		out = append(out, rowToProduct(r))
	}
	return out, rows.Err()
}

// suggestions returns query completions drawn from product names and tags —
// what the search box offers as the shopper types, and what rescues a query
// that returned nothing.
func (s *CatalogueService) suggestions(ctx context.Context, q string) ([]string, error) {
	term := strings.ToLower(q)

	// Short queries use prefix matching; trigram similarity is noise below
	// three characters.
	var (
		rows *sql.Rows
		err  error
	)
	if len(term) < shortQueryLen {
		rows, err = s.db.QueryContext(ctx, `
			SELECT DISTINCT name FROM products
			WHERE is_published = TRUE AND LOWER(name) LIKE $1 || '%'
			ORDER BY name LIMIT 8`, term)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT term FROM (
				SELECT name AS term, similarity(LOWER(name), $1) AS sim
				FROM products
				WHERE is_published = TRUE
				  AND (LOWER(name) % $1 OR similarity(LOWER(name), $1) >= $2
			       OR LOWER(name) LIKE '%' || $1 || '%')
				UNION
				SELECT t AS term, similarity(LOWER(t), $1) AS sim
				FROM products, unnest(tags) t
				WHERE is_published = TRUE
				  AND (LOWER(t) % $1 OR similarity(LOWER(t), $1) >= $2
				       OR LOWER(t) LIKE '%' || $1 || '%')
			) s
			ORDER BY sim DESC, term
			LIMIT 8`, term, trigramFloor)
	}
	if err != nil {
		return nil, fmt.Errorf("suggestions: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0, 8)
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan suggestion: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func nullStrPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

// SearchCanonicalProducts backs the typeahead in the vendor product form:
// the vendor picks the shared catalogue entry their listing represents, which
// is what lets the consumer app group the same item across vendors.
//
// The table is curated, so an empty result simply means no canonical entry
// exists yet and the listing saves unlinked.
func (s *CatalogueService) SearchCanonicalProducts(ctx context.Context, q string) (dto.CanonicalProductSearchResp, error) {
	resp := dto.CanonicalProductSearchResp{Products: []dto.CanonicalProductResp{}}
	q = strings.TrimSpace(q)
	if q == "" {
		return resp, nil
	}

	term := strings.ToLower(q)
	rows, err := s.db.QueryxContext(ctx, `
		SELECT id, name, representative_image
		FROM canonical_products
		WHERE LOWER(name) % $1
		   OR similarity(LOWER(name), $1) >= $2
		   OR LOWER(name) LIKE '%' || $1 || '%'
		ORDER BY
			CASE WHEN LOWER(name) LIKE $1 || '%' THEN 0 ELSE 1 END,
			similarity(LOWER(name), $1) DESC,
			name
		LIMIT 10`, term, trigramFloor)
	if err != nil {
		return dto.CanonicalProductSearchResp{}, fmt.Errorf("search canonical products: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cp  dto.CanonicalProductResp
			img sql.NullString
		)
		if err := rows.Scan(&cp.ID, &cp.Name, &img); err != nil {
			return dto.CanonicalProductSearchResp{}, fmt.Errorf("scan canonical product: %w", err)
		}
		if img.Valid {
			v := img.String
			cp.RepresentativeImage = &v
		}
		resp.Products = append(resp.Products, cp)
	}
	return resp, rows.Err()
}
