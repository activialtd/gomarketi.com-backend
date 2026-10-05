// Package main finds money we took that never finished becoming an order.
//
// Orders are created before payment now, so a charge can no longer arrive with
// nothing attached to it. What can still happen is an order left unconfirmed:
// the buyer paid, but neither their browser nor Paystack's webhook told us, so
// it sits awaiting payment or aged into abandoned while the money is ours.
//
// This walks Paystack's own transaction list and reports every successful
// charge whose order is not confirmed.
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
	"strings"
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
	txn    paystackTxn
	status string // the order's current status, "" when no order exists at all
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
		// An order on this reference that is still awaiting payment or has
		// aged into abandoned means the charge never got confirmed.
		var status string
		err := db.QueryRowContext(ctx, `
			SELECT status FROM orders
			WHERE payment_reference = $1
			ORDER BY CASE status WHEN 'awaiting_payment' THEN 0 WHEN 'abandoned' THEN 1 ELSE 2 END
			LIMIT 1`, t.Reference).Scan(&status)
		if err == sql.ErrNoRows {
			orphans = append(orphans, orphan{txn: t})
			continue
		}
		if err != nil {
			log.Fatalf("look up order: %v", err)
		}
		if status == "awaiting_payment" || status == "abandoned" {
			orphans = append(orphans, orphan{txn: t, status: status})
		}
	}

	if len(orphans) == 0 {
		fmt.Println("Every successful charge has an order. Nothing to recover.")
		return
	}

	report(orphans)

	if !*retry {
		fmt.Println("\nRe-run with -retry to confirm the ones that have an order.")
		return
	}
	replay(ctx, orphans)
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
		state := "no order at all — predates order-first checkout"
		if o.status != "" {
			state = "order " + o.status + " — can confirm"
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

// replay asks the orders service to confirm each paid-but-unconfirmed order,
// through the same public endpoint the browser and the webhook use — so the
// charge is verified against our own total exactly as it would be normally,
// rather than this script writing statuses behind the service's back.
func replay(ctx context.Context, orphans []orphan) {
	base := strings.TrimRight(os.Getenv("ORDERS_API_URL"), "/")
	if base == "" {
		base = "https://api.gomarketi.com"
	}
	client := &http.Client{Timeout: 30 * time.Second}

	var ok, manual, failed int
	for _, o := range orphans {
		if o.status == "" {
			manual++
			continue
		}
		body := fmt.Sprintf(`{"payment_reference":%q}`, o.txn.Reference)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			base+"/v1/orders/public/confirm-payment", strings.NewReader(body))
		if err != nil {
			failed++
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("  FAILED  %s: %v\n", o.txn.Reference, err)
			failed++
			continue
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			fmt.Printf("  FAILED  %s: %d %s\n", o.txn.Reference, resp.StatusCode, bytes.TrimSpace(msg))
			failed++
			continue
		}
		fmt.Printf("  confirmed  %s (%s, %s)\n", o.txn.Reference, naira(o.txn.Amount), o.txn.Customer.Email)
		ok++
	}

	fmt.Printf("\nConfirmed %d, failed %d.\n", ok, failed)
	if manual > 0 {
		fmt.Printf("%d charge(s) have no order at all. Those predate order-first checkout,\n"+
			"so nothing records what was bought — contact the customer or refund in Paystack.\n", manual)
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
