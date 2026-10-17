package price

import (
	"context"
	"errors"
	"testing"
)

type fakeHistory struct {
	ticks     []Tick
	gotSymbol string
	gotLimit  int
}

func (f *fakeHistory) History(_ context.Context, symbol string, limit int) ([]Tick, error) {
	f.gotSymbol, f.gotLimit = symbol, limit
	return append([]Tick(nil), f.ticks...), nil
}

type fakeLatest struct{ ticks []Tick }

func (f fakeLatest) All(context.Context) ([]Tick, error) { return f.ticks, nil }

func TestHistoryReturnsOldestFirst(t *testing.T) {
	h := &fakeHistory{ticks: []Tick{{Symbol: "FPT", Ts: 3}, {Symbol: "FPT", Ts: 2}, {Symbol: "FPT", Ts: 1}}}
	svc := NewService(h, fakeLatest{})

	got, err := svc.History(context.Background(), "FPT", 3)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{1, 2, 3} {
		if got[i].Ts != want {
			t.Fatalf("got[%d].Ts = %d, want %d", i, got[i].Ts, want)
		}
	}
}

func TestHistoryNormalizesSymbolAndDefaultsLimit(t *testing.T) {
	h := &fakeHistory{}
	svc := NewService(h, fakeLatest{})

	if _, err := svc.History(context.Background(), " fpt ", 0); err != nil {
		t.Fatal(err)
	}
	if h.gotSymbol != "FPT" {
		t.Errorf("symbol = %q, want FPT", h.gotSymbol)
	}
	if h.gotLimit != DefaultHistoryLimit {
		t.Errorf("limit = %d, want %d", h.gotLimit, DefaultHistoryLimit)
	}
}

func TestHistoryRejectsBadInput(t *testing.T) {
	svc := NewService(&fakeHistory{}, fakeLatest{})

	tests := []struct {
		name   string
		symbol string
		limit  int
		want   error
	}{
		{"unknown symbol", "AAPL", 10, ErrUnknownSymbol},
		{"negative limit", "FPT", -1, ErrInvalidLimit},
		{"limit too large", "FPT", MaxHistoryLimit + 1, ErrInvalidLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.History(context.Background(), tt.symbol, tt.limit)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestLatestSortsBySymbol(t *testing.T) {
	svc := NewService(&fakeHistory{}, fakeLatest{ticks: []Tick{{Symbol: "VNM"}, {Symbol: "FPT"}, {Symbol: "HPG"}}})

	got, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"FPT", "HPG", "VNM"} {
		if got[i].Symbol != want {
			t.Fatalf("got[%d] = %s, want %s", i, got[i].Symbol, want)
		}
	}
}

func (f fakeLatest) Get(_ context.Context, symbol string) (Tick, error) {
	for _, t := range f.ticks {
		if t.Symbol == symbol {
			return t, nil
		}
	}
	return Tick{}, ErrNoPrice
}

func TestGetBySymbol(t *testing.T) {
	svc := NewService(&fakeHistory{}, fakeLatest{ticks: []Tick{{Symbol: "FPT", Price: 120}}})
	ctx := context.Background()

	got, err := svc.GetBySymbol(ctx, " fpt ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Symbol != "FPT" || got.Price != 120 {
		t.Fatalf("got %+v, want FPT 120", got)
	}

	tests := []struct {
		name   string
		symbol string
		want   error
	}{
		{"unknown symbol", "AAPL", ErrUnknownSymbol},
		{"no price yet", "VNM", ErrNoPrice},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.GetBySymbol(ctx, tt.symbol); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
