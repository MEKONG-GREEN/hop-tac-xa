package domain

import (
	"math"

	"github.com/shopspring/decimal"
)

// Lưới điểm lấy số liệu (BR-02, ADR-0065): tọa độ làm thô về 0,01° (≈ 1,1 km) trước khi gửi nhà cung cấp; vị trí cách một
// điểm có sẵn dưới 1 km dùng chung điểm đó. Nhà cung cấp chỉ thấy tọa độ đã làm thô, không thấy HTX nào.
const (
	GridDecimals = 2
	ShareRadiusM = 1000.0
)

// Point là tọa độ WGS84.
type Point struct{ Lat, Lon decimal.Decimal }

// Snap làm thô tọa độ về lưới.
func Snap(p Point) Point {
	return Point{Lat: p.Lat.Round(GridDecimals), Lon: p.Lon.Round(GridDecimals)}
}

// Valid cho biết tọa độ nằm trong miền hợp lệ.
func (p Point) Valid() bool {
	return p.Lat.Abs().LessThanOrEqual(decimal.NewFromInt(90)) && p.Lon.Abs().LessThanOrEqual(decimal.NewFromInt(180))
}

// DistanceM là khoảng cách Haversine (mét) giữa hai điểm.
func DistanceM(a, b Point) float64 {
	const r = 6371008.8
	la1, lo1 := a.Lat.InexactFloat64()*math.Pi/180, a.Lon.InexactFloat64()*math.Pi/180
	la2, lo2 := b.Lat.InexactFloat64()*math.Pi/180, b.Lon.InexactFloat64()*math.Pi/180
	h := math.Pow(math.Sin((la2-la1)/2), 2) + math.Cos(la1)*math.Cos(la2)*math.Pow(math.Sin((lo2-lo1)/2), 2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Nearest trả chỉ số của điểm gần p nhất trong khoảng ShareRadiusM (BR-02), -1 nếu không có.
func Nearest(p Point, pts []Point) int {
	best, bestD := -1, ShareRadiusM
	for i, q := range pts {
		if d := DistanceM(p, q); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}
