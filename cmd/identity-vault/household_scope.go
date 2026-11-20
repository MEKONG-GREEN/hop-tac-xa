package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
)

// A missing URL leaves only the narrow household API disabled. No startup
// request is made, avoiding a platform/Vault dependency cycle.
func householdScopesFromEnv(get func(string) string) (client.HouseholdScopeVerifier, error) {
	base := strings.TrimSpace(get("AG_VAULT_HOUSEHOLD_SCOPE_URL"))
	cert, key, ca := get("AG_PLATFORM_CLIENT_CERT"), get("AG_PLATFORM_CLIENT_KEY"), get("AG_PLATFORM_CA")
	if base == "" {
		if cert != "" || key != "" || ca != "" {
			return nil, errors.New("thiếu AG_VAULT_HOUSEHOLD_SCOPE_URL cho TLS xác minh phạm vi hộ")
		}
		return nil, nil
	}
	hc := &http.Client{Timeout: 5 * time.Second}
	if cert != "" || key != "" || ca != "" {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if cert != "" || key != "" {
			pair, err := tls.LoadX509KeyPair(cert, key)
			if err != nil {
				return nil, errors.New("chứng thư client xác minh phạm vi hộ không hợp lệ")
			}
			cfg.Certificates = []tls.Certificate{pair}
		}
		if ca != "" {
			pem, err := os.ReadFile(ca) //nolint:gosec // operator-supplied CA path
			if err != nil {
				return nil, errors.New("không đọc được CA xác minh phạm vi hộ")
			}
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(pem) {
				return nil, errors.New("CA xác minh phạm vi hộ không hợp lệ")
			}
			cfg.RootCAs = roots
		}
		hc.Transport = &http.Transport{TLSClientConfig: cfg}
	}
	return client.NewHouseholdScopeHTTP(base, hc)
}
