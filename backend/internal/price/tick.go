// Package price chứa mọi thứ về giá: model, sinh giá, lưu trữ, API.
package price

import (
	"errors"
	"slices"
	"strings"
)

type Tick struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
	Ts     int64   `json:"ts"` // unix milliseconds
}

// Universe là danh sách mã được hỗ trợ và giá khởi điểm của từng mã.
var Universe = map[string]float64{
	"VNM": 65, "FPT": 120, "HPG": 27, "VIC": 42, "MWG": 60, "VCB": 90,
}

var ErrUnknownSymbol = errors.New("unknown symbol")

// NormalizeSymbol chuẩn hoá mã (viết hoa, bỏ khoảng trắng) và kiểm tra mã có tồn tại.
func NormalizeSymbol(s string) (string, error) {
	sym := strings.ToUpper(strings.TrimSpace(s))
	if _, ok := Universe[sym]; !ok {
		return "", ErrUnknownSymbol
	}
	return sym, nil
}

func Symbols() []string {
	syms := make([]string, 0, len(Universe))
	for s := range Universe {
		syms = append(syms, s)
	}
	slices.Sort(syms)
	return syms
}
