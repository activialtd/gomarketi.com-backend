// Package main finds money we took and never turned into an order.
//
// Checkout charges Paystack first and saves the order second. Anything that
// interrupts the gap leaves a successful charge with nothing attached to it,
// and until checkout_intents existed there was no server-side record of what
// the payment was even for. This walks Paystack's own transaction list and
// reports every successful charge with no matching order.
//
// Usage (from repo root):
//
//	DATABASE_URL=... PAYSTACK_SECRET_KEY=sk_live_... go run ./scripts/payment-audit
//	... go run ./scripts/payment-audit -days 30
//	... go run ./scripts/payment-audit -retry        # replay the ones we can
//
// Read-only without -retry. With it, any orphan that has a stored checkout
// intent is replayed through the orders service; one without an intent cannot
// be rebuilt here, because only the buyer's browser ever knew the basket.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type paystackTxn struct {
	Reference string `json:"reference"`
	Amount    int64  `json:"amount"` // kobo
	Status    string `json:"status"`
	Channel   string `json:"channel"`
	PaidAt    string `json:"paid_at"`
	Customer  struct {
		Email string `json:"email"`
	} `json:"customer"`
}

type paystackPage struct {
	Status bool          `json:"status"`
	Data   []paystackTxn `json:"data"`
	Meta   struct {
		Total     int `json:"total"`
		PageCount int `json:"pageCount"`
	} `json:"meta"`
}

type orphan struct {
	txn       paystackTxn
	hasIntent bool
}

func main() {
	days := flag.Int("days", 14, "how far back to look")
	retry := flag.Bool("retry", false, "replay orphans that have a stored checkout intent")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	secret := os.Getenv("PAYSTACK_SECRET_KEY")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	if secret == "" {
		log.Fatal("PAYSTACK_SECRET_KEY is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("database unreachable: %v", err)
	}

	from := time.Now().AddDate(0, 0, -*days)
	txns, err := fetchSuccessful(ctx, secret, from)
	if err != nil {
		log.Fatalf("paystack: %v", err)
	}
	fmt.Printf("Checked %d successful Paystack charges since %s.\n\n", len(txns), from.Format("2006-01-02"))

	var orphans []orphan
	for _, t := range txns {
		// A dedicated_nuban charge is a vendor funding their own wallet, not
		// a checkout — it is never meant to produce an order.
		if t.Channel == "dedicated_nuban" {
			continue
		}
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM orders WHERE payment_reference = $1)`, t.Reference).Scan(&exists); err != nil {
			log.Fatalf("look up order: %v", err)
		}
		if exists {
			continue
		}
		var hasIntent bool
		_ = db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM checkout_intents WHERE payment_reference = $1 AND fulfilled_at IS NULL)`,
			t.Reference).Scan(&hasIntent)
		orphans = append(orphans, orphan{txn: t, hasIntent: hasIntent})
	}

	if len(orphans) == 0 {
		fmt.Println("Every successful charge has an order. Nothing to recover.")
		return
	}

	report(orphans)

	if !*retry {
		fmt.Println("\nRe-run with -retry to replay the ones marked 'intent stored'.")
		return
	}
	replay(ctx, db, orphans)
}

func fetchSuccessful(ctx context.Context, secret string, from time.Time) ([]paystackTxn, error) {
	var out []paystackTxn
	client := &http.Client{Timeout: 30 * time.Second}
	for page := 1; ; page++ {
		url := fmt.Sprintf(
			"https://api.paystack.co/transaction?status=success&perPage=100&page=%d&from=%s",
			page, from.Format("2006-01-02"))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+secret)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var body paystackPage
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode page %d: %w", page, err)
		}
		if resp.StatusCode >= 300 || !body.Status {
			return nil, fmt.Errorf("paystack returned %d on page %d", resp.StatusCode, page)
		}
		out = append(out, body.Data...)
		if page >= body.Meta.PageCount || len(body.Data) == 0 {
			return out, nil
		}
	}
}

func report(orphans []orphan) {
	fmt.Printf("%d successful charge(s) have no order:\n\n", len(orphans))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PAID AT\tAMOUNT\tCUSTOMER\tREFERENCE\tRECOVERABLE")
	for _, o := range orphans {
		state := "no intent — rebuild by hand"
		if o.hasIntent {
			state = "intent stored — can replay"
		}
		paid := o.txn.PaidAt
		if len(paid) >= 16 {
			paid = paid[:16]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			paid, naira(o.txn.Amount), o.txn.Customer.Email, o.txn.Reference, state)
	}
	w.Flush()
}

// replay asks the orders service to finish each recoverable checkout, by
// touching the intent so the service's own sweep picks it up on its next
// pass. Done through the database rather than an HTTP call so this works
// without the service being reachable from wherever it is run.
func replay(ctx context.Context, db *sql.DB, orphans []orphan) {
	var queued, manual int
	for _, o := range orphans {
		if !o.hasIntent {
			manual++
			continue
		}
		// Clearing attempts puts an intent that had exhausted its retries
		// back in the sweep's working set.
		if _, err := db.ExecContext(ctx,
			`UPDATE checkout_intents SET attempts = 0, last_error = NULL, updated_at = NOW()
			 WHERE payment_reference = $1 AND fulfilled_at IS NULL`, o.txn.Reference); err != nil {
			fmt.Printf("  FAILED  %s: %v\n", o.txn.Reference, err)
			continue
		}
		fmt.Printf("  queued  %s (%s, %s)\n", o.txn.Reference, naira(o.txn.Amount), o.txn.Customer.Email)
		queued++
	}
	fmt.Printf("\nQueued %d for the orders service to retry within 10 minutes.\n", queued)
	if manual > 0 {
		fmt.Printf("%d cannot be replayed: the payment predates checkout intents, so nothing\n"+
			"records what was bought. Contact the customer, or refund the charge in Paystack.\n", manual)
	}
}

func naira(kobo int64) string {
	return fmt.Sprintf("₦%s", addThousands(kobo/100))
}

func addThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
