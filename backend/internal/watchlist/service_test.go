package watchlist

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

type memStore struct{ syms []string }

func (m *memStore) List(context.Context) ([]string, error) {
	return slices.Sorted(slices.Values(m.syms)), nil
}

func (m *memStore) Add(_ context.Context, s string) error {
	if !slices.Contains(m.syms, s) {
		m.syms = append(m.syms, s)
	}
	return nil
}

func (m *memStore) Remove(_ context.Context, s string) (bool, error) {
	i := slices.Index(m.syms, s)
	if i < 0 {
		return false, nil
	}
	m.syms = slices.Delete(m.syms, i, i+1)
	return true, nil
}

func TestAddNormalizesAndDeduplicates(t *testing.T) {
	svc := NewService(&memStore{})
	ctx := context.Background()

	svc.Add(ctx, "vnm")
	got, err := svc.Add(ctx, " VNM ")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"VNM"}) {
		t.Fatalf("got %v, want [VNM]", got)
	}
}

func TestAddRejectsUnknownSymbol(t *testing.T) {
	svc := NewService(&memStore{})

	if _, err := svc.Add(context.Background(), "AAPL"); !errors.Is(err, price.ErrUnknownSymbol) {
		t.Fatalf("err = %v, want ErrUnknownSymbol", err)
	}
}

func TestRemoveMissingSymbol(t *testing.T) {
	svc := NewService(&memStore{syms: []string{"FPT"}})

	if err := svc.Remove(context.Background(), "VNM"); !errors.Is(err, ErrNotInWatchlist) {
		t.Fatalf("err = %v, want ErrNotInWatchlist", err)
	}
	if err := svc.Remove(context.Background(), "fpt"); err != nil {
		t.Fatalf("remove FPT: %v", err)
	}
}
