package wallet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	// DemoStartingBalance: ví mới được tặng 1.000.000đ để có tiền mà thử chuyển.
	// Đây là "tiền từ trên trời rơi xuống", chỉ hợp lý cho demo. Hệ thống thật
	// thì tiền chỉ vào ví qua nạp tiền (có giao dịch đối ứng), không tự sinh ra.
	DemoStartingBalance int64 = 1_000_000

	// MaxTransferAmount chặn số tiền vô lý (1.000 tỷ đồng). Còn cách rất xa giới hạn
	// int64 (~9,2 * 10^18), nên cộng vào số dư không thể bị tràn số.
	MaxTransferAmount int64 = 1_000_000_000_000

	maxIdempotencyKeyLen = 100
	defaultHistoryLimit  = 20
	maxHistoryLimit      = 100
)

// store là những gì Service cần từ tầng lưu trữ; test thay bằng store giả trong bộ nhớ.
type store interface {
	EnsureWallet(ctx context.Context, userID string, initialBalance int64) (Wallet, error)
	UserIDByUsername(ctx context.Context, username string) (string, error)
	Transfer(ctx context.Context, in TransferInput) (TransferResult, bool, error)
	ListTransfers(ctx context.Context, walletID string, limit int) ([]HistoryItem, error)
}

var _ store = (*Store)(nil)

type Service struct {
	store store
}

func NewService(s store) *Service { return &Service{store: s} }

// TransferRequest là những gì client gửi lên: body JSON và header Idempotency-Key.
type TransferRequest struct {
	ToUsername     string
	Amount         int64
	IdempotencyKey string
}

// Wallet trả ví của user, tạo lần đầu nếu chưa có.
func (s *Service) Wallet(ctx context.Context, userID string) (Wallet, error) {
	return s.store.EnsureWallet(ctx, userID, DemoStartingBalance)
}

// Transfer chuyển tiền từ ví của userID sang ví của req.ToUsername.
//
// replayed = true nghĩa là request này trùng key và trùng nội dung với một request
// đã thành công trước đó: tiền không bị chuyển lần nữa, trả lại kết quả cũ.
func (s *Service) Transfer(ctx context.Context, userID string, req TransferRequest) (TransferResult, bool, error) {
	if n := len(req.IdempotencyKey); n < 1 || n > maxIdempotencyKeyLen {
		return TransferResult{}, false, ErrInvalidIdempotencyKey
	}
	toUsername := strings.TrimSpace(req.ToUsername)
	if toUsername == "" {
		return TransferResult{}, false, fmt.Errorf("%w: to_username is required", ErrInvalidInput)
	}
	if req.Amount <= 0 || req.Amount > MaxTransferAmount {
		return TransferResult{}, false, fmt.Errorf("%w: amount must be between 1 and %d", ErrInvalidInput, MaxTransferAmount)
	}

	toUserID, err := s.store.UserIDByUsername(ctx, toUsername)
	if err != nil {
		return TransferResult{}, false, err
	}
	// So sánh user id chứ không so username: "Alice" và "alice" là cùng một người.
	if toUserID == userID {
		return TransferResult{}, false, ErrSelfTransfer
	}

	// (a) Tìm hoặc tạo hai ví. Làm NGOÀI transaction chuyển tiền vì:
	//   - tạo ví là thao tác idempotent, commit riêng cũng không sao;
	//   - nếu tạo ví trong transaction, A→B (tạo A rồi B) và B→A (tạo B rồi A) chạy
	//     cùng lúc với hai ví mới có thể chờ nhau (INSERT trùng unique phải đợi
	//     transaction kia kết thúc), tức lại là deadlock.
	from, err := s.store.EnsureWallet(ctx, userID, DemoStartingBalance)
	if err != nil {
		return TransferResult{}, false, err
	}
	to, err := s.store.EnsureWallet(ctx, toUserID, DemoStartingBalance)
	if err != nil {
		return TransferResult{}, false, err
	}

	hash := requestHash(to.ID, req.Amount)
	res, replayed, err := s.store.Transfer(ctx, TransferInput{
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         req.Amount,
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    hash,
	})
	if err != nil {
		return TransferResult{}, false, err
	}
	// Cùng key nhưng khác người nhận hoặc khác số tiền: gần như chắc chắn client
	// dùng nhầm key. Không chuyển tiền và báo lỗi, thay vì âm thầm trả kết quả cũ.
	if replayed && res.Transfer.RequestHash != hash {
		return TransferResult{}, false, ErrIdempotencyConflict
	}
	return res, replayed, nil
}

// History trả các giao dịch gần nhất của ví user. limit = 0 nghĩa là dùng mặc định.
func (s *Service) History(ctx context.Context, userID string, limit int) ([]HistoryItem, error) {
	if limit == 0 {
		limit = defaultHistoryLimit
	}
	if limit < 1 || limit > maxHistoryLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxHistoryLimit)
	}
	w, err := s.store.EnsureWallet(ctx, userID, DemoStartingBalance)
	if err != nil {
		return nil, err
	}
	return s.store.ListTransfers(ctx, w.ID, limit)
}

// requestHash là "dấu vân tay" của nội dung request, lưu kèm idempotency key.
// Chuỗi chuẩn hoá "to_wallet_id|amount" không phụ thuộc cách client viết JSON
// (thứ tự field, khoảng trắng, "Alice" hay "alice"), nên cùng ý nghĩa thì cùng hash.
func requestHash(toWalletID string, amount int64) string {
	sum := sha256.Sum256([]byte(toWalletID + "|" + strconv.FormatInt(amount, 10)))
	return hex.EncodeToString(sum[:])
}
