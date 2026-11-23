package openmeteo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
)

// Phản hồi mẫu theo định dạng Open-Meteo (timeformat=unixtime, timezone Asia/Ho_Chi_Minh): 23/11/2026 giờ Việt Nam.
const sample = `{"latitude":10.03,"longitude":105.78,"timezone":"Asia/Ho_Chi_Minh",
 "current":{"time":1795413600,"interval":900,"temperature_2m":30.4,"relative_humidity_2m":71,"precipitation":0.0,"weather_code":2,"wind_speed_10m":11.2,"wind_direction_10m":48},
 "hourly":{"time":[1795395600,1795399200],"temperature_2m":[25.1,null],"relative_humidity_2m":[90,88],"precipitation":[0.2,0],"precipitation_probability":[30,20],"weather_code":[61,3],"wind_speed_10m":[6.5,7.0],"wind_direction_10m":[10,20]},
 "daily":{"time":[1795395600,1795482000],"weather_code":[61,2],"temperature_2m_max":[32.0,31.5],"temperature_2m_min":[24.5,24.9],"precipitation_sum":[4.2,0],"precipitation_probability_max":[60,10],"wind_speed_10m_max":[14,12],"wind_direction_10m_dominant":[45,90]}}`

func TestFetch_ThamSoVaPhanTich(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		_, _ = w.Write([]byte(sample))
	}))
	defer srv.Close()
	p := domain.Point{Lat: decimal.RequireFromString("10.03"), Lon: decimal.RequireFromString("105.78")}
	f, err := Client{BaseURL: srv.URL}.Fetch(context.Background(), p, 14)
	if err != nil {
		t.Fatal(err)
	}
	q := got.URL.Query()
	if got.URL.Path != "/v1/forecast" || q.Get("latitude") != "10.03" || q.Get("longitude") != "105.78" || q.Get("forecast_days") != "14" ||
		q.Get("timezone") != "Asia/Ho_Chi_Minh" || q.Has("apikey") || got.Header.Get("Authorization") != "" || got.Header.Get("Cookie") != "" {
		t.Fatalf("yêu cầu gửi đi: %s %v", got.URL, got.Header)
	}
	if f.Provider != domain.ProviderOpenMeteo || f.Observed || f.Current == nil || *f.Current.TemperatureC != 30.4 || *f.Current.WeatherCode != 2 {
		t.Fatalf("hiện tại: %+v", f.Current)
	}
	if len(f.Hourly) != 2 || f.Hourly[1].TemperatureC != nil || *f.Hourly[0].PrecipitationMM != 0.2 {
		t.Fatalf("theo giờ: %+v", f.Hourly)
	}
	if len(f.Daily) != 2 || f.Daily[0].Day != "2026-11-23" || f.Daily[1].Day != "2026-11-24" || *f.Daily[0].PrecipitationMM != 4.2 ||
		f.Daily[0].HumidityAvg == nil || *f.Daily[0].HumidityAvg != 89 {
		t.Fatalf("theo ngày: %+v", f.Daily)
	}
}

func TestFetch_KhoaAPIVaLoi(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   string
	}{
		{429, `{"error":true,"reason":"Daily API request limit exceeded. Please try again tomorrow."}`, domain.ErrCodeQuota},
		{400, `{"error":true,"reason":"Hourly API request limit exceeded"}`, domain.ErrCodeQuota},
		{401, `{"error":true,"reason":"Invalid API key"}`, domain.ErrCodeAuth},
		{502, `bad gateway`, domain.ErrCodeUnavailable},
		{400, `{"error":true,"reason":"Latitude must be in range"}`, domain.ErrCodeBadResponse},
		{200, `{"hourly":{}}`, domain.ErrCodeBadResponse},
	}
	for _, tc := range cases {
		var key string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key = r.URL.Query().Get("apikey")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := Client{BaseURL: srv.URL, APIKey: "secret-key-1234"}.Fetch(context.Background(), domain.Point{}, 1)
		srv.Close()
		var pe domain.ProviderError
		if !errors.As(err, &pe) || pe.Code != tc.code || key != "secret-key-1234" {
			t.Errorf("%d %s: %v (khóa %q)", tc.status, tc.body, err, key)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Error("lỗi không được chứa khóa API")
		}
	}
	// Mất kết nối.
	if _, err := (Client{BaseURL: "http://127.0.0.1:1"}).Fetch(context.Background(), domain.Point{}, 1); !errors.Is(err, domain.ProviderError{Code: domain.ErrCodeUnavailable}) {
		t.Fatalf("mất kết nối: %v", err)
	}
	if (Client{}).base() != FreeURL || (Client{APIKey: "k"}).base() != CustomerURL {
		t.Fatal("endpoint theo khóa")
	}
}
