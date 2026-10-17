package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// memStore là store giả trong bộ nhớ, mô phỏng đúng các quy tắc của DB thật:
// email/username unique không phân biệt hoa thường, token thu hồi/hết hạn thì không dùng được.
type memStore struct {
	mu     sync.Mutex
	users  []User
	tokens map[string]*memToken // key là token hash
}

type memToken struct {
	userID    string
	expiresAt time.Time
	revoked   bool
}

func newMemStore() *memStore { return &memStore{tokens: map[string]*memToken{}} }

func (m *memStore) CreateUser(_ context.Context, email, username, hash string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
			return User{}, ErrEmailTaken
		}
		if strings.EqualFold(u.Username, username) {
			return User{}, ErrUsernameTaken
		}
	}
	u := User{
		ID:           "user-" + strconv.Itoa(len(m.users)+1),
		Email:        email,
		Username:     username,
		PasswordHash: hash,
		CreatedAt:    time.Now(),
	}
	m.users = append(m.users, u)
	return u, nil
}

func (m *memStore) find(match func(User) bool) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if match(u) {
			return u, nil
		}
	}
	return User{}, ErrUserNotFound
}

func (m *memStore) UserByEmail(_ context.Context, email string) (User, error) {
	return m.find(func(u User) bool { return strings.EqualFold(u.Email, email) })
}

func (m *memStore) UserByID(_ context.Context, id string) (User, error) {
	return m.find(func(u User) bool { return u.ID == id })
}

func (m *memStore) UpdateLastLogin(_ context.Context, id string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.users {
		if m.users[i].ID == id {
			now := time.Now()
			m.users[i].LastLoginAt = &now
			return now, nil
		}
	}
	return time.Time{}, ErrUserNotFound
}

func (m *memStore) SaveRefreshToken(_ context.Context, userID, hash string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[hash] = &memToken{userID: userID, expiresAt: expiresAt}
	return nil
}

// RotateRefreshToken giữ mutex suốt hàm, tương đương transaction + FOR UPDATE ở DB thật.
func (m *memStore) RotateRefreshToken(_ context.Context, oldHash, newHash string, newExpiresAt time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[oldHash]
	if !ok || t.revoked || !time.Now().Before(t.expiresAt) {
		return "", ErrInvalidToken
	}
	t.revoked = true
	m.tokens[newHash] = &memToken{userID: t.userID, expiresAt: newExpiresAt}
	return t.userID, nil
}

func (m *memStore) RevokeRefreshToken(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tokens[hash]; ok {
		t.revoked = true
	}
	return nil
}

var testTokens = Tokens{Secret: []byte("test-secret-at-least-32-bytes-long"), TTL: time.Minute}

func newTestService() *Service {
	return NewService(newMemStore(), testTokens, time.Hour)
}

// mustRegister tạo sẵn user alice@example.com / alice / password123.
func mustRegister(t *testing.T, svc *Service) User {
	t.Helper()
	u, err := svc.Register(context.Background(), "alice@example.com", "alice", "password123")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return u
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		username string
		password string
		wantErr  error
	}{
		{"ok", "bob@example.com", "bob_99", "password123", nil},
		{"duplicate email different case", " ALICE@Example.com ", "alice2", "password123", ErrEmailTaken},
		{"duplicate username different case", "other@example.com", "ALICE", "password123", ErrUsernameTaken},
		{"empty email", "", "bob", "password123", ErrInvalidInput},
		{"email without @", "bob.example.com", "bob", "password123", ErrInvalidInput},
		{"email with two @", "bob@@example.com", "bob", "password123", ErrInvalidInput},
		{"email with display name", "Bob <bob@example.com>", "bob", "password123", ErrInvalidInput},
		{"username too short", "bob@example.com", "bo", "password123", ErrInvalidInput},
		{"username too long", "bob@example.com", strings.Repeat("b", 51), "password123", ErrInvalidInput},
		{"username with invalid char", "bob@example.com", "bob-smith", "password123", ErrInvalidInput},
		{"password too short", "bob@example.com", "bob", "short", ErrInvalidInput},
		{"password too long", "bob@example.com", "bob", strings.Repeat("p", 73), ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService()
			mustRegister(t, svc)

			u, err := svc.Register(context.Background(), tt.email, tt.username, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (u.ID == "" || u.PasswordHash == tt.password) {
				t.Fatalf("unexpected user %+v", u)
			}
		})
	}
}

func TestRegisterNormalizesEmail(t *testing.T) {
	svc := newTestService()
	u, err := svc.Register(context.Background(), "  Bob@Example.COM ", " bob ", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "bob@example.com" || u.Username != "bob" {
		t.Fatalf("got email %q username %q", u.Email, u.Username)
	}
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{"ok", "alice@example.com", "password123", nil},
		{"ok with different email case", "ALICE@example.com", "password123", nil},
		{"wrong password", "alice@example.com", "wrong-password", ErrInvalidCredentials},
		// Cùng một lỗi với sai mật khẩu, để không lộ email nào đã đăng ký.
		{"unknown email", "nobody@example.com", "password123", ErrInvalidCredentials},
		{"empty password", "alice@example.com", "", ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService()
			alice := mustRegister(t, svc)

			pair, err := svc.Login(context.Background(), tt.email, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if pair.AccessToken == "" || pair.RefreshToken == "" || pair.ExpiresIn != 60 {
				t.Fatalf("unexpected pair %+v", pair)
			}
			if pair.User.ID != alice.ID || pair.User.LastLoginAt == nil {
				t.Fatalf("unexpected user %+v", pair.User)
			}
			sub, err := testTokens.ParseAccess(pair.AccessToken)
			if err != nil || sub != alice.ID {
				t.Fatalf("access token sub = %q, err = %v", sub, err)
			}
		})
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	mustRegister(t, svc)
	first, err := svc.Login(ctx, "alice@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}

	second, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("expected a new refresh token")
	}
	if second.User.ID != first.User.ID {
		t.Fatalf("user = %q, want %q", second.User.ID, first.User.ID)
	}

	// Token cũ đã bị thu hồi khi rotate, dùng lại phải bị từ chối.
	if _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reuse old token: err = %v, want ErrInvalidToken", err)
	}
	// Token mới vẫn dùng được.
	if _, err := svc.Refresh(ctx, second.RefreshToken); err != nil {
		t.Fatalf("refresh with new token: %v", err)
	}
}

func TestRefreshRejectsInvalidTokens(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown token", func(t *testing.T) {
		if _, err := newTestService().Refresh(ctx, "not-a-real-token"); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("err = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("empty token", func(t *testing.T) {
		if _, err := newTestService().Refresh(ctx, ""); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("err = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("revoked by logout", func(t *testing.T) {
		svc := newTestService()
		mustRegister(t, svc)
		pair, err := svc.Login(ctx, "alice@example.com", "password123")
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Logout(ctx, pair.RefreshToken); err != nil {
			t.Fatalf("Logout: %v", err)
		}
		if _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("err = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		// refreshTTL âm: token hết hạn ngay khi được tạo.
		svc := NewService(newMemStore(), testTokens, -time.Minute)
		mustRegister(t, svc)
		pair, err := svc.Login(ctx, "alice@example.com", "password123")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("err = %v, want ErrInvalidToken", err)
		}
	})
}

func TestLogoutIsIdempotent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	mustRegister(t, svc)
	pair, err := svc.Login(ctx, "alice@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{pair.RefreshToken, pair.RefreshToken, "unknown", ""} {
		if err := svc.Logout(ctx, raw); err != nil {
			t.Fatalf("Logout(%q): %v", raw, err)
		}
	}
}

func TestMe(t *testing.T) {
	svc := newTestService()
	alice := mustRegister(t, svc)

	got, err := svc.Me(context.Background(), alice.ID)
	if err != nil || got.Email != alice.Email {
		t.Fatalf("Me = %+v, %v", got, err)
	}
	if _, err := svc.Me(context.Background(), "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}
