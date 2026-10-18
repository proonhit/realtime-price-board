// Kiểm tra phía client, giống luật của backend (backend/internal/auth/service.go).
// Chỉ để báo lỗi sớm cho người dùng; backend vẫn kiểm tra lại vì client có thể bị bỏ qua.

import { ApiError } from "../../lib/apiClient";

export type FieldErrors<F extends string> = Partial<Record<F, string>>;

const emailRe = /^[^\s@]+@[^\s@]+$/;
const usernameRe = /^[A-Za-z0-9_]{3,50}$/;

// bcrypt giới hạn 72 BYTE, chữ có dấu chiếm 2-3 byte nên phải đếm byte chứ không đếm ký tự.
const byteLength = (s: string) => new TextEncoder().encode(s).length;

export function validateLogin(v: { email: string; password: string }): FieldErrors<"email" | "password"> {
  const errors: FieldErrors<"email" | "password"> = {};
  if (!v.email) errors.email = "Vui lòng nhập email.";
  else if (!emailRe.test(v.email)) errors.email = "Email không hợp lệ.";
  if (!v.password) errors.password = "Vui lòng nhập mật khẩu.";
  return errors;
}

export function validateRegister(v: {
  email: string;
  username: string;
  password: string;
}): FieldErrors<"email" | "username" | "password"> {
  const errors: FieldErrors<"email" | "username" | "password"> = {};
  if (!v.email) errors.email = "Vui lòng nhập email.";
  else if (v.email.length > 255 || !emailRe.test(v.email)) errors.email = "Email không hợp lệ.";
  if (!usernameRe.test(v.username)) errors.username = "Tên đăng nhập gồm 3–50 ký tự: chữ không dấu, số hoặc _.";
  const bytes = byteLength(v.password);
  if (bytes < 8) errors.password = "Mật khẩu tối thiểu 8 ký tự.";
  else if (bytes > 72) errors.password = "Mật khẩu quá dài (tối đa 72 byte).";
  return errors;
}

// Backend trả lỗi bằng tiếng Anh; dịch các lỗi đã biết, còn lại hiện nguyên văn.
const serverMessages: Record<string, string> = {
  "invalid email or password": "Email hoặc mật khẩu không đúng.",
  "email is already registered": "Email này đã được đăng ký.",
  "username is already taken": "Tên đăng nhập đã có người dùng.",
};

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return serverMessages[err.message] ?? err.message;
  return "Không kết nối được máy chủ. Vui lòng thử lại.";
}
