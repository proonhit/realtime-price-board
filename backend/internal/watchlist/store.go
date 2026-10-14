// Package watchlist quản lý danh sách mã theo dõi.
//
// Hiện chưa có đăng nhập nên chỉ có một danh sách chung; khi thêm auth,
// bảng watchlist sẽ có thêm cột user_id.
package watchlist

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT symbol FROM watchlist ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

// Add thêm mã vào danh sách; thêm mã đã có thì không làm gì.
func (s *Store) Add(ctx context.Context, symbol string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO watchlist (symbol) VALUES ($1) ON CONFLICT DO NOTHING`, symbol)
	return err
}

// Remove xoá mã và báo lại mã đó có nằm trong danh sách hay không.
func (s *Store) Remove(ctx context.Context, symbol string) (bool, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM watchlist WHERE symbol = $1`, symbol)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
