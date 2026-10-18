// Package watchlist quản lý danh sách mã theo dõi của từng user.
//
// Mọi câu SQL đều có điều kiện user_id = $1: user chỉ đọc/sửa được dòng của mình.
// user_id lấy từ access token (qua middleware), không bao giờ lấy từ body hay URL.
package watchlist

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Query(ctx,
		`SELECT symbol FROM watchlist WHERE user_id = $1 ORDER BY symbol`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Khởi tạo slice rỗng (không phải nil) để JSON trả [] thay vì null.
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

// Add thêm mã vào danh sách của user; thêm mã đã có thì không làm gì
// (khoá chính (user_id, symbol) chặn trùng, ON CONFLICT bỏ qua lỗi đó).
func (s *Store) Add(ctx context.Context, userID, symbol string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO watchlist (user_id, symbol) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, symbol)
	return err
}

// Remove xoá mã khỏi danh sách của user và báo lại mã đó có trong danh sách hay không.
func (s *Store) Remove(ctx context.Context, userID, symbol string) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM watchlist WHERE user_id = $1 AND symbol = $2`, userID, symbol)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
