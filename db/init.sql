CREATE TABLE IF NOT EXISTS ticks (
  id      BIGSERIAL PRIMARY KEY,
  symbol  VARCHAR(10)   NOT NULL,
  price   NUMERIC(12,2) NOT NULL,
  ts      TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ticks_symbol_ts ON ticks(symbol, ts DESC);