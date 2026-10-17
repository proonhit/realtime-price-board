import { useState, type FormEvent } from "react";
import { Button } from "../../../components/ui/Button";
import { useRegister } from "../api/auth";
import { errorMessage, validateRegister, type FieldErrors } from "../validation";
import { Field } from "./Field";

/** Form đăng ký. Thành công thì tự đăng nhập luôn (xem useRegister). */
export function RegisterForm() {
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [errors, setErrors] = useState<FieldErrors<"email" | "username" | "password">>({});
  const registerUser = useRegister();

  const onSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const input = { email: email.trim(), username: username.trim(), password };
    const found = validateRegister(input);
    setErrors(found);
    if (Object.keys(found).length === 0) registerUser.mutate(input);
  };

  return (
    <form className="form" onSubmit={onSubmit} noValidate>
      <Field
        id="register-email"
        label="Email"
        type="email"
        autoComplete="email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        error={errors.email}
      />
      <Field
        id="register-username"
        label="Tên đăng nhập"
        hint="3–50 ký tự: chữ không dấu, số hoặc _"
        autoComplete="username"
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        error={errors.username}
      />
      <Field
        id="register-password"
        label="Mật khẩu"
        hint="Tối thiểu 8 ký tự"
        type="password"
        autoComplete="new-password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={errors.password}
      />
      {registerUser.isError && (
        <p className="form-error" role="alert">
          {errorMessage(registerUser.error)}
        </p>
      )}
      <Button type="submit" variant="primary" disabled={registerUser.isPending}>
        {registerUser.isPending ? "Đang tạo tài khoản…" : "Đăng ký"}
      </Button>
    </form>
  );
}
