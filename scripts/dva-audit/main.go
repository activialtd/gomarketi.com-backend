// Package main reports every vendor who has a store but no Paystack
// Dedicated Virtual Account — i.e. finished onboarding with nowhere for
// buyers' money to land — and can optionally provision the missing ones.
//
// Usage (from repo root):
//
//	DATABASE_URL=... go run ./scripts/dva-audit
//	DATABASE_URL=... go run ./scripts/dva-audit -provision
//
// Read-only by default. -provision calls identity's internal endpoint for
// each vendor listed, which needs the identity service reachable:
//
//	IDENTITY_INTERNAL_URL=http://identity:8081 \
//	INTERNAL_API_KEY=... \
//	DATABASE_URL=... go run ./scripts/dva-audit -provision
//
// Provisioning is idempotent on identity's side, so re-running is safe.
// Note that identity also sweeps for these vendors hourly on its own
// (StartDVABackfillLoop) — this script is for seeing the list, and for
// forcing the sweep now rather than waiting for the next tick.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type vendor struct {
	UserID         string
	StoreName      string
	StoreSlug      string
	Email          string
	OnboardingStep string
	CustomerCode   string // set = Paystack customer exists, DVA step is what failed
	StoreCreated   time.Time
}

// Mirrors identity's vendorsMissingDVAQuery, with the extra diagnostic
// columns this report shows. A vendor with no store is excluded on purpose:
// the DVA is named after the store, so there is nothing to name it yet.
const query = `
	SELECT DISTINCT ON (vp.user_id)
	       vp.user_id::text,
	       s.name,
	       s.slug,
	       COALESCE(u.email, ''),
	       vp.onboarding_step::text,
	       COALESCE(vp.paystack_customer_code, ''),
	       s.created_at
	FROM vendor_profiles vp
	JOIN stores s ON s.vendor_id = vp.user_id
	JOIN users  u ON u.id        = vp.user_id
	WHERE vp.paystack_dva_account_number IS NULL
	ORDER BY vp.user_id, s.created_at`

func main() {
	provision := flag.Bool("provision", false, "call identity to provision the missing accounts (default: report only)")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("database unreachable: %v", err)
	}

	vendors, err := missingDVA(ctx, db)
	if err != nil {
		log.Fatalf("query: %v", err)
	}

	total, err := storeCount(ctx, db)
	if err != nil {
		log.Fatalf("count stores: %v", err)
	}

	report(vendors, total)

	if !*provision || len(vendors) == 0 {
		return
	}
	provisionAll(vendors)
}

func missingDVA(ctx context.Context, db *sql.DB) ([]vendor, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []vendor
	for rows.Next() {
		var v vendor
		if err := rows.Scan(&v.UserID, &v.StoreName, &v.StoreSlug, &v.Email,
			&v.OnboardingStep, &v.CustomerCode, &v.StoreCreated); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func storeCount(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT vendor_id) FROM stores`).Scan(&n)
	return n, err
}

func report(vendors []vendor, totalVendorsWithStores int) {
	if len(vendors) == 0 {
		fmt.Printf("All %d vendors with a store have a virtual account.\n", totalVendorsWithStores)
		return
	}

	fmt.Printf("%d of %d vendors with a store have no virtual account:\n\n", len(vendors), totalVendorsWithStores)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STORE\tSLUG\tEMAIL\tONBOARDING\tSTAGE REACHED\tSTORE CREATED\tUSER ID")
	for _, v := range vendors {
		// A customer code with no account number narrows the failure down:
		// Paystack accepted the customer and rejected (or never received)
		// the dedicated_account call, rather than the whole call failing.
		stage := "no paystack customer"
		if v.CustomerCode != "" {
			stage = "customer created, DVA missing"
		}
		email := v.Email
		if email == "" {
			// ProvisionVendorDVA refuses a vendor with no email, so this is
			// a permanent failure until the account has one.
			email = "(none — blocks provisioning)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			v.StoreName, v.StoreSlug, email, v.OnboardingStep, stage,
			v.StoreCreated.Format("2006-01-02"), v.UserID)
	}
	w.Flush()

	fmt.Println("\nRe-run with -provision to create the missing accounts now.")
}

func provisionAll(vendors []vendor) {
	baseURL := os.Getenv("IDENTITY_INTERNAL_URL")
	if baseURL == "" {
		log.Fatal("-provision needs IDENTITY_INTERNAL_URL (e.g. http://identity:8081)")
	}
	key := os.Getenv("INTERNAL_API_KEY")
	if key == "" {
		log.Fatal("-provision needs INTERNAL_API_KEY — the same value identity is running with")
	}

	fmt.Printf("\nProvisioning %d vendors via %s …\n\n", len(vendors), baseURL)

	client := &http.Client{Timeout: 30 * time.Second}
	var ok, failed int
	for _, v := range vendors {
		if err := provisionOne(client, baseURL, key, v); err != nil {
			fmt.Printf("  FAILED  %-30s %v\n", v.StoreName, err)
			failed++
			continue
		}
		fmt.Printf("  ok      %s\n", v.StoreName)
		ok++
	}

	fmt.Printf("\nProvisioned %d, failed %d.\n", ok, failed)
	if failed > 0 {
		// A non-zero exit makes this usable as a deploy/CI check, and keeps
		// a partial run from reading as a success in logs.
		os.Exit(1)
	}
}

func provisionOne(client *http.Client, baseURL, key string, v vendor) error {
	body, err := json.Marshal(map[string]string{"user_id": v.UserID, "store_name": v.StoreName})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/v1/identity/internal/provision-dva", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", key)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("identity returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	return nil
}
