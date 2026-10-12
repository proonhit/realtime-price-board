import { useEffect, useState } from "react";

type Tick = { symbol: string; price: number; ts: number };
type Row = Tick & { prev?: number };

const WS_URL = import.meta.env.VITE_WS_URL ?? "ws://localhost:8080/ws";

export default function App() {
  const [rows, setRows] = useState<Record<string, Row>>({});
  const [status, setStatus] = useState("connecting");

  useEffect(() => {
    const ws = new WebSocket(WS_URL);
    ws.onopen = () => setStatus("connected");
    ws.onclose = () => setStatus("disconnected");
    ws.onmessage = (e) => {
      const t: Tick = JSON.parse(e.data);
      setRows((r) => ({ ...r, [t.symbol]: { ...t, prev: r[t.symbol]?.price } }));
    };
    return () => ws.close();
  }, []);

  const color = (r: Row) =>
    r.prev === undefined || r.prev === r.price ? "#ca8a04" : r.price > r.prev ? "#16a34a" : "#dc2626";

  return (
    <div style={{ padding: 24, fontFamily: "sans-serif" }}>
      <h2>Realtime Price Board <small>({status})</small></h2>
      <table cellPadding={8} style={{ borderCollapse: "collapse", minWidth: 360 }}>
        <thead>
          <tr><th align="left">Mã</th><th align="right">Giá</th><th align="right">Cập nhật</th></tr>
        </thead>
        <tbody>
          {Object.values(rows).sort((a, b) => a.symbol.localeCompare(b.symbol)).map((r) => (
            <tr key={r.symbol} style={{ borderTop: "1px solid #ddd" }}>
              <td><b>{r.symbol}</b></td>
              <td align="right" style={{ color: color(r), fontWeight: 600 }}>{r.price.toFixed(2)}</td>
              <td align="right">{new Date(r.ts).toLocaleTimeString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}