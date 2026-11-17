package keycloak

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
)

// LoginConfig là client bí mật của màn đăng nhập riêng (FN-01, ADR-0060): phát token thật qua luồng direct grant có OTP.
type LoginConfig struct {
	ClientID     string
	ClientSecret string `json:"-"`
}

// LoginConfigFromEnv đọc AG_KC_LOGIN_CLIENT_ID, AG_KC_LOGIN_CLIENT_SECRET. Fail-closed: không đặt biến nào → ok=false (đăng nhập tắt); đặt thiếu → lỗi nêu tên biến.
func LoginConfigFromEnv() (lc LoginConfig, ok bool, err error) {
	vars := []struct {
		name string
		dst  *string
	}{
		{"AG_KC_LOGIN_CLIENT_ID", &lc.ClientID},
		{"AG_KC_LOGIN_CLIENT_SECRET", &lc.ClientSecret},
	}
	var missing []string
	for _, v := range vars {
		if *v.dst = os.Getenv(v.name); *v.dst == "" {
			missing = append(missing, v.name)
		}
	}
	switch {
	case len(missing) == len(vars):
		return LoginConfig{}, false, nil
	case len(missing) > 0:
		return LoginConfig{}, false, fmt.Errorf("keycloak: đăng nhập thiếu cấu hình: %s", strings.Join(missing, ", "))
	}
	return lc, true, nil
}

// Login là port.PasswordLogin qua direct grant của Keycloak. An toàn khi dùng đồng thời.
type Login struct {
	lc       LoginConfig
	hc       *Client // dùng chung BaseURL, realm, HTTP client, timeout
	tokenURL string
	logout   string
}

var _ port.PasswordLogin = (*Login)(nil)

// NewLogin dựng Login dùng chung BaseURL, Realm, HTTP client và Timeout với Client.
func NewLogin(cfg Config, lc LoginConfig) (*Login, error) {
	if lc.ClientID == "" || lc.ClientSecret == "" {
		return nil, errors.New("keycloak: thiếu client hoặc bí mật của đăng nhập")
	}
	c, err := New(cfg)
	if err != nil {
		return nil, err
	}
	root := strings.TrimRight(cfg.BaseURL, "/") + "/realms/" + url.PathEscape(cfg.Realm) + "/protocol/openid-connect"
	return &Login{lc: lc, hc: c, tokenURL: root + "/token", logout: root + "/logout"}, nil
}

// post gửi form tới Keycloak bằng Basic auth của client. Thân phản hồi lỗi chỉ được đọc để phân loại, không bao giờ
// đưa vào lỗi hay log (mang mật khẩu, tên đăng nhập — R-14).
func (l *Login) post(ctx context.Context, op, endpoint string, id, secret string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(url.QueryEscape(id), url.QueryEscape(secret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := l.hc.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", port.ErrUnavailable, op)
	}
	return res, nil
}

// grantError phân loại phản hồi không thành công của endpoint token.
func grantError(op string, res *http.Response, invalid error) error {
	var body struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body)
	se := &statusError{op: op, status: res.StatusCode}
	desc := strings.ToLower(body.Description)
	switch {
	case res.StatusCode >= 500 || body.Error == "unauthorized_client" || body.Error == "invalid_client":
		return fmt.Errorf("%w: %w", port.ErrUnavailable, se)
	case body.Error == "invalid_grant" && strings.Contains(desc, "not fully set up"):
		return port.ErrAccountNotSetUp
	case body.Error == "invalid_grant" && strings.Contains(desc, "account disabled"):
		return port.ErrAccountDisabled
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusBadRequest:
		if invalid != nil && (body.Error == "invalid_grant" || res.StatusCode == http.StatusUnauthorized) {
			return invalid
		}
	}
	return fmt.Errorf("%w: %w", port.ErrUnavailable, se)
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
}

func (l *Login) decodeTokens(res *http.Response, op string) (port.TokenSet, error) {
	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tr); err != nil || tr.AccessToken == "" {
		return port.TokenSet{}, fmt.Errorf("%w: %s: phản hồi không đọc được", port.ErrUnavailable, op)
	}
	ts := port.TokenSet{
		AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, ExpiresIn: tr.ExpiresIn, RefreshExpiresIn: tr.RefreshExpiresIn,
	}
	ts.Roles, ts.AMR, ts.SessionID = tokenClaims(tr.AccessToken)
	return ts, nil
}

// tokenClaims đọc realm_access.roles, amr và phiên Keycloak (sid, dự phòng session_state) từ phần payload của access
// token. Token đến thẳng từ Keycloak qua kênh nội bộ nên không cần kiểm chữ ký; payload hỏng → rỗng (kiểm lại sẽ coi là
// thiếu OTP, không ghi được phiên đã qua OTP email).
func tokenClaims(access string) (roles, amr []string, sid string) {
	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		return nil, nil, ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil, ""
	}
	var claims struct {
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
		AMR          []string `json:"amr"`
		SID          string   `json:"sid"`
		SessionState string   `json:"session_state"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return nil, nil, ""
	}
	sid = claims.SID
	if sid == "" {
		sid = claims.SessionState
	}
	return claims.RealmAccess.Roles, claims.AMR, sid
}

// PasswordGrant thực hiện port.PasswordLogin.
func (l *Login) PasswordGrant(ctx context.Context, username, password, otp string) (port.TokenSet, error) {
	form := url.Values{"grant_type": {"password"}, "username": {username}, "password": {password}, "scope": {"openid"}}
	if otp != "" {
		form.Set("totp", otp)
	}
	res, err := l.post(ctx, "đăng nhập", l.tokenURL, l.lc.ClientID, l.lc.ClientSecret, form)
	if err != nil {
		return port.TokenSet{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return port.TokenSet{}, grantError("đăng nhập", res, port.ErrInvalidGrant)
	}
	return l.decodeTokens(res, "đăng nhập")
}

// Refresh thực hiện port.PasswordLogin.
func (l *Login) Refresh(ctx context.Context, refresh string) (port.TokenSet, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}
	res, err := l.post(ctx, "làm mới token", l.tokenURL, l.lc.ClientID, l.lc.ClientSecret, form)
	if err != nil {
		return port.TokenSet{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return port.TokenSet{}, grantError("làm mới token", res, port.ErrSessionEnded)
	}
	return l.decodeTokens(res, "làm mới token")
}

// EndSession thực hiện port.PasswordLogin: phiên đã hết (400 invalid_grant) coi như thành công.
func (l *Login) EndSession(ctx context.Context, refresh string) error {
	res, err := l.post(ctx, "đăng xuất", l.logout, l.lc.ClientID, l.lc.ClientSecret, url.Values{"refresh_token": {refresh}})
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return nil
	case res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnauthorized:
		if err := grantError("đăng xuất", res, port.ErrSessionEnded); errors.Is(err, port.ErrSessionEnded) {
			return nil
		}
	}
	return fmt.Errorf("%w: %w", port.ErrUnavailable, &statusError{op: "đăng xuất", status: res.StatusCode})
}
