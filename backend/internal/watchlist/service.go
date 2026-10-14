package watchlist

import (
	"context"
	"errors"

	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

var ErrNotInWatchlist = errors.New("symbol is not in watchlist")

type store interface {
	List(ctx context.Context) ([]string, error)
	Add(ctx context.Context, symbol string) error
	Remove(ctx context.Context, symbol string) (bool, error)
}

type Service struct {
	store store
}

func NewService(s store) *Service { return &Service{store: s} }

func (s *Service) List(ctx context.Context) ([]string, error) {
	return s.store.List(ctx)
}

// Add thêm mã (chỉ nhận mã có trong price.Universe) và trả về danh sách mới.
func (s *Service) Add(ctx context.Context, symbol string) ([]string, error) {
	sym, err := price.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if err := s.store.Add(ctx, sym); err != nil {
		return nil, err
	}
	return s.store.List(ctx)
}

func (s *Service) Remove(ctx context.Context, symbol string) error {
	sym, err := price.NormalizeSymbol(symbol)
	if err != nil {
		return err
	}
	removed, err := s.store.Remove(ctx, sym)
	if err != nil {
		return err
	}
	if !removed {
		return ErrNotInWatchlist
	}
	return nil
}
