package wallet

import "errors"

// Các lỗi nghiệp vụ của package wallet. Handler dùng errors.Is để đổi chúng
// thành HTTP status; lỗi khác là lỗi 500.
var (
	ErrInvalidInput          = errors.New("invalid input")
	ErrInvalidIdempotencyKey = errors.New("Idempotency-Key header is required and must be 1-100 characters")
	ErrSelfTransfer          = errors.New("cannot transfer to yourself")
	ErrRecipientNotFound     = errors.New("recipient not found")
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrIdempotencyConflict   = errors.New("Idempotency-Key was already used with a different request")
	// ErrUserNotFound: token còn hạn nhưng user đã bị xoá, không tạo được ví.
	ErrUserNotFound = errors.New("user not found")
)
