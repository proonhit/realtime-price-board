package wallet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Test này chạy với Postgres thật (đã chạy migrations tới 0007). Mặc định bị bỏ qua;
// muốn chạy thì set biến môi trường, ví dụ:
//
//	TEST_DATABASE_URL=postgres://app:app@localhost:5432/priceboard?sslmode=disable go test ./internal/wallet/
//
// Nó tạo user/ví riêng (email duy nhất theo thời điểm chạy) và xoá sạch khi xong.

func openTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping Postgres integration test")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	// Nhiều connection hơn mặc định để các goroutine thật sự chạy song song trong DB.
	cfg.MaxConns = 20
	db, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

type testUser struct{ id, username string }

// createUsers tạo n user với email/username duy nhất cho lần chạy này, và đăng ký
// dọn dẹp: xoá transfers trước (FK không cascade), rồi users (ví tự xoá theo).
func createUsers(t *testing.T, db *pgxpool.Pool, n int) []testUser {
	t.Helper()
	ctx := context.Background()
	run := time.Now().UnixNano()
	users := make([]testUser, n)
	for i := range users {
		name := fmt.Sprintf("wit_%d_%d", run, i)
		err := db.QueryRow(ctx,
			`INSERT INTO users (email, username, password_hash) VALUES ($1, $2, 'x') RETURNING id::text`,
			name+"@test.local", name).Scan(&users[i].id)
		if err != nil {
			t.Fatal(err)
		}
		users[i].username = name
	}
	t.Cleanup(func() {
		ids := make([]string, n)
		for i, u := range users {
			ids[i] = u.id
		}
		ctx := context.Background()
		if _, err := db.Exec(ctx,
			`DELETE FROM transfers WHERE from_wallet_id IN (SELECT id FROM wallets WHERE user_id = ANY($1))
			    OR to_wallet_id IN (SELECT id FROM wallets WHERE user_id = ANY($1))`, ids); err != nil {
			t.Errorf("cleanup transfers: %v", err)
		}
		if _, err := db.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, ids); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})
	return users
}

func balanceOf(t *testing.T, svc *Service, userID string) int64 {
	t.Helper()
	w, err := svc.Wallet(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return w.Balance
}

// 50 giao dịch A→B và B→A chạy đồng thời. Nếu khoá hai ví không theo thứ tự cố định,
// Postgres sẽ phát hiện deadlock và huỷ một transaction (lỗi 40P01).
func TestIntegrationConcurrentTransfersConserveMoney(t *testing.T) {
	db := openTestDB(t)
	svc := NewService(NewStore(db))
	ctx := context.Background()
	users := createUsers(t, db, 2)
	a, b := users[0], users[1]

	before := balanceOf(t, svc, a.id) + balanceOf(t, svc, b.id)

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{}) // đóng channel để mọi goroutine xuất phát cùng lúc
	for i := 0; i < n; i++ {
		from, to := a, b
		if i%2 == 1 {
			from, to = b, a
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := svc.Transfer(ctx, from.id, TransferRequest{
				ToUsername:     to.username,
				Amount:         int64(1000 + i*137),
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
			})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	ok := 0
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrInsufficientFunds):
			// Hợp lệ về nghiệp vụ, nhưng với số tiền nhỏ như trên thì không nên xảy ra.
			t.Logf("insufficient funds: %v", err)
		default:
			// Bao gồm deadlock_detected (40P01): không được phép xuất hiện.
			t.Errorf("unexpected transfer error: %v", err)
		}
	}

	balA, balB := balanceOf(t, svc, a.id), balanceOf(t, svc, b.id)
	if balA < 0 || balB < 0 {
		t.Fatalf("negative balance: A=%d B=%d", balA, balB)
	}
	if balA+balB != before {
		t.Fatalf("money not conserved: before=%d after=%d (A=%d B=%d)", before, balA+balB, balA, balB)
	}

	var rows int
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM transfers t JOIN wallets w ON w.id = t.from_wallet_id
		 WHERE w.user_id = ANY($1)`, []string{a.id, b.id}).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != ok {
		t.Fatalf("transfer rows = %d, successful calls = %d", rows, ok)
	}
	t.Logf("%d/%d transfers succeeded; A=%d B=%d total=%d", ok, n, balA, balB, balA+balB)
}

// Cùng một Idempotency-Key gửi 10 lần đồng thời: chỉ được có đúng một giao dịch,
// tiền chỉ bị trừ một lần, và cả 10 lời gọi đều nhận cùng một transfer id.
func TestIntegrationConcurrentSameIdempotencyKey(t *testing.T) {
	db := openTestDB(t)
	svc := NewService(NewStore(db))
	ctx := context.Background()
	users := createUsers(t, db, 2)
	a, b := users[0], users[1]
	startA := balanceOf(t, svc, a.id)

	const n = 10
	const amount int64 = 12_345
	type result struct {
		id       string
		replayed bool
		err      error
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, replayed, err := svc.Transfer(ctx, a.id, TransferRequest{
				ToUsername: b.username, Amount: amount, IdempotencyKey: "same-key",
			})
			results <- result{res.Transfer.ID, replayed, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	ids := map[string]bool{}
	fresh := 0
	for r := range results {
		if r.err != nil {
			t.Fatalf("transfer error: %v", r.err)
		}
		ids[r.id] = true
		if !r.replayed {
			fresh++
		}
	}
	if len(ids) != 1 || fresh != 1 {
		t.Fatalf("distinct transfer ids = %d, non-replayed = %d; want 1 and 1", len(ids), fresh)
	}

	var rows int
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM transfers t JOIN wallets w ON w.id = t.from_wallet_id
		 WHERE w.user_id = $1 AND t.idempotency_key = 'same-key'`, a.id).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("transfer rows with same key = %d, want 1", rows)
	}
	if got := balanceOf(t, svc, a.id); got != startA-amount {
		t.Fatalf("A balance = %d, want %d (debited exactly once)", got, startA-amount)
	}

	// Dùng lại key với số tiền khác → lỗi, không chuyển tiền.
	_, _, err := svc.Transfer(ctx, a.id, TransferRequest{
		ToUsername: b.username, Amount: amount + 1, IdempotencyKey: "same-key",
	})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("reuse with different amount: err = %v, want ErrIdempotencyConflict", err)
	}
	t.Logf("10 concurrent calls -> %d transfer row, 1 fresh + %d replayed", rows, n-fresh)
}

func TestIntegrationInsufficientFundsAndHistory(t *testing.T) {
	db := openTestDB(t)
	svc := NewService(NewStore(db))
	ctx := context.Background()
	users := createUsers(t, db, 2)
	a, b := users[0], users[1]

	_, _, err := svc.Transfer(ctx, a.id, TransferRequest{
		ToUsername: b.username, Amount: DemoStartingBalance + 1, IdempotencyKey: "too-much",
	})
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("err = %v, want ErrInsufficientFunds", err)
	}
	if got := balanceOf(t, svc, a.id); got != DemoStartingBalance {
		t.Fatalf("A balance = %d after failed transfer, want unchanged", got)
	}

	if _, _, err := svc.Transfer(ctx, a.id, TransferRequest{ToUsername: b.username, Amount: 100, IdempotencyKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Transfer(ctx, b.id, TransferRequest{ToUsername: a.username, Amount: 40, IdempotencyKey: "k1"}); err != nil {
		t.Fatal(err) // cùng key "k1" nhưng ví gửi khác → giao dịch mới hợp lệ
	}

	hist, err := svc.History(ctx, a.id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("history len = %d, want 2", len(hist))
	}
	// Mới nhất trước: B→A 40 (in), rồi A→B 100 (out).
	if hist[0].Direction != "in" || hist[0].Amount != 40 || hist[0].Counterparty != b.username {
		t.Fatalf("hist[0] = %+v", hist[0])
	}
	if hist[1].Direction != "out" || hist[1].Amount != 100 || hist[1].Counterparty != b.username {
		t.Fatalf("hist[1] = %+v", hist[1])
	}
}
