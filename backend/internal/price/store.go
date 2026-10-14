package price

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store lưu lịch sử giá trong PostgreSQL.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) Insert(ctx context.Context, t Tick) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO ticks (symbol, price, ts) VALUES ($1, $2, $3)`,
		t.Symbol, t.Price, time.UnixMilli(t.Ts))
	return err
}

// History trả về tối đa limit tick gần nhất của một mã, mới nhất trước.
func (s *Store) History(ctx context.Context, symbol string, limit int) ([]Tick, error) {
	rows, err := s.db.Query(ctx,
		`SELECT symbol, price::float8, ts FROM ticks
		 WHERE symbol = $1 ORDER BY ts DESC LIMIT $2`, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Tick{}
	for rows.Next() {
		var t Tick
		var ts time.Time
		if err := rows.Scan(&t.Symbol, &t.Price, &ts); err != nil {
			return nil, err
		}
		t.Ts = ts.UnixMilli()
		out = append(out, t)
	}
	return out, rows.Err()
}
