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

// ── Collections ───────────────────────────────────────────────────────────────

type collectionRow struct {
	ID          uuid.UUID      `db:"id"`
	StoreID     uuid.UUID      `db:"store_id"`
	Name        string         `db:"name"`
	Description sql.NullString `db:"description"`
	ImageURL    sql.NullString `db:"image_url"`
	IsPublished bool           `db:"is_published"`
	CreatedAt   time.Time      `db:"created_at"`
}

func (s *CatalogueService) ListCollections(ctx context.Context, storeID uuid.UUID) (dto.CollectionListResp, error) {
	rows, err := s.db.QueryxContext(ctx, `SELECT id, store_id, name, description, image_url, is_published, created_at FROM collections WHERE store_id=$1 ORDER BY created_at DESC`, storeID)
	if err != nil {
		return dto.CollectionListResp{}, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close()

	var out []dto.CollectionResp
	for rows.Next() {
		var r collectionRow
		if err := rows.StructScan(&r); err != nil {
			return dto.CollectionListResp{}, err
		}
		col := rowToCollection(r)
		col.ProductIDs = s.collectionProductIDs(ctx, r.ID)
		out = append(out, col)
	}
	if out == nil {
		out = []dto.CollectionResp{}
	}
	return dto.CollectionListResp{Collections: out}, nil
}

func (s *CatalogueService) CreateCollection(ctx context.Context, storeID uuid.UUID, req dto.CreateCollectionReq) (dto.CollectionResp, error) {
	var r collectionRow
	err := s.db.QueryRowxContext(ctx, `
		INSERT INTO collections (store_id, name, description, image_url)
		VALUES ($1,$2,$3,$4)
		RETURNING id, store_id, name, description, image_url, is_published, created_at`,
		storeID, req.Name, req.Description, req.ImageURL,
	).StructScan(&r)
	if err != nil {
		if isPGConflict(err) {
			return dto.CollectionResp{}, apperrors.Conflict("a collection with that name already exists")
		}
		return dto.CollectionResp{}, fmt.Errorf("create collection: %w", err)
	}
	col := rowToCollection(r)
	if len(req.ProductIDs) > 0 {
		_ = s.setCollectionProducts(ctx, r.ID, req.ProductIDs)
		col.ProductIDs = req.ProductIDs
	} else {
		col.ProductIDs = []string{}
	}
	return col, nil
}

func (s *CatalogueService) UpdateCollection(ctx context.Context, storeID, colID uuid.UUID, req dto.UpdateCollectionReq) (dto.CollectionResp, error) {
	var r collectionRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE collections SET
			name        = COALESCE($1, name),
			description = COALESCE($2, description),
			image_url   = COALESCE($3, image_url),
			updated_at  = NOW()
		WHERE id=$4 AND store_id=$5
		RETURNING id, store_id, name, description, image_url, is_published, created_at`,
		req.Name, req.Description, req.ImageURL, colID, storeID,
	).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.CollectionResp{}, apperrors.NotFound("collection not found")
	}
	if err != nil {
		return dto.CollectionResp{}, fmt.Errorf("update collection: %w", err)
	}
	col := rowToCollection(r)
	if req.ProductIDs != nil {
		_ = s.setCollectionProducts(ctx, r.ID, req.ProductIDs)
		col.ProductIDs = req.ProductIDs
	} else {
		col.ProductIDs = s.collectionProductIDs(ctx, r.ID)
	}
	return col, nil
}

func (s *CatalogueService) DeleteCollection(ctx context.Context, storeID, colID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM collections WHERE id=$1 AND store_id=$2`, colID, storeID)
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperrors.NotFound("collection not found")
	}
	return nil
}

func (s *CatalogueService) PublishCollection(ctx context.Context, storeID, colID uuid.UUID, publish bool) (dto.CollectionResp, error) {
	var r collectionRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE collections SET is_published=$1, updated_at=NOW()
		WHERE id=$2 AND store_id=$3
		RETURNING id, store_id, name, description, image_url, is_published, created_at`,
		publish, colID, storeID,
	).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.CollectionResp{}, apperrors.NotFound("collection not found")
	}
	if err != nil {
		return dto.CollectionResp{}, fmt.Errorf("publish collection: %w", err)
	}
	col := rowToCollection(r)
	col.ProductIDs = s.collectionProductIDs(ctx, r.ID)
	return col, nil
}

func (s *CatalogueService) setCollectionProducts(ctx context.Context, colID uuid.UUID, productIDs []string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM collection_products WHERE collection_id=$1`, colID); err != nil {
		return err
	}
	for i, pid := range productIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO collection_products (collection_id, product_id, position) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, colID, pid, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *CatalogueService) collectionProductIDs(ctx context.Context, colID uuid.UUID) []string {
	rows, err := s.db.QueryContext(ctx, `SELECT product_id FROM collection_products WHERE collection_id=$1 ORDER BY position`, colID)
	if err != nil {
		return []string{}
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	if ids == nil {
		return []string{}
	}
	return ids
}

func rowToCollection(r collectionRow) dto.CollectionResp {
	col := dto.CollectionResp{
		ID:          r.ID.String(),
		StoreID:     r.StoreID.String(),
		Name:        r.Name,
		IsPublished: r.IsPublished,
		CreatedAt:   r.CreatedAt.UTC().Format(time.RFC3339),
		ProductIDs:  []string{},
	}
	if r.Description.Valid {
		col.Description = &r.Description.String
	}
	if r.ImageURL.Valid {
		col.ImageURL = &r.ImageURL.String
	}
	return col
}

func isPGConflict(err error) bool {
	var pgErr *pq.Error
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
