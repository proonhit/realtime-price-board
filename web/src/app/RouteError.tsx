import { isRouteErrorResponse, Link, useRouteError } from "react-router";
import { Card } from "../components/ui/Card";

export function RouteError() {
  const error = useRouteError();
  // Không có error nghĩa là đang render từ route "*" (URL không khớp trang nào).
  const notFound = !error || (isRouteErrorResponse(error) && error.status === 404);

  return (
    <Card title={notFound ? "Không tìm thấy trang" : "Đã có lỗi xảy ra"}>
      <p className="empty">
        <Link to="/">Về bảng giá</Link>
      </p>
    </Card>
  );
}
