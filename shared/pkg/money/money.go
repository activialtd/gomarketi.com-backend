// Package money provides type-safe Kobo arithmetic for GoMarket financials.
//
// All monetary values in GoMarket are stored and computed in Kobo — the smallest
// Nigerian Naira denomination (100 Kobo = 1 Naira). The Money type enforces this
// at compile time: passing a raw int64 where a Money is expected is a type error.
//
// float64 is NEVER used for money — not in arithmetic, not in storage, not in
// inter-service communication. Formatting to Naira strings is only for display.
package money

import (
	"errors"
	"fmt"
)

// Money represents an amount in Kobo. Always use this type for financial
// values — never raw int64, never float64.
type Money int64

// ErrNegativeResult is returned by Sub when the subtraction would produce
// a negative balance. Balances in GoMarket must always be >= 0.
var ErrNegativeResult = errors.New("money: result would be negative")

// ErrNegativeAmount is returned when constructing a Money value from a
// negative integer.
var ErrNegativeAmount = errors.New("money: amount must be >= 0")

// FromKobo constructs a Money from a raw Kobo amount. Returns
// ErrNegativeAmount if kobo < 0.
func FromKobo(kobo int64) (Money, error) {
	if kobo < 0 {
		return 0, ErrNegativeAmount
	}
	return Money(kobo), nil
}

// MustFromKobo is like FromKobo but panics on a negative input. Use only in
// tests and initialisation code where the value is a compile-time constant.
func MustFromKobo(kobo int64) Money {
	m, err := FromKobo(kobo)
	if err != nil {
		panic(fmt.Sprintf("money.MustFromKobo(%d): %v", kobo, err))
	}
	return m
}

// FromNaira converts a whole-Naira amount to Kobo. e.g. FromNaira(500) == 50000.
// Returns ErrNegativeAmount if naira < 0.
func FromNaira(naira int64) (Money, error) {
	if naira < 0 {
		return 0, ErrNegativeAmount
	}
	return Money(naira * 100), nil
}

// MustFromNaira is like FromNaira but panics on a negative input.
func MustFromNaira(naira int64) Money {
	m, err := FromNaira(naira)
	if err != nil {
		panic(fmt.Sprintf("money.MustFromNaira(%d): %v", naira, err))
	}
	return m
}

// Kobo returns the raw Kobo value as int64. Use this when writing to the
// database — the column type is BIGINT.
func (m Money) Kobo() int64 { return int64(m) }

// Add returns m + other. Both values must be non-negative (guaranteed if
// constructed via the From* constructors).
func (m Money) Add(other Money) Money { return m + other }

// Sub returns m - other. Returns ErrNegativeResult if other > m,
// because GoMarket balances may never go below zero.
func (m Money) Sub(other Money) (Money, error) {
	if other > m {
		return 0, fmt.Errorf("%w: %s - %s", ErrNegativeResult, m, other)
	}
	return m - other, nil
}

// MulInt returns m × factor. Used for line-item totals: unit_price × quantity.
// factor must be > 0 in production call sites.
func (m Money) MulInt(factor int64) Money { return Money(int64(m) * factor) }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m == 0 }

// GreaterThan reports whether m > other.
func (m Money) GreaterThan(other Money) bool { return m > other }

// String formats the amount as a display-only Naira string, e.g. "₦1,500.00".
// NEVER use this for storage, computation, or API responses — use Kobo() for those.
func (m Money) String() string {
	koboAbs := int64(m)
	naira := koboAbs / 100
	kobo := koboAbs % 100
	return fmt.Sprintf("₦%d.%02d", naira, kobo)
}
