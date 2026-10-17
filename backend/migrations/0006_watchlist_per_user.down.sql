-- Quay về một danh sách chung: khoá chính lại là (symbol).
ALTER TABLE watchlist DROP CONSTRAINT watchlist_pkey;

-- Nhiều user có thể cùng theo dõi một mã, nên phải bỏ dòng trùng trước khi tạo
-- lại khoá chính (symbol), nếu không ADD PRIMARY KEY sẽ fail.
-- Với mỗi symbol chỉ giữ dòng thêm sớm nhất (hoà thì so tiếp user_id cho chắc chắn).
DELETE FROM watchlist w
USING watchlist older
WHERE w.symbol = older.symbol
  AND (older.added_at, older.user_id) < (w.added_at, w.user_id);

ALTER TABLE watchlist DROP COLUMN user_id;
ALTER TABLE watchlist ADD PRIMARY KEY (symbol);
