-- Email đã có unique index trên lower(email) (idx_users_email) nên constraint
-- UNIQUE(email) cũ là thừa: hai email trùng chính xác thì cũng trùng sau lower().
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;

-- Username cũng so sánh không phân biệt hoa thường: "Alice" và "alice" là một người.
-- Lưu ý: migration này fail nếu DB đã có hai username chỉ khác nhau hoa/thường.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_username_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (lower(username));
