-- Watchlist chuyển từ một danh sách chung sang mỗi user một danh sách.
--
-- Các dòng cũ không thuộc về user nào nên không thể gán user_id cho chúng.
-- Đây mới là dữ liệu dev (chưa có người dùng thật) nên xoá đi là chấp nhận được.
-- Nếu đã có dữ liệu thật thì phải chọn cách khác, ví dụ gán cho một user
-- mặc định hoặc copy cho mọi user.
DELETE FROM watchlist;

-- Thêm cột sau khi bảng đã rỗng nên NOT NULL không vướng dòng cũ nào.
-- ON DELETE CASCADE: xoá user thì watchlist của user đó cũng mất theo.
ALTER TABLE watchlist
    ADD COLUMN user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE;

-- Khoá chính cũ là (symbol): cả hệ thống chỉ có một dòng FPT. Khoá mới là
-- (user_id, symbol): mỗi user có một dòng FPT của riêng mình.
-- user_id đứng trước nên index của khoá chính phục vụ luôn câu
-- "WHERE user_id = $1 ORDER BY symbol", không cần tạo index riêng.
ALTER TABLE watchlist DROP CONSTRAINT watchlist_pkey;
ALTER TABLE watchlist ADD PRIMARY KEY (user_id, symbol);
