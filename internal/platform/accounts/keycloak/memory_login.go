package keycloak

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
)

var _ port.PasswordLogin = (*Memory)(nil)

// memTokenLife là hạn access token giả lập.
const memTokenLife = 5 * time.Minute

// SetOTPCode đặt mã TOTP đúng của người dùng (cùng SetOTP(id, true)); client chính phải gửi đúng mã này.
func (m *Memory) SetOTPCode(id uuid.UUID, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		u.otpCode = code
	}
}

// LagFailureVisibility mô phỏng Keycloak ghi nhận lần sai chậm một nhịp: lần sai chỉ hiện ở LoginLockStatus sau d
// (theo đồng hồ m.now). Khóa thật (từ chối đăng nhập) có hiệu lực ngay. d = 0 → hiện ngay.
func (m *Memory) LagFailureVisibility(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failLag = d
}

// ActiveLoginSessions trả số phiên direct grant còn mở của người dùng (kiểm thử).
func (m *Memory) ActiveLoginSessions(id uuid.UUID) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, ls := range m.loginSessions {
		if ls.user == id {
			n++
		}
	}
	return n
}

// PasswordGrants trả số lần mật khẩu của người dùng thật sự được kiểm (không tính lần bị chặn vì đang khóa tạm).
func (m *Memory) PasswordGrants(id uuid.UUID) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.grants[id]
}

func (m *Memory) byLoginName(username string) (uuid.UUID, *memUser) {
	for id, u := range m.users {
		if fold(u.u.Username) == fold(username) {
			return id, u
		}
	}
	return uuid.Nil, nil
}

func (m *Memory) recordFailure(u *memUser) {
	m.flush(u)
	u.probe = u.probe.Fail(m.now())
	if m.failLag <= 0 {
		u.shown, u.pending = u.probe, nil
		return
	}
	p := u.probe
	u.pending, u.pendingAt = &p, m.now().Add(m.failLag)
}

func b64(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// jwt dựng token giả KHÔNG ký (chỉ kiểm thử; tokenClaims không kiểm chữ ký).
func (m *Memory) jwt(u *memUser, c port.LoginClient) (access string, roles, amr []string) {
	claims := map[string]any{"sub": u.u.ID.String(), "iat": m.now().Unix(), "exp": m.now().Add(memTokenLife).Unix()}
	if c == port.LoginMain {
		roles = slices.Clone(u.roles)
		if port.NeedsLoginTOTP(roles) {
			roles = append(roles, port.RoleLoginTOTP)
		}
		amr = []string{"pwd"}
		if u.otp {
			amr = append(amr, "otp")
		}
		claims["realm_access"] = map[string]any{"roles": roles}
		claims["amr"] = amr
	} else {
		amr = []string{"pwd"} // client kiểm: chỉ biết mật khẩu đúng, không vai, không aud
		claims["amr"] = amr
	}
	return b64(map[string]string{"alg": "none", "typ": "JWT"}) + "." + b64(claims) + ".", roles, amr
}

func (m *Memory) issue(id uuid.UUID, u *memUser, c port.LoginClient, refresh string) port.TokenSet {
	access, roles, amr := m.jwt(u, c)
	if refresh == "" {
		refresh = uuid.NewString() + uuid.NewString()
		m.loginSessions[refresh] = loginSession{user: id, client: c}
	}
	return port.TokenSet{AccessToken: access, RefreshToken: refresh, ExpiresIn: int(memTokenLife.Seconds()),
		RefreshExpiresIn: int((10 * time.Hour).Seconds()), Roles: roles, AMR: amr}
}

// PasswordGrant thực hiện port.PasswordLogin như direct grant của Keycloak: tài khoản tắt → ErrAccountDisabled;
// đang khóa tạm → ErrInvalidGrant mà không kiểm mật khẩu; sai mật khẩu hoặc (client chính, đã cấu hình OTP) sai/thiếu
// mã → ErrInvalidGrant và tính một lần sai; đúng mà còn required action → ErrAccountNotSetUp; đúng → xóa bộ đếm.
func (m *Memory) PasswordGrant(_ context.Context, c port.LoginClient, username, password, otp string) (port.TokenSet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("PasswordGrant"); err != nil {
		return port.TokenSet{}, err
	}
	id, u := m.byLoginName(username)
	if u == nil {
		return port.TokenSet{}, port.ErrInvalidGrant
	}
	if !u.u.Enabled {
		return port.TokenSet{}, port.ErrAccountDisabled
	}
	if locked, _ := u.probe.Locked(m.now()); locked {
		return port.TokenSet{}, port.ErrInvalidGrant
	}
	m.grants[id]++
	sum := sha256.Sum256([]byte(password))
	want, ok := m.passwords[id]
	good := ok && subtle.ConstantTimeCompare(sum[:], want[:]) == 1
	if good && c == port.LoginMain && u.otp {
		good = u.otpCode != "" && subtle.ConstantTimeCompare([]byte(otp), []byte(u.otpCode)) == 1
	}
	if !good {
		m.recordFailure(u)
		return port.TokenSet{}, port.ErrInvalidGrant
	}
	if len(u.u.RequiredActions) > 0 {
		return port.TokenSet{}, port.ErrAccountNotSetUp
	}
	u.probe, u.shown, u.pending = domain.LoginProbe{}, domain.LoginProbe{}, nil
	return m.issue(id, u, c, ""), nil
}

// Refresh thực hiện port.PasswordLogin.
func (m *Memory) Refresh(_ context.Context, refresh string) (port.TokenSet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("Refresh"); err != nil {
		return port.TokenSet{}, err
	}
	ls, ok := m.loginSessions[refresh]
	if !ok || ls.client != port.LoginMain {
		return port.TokenSet{}, port.ErrSessionEnded
	}
	u, err := m.get(ls.user)
	if err != nil || !u.u.Enabled {
		delete(m.loginSessions, refresh)
		return port.TokenSet{}, port.ErrSessionEnded
	}
	return m.issue(ls.user, u, ls.client, refresh), nil
}

// EndSession thực hiện port.PasswordLogin: phiên không còn coi như đã kết thúc.
func (m *Memory) EndSession(_ context.Context, c port.LoginClient, refresh string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("EndSession"); err != nil {
		return err
	}
	if ls, ok := m.loginSessions[refresh]; ok && ls.client == c {
		delete(m.loginSessions, refresh)
	}
	return nil
}
