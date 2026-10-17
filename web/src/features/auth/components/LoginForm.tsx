import { useState, type FormEvent } from "react";
import { Button } from "../../../components/ui/Button";
import { useLogin } from "../api/auth";
import { errorMessage, validateLogin, type FieldErrors } from "../validation";
import { Field } from "./Field";

/** Form đăng nhập. Đăng nhập xong thì useAuth() chuyển sang "authenticated"; trang tự điều hướng. */
export function LoginForm() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [errors, setErrors] = useState<FieldErrors<"email" | "password">>({});
  const login = useLogin();

  const onSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault(); // chặn trình duyệt submit form kiểu cũ (tải lại trang)
    const input = { email: email.trim(), password };
    const found = validateLogin(input);
    setErrors(found);
    if (Object.keys(found).length === 0) login.mutate(input);
  };

  return (
    <form className="form" onSubmit={onSubmit} noValidate>
      <Field
        id="login-email"
        label="Email"
        type="email"
        autoComplete="email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        error={errors.email}
      />
      <Field
        id="login-password"
        label="Mật khẩu"
        type="password"
        autoComplete="current-password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={errors.password}
      />
      {login.isError && (
        <p className="form-error" role="alert">
          {errorMessage(login.error)}
        </p>
      )}
      <Button type="submit" variant="primary" disabled={login.isPending}>
        {login.isPending ? "Đang đăng nhập…" : "Đăng nhập"}
      </Button>
    </form>
  );
}
