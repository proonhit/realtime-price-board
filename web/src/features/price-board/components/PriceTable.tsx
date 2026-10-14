import type { ReactNode } from "react";
import { Link } from "react-router";
import { formatPrice, formatTime } from "../../../lib/format";
import { usePriceStream } from "../hooks/usePriceStream";
import { direction } from "../types";

type Props = {
  /** Chỉ hiện các mã này; bỏ trống để hiện tất cả. */
  symbols?: string[];
  /** Nội dung cột cuối mỗi dòng, ví dụ nút theo dõi. Trang quyết định, không phải bảng. */
  actions?: (symbol: string) => ReactNode;
  emptyText?: string;
};

const arrow = { up: "▲", down: "▼", flat: "■" };

export function PriceTable({ symbols, actions, emptyText = "Đang chờ dữ liệu…" }: Props) {
  const { prices } = usePriceStream();
  const rows = Object.values(prices)
    .filter((r) => !symbols || symbols.includes(r.symbol))
    .sort((a, b) => a.symbol.localeCompare(b.symbol));

  if (rows.length === 0) return <p className="empty">{emptyText}</p>;

  return (
    <table className="price-table">
      <thead>
        <tr>
          <th>Mã</th>
          <th className="num">Giá</th>
          <th className="num">Cập nhật</th>
          {actions && <th aria-label="Thao tác" />}
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => {
          const dir = direction(r);
          return (
            <tr key={r.symbol}>
              <td>
                <Link to={`/symbol/${r.symbol}`} className="symbol">
                  {r.symbol}
                </Link>
              </td>
              <td className={`num price ${dir}`}>
                {/* key đổi theo ts để chạy lại hiệu ứng nháy mỗi khi giá cập nhật */}
                <span key={r.ts} className={dir === "flat" ? undefined : `flash-${dir}`}>
                  {arrow[dir]} {formatPrice(r.price)}
                </span>
              </td>
              <td className="num muted">{formatTime(r.ts)}</td>
              {actions && <td className="actions">{actions(r.symbol)}</td>}
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
