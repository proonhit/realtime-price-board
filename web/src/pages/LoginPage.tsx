import { Link, Navigate, useSearchParams } from "react-router";
import { Card } from "../components/ui/Card";
import { LoginForm } from "../features/auth/components/LoginForm";
import { useAuth } from "../features/auth/hooks/useAuth";
import { safeNext } from "../features/auth/types";

export function LoginPage() {
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  const { status } = useAuth();

  // Đã đăng nhập (vừa đăng nhập xong, hoặc mở /login khi đang có phiên) → về trang `next`.
  if (status === "authenticated") return <Navigate to={next} replace />;

  return (
    <div className="auth">
      <Card title="Đăng nhập">
        <LoginForm />
        <p className="auth-switch">
          Chưa có tài khoản? <Link to={`/register?next=${encodeURIComponent(next)}`}>Đăng ký</Link>
        </p>
      </Card>
    </div>
  );
}
