package domain

// AH-153 (FN-06.6): so sánh sản lượng thực tế với dự kiến của vụ theo hộ. Hàm thuần, không đọc DB.

import "github.com/shopspring/decimal"

var thousandKg = decimal.NewFromInt(1000)

// TonnesFromKg đổi kg sang tấn, làm tròn half-up 2 chữ số lẻ (như hiển thị khai báo — KH-26).
func TonnesFromKg(kg decimal.Decimal) decimal.Decimal { return RoundTonnes(kg.Div(thousandKg)) }

// CompareYield trả chênh lệch thực tế − dự kiến (tấn) và phần trăm so với dự kiến (1 chữ số lẻ, làm tròn nửa xa số 0).
// Thiếu một trong hai giá trị → cả hai nil; dự kiến bằng 0 → phần trăm nil.
func CompareYield(expectedT, actualT *decimal.Decimal) (diffT, diffPct *decimal.Decimal) {
	if expectedT == nil || actualT == nil {
		return nil, nil
	}
	d := actualT.Sub(*expectedT)
	diffT = &d
	if expectedT.IsZero() {
		return diffT, nil
	}
	p := d.Mul(hundred).Div(*expectedT).Round(1)
	return diffT, &p
}
