// Package wallet quản lý ví ảo của user và chuyển tiền giữa hai ví.
//
// Tiền luôn là int64 theo đơn vị nhỏ nhất (VND: 1 đồng), không bao giờ dùng float.
// Chuyển tiền chạy trong một transaction, khoá hai dòng ví bằng SELECT ... FOR UPDATE
// theo thứ tự id cố định, và chống trùng bằng idempotency key.
package wallet

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Wallet struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Balance   int64     `json:"balance"` // đồng, số nguyên
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Transfer struct {
	ID           string    `json:"id"`
	FromWalletID string    `json:"from_wallet_id"`
	ToWalletID   string    `json:"to_wallet_id"`
	Amount       int64     `json:"amount"`
	CreatedAt    time.Time `json:"created_at"`
	// Hai trường dưới chỉ dùng nội bộ. Không trả key ra JSON vì lịch sử của người
	// nhận cũng chứa giao dịch này, mà key là của người gửi.
	IdempotencyKey string `json:"-"`
	RequestHash    string `json:"-"`
}

// HistoryItem là một dòng trong lịch sử giao dịch, nhìn từ phía một ví.
// Nhúng Transfer (embedding) nên JSON phẳng: {id, ..., amount, direction, counterparty}.
type HistoryItem struct {
	Transfer
	Direction    string `json:"direction"`    // "out" = tiền ra, "in" = tiền vào
	Counterparty string `json:"counterparty"` // username của bên còn lại
}

// TransferInput là mọi thứ store cần để chuyển tiền. Service đã kiểm tra
// input và tìm sẵn hai ví trước khi gọi.
type TransferInput struct {
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	IdempotencyKey string
	RequestHash    string
}

// TransferResult: giao dịch và số dư của ví gửi.
type TransferResult struct {
	Transfer Transfer `json:"transfer"`
	Balance  int64    `json:"balance"`
}

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

const walletColumns = `id::text, user_id::text, balance, currency, created_at, updated_at`

func scanWallet(row pgx.Row) (Wallet, error) {
	var w Wallet
	err := row.Scan(&w.ID, &w.UserID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

// EnsureWallet trả ví của user, tạo mới với số dư initialBalance nếu chưa có.
//
// Hầu hết các lần gọi là ví đã có, nên SELECT trước. Không chỉ để nhanh: INSERT
// ... ON CONFLICT vào một ví đang bị transaction chuyển tiền khác UPDATE thì phải
// chờ transaction đó kết thúc (Postgres cần biết dòng đang sửa có còn hay không
// để quyết định có conflict), tức mọi request chỉ để "đọc ví" cũng xếp hàng sau
// các giao dịch đang chạy. Integration test đã cho thấy điều này.
//
// Chưa có ví thì INSERT ... ON CONFLICT (user_id) DO NOTHING rồi SELECT lại:
// hai request đầu tiên đến cùng lúc thì một cái INSERT được, cái kia gặp conflict
// và không làm gì, cả hai cùng đọc ra một ví. Không dùng INSERT thường vì giữa
// SELECT và INSERT request khác có thể đã tạo ví.
func (s *Store) EnsureWallet(ctx context.Context, userID string, initialBalance int64) (Wallet, error) {
	const selectSQL = `SELECT ` + walletColumns + ` FROM wallets WHERE user_id = $1`
	w, err := scanWallet(s.db.QueryRow(ctx, selectSQL, userID))
	if !errors.Is(err, pgx.ErrNoRows) {
		return w, err // đã có ví (err = nil), hoặc lỗi thật
	}

	_, err = s.db.Exec(ctx,
		`INSERT INTO wallets (user_id, balance) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`,
		userID, initialBalance)
	// 23503 = foreign_key_violation: user_id không có trong bảng users.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return Wallet{}, ErrUserNotFound
	}
	if err != nil {
		return Wallet{}, err
	}
	return scanWallet(s.db.QueryRow(ctx, selectSQL, userID))
}

// UserIDByUsername tìm user nhận tiền. Bảng users thuộc package auth, nhưng ở đây
// chỉ cần một câu SELECT nên đọc thẳng, không kéo cả auth.Service vào.
// lower(username) để dùng được unique index idx_users_username (migration 0005).
func (s *Store) UserIDByUsername(ctx context.Context, username string) (string, error) {
	var id string
	err := s.db.QueryRow(ctx,
		`SELECT id::text FROM users WHERE lower(username) = lower($1)`, username).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRecipientNotFound
	}
	return id, err
}

const transferColumns = `id::text, from_wallet_id::text, to_wallet_id::text, amount, created_at, idempotency_key, request_hash`

func scanTransfer(row pgx.Row) (Transfer, error) {
	var t Transfer
	err := row.Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.CreatedAt, &t.IdempotencyKey, &t.RequestHash)
	return t, err
}

// querier là phần chung của *pgxpool.Pool và pgx.Tx, để một hàm chạy được
// cả trong lẫn ngoài transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// findReplay tìm giao dịch đã lưu với cùng (ví gửi, key) và số dư hiện tại của ví gửi.
// found = false nếu key chưa được dùng.
func findReplay(ctx context.Context, q querier, fromWalletID, key string) (res TransferResult, found bool, err error) {
	res.Transfer, err = scanTransfer(q.QueryRow(ctx,
		`SELECT `+transferColumns+` FROM transfers
		 WHERE from_wallet_id = $1 AND idempotency_key = $2`, fromWalletID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return TransferResult{}, false, nil
	}
	if err != nil {
		return TransferResult{}, false, err
	}
	err = q.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, fromWalletID).Scan(&res.Balance)
	return res, true, err
}

// Transfer chuyển in.Amount từ ví gửi sang ví nhận, tất cả trong MỘT transaction.
//
// replayed = true nghĩa là key này đã được dùng trước đó: không chuyển tiền nữa,
// trả lại giao dịch đã lưu (kèm RequestHash để service so với request hiện tại).
//
// Các bước:
//
//	(b) tìm giao dịch cũ theo (ví gửi, key): có thì trả lại luôn, không khoá gì
//	(c) khoá hai dòng ví bằng FOR UPDATE, theo thứ tự id
//	(d) kiểm tra số dư
//	(e) trừ ví gửi, cộng ví nhận
//	(f) lưu giao dịch; trùng key (23505) thì rollback và đọc lại giao dịch đã lưu
//	(g) commit
//
// (Bước (a), tìm/tạo ví, service làm trước khi gọi hàm này.)
func (s *Store) Transfer(ctx context.Context, in TransferInput) (TransferResult, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TransferResult{}, false, err
	}
	// Rollback sau Commit thì không làm gì; mọi nhánh return lỗi tự huỷ transaction,
	// tiền đã trừ/cộng ở bước (e) cũng được hoàn lại.
	defer tx.Rollback(ctx)

	// (b) Đường nhanh cho request gửi lại: không cần khoá ví.
	if res, found, err := findReplay(ctx, tx, in.FromWalletID, in.IdempotencyKey); err != nil || found {
		return res, found, err
	}

	// (c) Khoá cả hai ví. Thứ tự khoá luôn theo id (ORDER BY id) chứ không theo
	// "gửi trước, nhận sau". Nếu A→B khoá A rồi B, còn B→A cùng lúc khoá B rồi A,
	// mỗi bên giữ một khoá và chờ khoá của bên kia mãi mãi: deadlock. Sắp theo id
	// thì cả hai cùng khoá ví id nhỏ trước, bên đến sau chờ ngay từ khoá đầu tiên.
	// Postgres khoá từng dòng theo đúng thứ tự trả ra sau ORDER BY.
	rows, err := tx.Query(ctx,
		`SELECT id::text, balance FROM wallets WHERE id = ANY($1) ORDER BY id FOR UPDATE`,
		[]string{in.FromWalletID, in.ToWalletID})
	if err != nil {
		return TransferResult{}, false, err
	}
	balances := map[string]int64{}
	for rows.Next() {
		var id string
		var bal int64
		if err := rows.Scan(&id, &bal); err != nil {
			rows.Close()
			return TransferResult{}, false, err
		}
		balances[id] = bal
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return TransferResult{}, false, err
	}
	if len(balances) != 2 {
		return TransferResult{}, false, errors.New("wallet: transfer wallets not found")
	}

	// (d) Số dư đọc SAU khi đã khoá, nên không request nào khác sửa được nó
	// cho tới khi transaction này kết thúc.
	if balances[in.FromWalletID] < in.Amount {
		return TransferResult{}, false, ErrInsufficientFunds
	}

	// (e) "balance = balance - $2" tính trong DB trên dòng đang khoá, không ghi đè
	// bằng con số tính ở Go, nên không có chuyện ghi đè mất cập nhật của người khác.
	var res TransferResult
	if err := tx.QueryRow(ctx,
		`UPDATE wallets SET balance = balance - $2, updated_at = now() WHERE id = $1 RETURNING balance`,
		in.FromWalletID, in.Amount).Scan(&res.Balance); err != nil {
		return TransferResult{}, false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance + $2, updated_at = now() WHERE id = $1`,
		in.ToWalletID, in.Amount); err != nil {
		return TransferResult{}, false, err
	}

	// (f) Lưu giao dịch.
	res.Transfer, err = scanTransfer(tx.QueryRow(ctx,
		`INSERT INTO transfers (from_wallet_id, to_wallet_id, amount, idempotency_key, request_hash)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+transferColumns,
		in.FromWalletID, in.ToWalletID, in.Amount, in.IdempotencyKey, in.RequestHash))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "transfers_idempotency_key" {
		// Hai request cùng key đến cùng lúc: cả hai đều qua bước (b) vì lúc đó
		// chưa có dòng nào được commit. Request đến sau chờ khoá ở bước (c), rồi
		// INSERT bị unique constraint chặn. Transaction đã lỗi thì Postgres không
		// cho chạy thêm câu nào, nên rollback ngay (hoàn lại tiền ở bước (e) và nhả
		// khoá cho request khác), rồi đọc giao dịch mà request kia đã commit.
		if err := tx.Rollback(ctx); err != nil {
			return TransferResult{}, false, err
		}
		res, found, err := findReplay(ctx, s.db, in.FromWalletID, in.IdempotencyKey)
		if err == nil && !found {
			err = errors.New("wallet: idempotency key conflict but transfer not found")
		}
		return res, found, err
	}
	if err != nil {
		return TransferResult{}, false, err
	}

	// (g) Chỉ tới đây tiền mới thật sự đổi chủ. Server chết trước dòng này thì
	// Postgres tự rollback, không có trạng thái "trừ rồi mà chưa cộng".
	return res, false, tx.Commit(ctx)
}

// ListTransfers trả các giao dịch gần nhất (cả ra lẫn vào) của một ví, mới nhất trước.
//
// Không viết "WHERE from_wallet_id = $1 OR to_wallet_id = $1 ORDER BY created_at DESC":
// với OR, Postgres khó dùng index để đọc sẵn theo thứ tự. Tách hai nhánh, mỗi nhánh
// đọc đúng index (from_wallet_id, created_at DESC) hoặc (to_wallet_id, created_at DESC)
// và chỉ lấy tối đa limit dòng, rồi gộp lại và cắt còn limit.
// UNION ALL (không phải UNION) vì hai nhánh không bao giờ trùng nhau
// (CHECK from <> to), khỏi tốn công loại trùng.
func (s *Store) ListTransfers(ctx context.Context, walletID string, limit int) ([]HistoryItem, error) {
	rows, err := s.db.Query(ctx, `
		SELECT t.id::text, t.from_wallet_id::text, t.to_wallet_id::text, t.amount, t.created_at,
		       t.direction, u.username
		FROM (
		    (SELECT id, from_wallet_id, to_wallet_id, amount, created_at,
		            'out' AS direction, to_wallet_id AS other_wallet_id FROM transfers
		     WHERE from_wallet_id = $1 ORDER BY created_at DESC LIMIT $2)
		    UNION ALL
		    (SELECT id, from_wallet_id, to_wallet_id, amount, created_at,
		            'in' AS direction, from_wallet_id AS other_wallet_id FROM transfers
		     WHERE to_wallet_id = $1 ORDER BY created_at DESC LIMIT $2)
		) t
		JOIN wallets w ON w.id = t.other_wallet_id
		JOIN users u ON u.id = w.user_id
		ORDER BY t.created_at DESC, t.id
		LIMIT $2`, walletID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HistoryItem{}
	for rows.Next() {
		var h HistoryItem
		if err := rows.Scan(&h.ID, &h.FromWalletID, &h.ToWalletID, &h.Amount, &h.CreatedAt,
			&h.Direction, &h.Counterparty); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
