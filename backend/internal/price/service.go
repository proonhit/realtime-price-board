package price

import (
	"cmp"
	"context"
	"errors"
	"slices"
)

const (
	DefaultHistoryLimit = 100
	MaxHistoryLimit     = 1000
)

var ErrInvalidLimit = errors.New("limit must be between 1 and 1000")

type historyReader interface {
	History(ctx context.Context, symbol string, limit int) ([]Tick, error)
}

type latestReader interface {
	All(ctx context.Context) ([]Tick, error)
}

type Service struct {
	history historyReader
	latest  latestReader
}

func NewService(history historyReader, latest latestReader) *Service {
	return &Service{history: history, latest: latest}
}

// Latest trả về giá mới nhất của mọi mã, sắp xếp theo mã.
func (s *Service) Latest(ctx context.Context) ([]Tick, error) {
	ticks, err := s.latest.All(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(ticks, func(a, b Tick) int { return cmp.Compare(a.Symbol, b.Symbol) })
	return ticks, nil
}

// History trả về lịch sử giá của một mã theo thứ tự thời gian tăng dần (cũ → mới),
// tiện cho việc vẽ biểu đồ. limit = 0 nghĩa là dùng mặc định.
func (s *Service) History(ctx context.Context, symbol string, limit int) ([]Tick, error) {
	sym, err := NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = DefaultHistoryLimit
	}
	if limit < 1 || limit > MaxHistoryLimit {
		return nil, ErrInvalidLimit
	}
	ticks, err := s.history.History(ctx, sym, limit)
	if err != nil {
		return nil, err
	}
	slices.Reverse(ticks)
	return ticks, nil
}
