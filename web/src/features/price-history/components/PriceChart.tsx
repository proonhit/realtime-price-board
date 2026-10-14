import { formatPrice, formatTime } from "../../../lib/format";
import type { Tick } from "../../../types/api";

type Props = {
  ticks: Tick[]; // cũ → mới
  height?: number;
};

const W = 640;
const PAD = 8;

/** Biểu đồ đường SVG gọn nhẹ, không cần thư viện chart. */
export function PriceChart({ ticks, height = 220 }: Props) {
  if (ticks.length < 2) return <p className="empty">Chưa đủ dữ liệu để vẽ biểu đồ.</p>;

  const prices = ticks.map((t) => t.price);
  const min = Math.min(...prices);
  const max = Math.max(...prices);
  const span = max - min || 1;
  const x = (i: number) => PAD + (i / (ticks.length - 1)) * (W - PAD * 2);
  const y = (p: number) => PAD + (1 - (p - min) / span) * (height - PAD * 2);

  const line = ticks.map((t, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(t.price).toFixed(1)}`).join("");
  const area = `${line}L${x(ticks.length - 1)},${height}L${x(0)},${height}Z`;
  const first = ticks[0];
  const last = ticks[ticks.length - 1];
  const trend = last.price >= first.price ? "up" : "down";

  return (
    <figure className={`chart ${trend}`}>
      <svg viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" role="img"
        aria-label={`Giá từ ${formatPrice(first.price)} đến ${formatPrice(last.price)}`}>
        <path d={area} className="chart-area" />
        <path d={line} className="chart-line" vectorEffect="non-scaling-stroke" />
      </svg>
      <figcaption className="chart-meta muted">
        <span>{formatTime(first.ts)}</span>
        <span>
          Thấp {formatPrice(min)} · Cao {formatPrice(max)}
        </span>
        <span>{formatTime(last.ts)}</span>
      </figcaption>
    </figure>
  );
}
