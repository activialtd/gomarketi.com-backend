package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/rs/zerolog"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/catalogue/internal/dto"
)

type CatalogueService struct {
	db  *sqlx.DB
	log zerolog.Logger
}

func New(db *sqlx.DB, log zerolog.Logger) *CatalogueService {
	return &CatalogueService{db: db, log: log}
}

// ── Products ──────────────────────────────────────────────────────────────────

func (s *CatalogueService) ListProducts(ctx context.Context, storeID uuid.UUID, page, perPage int, categoryID *string, q *string, publishedOnly bool) (dto.ProductListResp, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	base := `FROM products WHERE store_id=$1`
	args := []any{storeID}
	i := 2

	if publishedOnly {
		base += fmt.Sprintf(` AND is_published=$%d`, i)
		args = append(args, true)
		i++
	}
	if categoryID != nil {
		base += fmt.Sprintf(` AND category_id=$%d`, i)
		args = append(args, *categoryID)
		i++
	}
	if q != nil && *q != "" {
		base += fmt.Sprintf(` AND name ILIKE $%d`, i)
		args = append(args, "%"+*q+"%")
		i++
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+base, args...).Scan(&total); err != nil {
		return dto.ProductListResp{}, fmt.Errorf("count products: %w", err)
	}

	orderArgs := append(args, perPage, offset)
	rows, err := s.db.QueryxContext(ctx,
		`SELECT id, store_id, name, description, category_id, price_kobo, stock, sku,
		        images, tags, is_digital, is_published, created_at, updated_at `+
			base+fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, i, i+1),
		orderArgs...)
	if err != nil {
		return dto.ProductListResp{}, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	products := make([]dto.ProductResp, 0)
	for rows.Next() {
		var r productRow
		if err := rows.StructScan(&r); err != nil {
			return dto.ProductListResp{}, err
		}
		products = append(products, rowToProduct(r))
	}
	return dto.ProductListResp{Products: products, Total: total, Page: page, PerPage: perPage}, nil
}

func (s *CatalogueService) CreateProduct(ctx context.Context, storeID uuid.UUID, req dto.CreateProductReq) (dto.ProductResp, error) {
	var r productRow
	err := s.db.QueryRowxContext(ctx, `
		INSERT INTO products (store_id, name, description, category_id, price_kobo, stock, sku, images, tags, is_digital)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, store_id, name, description, category_id, price_kobo, stock, sku,
		          images, tags, is_digital, is_published, created_at, updated_at`,
		storeID, req.Name, req.Description, req.CategoryID,
		req.PriceKobo, req.Stock, req.SKU,
		pq.Array(req.Images), pq.Array(req.Tags), req.IsDigital,
	).StructScan(&r)
	if err != nil {
		return dto.ProductResp{}, fmt.Errorf("create product: %w", err)
	}
	return rowToProduct(r), nil
}

func (s *CatalogueService) GetProduct(ctx context.Context, storeID uuid.UUID, productID uuid.UUID) (dto.ProductResp, error) {
	var r productRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT id, store_id, name, description, category_id, price_kobo, stock, sku,
		       images, tags, is_digital, is_published, created_at, updated_at
		FROM products WHERE id=$1 AND store_id=$2`, productID, storeID).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ProductResp{}, apperrors.NotFound("product not found")
	}
	if err != nil {
		return dto.ProductResp{}, fmt.Errorf("get product: %w", err)
	}
	return rowToProduct(r), nil
}

func (s *CatalogueService) UpdateProduct(ctx context.Context, storeID uuid.UUID, productID uuid.UUID, req dto.UpdateProductReq) (dto.ProductResp, error) {
	var r productRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE products SET
			name        = COALESCE($1, name),
			description = COALESCE($2, description),
			category_id = COALESCE($3::uuid, category_id),
			price_kobo  = COALESCE($4, price_kobo),
			stock       = COALESCE($5, stock),
			sku         = COALESCE($6, sku),
			images      = CASE WHEN $7::text[] IS NOT NULL THEN $7 ELSE images END,
			tags        = CASE WHEN $8::text[] IS NOT NULL THEN $8 ELSE tags END,
			updated_at  = NOW()
		WHERE id=$9 AND store_id=$10
		RETURNING id, store_id, name, description, category_id, price_kobo, stock, sku,
		          images, tags, is_digital, is_published, created_at, updated_at`,
		req.Name, req.Description, req.CategoryID,
		req.PriceKobo, req.Stock, req.SKU,
		pq.Array(req.Images), pq.Array(req.Tags),
		productID, storeID,
	).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ProductResp{}, apperrors.NotFound("product not found")
	}
	if err != nil {
		return dto.ProductResp{}, fmt.Errorf("update product: %w", err)
	}
	return rowToProduct(r), nil
}

func (s *CatalogueService) DeleteProduct(ctx context.Context, storeID uuid.UUID, productID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM products WHERE id=$1 AND store_id=$2`, productID, storeID)
	if err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperrors.NotFound("product not found")
	}
	return nil
}

func (s *CatalogueService) PublishProduct(ctx context.Context, storeID uuid.UUID, productID uuid.UUID) (dto.ProductResp, error) {
	return s.setPublished(ctx, storeID, productID, true)
}

func (s *CatalogueService) UnpublishProduct(ctx context.Context, storeID uuid.UUID, productID uuid.UUID) (dto.ProductResp, error) {
	return s.setPublished(ctx, storeID, productID, false)
}

func (s *CatalogueService) setPublished(ctx context.Context, storeID, productID uuid.UUID, published bool) (dto.ProductResp, error) {
	var r productRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE products SET is_published=$1, updated_at=NOW()
		WHERE id=$2 AND store_id=$3
		RETURNING id, store_id, name, description, category_id, price_kobo, stock, sku,
		          images, tags, is_digital, is_published, created_at, updated_at`,
		published, productID, storeID).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ProductResp{}, apperrors.NotFound("product not found")
	}
	if err != nil {
		return dto.ProductResp{}, fmt.Errorf("set published: %w", err)
	}
	return rowToProduct(r), nil
}

// ── Categories ────────────────────────────────────────────────────────────────

func (s *CatalogueService) ListCategories(ctx context.Context, storeID uuid.UUID) ([]dto.CategoryResp, error) {
	rows, err := s.db.QueryxContext(ctx,
		`SELECT id, store_id, name, parent_id FROM categories WHERE store_id=$1 ORDER BY name`,
		storeID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	out := make([]dto.CategoryResp, 0)
	for rows.Next() {
		var r categoryRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		out = append(out, rowToCategory(r))
	}
	return out, nil
}

func (s *CatalogueService) CreateCategory(ctx context.Context, storeID uuid.UUID, req dto.CategoryReq) (dto.CategoryResp, error) {
	var r categoryRow
	err := s.db.QueryRowxContext(ctx, `
		INSERT INTO categories (store_id, name, parent_id)
		VALUES ($1,$2,$3)
		ON CONFLICT (store_id, name) DO UPDATE SET name=EXCLUDED.name
		RETURNING id, store_id, name, parent_id`,
		storeID, req.Name, req.ParentID,
	).StructScan(&r)
	if err != nil {
		return dto.CategoryResp{}, fmt.Errorf("create category: %w", err)
	}
	return rowToCategory(r), nil
}

func (s *CatalogueService) UpdateCategory(ctx context.Context, storeID uuid.UUID, categoryID uuid.UUID, req dto.CategoryReq) (dto.CategoryResp, error) {
	var r categoryRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE categories SET name=$1, parent_id=$2
		WHERE id=$3 AND store_id=$4
		RETURNING id, store_id, name, parent_id`,
		req.Name, req.ParentID, categoryID, storeID,
	).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.CategoryResp{}, apperrors.NotFound("category not found")
	}
	if err != nil {
		return dto.CategoryResp{}, fmt.Errorf("update category: %w", err)
	}
	return rowToCategory(r), nil
}

func (s *CatalogueService) DeleteCategory(ctx context.Context, storeID uuid.UUID, categoryID uuid.UUID) error {
	// Products in this category are already set to NULL by the FK ON DELETE SET NULL.
	res, err := s.db.ExecContext(ctx, `DELETE FROM categories WHERE id=$1 AND store_id=$2`, categoryID, storeID)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperrors.NotFound("category not found")
	}
	return nil
}

// ── Row types ─────────────────────────────────────────────────────────────────

type productRow struct {
	ID          uuid.UUID      `db:"id"`
	StoreID     uuid.UUID      `db:"store_id"`
	Name        string         `db:"name"`
	Description sql.NullString `db:"description"`
	CategoryID  sql.NullString `db:"category_id"`
	PriceKobo   int64          `db:"price_kobo"`
	Stock       int32          `db:"stock"`
	SKU         sql.NullString `db:"sku"`
	Images      pq.StringArray `db:"images"`
	Tags        pq.StringArray `db:"tags"`
	IsDigital   bool           `db:"is_digital"`
	IsPublished bool           `db:"is_published"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
}

type categoryRow struct {
	ID       uuid.UUID      `db:"id"`
	StoreID  uuid.UUID      `db:"store_id"`
	Name     string         `db:"name"`
	ParentID sql.NullString `db:"parent_id"`
}

func rowToProduct(r productRow) dto.ProductResp {
	images := []string(r.Images)
	if images == nil {
		images = []string{}
	}
	tags := []string(r.Tags)
	if tags == nil {
		tags = []string{}
	}
	p := dto.ProductResp{
		ID:          r.ID.String(),
		StoreID:     r.StoreID.String(),
		Name:        r.Name,
		PriceKobo:   r.PriceKobo,
		Stock:       r.Stock,
		Images:      images,
		Tags:        tags,
		IsDigital:   r.IsDigital,
		IsPublished: r.IsPublished,
		CreatedAt:   r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   r.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if r.Description.Valid {
		p.Description = &r.Description.String
	}
	if r.CategoryID.Valid {
		p.CategoryID = &r.CategoryID.String
	}
	if r.SKU.Valid {
		p.SKU = &r.SKU.String
	}
	return p
}

func rowToCategory(r categoryRow) dto.CategoryResp {
	c := dto.CategoryResp{
		ID:      r.ID.String(),
		StoreID: r.StoreID.String(),
		Name:    r.Name,
	}
	if r.ParentID.Valid {
		c.ParentID = &r.ParentID.String
	}
	return c
}
