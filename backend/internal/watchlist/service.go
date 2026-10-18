package watchlist

import (
	"context"
	"errors"

	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

var ErrNotInWatchlist = errors.New("symbol is not in watchlist")

// store là những gì Service cần từ tầng lưu trữ; test thay bằng store giả trong bộ nhớ.
type store interface {
	List(ctx context.Context, userID string) ([]string, error)
	Add(ctx context.Context, userID, symbol string) error
	Remove(ctx context.Context, userID, symbol string) (bool, error)
}

// Bắt compiler kiểm tra *Store có đủ method của store.
var _ store = (*Store)(nil)

type Service struct {
	store store
}

func NewService(s store) *Service { return &Service{store: s} }

func (s *Service) List(ctx context.Context, userID string) ([]string, error) {
	return s.store.List(ctx, userID)
}

// Add thêm mã (chỉ nhận mã có trong price.Universe) và trả về danh sách mới của user.
func (s *Service) Add(ctx context.Context, userID, symbol string) ([]string, error) {
	sym, err := price.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if err := s.store.Add(ctx, userID, sym); err != nil {
		return nil, err
	}
	return s.store.List(ctx, userID)
}

func (s *Service) Remove(ctx context.Context, userID, symbol string) error {
	sym, err := price.NormalizeSymbol(symbol)
	if err != nil {
		return err
	}
	removed, err := s.store.Remove(ctx, userID, sym)
	if err != nil {
		return err
	}
	if !removed {
		return ErrNotInWatchlist
	}
	return nil
}
