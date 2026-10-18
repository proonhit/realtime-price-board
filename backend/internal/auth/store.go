// Package auth lo đăng ký, đăng nhập và cấp token: access token là JWT sống ngắn,
// refresh token là chuỗi ngẫu nhiên sống dài, chỉ lưu hash trong bảng refresh_tokens.
package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"` // không bao giờ trả hash ra JSON
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at"` // nil (null) khi chưa đăng nhập lần nào
}

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// userColumns dùng chung cho mọi câu SELECT/RETURNING để thứ tự cột luôn khớp scanUser.
// id::text: đọc UUID thành string cho đơn giản, không cần kiểu UUID riêng.
const userColumns = `id::text, email, username, password_hash, created_at, last_login`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

// CreateUser thêm user mới. Không kiểm tra trùng trước bằng SELECT vì giữa SELECT
// và INSERT có thể có request khác chen vào; để unique index trong DB quyết định.
func (s *Store) CreateUser(ctx context.Context, email, username, passwordHash string) (User, error) {
	row := s.db.QueryRow(ctx,
		`INSERT INTO users (email, username, password_hash) VALUES ($1, $2, $3)
		 RETURNING `+userColumns,
		email, username, passwordHash)
	u, err := scanUser(row)

	// 23505 = unique_violation. Tên constraint/index cho biết cột nào bị trùng.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch {
		case strings.Contains(pgErr.ConstraintName, "email"):
			return User{}, ErrEmailTaken
		case strings.Contains(pgErr.ConstraintName, "username"):
			return User{}, ErrUsernameTaken
		}
	}
	return u, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	// So sánh lower(email) để dùng được index idx_users_email.
	return scanUser(s.db.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, email))
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.db.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

// UpdateLastLogin ghi thời điểm đăng nhập và trả lại giá trị DB đã lưu.
func (s *Store) UpdateLastLogin(ctx context.Context, id string) (time.Time, error) {
	var at time.Time
	err := s.db.QueryRow(ctx,
		`UPDATE users SET last_login = now() WHERE id = $1 RETURNING last_login`, id).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrUserNotFound
	}
	return at, err
}

// SaveRefreshToken lưu hash (không lưu token gốc) của refresh token mới.
func (s *Store) SaveRefreshToken(ctx context.Context, userID, hash string, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, expiresAt)
	return err
}

// RotateRefreshToken đổi token cũ lấy token mới: thu hồi oldHash, lưu newHash,
// và trả về user_id của token. Token cũ không tồn tại, đã thu hồi hoặc hết hạn
// thì trả ErrInvalidToken.
//
// Phải làm trong một transaction và khoá dòng bằng SELECT ... FOR UPDATE.
// Nếu không, hai request refresh gửi cùng lúc với cùng một token đều có thể đọc
// thấy "chưa thu hồi" rồi cùng cấp token mới, tức một token dùng được hai lần.
// Với FOR UPDATE, request thứ hai phải chờ request thứ nhất commit, sau đó đọc lại
// dòng thấy revoked_at đã có giá trị nên bị từ chối.
func (s *Store) RotateRefreshToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	// Rollback sau khi Commit thành công thì không làm gì, nên luôn defer được.
	// Mọi nhánh return sớm (lỗi) sẽ tự huỷ transaction.
	defer tx.Rollback(ctx)

	var (
		userID    string
		expiresAt time.Time
		revokedAt *time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT user_id::text, expires_at, revoked_at FROM refresh_tokens
		 WHERE token_hash = $1 FOR UPDATE`, oldHash).Scan(&userID, &expiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", err
	}
	if revokedAt != nil || !time.Now().Before(expiresAt) {
		return "", ErrInvalidToken
	}

	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1`, oldHash); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, newHash, newExpiresAt); err != nil {
		return "", err
	}
	return userID, tx.Commit(ctx)
}

// RevokeRefreshToken thu hồi token (dùng khi logout). Token không tồn tại hoặc
// đã thu hồi thì không báo lỗi: logout gọi nhiều lần vẫn cho cùng kết quả.
func (s *Store) RevokeRefreshToken(ctx context.Context, hash string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now()
		 WHERE token_hash = $1 AND revoked_at IS NULL`, hash)
	return err
}
