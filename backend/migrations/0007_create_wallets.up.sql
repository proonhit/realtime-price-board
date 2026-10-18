-- Ví ảo: mỗi user đúng một ví (user_id UNIQUE).
--
-- Tiền lưu bằng số nguyên theo đơn vị nhỏ nhất của loại tiền. VND không có đơn vị
-- lẻ (không có "xu") nên đơn vị nhỏ nhất là 1 đồng: 50.000đ lưu là 50000.
-- Nếu sau này có USD thì lưu theo cent (12,34 USD = 1234).
--
-- Vì sao BIGINT mà không phải float/double? Float lưu số theo hệ nhị phân nên
-- không biểu diễn chính xác được nhiều số thập phân (0.1 + 0.2 = 0.30000000000000004),
-- cộng trừ nhiều lần sẽ lệch tiền. BIGINT chính xác tuyệt đối, tối đa ~9,2 * 10^18.
-- NUMERIC(20,0) hay NUMERIC(19,4) cũng chính xác, nhưng chậm hơn và Go không có
-- kiểu sẵn tương ứng (phải dùng thư viện decimal); với VND, int64 là đủ.
CREATE TABLE wallets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    -- CHECK là lưới an toàn cuối cùng: dù code có bug, DB cũng không cho số dư âm.
    balance    BIGINT NOT NULL CHECK (balance >= 0),
    currency   CHAR(3) NOT NULL DEFAULT 'VND',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Mỗi dòng là một lần chuyển tiền đã thành công. Không có ON DELETE CASCADE:
-- lịch sử tiền không nên tự biến mất. Muốn xoá user đã có giao dịch thì DB
-- sẽ chặn (thực tế thường "xoá mềm" user thay vì xoá hẳn).
CREATE TABLE transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id  UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id    UUID NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    -- Idempotency key do client gửi trong header Idempotency-Key.
    idempotency_key TEXT NOT NULL,
    -- sha256 (hex, 64 ký tự) của "to_wallet_id|amount": dùng để phát hiện client
    -- gửi lại cùng key nhưng nội dung khác.
    request_hash    CHAR(64) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id),
    -- Key chỉ cần duy nhất trong phạm vi ví gửi: hai user khác nhau dùng trùng
    -- key "abc" là chuyện bình thường. Đặt tên constraint để code nhận ra nó
    -- khi gặp lỗi 23505.
    CONSTRAINT transfers_idempotency_key UNIQUE (from_wallet_id, idempotency_key)
);

-- Lịch sử giao dịch của một ví = tiền ra (from) + tiền vào (to), mới nhất trước.
CREATE INDEX idx_transfers_from_created ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX idx_transfers_to_created   ON transfers (to_wallet_id, created_at DESC);
