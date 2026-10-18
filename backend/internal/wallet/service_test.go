package wallet

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// memStore là store giả trong bộ nhớ. Mutex đóng vai transaction + FOR UPDATE:
// cả hàm Transfer chạy trọn vẹn trước khi lời gọi khác được vào.
type memStore struct {
	mu        sync.Mutex
	users     map[string]string  // username (chữ thường) → user id
	wallets   map[string]*Wallet // user id → ví
	transfers []Transfer
}

func newMemStore(usernames ...string) *memStore {
	m := &memStore{users: map[string]string{}, wallets: map[string]*Wallet{}}
	for _, u := range usernames {
		m.users[strings.ToLower(u)] = "id-" + u
	}
	return m
}

func (m *memStore) EnsureWallet(_ context.Context, userID string, initial int64) (Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.wallets[userID]; ok {
		return *w, nil
	}
	w := &Wallet{ID: "w-" + userID, UserID: userID, Balance: initial, Currency: "VND"}
	m.wallets[userID] = w
	return *w, nil
}

func (m *memStore) UserIDByUsername(_ context.Context, username string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.users[strings.ToLower(username)]; ok {
		return id, nil
	}
	return "", ErrRecipientNotFound
}

func (m *memStore) walletByID(id string) *Wallet {
	for _, w := range m.wallets {
		if w.ID == id {
			return w
		}
	}
	return nil
}

func (m *memStore) Transfer(_ context.Context, in TransferInput) (TransferResult, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	from, to := m.walletByID(in.FromWalletID), m.walletByID(in.ToWalletID)
	for _, t := range m.transfers {
		if t.FromWalletID == in.FromWalletID && t.IdempotencyKey == in.IdempotencyKey {
			return TransferResult{Transfer: t, Balance: from.Balance}, true, nil
		}
	}
	if from.Balance < in.Amount {
		return TransferResult{}, false, ErrInsufficientFunds
	}
	from.Balance -= in.Amount
	to.Balance += in.Amount
	t := Transfer{
		ID:             "t-" + strconv.Itoa(len(m.transfers)+1),
		FromWalletID:   in.FromWalletID,
		ToWalletID:     in.ToWalletID,
		Amount:         in.Amount,
		CreatedAt:      time.Now(),
		IdempotencyKey: in.IdempotencyKey,
		RequestHash:    in.RequestHash,
	}
	m.transfers = append(m.transfers, t)
	return TransferResult{Transfer: t, Balance: from.Balance}, false, nil
}

func (m *memStore) ListTransfers(_ context.Context, walletID string, limit int) ([]HistoryItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []HistoryItem{}
	for i := len(m.transfers) - 1; i >= 0 && len(out) < limit; i-- {
		t := m.transfers[i]
		switch walletID {
		case t.FromWalletID:
			out = append(out, HistoryItem{Transfer: t, Direction: "out", Counterparty: m.walletByID(t.ToWalletID).UserID})
		case t.ToWalletID:
			out = append(out, HistoryItem{Transfer: t, Direction: "in", Counterparty: m.walletByID(t.FromWalletID).UserID})
		}
	}
	return out, nil
}

func TestTransferValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     TransferRequest
		wantErr error
	}{
		{"missing key", TransferRequest{ToUsername: "bob", Amount: 100}, ErrInvalidIdempotencyKey},
		{"key too long", TransferRequest{ToUsername: "bob", Amount: 100, IdempotencyKey: strings.Repeat("k", 101)}, ErrInvalidIdempotencyKey},
		{"key max length ok", TransferRequest{ToUsername: "bob", Amount: 100, IdempotencyKey: strings.Repeat("k", 100)}, nil},
		{"missing recipient", TransferRequest{ToUsername: "  ", Amount: 100, IdempotencyKey: "k"}, ErrInvalidInput},
		{"zero amount", TransferRequest{ToUsername: "bob", Amount: 0, IdempotencyKey: "k"}, ErrInvalidInput},
		{"negative amount", TransferRequest{ToUsername: "bob", Amount: -5, IdempotencyKey: "k"}, ErrInvalidInput},
		{"amount over max", TransferRequest{ToUsername: "bob", Amount: MaxTransferAmount + 1, IdempotencyKey: "k"}, ErrInvalidInput},
		{"unknown recipient", TransferRequest{ToUsername: "nobody", Amount: 100, IdempotencyKey: "k"}, ErrRecipientNotFound},
		{"self transfer", TransferRequest{ToUsername: "alice", Amount: 100, IdempotencyKey: "k"}, ErrSelfTransfer},
		{"self transfer other case", TransferRequest{ToUsername: "ALICE", Amount: 100, IdempotencyKey: "k"}, ErrSelfTransfer},
		{"insufficient funds", TransferRequest{ToUsername: "bob", Amount: DemoStartingBalance + 1, IdempotencyKey: "k"}, ErrInsufficientFunds},
		{"whole balance ok", TransferRequest{ToUsername: "bob", Amount: DemoStartingBalance, IdempotencyKey: "k"}, nil},
		{"recipient name is trimmed", TransferRequest{ToUsername: " Bob ", Amount: 1, IdempotencyKey: "k"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMemStore("alice", "bob")
			_, _, err := NewService(m).Transfer(context.Background(), "id-alice", tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			// Lỗi thì tiền không được dịch chuyển.
			if err != nil && len(m.transfers) != 0 {
				t.Fatalf("failed transfer stored %d rows", len(m.transfers))
			}
		})
	}
}

func TestTransferMovesMoney(t *testing.T) {
	m := newMemStore("alice", "bob")
	svc := NewService(m)
	ctx := context.Background()

	res, replayed, err := svc.Transfer(ctx, "id-alice", TransferRequest{ToUsername: "bob", Amount: 50_000, IdempotencyKey: "k1"})
	if err != nil || replayed {
		t.Fatalf("err = %v, replayed = %v", err, replayed)
	}
	if res.Balance != DemoStartingBalance-50_000 {
		t.Fatalf("balance = %d", res.Balance)
	}
	if bob, _ := svc.Wallet(ctx, "id-bob"); bob.Balance != DemoStartingBalance+50_000 {
		t.Fatalf("bob balance = %d", bob.Balance)
	}
}

func TestTransferIdempotency(t *testing.T) {
	first := TransferRequest{ToUsername: "bob", Amount: 50_000, IdempotencyKey: "k1"}
	tests := []struct {
		name         string
		second       TransferRequest
		wantErr      error
		wantReplayed bool
	}{
		{"same request is replayed", first, nil, true},
		{"same meaning, different spelling", TransferRequest{ToUsername: "BOB", Amount: 50_000, IdempotencyKey: "k1"}, nil, true},
		{"same key, different amount", TransferRequest{ToUsername: "bob", Amount: 60_000, IdempotencyKey: "k1"}, ErrIdempotencyConflict, false},
		{"same key, different recipient", TransferRequest{ToUsername: "carol", Amount: 50_000, IdempotencyKey: "k1"}, ErrIdempotencyConflict, false},
		{"different key is a new transfer", TransferRequest{ToUsername: "bob", Amount: 50_000, IdempotencyKey: "k2"}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMemStore("alice", "bob", "carol")
			svc := NewService(m)
			ctx := context.Background()
			orig, _, err := svc.Transfer(ctx, "id-alice", first)
			if err != nil {
				t.Fatal(err)
			}

			res, replayed, err := svc.Transfer(ctx, "id-alice", tt.second)
			if !errors.Is(err, tt.wantErr) || replayed != tt.wantReplayed {
				t.Fatalf("err = %v, replayed = %v; want %v, %v", err, replayed, tt.wantErr, tt.wantReplayed)
			}
			if tt.wantReplayed && res.Transfer.ID != orig.Transfer.ID {
				t.Fatalf("replay returned transfer %s, want %s", res.Transfer.ID, orig.Transfer.ID)
			}

			// Chỉ "different key" mới được chuyển thêm tiền.
			wantRows := 1
			if tt.second.IdempotencyKey != first.IdempotencyKey {
				wantRows = 2
			}
			if len(m.transfers) != wantRows {
				t.Fatalf("transfers = %d, want %d", len(m.transfers), wantRows)
			}
			if alice, _ := svc.Wallet(ctx, "id-alice"); alice.Balance != DemoStartingBalance-int64(wantRows)*50_000 {
				t.Fatalf("alice balance = %d", alice.Balance)
			}
		})
	}
}

func TestReplayAfterBalanceDrained(t *testing.T) {
	// Gửi lại request đã thành công phải trả kết quả cũ, kể cả khi bây giờ số dư
	// không còn đủ: request gửi lại không phải là một lần chuyển mới.
	m := newMemStore("alice", "bob")
	svc := NewService(m)
	ctx := context.Background()
	req := TransferRequest{ToUsername: "bob", Amount: DemoStartingBalance, IdempotencyKey: "all-in"}

	if _, _, err := svc.Transfer(ctx, "id-alice", req); err != nil {
		t.Fatal(err)
	}
	res, replayed, err := svc.Transfer(ctx, "id-alice", req)
	if err != nil || !replayed || res.Balance != 0 {
		t.Fatalf("err = %v, replayed = %v, balance = %d", err, replayed, res.Balance)
	}
}

func TestHistoryLimit(t *testing.T) {
	tests := []struct {
		limit   int
		wantErr error
	}{
		{0, nil}, // mặc định
		{1, nil},
		{100, nil},
		{-1, ErrInvalidInput},
		{101, ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.limit), func(t *testing.T) {
			_, err := NewService(newMemStore("alice")).History(context.Background(), "id-alice", tt.limit)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequestHashIsStable(t *testing.T) {
	h := requestHash("w-1", 50_000)
	if len(h) != 64 {
		t.Fatalf("hash length = %d, want 64", len(h))
	}
	if h != requestHash("w-1", 50_000) {
		t.Fatal("same input gave different hashes")
	}
	if h == requestHash("w-1", 50_001) || h == requestHash("w-2", 50_000) {
		t.Fatal("different input gave the same hash")
	}
}
