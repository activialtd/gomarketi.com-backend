// Package dto defines request and response shapes for the catalogue service.
package dto

// ── Products ──────────────────────────────────────────────────────────────────

// CreateProductReq is the body for POST /v1/catalogue/products.
type CreateProductReq struct {
	Name        string   `json:"name"         validate:"required,min=1,max=500"`
	Description *string  `json:"description"`
	CategoryID  *string  `json:"category_id"  validate:"omitempty,uuid"`
	PriceKobo   int64    `json:"price_kobo"   validate:"min=0"`
	Stock       int32    `json:"stock"        validate:"min=0"`
	SKU         *string  `json:"sku"          validate:"omitempty,max=100"`
	Images      []string `json:"images"`
	Tags        []string `json:"tags"`
	IsDigital   bool     `json:"is_digital"`
	// CanonicalProductID links this listing to the shared catalogue entry,
	// so the same item sold by several vendors can be grouped.
	CanonicalProductID *string `json:"canonical_product_id" validate:"omitempty,uuid"`
}

// UpdateProductReq is the body for PATCH /v1/catalogue/products/:id.
// All fields are optional (PATCH semantics).
type UpdateProductReq struct {
	CanonicalProductID *string  `json:"canonical_product_id" validate:"omitempty,uuid"`
	Name               *string  `json:"name"        validate:"omitempty,min=1,max=500"`
	Description        *string  `json:"description"`
	CategoryID         *string  `json:"category_id" validate:"omitempty,uuid"`
	PriceKobo          *int64   `json:"price_kobo"  validate:"omitempty,min=0"`
	Stock              *int32   `json:"stock"       validate:"omitempty,min=0"`
	SKU                *string  `json:"sku"         validate:"omitempty,max=100"`
	Images             []string `json:"images"`
	Tags               []string `json:"tags"`
}

// ProductResp is returned for any product read or write operation.
type ProductResp struct {
	ID                 string   `json:"id"`
	CanonicalProductID *string  `json:"canonical_product_id,omitempty"`
	StoreID            string   `json:"store_id"`
	Name               string   `json:"name"`
	Description        *string  `json:"description,omitempty"`
	CategoryID         *string  `json:"category_id,omitempty"`
	PriceKobo          int64    `json:"price_kobo"`
	Stock              int32    `json:"stock"`
	SKU                *string  `json:"sku,omitempty"`
	Images             []string `json:"images"`
	Tags               []string `json:"tags"`
	IsDigital          bool     `json:"is_digital"`
	IsPublished        bool     `json:"is_published"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

// ProductListResp wraps a paginated list of products.
type ProductListResp struct {
	Products []ProductResp `json:"products"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PerPage  int           `json:"per_page"`
}

// ── Categories ────────────────────────────────────────────────────────────────

// CategoryReq is the body for POST and PATCH /v1/catalogue/categories[/:id].
type CategoryReq struct {
	Name     string  `json:"name"      validate:"required,min=1,max=100"`
	ParentID *string `json:"parent_id" validate:"omitempty,uuid"`
}

// CategoryResp is a single category in the response.
type CategoryResp struct {
	ID       string  `json:"id"`
	StoreID  string  `json:"store_id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id,omitempty"`
}

// ── Collections ───────────────────────────────────────────────────────────────

type CreateCollectionReq struct {
	Name        string   `json:"name"        validate:"required,min=1,max=200"`
	Description *string  `json:"description"`
	ImageURL    *string  `json:"image_url"`
	ProductIDs  []string `json:"product_ids" validate:"omitempty,dive,uuid"`
}

type UpdateCollectionReq struct {
	Name        *string  `json:"name"        validate:"omitempty,min=1,max=200"`
	Description *string  `json:"description"`
	ImageURL    *string  `json:"image_url"`
	ProductIDs  []string `json:"product_ids" validate:"omitempty,dive,uuid"`
}

type CollectionResp struct {
	ID          string   `json:"id"`
	StoreID     string   `json:"store_id"`
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	ImageURL    *string  `json:"image_url,omitempty"`
	IsPublished bool     `json:"is_published"`
	ProductIDs  []string `json:"product_ids"`
	CreatedAt   string   `json:"created_at"`
}

type CollectionListResp struct {
	Collections []CollectionResp `json:"collections"`
}

// ── Shared ────────────────────────────────────────────────────────────────────

// ErrorResp is the standard error envelope.
type ErrorResp struct {
	Error string `json:"error"`
}

// ValidationErrorResp wraps field-level validation failures.
type ValidationErrorResp struct {
	Error  string       `json:"error"`
	Fields []FieldError `json:"fields,omitempty"`
}

// FieldError is a single field validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ── Search ────────────────────────────────────────────────────────────────────

// VendorResult is a store in a search response — enough to render a vendor
// card and link through to the storefront.
type VendorResult struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Slug       string  `json:"slug"`
	Category   string  `json:"category"`
	Tagline    *string `json:"tagline,omitempty"`
	LogoURL    *string `json:"logo_url,omitempty"`
	City       *string `json:"city,omitempty"`
	State      *string `json:"state,omitempty"`
	MarketID   *string `json:"market_id,omitempty"`
	MarketName *string `json:"market_name,omitempty"`
	// ProductCount is how many published products the vendor has, so the UI
	// can say "24 products" on a vendor card.
	ProductCount int64 `json:"product_count"`
}

// SearchResp is the unified search payload: products and vendors for one
// query, plus related products and query suggestions.
//
// Both lists are always present (empty rather than null) so the client can
// render a "Products" and a "Vendors" section without null checks.
type SearchResp struct {
	Query           string         `json:"query"`
	Products        []ProductResp  `json:"products"`
	ProductsHasMore bool           `json:"products_has_more"`
	Vendors         []VendorResult `json:"vendors"`
	VendorsHasMore  bool           `json:"vendors_has_more"`
	// RelatedProducts share a category or tag with the top matches but did
	// not match the query themselves — the "you might also like" rail.
	RelatedProducts []ProductResp `json:"related_products"`
	// Suggestions are query completions drawn from product names and tags,
	// for the "did you mean" / autocomplete row.
	Suggestions []string `json:"suggestions"`
}

// CanonicalProductResp is one entry in the shared catalogue.
type CanonicalProductResp struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	RepresentativeImage *string `json:"representative_image,omitempty"`
}

// CanonicalProductSearchResp wraps the typeahead results.
type CanonicalProductSearchResp struct {
	Products []CanonicalProductResp `json:"products"`
}
