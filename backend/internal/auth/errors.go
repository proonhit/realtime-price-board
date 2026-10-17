package auth

import "errors"

// Các lỗi nghiệp vụ của package auth. Handler dùng errors.Is để đổi chúng
// thành HTTP status; lỗi nào không nằm trong danh sách này là lỗi 500.
var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrUserNotFound       = errors.New("user not found")
)
