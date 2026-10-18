package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// store là những gì Service cần từ tầng lưu trữ. Khai báo interface ở phía dùng
// (không phải phía Store) để test có thể thay bằng store giả trong bộ nhớ.
type store interface {
	CreateUser(ctx context.Context, email, username, passwordHash string) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	UpdateLastLogin(ctx context.Context, id string) (time.Time, error)
	SaveRefreshToken(ctx context.Context, userID, hash string, expiresAt time.Time) error
	RotateRefreshToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (string, error)
	RevokeRefreshToken(ctx context.Context, hash string) error
}

// Dòng này không chạy gì cả: nó bắt compiler kiểm tra *Store có đủ method của store.
var _ store = (*Store)(nil)

// TokenPair là kết quả của login và refresh.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // số giây access token còn hiệu lực
	User         User   `json:"user"`
}

type Service struct {
	store      store
	tokens     Tokens
	refreshTTL time.Duration
}

func NewService(s store, tokens Tokens, refreshTTL time.Duration) *Service {
	return &Service{store: s, tokens: tokens, refreshTTL: refreshTTL}
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,50}$`)

// dummyHash dùng khi login với email không tồn tại, xem Login.
var dummyHash, _ = HashPassword("dummy-password-for-timing")

// Register kiểm tra input, hash mật khẩu rồi tạo user.
func (s *Service) Register(ctx context.Context, email, username, password string) (User, error) {
	email = normalizeEmail(email)
	username = strings.TrimSpace(username)

	if !validEmail(email) {
		return User{}, fmt.Errorf("%w: email is not valid", ErrInvalidInput)
	}
	if !usernameRe.MatchString(username) {
		return User{}, fmt.Errorf("%w: username must be 3-50 characters of letters, digits or _", ErrInvalidInput)
	}
	// bcrypt chỉ dùng 72 byte đầu của mật khẩu (bản Go còn báo lỗi nếu dài hơn),
	// nên giới hạn theo byte chứ không theo số ký tự: chữ có dấu chiếm 2-3 byte.
	if len(password) < 8 || len(password) > 72 {
		return User{}, fmt.Errorf("%w: password must be 8-72 bytes", ErrInvalidInput)
	}

	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	return s.store.CreateUser(ctx, email, username, hash)
}

// Login trả cùng một lỗi ErrInvalidCredentials cho cả "sai email" lẫn "sai mật khẩu".
// Nếu báo riêng "email không tồn tại", kẻ tấn công có thể dò xem email nào đã
// đăng ký (user enumeration).
func (s *Service) Login(ctx context.Context, email, password string) (TokenPair, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return TokenPair{}, fmt.Errorf("%w: email and password are required", ErrInvalidInput)
	}

	user, err := s.store.UserByEmail(ctx, email)
	if errors.Is(err, ErrUserNotFound) {
		// Vẫn chạy bcrypt với hash giả để thời gian phản hồi gần bằng trường hợp
		// sai mật khẩu. Nếu trả về ngay, đo thời gian cũng đủ biết email có tồn tại.
		CheckPassword(dummyHash, password)
		return TokenPair{}, ErrInvalidCredentials
	}
	if err != nil {
		return TokenPair{}, err
	}
	if !CheckPassword(user.PasswordHash, password) {
		return TokenPair{}, ErrInvalidCredentials
	}

	at, err := s.store.UpdateLastLogin(ctx, user.ID)
	if err != nil {
		return TokenPair{}, err
	}
	user.LastLoginAt = &at

	raw, hash, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.store.SaveRefreshToken(ctx, user.ID, hash, time.Now().Add(s.refreshTTL)); err != nil {
		return TokenPair{}, err
	}
	return s.pair(user, raw)
}

// Refresh đổi refresh token cũ lấy cặp token mới. Token cũ bị thu hồi ngay
// (rotation), nên mỗi refresh token chỉ dùng được một lần.
func (s *Service) Refresh(ctx context.Context, raw string) (TokenPair, error) {
	if raw == "" {
		return TokenPair{}, ErrInvalidToken
	}
	newRaw, newHash, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	userID, err := s.store.RotateRefreshToken(ctx, HashRefreshToken(raw), newHash, time.Now().Add(s.refreshTTL))
	if err != nil {
		return TokenPair{}, err
	}

	user, err := s.store.UserByID(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return TokenPair{}, ErrInvalidToken
	}
	if err != nil {
		return TokenPair{}, err
	}
	return s.pair(user, newRaw)
}

// Logout thu hồi refresh token. Token rỗng, sai hoặc đã thu hồi đều coi là thành công.
//
// Access token (JWT) không thu hồi được: nó vẫn dùng được tới khi hết hạn,
// vì vậy TTL của access token nên ngắn (mặc định 15 phút).
func (s *Service) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	return s.store.RevokeRefreshToken(ctx, HashRefreshToken(raw))
}

func (s *Service) Me(ctx context.Context, userID string) (User, error) {
	return s.store.UserByID(ctx, userID)
}

// pair ký access token cho user và ghép với refresh token đã lưu.
func (s *Service) pair(user User, refreshRaw string) (TokenPair, error) {
	access, err := s.tokens.IssueAccess(user.ID)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refreshRaw,
		ExpiresIn:    int(s.tokens.TTL.Seconds()),
		User:         user,
	}, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// validEmail chỉ kiểm tra cơ bản. Muốn chắc email có thật thì phải gửi mail xác nhận.
func validEmail(email string) bool {
	if len(email) > 255 || strings.Count(email, "@") != 1 {
		return false
	}
	// ParseAddress cũng chấp nhận dạng "Tên <a@b.com>"; so sánh Address == email
	// để chỉ nhận đúng địa chỉ trần.
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}
