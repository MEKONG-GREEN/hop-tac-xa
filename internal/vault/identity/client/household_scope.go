package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/google/uuid"
)

// HouseholdIdentityScope is owner metadata, never an identity payload. The
// owner verifies the original user's membership for every request.
type HouseholdIdentityScope struct {
	Ref            uuid.UUID `json:"ref"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	Active         bool      `json:"active"`
	ContactAccess  string    `json:"contact_access"`
	CanWrite       bool      `json:"can_write"`
}

type HouseholdScopeVerifier interface {
	HouseholdIdentityScope(context.Context, uuid.UUID) (HouseholdIdentityScope, error)
	HouseholdRegistrationScope(context.Context, uuid.UUID) (HouseholdIdentityScope, error)
}

var ProblemHouseholdScopeUnavailable = problem.New(http.StatusServiceUnavailable, "vault.household_scope_unavailable", "Chưa xác minh được phạm vi định danh hộ")

type householdScopeHTTP struct {
	base string
	hc   *http.Client
}

// NewHouseholdScopeHTTP forwards the original access token to the configured
// platform API. Redirects are always refused so credentials cannot leave that
// endpoint. The injected transport may provide the deployment's mTLS policy.
func NewHouseholdScopeHTTP(baseURL string, hc *http.Client) (HouseholdScopeVerifier, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("vault: URL xác minh phạm vi hộ không hợp lệ")
	}
	cloned := http.Client{Timeout: 10 * time.Second}
	if hc != nil {
		cloned = *hc
		if cloned.Timeout == 0 {
			cloned.Timeout = 10 * time.Second
		}
	}
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &householdScopeHTTP{base: strings.TrimRight(u.String(), "/"), hc: &cloned}, nil
}

func (c *householdScopeHTTP) HouseholdIdentityScope(ctx context.Context, ref uuid.UUID) (HouseholdIdentityScope, error) {
	return c.read(ctx, ref, "/v1/households/"+ref.String()+"/identity-scope", false)
}

func (c *householdScopeHTTP) HouseholdRegistrationScope(ctx context.Context, ref uuid.UUID) (HouseholdIdentityScope, error) {
	return c.read(ctx, ref, "/v1/cooperatives/"+ref.String()+"/household-identity-scope", true)
}

func (c *householdScopeHTTP) read(ctx context.Context, ref uuid.UUID, path string, registration bool) (HouseholdIdentityScope, error) {
	if ref == uuid.Nil {
		return HouseholdIdentityScope{}, ProblemNotFound
	}
	raw, ok := authz.RawToken(ctx)
	if !ok {
		return HouseholdIdentityScope{}, problem.Unauthenticated.WithCause(ErrNoToken)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return HouseholdIdentityScope{}, ProblemHouseholdScopeUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	resp, err := c.hc.Do(req)
	if err != nil {
		return HouseholdIdentityScope{}, ProblemHouseholdScopeUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return HouseholdIdentityScope{}, problem.Unauthenticated
	case http.StatusForbidden:
		return HouseholdIdentityScope{}, problem.Forbidden
	case http.StatusNotFound:
		return HouseholdIdentityScope{}, ProblemNotFound
	case http.StatusOK:
	default:
		return HouseholdIdentityScope{}, ProblemHouseholdScopeUnavailable
	}
	const maxScope = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScope+1))
	if err != nil || len(body) > maxScope {
		return HouseholdIdentityScope{}, ProblemHouseholdScopeUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var scope HouseholdIdentityScope
	if err := decoder.Decode(&scope); err != nil {
		return HouseholdIdentityScope{}, ProblemHouseholdScopeUnavailable
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF || scope.Ref != ref ||
		(scope.ContactAccess != "full" && scope.ContactAccess != "masked") || (scope.CanWrite && scope.ContactAccess != "full") ||
		(registration && (scope.CooperativeRef != ref || scope.ContactAccess != "full" || scope.CanWrite != scope.Active)) {
		return HouseholdIdentityScope{}, ProblemHouseholdScopeUnavailable
	}
	return scope, nil
}
