import { Link, Navigate, useSearchParams } from "react-router";
import { Card } from "../components/ui/Card";
import { RegisterForm } from "../features/auth/components/RegisterForm";
import { useAuth } from "../features/auth/hooks/useAuth";
import { safeNext } from "../features/auth/types";

export function RegisterPage() {
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  const { status } = useAuth();

  // RegisterForm đăng ký xong tự đăng nhập → status thành "authenticated" → chuyển trang.
  if (status === "authenticated") return <Navigate to={next} replace />;

  return (
    <div className="auth">
      <Card title="Đăng ký">
        <RegisterForm />
        <p className="auth-switch">
          Đã có tài khoản? <Link to={`/login?next=${encodeURIComponent(next)}`}>Đăng nhập</Link>
        </p>
      </Card>
    </div>
  );
}
