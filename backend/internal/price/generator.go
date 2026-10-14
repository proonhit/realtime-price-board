package price

import (
	"math"
	"math/rand/v2"
	"time"
)

// Generator sinh giá giả lập theo kiểu random walk, dao động tối đa ±0.2% mỗi bước.
type Generator struct {
	prices map[string]float64
	rnd    *rand.Rand
}

func NewGenerator(start map[string]float64, rnd *rand.Rand) *Generator {
	prices := make(map[string]float64, len(start))
	for s, p := range start {
		prices[s] = p
	}
	return &Generator{prices: prices, rnd: rnd}
}

// Next trả về một tick mới cho mỗi mã, sắp xếp theo mã.
func (g *Generator) Next(now time.Time) []Tick {
	ticks := make([]Tick, 0, len(g.prices))
	for _, sym := range Symbols() {
		p, ok := g.prices[sym]
		if !ok {
			continue
		}
		p = math.Round(p*(1+(g.rnd.Float64()-0.5)*0.004)*100) / 100
		g.prices[sym] = p
		ticks = append(ticks, Tick{Symbol: sym, Price: p, Ts: now.UnixMilli()})
	}
	return ticks
}
