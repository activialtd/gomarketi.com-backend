// Package main seeds ~20 demo stores scattered across real Lagos markets,
// each with a handful of products, so the consumer-app search/browse flow
// has real data to work against on staging.
//
// Usage (from repo root):
//
//	DATABASE_URL=... go run ./scripts/seed-marketplace
//
// Safe to re-run: stores are upserted by slug, products are only inserted
// the first time a store is created (checked via product count).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type product struct {
	name      string
	priceKobo int64
	stock     int32
	tags      []string
	desc      string
}

type market struct {
	name    string
	city    string
	state   string
	aliases []string
}

type store struct {
	name     string
	slug     string
	category string
	tagline  string
	area     string // market/neighbourhood name (display only, e.g. "Balogun Market")
	market   string // matches a market.name above; empty = standalone, no named market
	city     string
	state    string
	lat      float64
	lng      float64
	products []product
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("opening connection: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(3)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	log.Println("connected to database")

	marketIDs := make(map[string]string) // market name -> id
	for _, m := range buildMarkets() {
		id, err := seedMarket(ctx, db, m)
		if err != nil {
			log.Fatalf("seeding market %s: %v", m.name, err)
		}
		marketIDs[m.name] = id
		log.Printf("  market  %-28s %s, %s", m.name, m.city, m.state)
	}

	stores := buildStores()

	for _, st := range stores {
		var marketID *string
		if st.market != "" {
			id, ok := marketIDs[st.market]
			if !ok {
				log.Fatalf("store %s references unknown market %q", st.name, st.market)
			}
			marketID = &id
		}
		if err := seedStore(ctx, db, st, marketID); err != nil {
			log.Fatalf("seeding %s: %v", st.name, err)
		}
		log.Printf("  ok  %-32s %-12s %s", st.name, st.category, st.area)
	}

	log.Printf("done — %d markets, %d stores seeded", len(marketIDs), len(stores))
}

func seedMarket(ctx context.Context, db *sql.DB, m market) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, `
		INSERT INTO markets (name, city, state, aliases)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name, state) DO UPDATE SET
			city    = EXCLUDED.city,
			aliases = EXCLUDED.aliases
		RETURNING id`,
		m.name, m.city, m.state, m.aliases,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("upsert market: %w", err)
	}
	return id, nil
}

func seedStore(ctx context.Context, db *sql.DB, st store, marketID *string) error {
	vendorID := uuid.New()

	var storeID string
	err := db.QueryRowContext(ctx, `
		INSERT INTO stores (vendor_id, name, slug, category, currency, address, city, state, tagline, coordinates, market_id, is_active)
		VALUES ($1, $2, $3, $4, 'NGN', $5, $6, $7, $8, ST_SetSRID(ST_MakePoint($9, $10), 4326), $11, TRUE)
		ON CONFLICT (slug) DO UPDATE SET
			category    = EXCLUDED.category,
			address     = EXCLUDED.address,
			city        = EXCLUDED.city,
			state       = EXCLUDED.state,
			tagline     = EXCLUDED.tagline,
			coordinates = EXCLUDED.coordinates,
			market_id   = EXCLUDED.market_id
		RETURNING id`,
		vendorID, st.name, st.slug, st.category, st.area, st.city, st.state, st.tagline, st.lng, st.lat, marketID,
	).Scan(&storeID)
	if err != nil {
		return fmt.Errorf("upsert store: %w", err)
	}

	var existingProducts int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE store_id = $1`, storeID).Scan(&existingProducts); err != nil {
		return fmt.Errorf("count products: %w", err)
	}
	if existingProducts > 0 {
		return nil // already seeded, don't duplicate products on re-run
	}

	for _, p := range st.products {
		sku := strings.ToUpper(st.slug[:min(4, len(st.slug))]) + "-" + uuid.New().String()[:8]
		_, err := db.ExecContext(ctx, `
			INSERT INTO products (store_id, name, description, price_kobo, stock, sku, images, tags, is_published)
			VALUES ($1, $2, $3, $4, $5, $6, '{}', $7, TRUE)`,
			storeID, p.name, p.desc, p.priceKobo, p.stock, sku, p.tags,
		)
		if err != nil {
			return fmt.Errorf("insert product %q: %w", p.name, err)
		}
	}
	return nil
}

func buildMarkets() []market {
	return []market{
		{name: "Balogun Market", city: "Lagos Island", state: "Lagos", aliases: []string{"balogun"}},
		{name: "Idumota Market", city: "Lagos Island", state: "Lagos", aliases: []string{"idumota"}},
		{name: "Surulere Market", city: "Surulere", state: "Lagos", aliases: []string{"surulere market"}},
		{name: "Yaba Market", city: "Yaba", state: "Lagos", aliases: []string{"yaba market"}},
		{name: "Tejuosho Market", city: "Yaba", state: "Lagos", aliases: []string{"tejuosho"}},
		{name: "Ojuelegba Market", city: "Surulere", state: "Lagos", aliases: []string{"ojuelegba"}},
		{name: "Ketu Market", city: "Ketu", state: "Lagos", aliases: []string{"ketu market"}},
		{name: "Alaba International Market", city: "Ojo", state: "Lagos", aliases: []string{"alaba", "alaba international"}},
		{name: "Computer Village", city: "Ikeja", state: "Lagos", aliases: []string{"computer village", "cv"}},
		{name: "Trade Fair Complex", city: "Okokomaiko", state: "Lagos", aliases: []string{"trade fair", "tradefair"}},
		{name: "Mile 12 Market", city: "Kosofe", state: "Lagos", aliases: []string{"mile 12", "mile12"}},
		{name: "Oyingbo Market", city: "Ebute-Metta", state: "Lagos", aliases: []string{"oyingbo"}},
		{name: "Agege Market", city: "Agege", state: "Lagos", aliases: []string{"agege market"}},
		{name: "Oshodi Market", city: "Oshodi", state: "Lagos", aliases: []string{"oshodi market"}},
		{name: "Lekki Market", city: "Lekki", state: "Lagos", aliases: []string{"lekki market", "lekki arts"}},
		{name: "Ajah Market", city: "Ajah", state: "Lagos", aliases: []string{"ajah market"}},
		{name: "Apapa Market", city: "Apapa", state: "Lagos", aliases: []string{"apapa market"}},
		{name: "Ikorodu Market", city: "Ikorodu", state: "Lagos", aliases: []string{"ikorodu market"}},
		{name: "Awka Main Market", city: "Awka", state: "Anambra", aliases: []string{"awka main", "eke awka"}},
		{name: "Onitsha Main Market", city: "Onitsha", state: "Anambra", aliases: []string{"onitsha main", "onitsha market"}},
		{name: "Nkwo Nnewi Market", city: "Nnewi", state: "Anambra", aliases: []string{"nkwo nnewi", "nnewi market"}},
	}
}

func buildStores() []store {
	return []store{
		// ── Fashion (Balogun, Idumota, Surulere) ──────────────────────────────
		{
			name: "Balogun Fashion House", slug: "balogun-fashion-house", category: "fashion",
			tagline: "Native wear and ready-to-wear for every occasion",
			area:    "Balogun Market", market: "Balogun Market", city: "Lagos Island", state: "Lagos", lat: 6.4531, lng: 3.3958,
			products: []product{
				{"Men's Ankara Kaftan", 1500000, 40, []string{"men", "clothes", "native wear"}, "Hand-tailored ankara kaftan, breathable cotton lining."},
				{"Women's Lace Gown", 2200000, 25, []string{"women", "clothes", "gown"}, "Premium French lace gown, made to order."},
				{"Men's Senator Wear", 1800000, 30, []string{"men", "clothes", "native wear"}, "Classic senator two-piece, dry-clean only."},
				{"Ankara Print Shirt", 900000, 60, []string{"men", "clothes", "shirt"}, "Short-sleeve ankara shirt, sizes M-XXL."},
			},
		},
		{
			name: "Balogun Textile Emporium", slug: "balogun-textile-emporium", category: "fashion",
			tagline: "Wholesale fabrics and made-to-measure tailoring",
			area:    "Balogun Market", market: "Balogun Market", city: "Lagos Island", state: "Lagos", lat: 6.4534, lng: 3.3961,
			products: []product{
				{"Aso Oke Fabric (Full Set)", 3500000, 18, []string{"men", "women", "clothes", "fabric"}, "Hand-woven aso oke, full gele and wrapper set."},
				{"Men's Tailored Shirt", 1100000, 30, []string{"men", "clothes", "shirt"}, "Made-to-measure, choice of fabric."},
			},
		},
		{
			name: "Idumota Trendy Wears", slug: "idumota-trendy-wears", category: "fashion",
			tagline: "Everyday fashion at Lagos Island prices",
			area:    "Idumota Market", market: "Idumota Market", city: "Lagos Island", state: "Lagos", lat: 6.4525, lng: 3.3903,
			products: []product{
				{"Men's Denim Jeans", 800000, 50, []string{"men", "clothes", "jeans", "trouser"}, "Slim-fit stretch denim."},
				{"Women's Ankara Skirt Set", 1300000, 20, []string{"women", "clothes"}, "Two-piece skirt and blouse set."},
				{"Men's Polo Shirt", 650000, 70, []string{"men", "clothes", "shirt"}, "Pique cotton polo, multiple colours."},
				{"Unisex Sneakers", 1200000, 35, []string{"shoe", "sneaker", "clothes"}, "Canvas sneakers, sizes 38-45."},
			},
		},
		{
			name: "Surulere Style Hub", slug: "surulere-style-hub", category: "fashion",
			tagline: "Street style meets classic tailoring",
			area:    "Surulere Market", market: "Surulere Market", city: "Surulere", state: "Lagos", lat: 6.4926, lng: 3.3554,
			products: []product{
				{"Men's Agbada Set", 2500000, 15, []string{"men", "clothes", "native wear"}, "Full three-piece agbada, embroidered."},
				{"Women's Jumpsuit", 1100000, 25, []string{"women", "clothes"}, "Ankara jumpsuit, adjustable waist tie."},
				{"Men's Chino Trouser", 700000, 45, []string{"men", "clothes", "trouser"}, "Slim-fit chino, 4 colours."},
			},
		},
		{
			name: "Ikoyi Designer Boutique", slug: "ikoyi-designer-boutique", category: "fashion",
			tagline: "Curated designer pieces, by appointment",
			area:    "Ikoyi", city: "Ikoyi", state: "Lagos", lat: 6.4500, lng: 3.4333, // standalone — unique address, no named market
			products: []product{
				{"Designer Evening Gown", 8500000, 6, []string{"women", "clothes", "gown"}, "Limited-run designer evening gown."},
				{"Men's Italian Suit", 12000000, 5, []string{"men", "clothes", "suit"}, "Half-canvas Italian wool suit, tailored fit."},
			},
		},

		// ── Thrift (Yaba/Tejuosho, Ojuelegba, Ketu) ───────────────────────────
		{
			name: "Yaba Bend-Down Boutique", slug: "yaba-bend-down-boutique", category: "thrift",
			tagline: "Quality okrika, sorted and priced fair",
			area:    "Yaba Market", market: "Yaba Market", city: "Yaba", state: "Lagos", lat: 6.5095, lng: 3.3711,
			products: []product{
				{"Thrift Denim Jacket", 450000, 20, []string{"thrift", "clothes", "jacket"}, "Vintage denim jacket, graded A."},
				{"Thrift Men's Jeans", 350000, 30, []string{"thrift", "clothes", "men", "jeans"}, "Assorted brands, sizes 30-38."},
				{"Thrift Women's Blouse", 250000, 40, []string{"thrift", "clothes", "women"}, "Mixed prints, graded A/B."},
			},
		},
		{
			name: "Yaba Vintage Finds", slug: "yaba-vintage-finds", category: "thrift",
			tagline: "Curated vintage and designer thrift",
			area:    "Yaba Market", market: "Yaba Market", city: "Yaba", state: "Lagos", lat: 6.5098, lng: 3.3715,
			products: []product{
				{"Vintage Leather Jacket", 950000, 10, []string{"thrift", "clothes", "jacket"}, "Genuine leather, graded A, rare find."},
				{"Thrift Designer Handbag", 700000, 8, []string{"thrift", "bag", "women"}, "Pre-owned designer handbag, authenticated."},
			},
		},
		{
			name: "Tejuosho Thrift Corner", slug: "tejuosho-thrift-corner", category: "thrift",
			tagline: "First-grade bales, new stock every week",
			area:    "Tejuosho Market", market: "Tejuosho Market", city: "Yaba", state: "Lagos", lat: 6.5027, lng: 3.3720,
			products: []product{
				{"Thrift Sneakers", 500000, 25, []string{"thrift", "shoe", "sneaker"}, "Second-hand sneakers, cleaned and graded."},
				{"Thrift Blazer", 550000, 15, []string{"thrift", "clothes", "blazer"}, "Wool-blend blazer, dry-cleaned."},
				{"Thrift Ankara Dress", 400000, 20, []string{"thrift", "clothes", "women"}, "Pre-owned ankara dress, various sizes."},
			},
		},
		{
			name: "Ojuelegba Okrika Store", slug: "ojuelegba-okrika-store", category: "thrift",
			tagline: "Affordable thrift for the whole family",
			area:    "Ojuelegba Market", market: "Ojuelegba Market", city: "Surulere", state: "Lagos", lat: 6.5107, lng: 3.3620,
			products: []product{
				{"Thrift Kids Wear Bundle", 300000, 30, []string{"thrift", "clothes", "kids"}, "Bundle of 3 kids' items, assorted."},
				{"Thrift Men's Shirt", 200000, 50, []string{"thrift", "clothes", "men", "shirt"}, "Long-sleeve shirts, graded A."},
			},
		},
		{
			name: "Ketu Second Hand Fashion", slug: "ketu-second-hand-fashion", category: "thrift",
			tagline: "Bend-down select, sorted by size",
			area:    "Ketu Market", market: "Ketu Market", city: "Ketu", state: "Lagos", lat: 6.5928, lng: 3.3960,
			products: []product{
				{"Thrift Trouser", 280000, 35, []string{"thrift", "clothes", "trouser"}, "Assorted trousers, sizes 30-40."},
				{"Thrift Handbag", 350000, 20, []string{"thrift", "bag", "women"}, "Pre-owned leather handbags."},
			},
		},

		// ── Electronics (Alaba, Computer Village, Trade Fair) ─────────────────
		{
			name: "Alaba Electronics Plaza", slug: "alaba-electronics-plaza", category: "electronics",
			tagline: "Wholesale and retail electronics, all brands",
			area:    "Alaba International Market", market: "Alaba International Market", city: "Ojo", state: "Lagos", lat: 6.4698, lng: 3.1974,
			products: []product{
				{"Bluetooth Earpiece", 850000, 60, []string{"gadget", "electronics", "earpiece"}, "Wireless earbuds with charging case."},
				{"20000mAh Power Bank", 1200000, 40, []string{"gadget", "electronics"}, "Fast-charging dual USB power bank."},
				{"LED Television 43-inch", 18500000, 10, []string{"gadget", "electronics", "tv", "television"}, "Full HD smart LED TV."},
			},
		},
		{
			name: "Computer Village Gadgets", slug: "computer-village-gadgets", category: "electronics",
			tagline: "Phones, laptops, and accessories",
			area:    "Computer Village", market: "Computer Village", city: "Ikeja", state: "Lagos", lat: 6.5966, lng: 3.3389,
			products: []product{
				{"Smartphone 128GB", 22000000, 20, []string{"gadget", "phone", "electronics"}, "Android smartphone, 128GB storage, dual SIM."},
				{"HP Laptop i5", 45000000, 8, []string{"gadget", "laptop", "electronics", "computer"}, "Core i5, 8GB RAM, 256GB SSD."},
				{"Fast Charger + Cable", 650000, 80, []string{"gadget", "electronics", "charger"}, "20W USB-C fast charger with cable."},
			},
		},
		{
			name: "CV Phone Palace", slug: "cv-phone-palace", category: "electronics",
			tagline: "New and UK-used phones, all brands",
			area:    "Computer Village", market: "Computer Village", city: "Ikeja", state: "Lagos", lat: 6.5969, lng: 3.3392,
			products: []product{
				{"UK-Used iPhone", 28000000, 15, []string{"gadget", "phone", "electronics"}, "Grade A UK-used, 90-day warranty."},
				{"Wireless Charging Pad", 550000, 45, []string{"gadget", "electronics", "charger"}, "15W fast wireless charging pad."},
			},
		},
		{
			name: "Trade Fair Tech Store", slug: "trade-fair-tech-store", category: "electronics",
			tagline: "Bulk electronics at wholesale prices",
			area:    "Trade Fair Complex", market: "Trade Fair Complex", city: "Okokomaiko", state: "Lagos", lat: 6.4610, lng: 3.2600,
			products: []product{
				{"Bluetooth Speaker", 1500000, 25, []string{"gadget", "electronics"}, "Portable bluetooth speaker, 10hr battery."},
				{"Home Theatre System", 6500000, 12, []string{"gadget", "electronics"}, "5.1 channel home theatre system."},
			},
		},
		{
			name: "Gbagada Gadget Store", slug: "gbagada-gadget-store", category: "electronics",
			tagline: "Neighbourhood gadget shop, honest prices",
			area:    "Gbagada", city: "Gbagada", state: "Lagos", lat: 6.5522, lng: 3.3833, // standalone
			products: []product{
				{"Bluetooth Earbuds", 700000, 30, []string{"gadget", "electronics", "earpiece"}, "In-ear bluetooth earbuds with case."},
				{"Phone Screen Protector", 80000, 100, []string{"gadget", "electronics"}, "Tempered glass, fits most models."},
			},
		},

		// ── Groceries (Mile 12, Oyingbo, Agege, Oshodi) ───────────────────────
		{
			name: "Mile 12 Fresh Market", slug: "mile-12-fresh-market", category: "groceries",
			tagline: "Fresh produce and foodstuff, farm to table",
			area:    "Mile 12 Market", market: "Mile 12 Market", city: "Kosofe", state: "Lagos", lat: 6.5850, lng: 3.3959,
			products: []product{
				{"Rice 50kg Bag", 4500000, 30, []string{"grocery", "foodstuff", "rice"}, "Long grain parboiled rice, 50kg."},
				{"Fresh Tomatoes Basket", 800000, 25, []string{"grocery", "foodstuff"}, "Medium basket of fresh tomatoes."},
				{"Vegetable Oil 25L", 3200000, 20, []string{"grocery", "foodstuff", "oil"}, "Pure vegetable cooking oil, 25 litres."},
			},
		},
		{
			name: "Mile 12 Bulk Provisions", slug: "mile-12-bulk-provisions", category: "groceries",
			tagline: "Bulk foodstuff for restaurants and events",
			area:    "Mile 12 Market", market: "Mile 12 Market", city: "Kosofe", state: "Lagos", lat: 6.5853, lng: 3.3962,
			products: []product{
				{"Onions 1 Bag", 2500000, 15, []string{"grocery", "foodstuff"}, "Full bag of fresh onions."},
				{"Dried Fish (Bundle)", 1800000, 20, []string{"grocery", "foodstuff"}, "Smoked dried fish, bundle of 10."},
			},
		},
		{
			name: "Oyingbo Grocery Store", slug: "oyingbo-grocery-store", category: "groceries",
			tagline: "Your neighbourhood provisions store",
			area:    "Oyingbo Market", market: "Oyingbo Market", city: "Ebute-Metta", state: "Lagos", lat: 6.4884, lng: 3.3795,
			products: []product{
				{"Beans 1 Paint Bucket", 1200000, 40, []string{"grocery", "foodstuff", "beans"}, "Brown beans, 1 paint bucket measure."},
				{"Spaghetti Pack (Carton)", 900000, 35, []string{"grocery", "foodstuff", "spaghetti"}, "Carton of 20 spaghetti packs."},
				{"Garri 1 Bag", 1800000, 25, []string{"grocery", "foodstuff"}, "White garri, 1 bag (50kg)."},
			},
		},
		{
			name: "Agege Foodstuff Depot", slug: "agege-foodstuff-depot", category: "groceries",
			tagline: "Bulk foodstuff for homes and restaurants",
			area:    "Agege Market", market: "Agege Market", city: "Agege", state: "Lagos", lat: 6.6155, lng: 3.3260,
			products: []product{
				{"Semovita 10kg", 1100000, 30, []string{"grocery", "foodstuff"}, "Semovita swallow, 10kg pack."},
				{"Groundnut Oil 5L", 900000, 40, []string{"grocery", "foodstuff", "oil"}, "Pure groundnut oil, 5 litres."},
			},
		},
		{
			name: "Oshodi Provisions", slug: "oshodi-provisions", category: "groceries",
			tagline: "Everyday provisions at market prices",
			area:    "Oshodi Market", market: "Oshodi Market", city: "Oshodi", state: "Lagos", lat: 6.5560, lng: 3.3480,
			products: []product{
				{"Tomato Paste Carton", 1500000, 30, []string{"grocery", "foodstuff"}, "Carton of 24 tomato paste tins."},
				{"Sugar 1kg Pack", 150000, 100, []string{"grocery", "foodstuff"}, "Refined granulated sugar, 1kg."},
			},
		},
		{
			name: "Magodo Grocery Corner", slug: "magodo-grocery-corner", category: "groceries",
			tagline: "Fresh groceries, delivered same day",
			area:    "Magodo", city: "Magodo", state: "Lagos", lat: 6.6086, lng: 3.3784, // standalone
			products: []product{
				{"Fresh Vegetable Basket", 650000, 30, []string{"grocery", "foodstuff"}, "Assorted fresh vegetables, weekly basket."},
				{"Rice 5kg Pack", 550000, 50, []string{"grocery", "foodstuff", "rice"}, "Local rice, 5kg pack."},
			},
		},

		// ── Jewelry (Lekki, Ikeja City Mall area, Victoria Island) ────────────
		{
			name: "Lekki Gold & Gems", slug: "lekki-gold-and-gems", category: "jewelry",
			tagline: "Fine gold and gemstone jewelry",
			area:    "Lekki Market", market: "Lekki Market", city: "Lekki", state: "Lagos", lat: 6.4698, lng: 3.5852,
			products: []product{
				{"18k Gold Necklace", 15000000, 10, []string{"jewelry", "gold", "necklace"}, "18-karat gold necklace, 20 inches."},
				{"Diamond Engagement Ring", 35000000, 5, []string{"jewelry", "ring", "diamond"}, "Solitaire diamond ring, 18k white gold band."},
				{"Gold Hoop Earrings", 4500000, 20, []string{"jewelry", "gold", "earring"}, "14-karat gold hoop earrings."},
			},
		},
		{
			// Standalone — a distinct address in the Lekki/Chevron area, not affiliated with Lekki Market.
			name: "Chevron Drive Jewelers", slug: "chevron-drive-jewelers", category: "jewelry",
			tagline: "Bespoke jewelry, private consultations",
			area:    "Chevron Drive", city: "Lekki", state: "Lagos", lat: 6.4400, lng: 3.5450,
			products: []product{
				{"Custom Engagement Ring", 28000000, 6, []string{"jewelry", "ring", "diamond"}, "Bespoke design, choice of stone and metal."},
				{"Men's Gold Chain", 9500000, 10, []string{"jewelry", "gold", "necklace"}, "22-karat solid gold chain, 24 inches."},
			},
		},
		{
			// Standalone — mall-adjacent shop, not part of a traditional market.
			name: "Ikeja Jewelry House", slug: "ikeja-jewelry-house", category: "jewelry",
			tagline: "Custom and ready-made jewelry pieces",
			area:    "Ikeja City Mall Area", city: "Ikeja", state: "Lagos", lat: 6.5928, lng: 3.3527,
			products: []product{
				{"Silver Charm Bracelet", 2500000, 25, []string{"jewelry", "bracelet", "silver"}, "Sterling silver charm bracelet."},
				{"Wedding Ring Set", 12000000, 8, []string{"jewelry", "ring", "wedding"}, "His and hers wedding band set, 18k gold."},
			},
		},
		{
			// Standalone — general Victoria Island district, not a named market.
			name: "VI Luxury Jewels", slug: "vi-luxury-jewels", category: "jewelry",
			tagline: "Premium jewelry for every milestone",
			area:    "Victoria Island", city: "Victoria Island", state: "Lagos", lat: 6.4281, lng: 3.4219,
			products: []product{
				{"Pearl Necklace Set", 8500000, 12, []string{"jewelry", "necklace", "pearl"}, "Freshwater pearl necklace and earring set."},
				{"Gold Signet Ring", 6000000, 15, []string{"jewelry", "ring", "gold"}, "Men's 18k gold signet ring."},
			},
		},

		// ── Food (Ajah, Apapa, Ikorodu) ────────────────────────────────────────
		{
			name: "Ajah Kitchen Delights", slug: "ajah-kitchen-delights", category: "food",
			tagline: "Home-style Nigerian meals, ready to eat",
			area:    "Ajah Market", market: "Ajah Market", city: "Ajah", state: "Lagos", lat: 6.4720, lng: 3.6020,
			products: []product{
				{"Jollof Rice Combo", 350000, 50, []string{"food", "meal", "jollof"}, "Jollof rice with chicken and plantain."},
				{"Fried Rice Combo", 380000, 45, []string{"food", "meal"}, "Fried rice with grilled chicken."},
			},
		},
		{
			name: "Apapa Local Dishes", slug: "apapa-local-dishes", category: "food",
			tagline: "Authentic local dishes, made fresh daily",
			area:    "Apapa Market", market: "Apapa Market", city: "Apapa", state: "Lagos", lat: 6.4432, lng: 3.3592,
			products: []product{
				{"Amala & Ewedu", 300000, 40, []string{"food", "meal", "amala"}, "Amala with ewedu soup and assorted meat."},
				{"Pepper Soup", 250000, 35, []string{"food", "meal"}, "Spicy goat meat pepper soup."},
			},
		},
		{
			name: "Ikorodu Suya Spot", slug: "ikorodu-suya-spot", category: "food",
			tagline: "Grilled suya and small chops",
			area:    "Ikorodu Market", market: "Ikorodu Market", city: "Ikorodu", state: "Lagos", lat: 6.6018, lng: 3.5106,
			products: []product{
				{"Beef Suya (500g)", 400000, 60, []string{"food", "suya", "meat"}, "Freshly grilled spicy beef suya."},
				{"Chicken Suya (500g)", 420000, 50, []string{"food", "suya", "meat"}, "Freshly grilled spicy chicken suya."},
			},
		},

		// ── Home (standalone) ──────────────────────────────────────────────────
		{
			name: "Yaba Home Essentials", slug: "yaba-home-essentials", category: "home",
			tagline: "Furniture and kitchenware for every home",
			area:    "Yaba", city: "Yaba", state: "Lagos", lat: 6.5140, lng: 3.3700, // standalone — near, but not part of, Yaba Market
			products: []product{
				{"Non-Stick Cookware Set", 1800000, 20, []string{"home", "kitchenware"}, "8-piece non-stick pot and pan set."},
				{"Bedside Table Lamp", 650000, 25, []string{"home", "decor"}, "Warm-white LED bedside lamp."},
			},
		},

		// ── Anambra State (Awka, Onitsha, Nnewi) ──────────────────────────────
		{
			name: "Awka Fashion Hub", slug: "awka-fashion-hub", category: "fashion",
			tagline: "Latest styles for the South-East",
			area:    "Awka Main Market", market: "Awka Main Market", city: "Awka", state: "Anambra", lat: 6.2120, lng: 7.0740,
			products: []product{
				{"Men's Kaftan", 1300000, 25, []string{"men", "clothes", "native wear"}, "Embroidered kaftan, breathable cotton."},
				{"Women's Ankara Dress", 1100000, 30, []string{"women", "clothes"}, "Bold-print ankara dress, made to order."},
			},
		},
		{
			name: "Onitsha Main Traders", slug: "onitsha-main-traders", category: "groceries",
			tagline: "West Africa's largest market, wholesale prices",
			area:    "Onitsha Main Market", market: "Onitsha Main Market", city: "Onitsha", state: "Anambra", lat: 6.1450, lng: 6.7850,
			products: []product{
				{"Rice 50kg Bag", 4400000, 40, []string{"grocery", "foodstuff", "rice"}, "Long grain rice, wholesale, 50kg."},
				{"Palm Oil 25L", 2800000, 25, []string{"grocery", "foodstuff", "oil"}, "Pure red palm oil, 25 litres."},
			},
		},
		{
			name: "Nnewi Auto Spares", slug: "nnewi-auto-spares", category: "auto",
			tagline: "Genuine and aftermarket auto parts",
			area:    "Nkwo Nnewi Market", market: "Nkwo Nnewi Market", city: "Nnewi", state: "Anambra", lat: 6.0170, lng: 6.9150,
			products: []product{
				{"Brake Pad Set", 850000, 30, []string{"auto", "parts"}, "Fits most sedans, ceramic compound."},
				{"Car Battery 12V", 2200000, 20, []string{"auto", "parts", "battery"}, "12V 65Ah maintenance-free battery."},
			},
		},
		{
			name: "Awka Standalone Electronics", slug: "awka-standalone-electronics", category: "electronics",
			tagline: "Phones and accessories, Awka",
			area:    "Awka", city: "Awka", state: "Anambra", lat: 6.2095, lng: 7.0700, // standalone — no named market
			products: []product{
				{"Smartphone 64GB", 15000000, 18, []string{"gadget", "phone", "electronics"}, "Android smartphone, 64GB storage."},
				{"Bluetooth Earpiece", 600000, 40, []string{"gadget", "electronics", "earpiece"}, "Wireless earbuds, 8hr battery."},
			},
		},
	}
}
