package money_test

import (
	"testing"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/money"
)

func TestFromKobo(t *testing.T) {
	tests := []struct {
		name    string
		input   int64
		wantErr bool
		want    int64
	}{
		{"zero", 0, false, 0},
		{"positive", 50_000, false, 50_000},
		{"negative", -1, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := money.FromKobo(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("FromKobo(%d) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if !tc.wantErr && m.Kobo() != tc.want {
				t.Errorf("Kobo() = %d, want %d", m.Kobo(), tc.want)
			}
		})
	}
}

func TestFromNaira(t *testing.T) {
	tests := []struct {
		naira    int64
		wantKobo int64
		wantErr  bool
	}{
		{0, 0, false},
		{1, 100, false},
		{500, 50_000, false},
		{-1, 0, true},
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			m, err := money.FromNaira(tc.naira)
			if (err != nil) != tc.wantErr {
				t.Fatalf("FromNaira(%d) error = %v, wantErr %v", tc.naira, err, tc.wantErr)
			}
			if !tc.wantErr && m.Kobo() != tc.wantKobo {
				t.Errorf("Kobo() = %d, want %d", m.Kobo(), tc.wantKobo)
			}
		})
	}
}

func TestAdd(t *testing.T) {
	a := money.MustFromNaira(100) // 10000
	b := money.MustFromNaira(50)  // 5000
	got := a.Add(b)
	want := money.MustFromNaira(150)
	if got != want {
		t.Errorf("Add() = %s, want %s", got, want)
	}
}

func TestSub(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		a := money.MustFromNaira(100)
		b := money.MustFromNaira(30)
		got, err := a.Sub(b)
		if err != nil {
			t.Fatal(err)
		}
		want := money.MustFromNaira(70)
		if got != want {
			t.Errorf("Sub() = %s, want %s", got, want)
		}
	})
	t.Run("exact", func(t *testing.T) {
		a := money.MustFromNaira(50)
		got, err := a.Sub(a)
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsZero() {
			t.Errorf("Sub(self) = %s, want zero", got)
		}
	})
	t.Run("negative result", func(t *testing.T) {
		a := money.MustFromNaira(10)
		b := money.MustFromNaira(100)
		_, err := a.Sub(b)
		if err == nil {
			t.Fatal("expected ErrNegativeResult, got nil")
		}
	})
}

func TestMulInt(t *testing.T) {
	price := money.MustFromNaira(500) // 50000 kobo
	total := price.MulInt(3)
	want := money.MustFromNaira(1500)
	if total != want {
		t.Errorf("MulInt(3) = %s, want %s", total, want)
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		kobo int64
		want string
	}{
		{100, "₦1.00"},
		{50, "₦0.50"},
		{15000, "₦150.00"},
		{0, "₦0.00"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			m := money.MustFromKobo(tc.kobo)
			if got := m.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsZero(t *testing.T) {
	if !money.MustFromKobo(0).IsZero() {
		t.Error("0 kobo should be zero")
	}
	if money.MustFromKobo(1).IsZero() {
		t.Error("1 kobo should not be zero")
	}
}

func TestMustFromKobo_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for negative kobo")
		}
	}()
	money.MustFromKobo(-1)
}
