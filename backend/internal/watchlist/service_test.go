package watchlist

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

// memStore giữ watchlist của từng user trong map: key là user id.
type memStore struct{ byUser map[string][]string }

func newMemStore() *memStore { return &memStore{byUser: map[string][]string{}} }

func (m *memStore) List(_ context.Context, userID string) ([]string, error) {
	// slices.Sorted luôn trả slice mới (không nil) nên JSON là [] khi rỗng.
	return slices.Sorted(slices.Values(m.byUser[userID])), nil
}

func (m *memStore) Add(_ context.Context, userID, s string) error {
	if !slices.Contains(m.byUser[userID], s) {
		m.byUser[userID] = append(m.byUser[userID], s)
	}
	return nil
}

func (m *memStore) Remove(_ context.Context, userID, s string) (bool, error) {
	syms := m.byUser[userID]
	i := slices.Index(syms, s)
	if i < 0 {
		return false, nil
	}
	m.byUser[userID] = slices.Delete(syms, i, i+1)
	return true, nil
}

func TestAddNormalizesAndDeduplicates(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()

	svc.Add(ctx, "alice", "vnm")
	got, err := svc.Add(ctx, "alice", " VNM ")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"VNM"}) {
		t.Fatalf("got %v, want [VNM]", got)
	}
}

func TestAddRejectsUnknownSymbol(t *testing.T) {
	svc := NewService(newMemStore())

	if _, err := svc.Add(context.Background(), "alice", "AAPL"); !errors.Is(err, price.ErrUnknownSymbol) {
		t.Fatalf("err = %v, want ErrUnknownSymbol", err)
	}
}

func TestRemove(t *testing.T) {
	tests := []struct {
		name    string
		userID  string
		symbol  string
		wantErr error
	}{
		{"own symbol", "alice", "fpt", nil},
		{"not in list", "alice", "VNM", ErrNotInWatchlist},
		{"unknown symbol", "alice", "AAPL", price.ErrUnknownSymbol},
		// bob không xoá được mã nằm trong danh sách của alice.
		{"other user's symbol", "bob", "FPT", ErrNotInWatchlist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMemStore()
			m.byUser["alice"] = []string{"FPT"}
			err := NewService(m).Remove(context.Background(), tt.userID, tt.symbol)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestListIsolatedPerUser(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()

	svc.Add(ctx, "alice", "FPT")
	svc.Add(ctx, "bob", "VNM")

	alice, _ := svc.List(ctx, "alice")
	bob, _ := svc.List(ctx, "bob")
	if !slices.Equal(alice, []string{"FPT"}) || !slices.Equal(bob, []string{"VNM"}) {
		t.Fatalf("alice = %v, bob = %v; want [FPT] and [VNM]", alice, bob)
	}
}
