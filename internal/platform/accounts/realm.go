package accounts

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

// NativeHTXKeyFromEnv is enabled only for the dedicated HTX deployment. The
// operation key must remain stable for durable retries.
func NativeHTXKeyFromEnv(getenv func(string) string) ([]byte, error) {
	if getenv("AG_KC_REALM") != "agrigreen-htx" {
		return nil, nil
	}
	if err := ValidateHTXRealmBinding(getenv); err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(getenv("AG_HTX_ACCOUNTS_OPERATION_KEY"))
	if err != nil || len(key) != 32 {
		return nil, errors.New("accounts: AG_HTX_ACCOUNTS_OPERATION_KEY phải là base64 của 32 byte")
	}
	return key, nil
}

// ValidateHTXRealmBinding prevents a dedicated HTX API from provisioning users
// in KN or validating KN tokens. Each deployment still trusts exactly one issuer.
func ValidateHTXRealmBinding(getenv func(string) string) error {
	const realm = "agrigreen-htx"
	u, err := url.Parse(getenv("AG_OIDC_ISSUER"))
	nativeIssuer := err == nil && strings.TrimSuffix(u.Path, "/") == "/realms/"+realm
	if getenv("AG_KC_REALM") != realm && !nativeIssuer {
		return nil
	}
	if !nativeIssuer || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || getenv("AG_KC_REALM") != realm {
		return errors.New("accounts: realm HTX yêu cầu AG_OIDC_ISSUER và AG_KC_REALM cùng agrigreen-htx")
	}
	if getenv("AG_OIDC_AUDIENCE") != "agrigreen-htx-api" || getenv("AG_KC_CLIENT_ID") != "agrigreen-htx-accounts" {
		return errors.New("accounts: realm HTX yêu cầu audience agrigreen-htx-api và client agrigreen-htx-accounts")
	}
	if getenv("AG_KC_LOGIN_CLIENT_ID") != "agrigreen-htx-login" || getenv("AG_KC_ACTIONS_CLIENT_ID") != "agrigreen-htx-web" {
		return errors.New("accounts: realm HTX yêu cầu client đăng nhập và hoàn tất thiết lập đúng HTX")
	}
	if jwks := getenv("AG_OIDC_JWKS_URL"); jwks != "" {
		keyURL, err := url.Parse(jwks)
		if err != nil || keyURL.Path != "/realms/"+realm+"/protocol/openid-connect/certs" || keyURL.Host == "" || keyURL.User != nil || keyURL.RawQuery != "" || keyURL.Fragment != "" || (keyURL.Scheme != "https" && keyURL.Scheme != "http") {
			return errors.New("accounts: JWKS phải thuộc realm agrigreen-htx")
		}
	}
	return nil
}
