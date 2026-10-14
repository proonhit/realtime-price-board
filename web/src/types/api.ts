// Kiểu dữ liệu khớp với backend/openapi.yaml.

export type Tick = {
  symbol: string;
  price: number;
  ts: number; // unix milliseconds
};
