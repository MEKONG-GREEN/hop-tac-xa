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
	Shared         bool      `json:"shared"`
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
	var scope HouseholdIdentityScope
	if err := c.readObject(ctx, path, &scope); err != nil {
		return HouseholdIdentityScope{}, err
	}
	if scope.Ref != ref || (scope.ContactAccess != "full" && scope.ContactAccess != "masked") || (scope.CanWrite && scope.ContactAccess != "full") ||
		(registration && (scope.CooperativeRef != ref || scope.ContactAccess != "full" || scope.CanWrite != scope.Active)) {
		return HouseholdIdentityScope{}, ProblemHouseholdScopeUnavailable
	}
	return scope, nil
}

func (c *householdScopeHTTP) HouseholdCommandScope(ctx context.Context, command uuid.UUID) (HouseholdCommandScope, error) {
	var scope HouseholdCommandScope
	if command == uuid.Nil {
		return scope, ProblemNotFound
	}
	if err := c.readObject(ctx, "/v1/household-commands/"+command.String()+"/identity-scope", &scope); err != nil {
		return scope, err
	}
	if scope.CommandRef != command || scope.HouseholdRef == uuid.Nil || scope.CooperativeRef == uuid.Nil || scope.CommuneCode == "" ||
		(scope.Operation != "create" && scope.Operation != "update") || (scope.Operation == "create" && scope.ExpectedRevision != 0) || (scope.Operation == "update" && scope.ExpectedRevision < 1) {
		return HouseholdCommandScope{}, ProblemHouseholdScopeUnavailable
	}
	return scope, nil
}

func (c *householdScopeHTTP) readObject(ctx context.Context, path string, out any) error {
	raw, ok := authz.RawToken(ctx)
	if !ok {
		return problem.Unauthenticated.WithCause(ErrNoToken)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return ProblemHouseholdScopeUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	forwardWorkingRole(ctx, req)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	resp, err := c.hc.Do(req)
	if err != nil {
		return ProblemHouseholdScopeUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return problem.Unauthenticated
	case http.StatusForbidden:
		return problem.Forbidden
	case http.StatusNotFound:
		return ProblemNotFound
	case http.StatusOK:
	default:
		return ProblemHouseholdScopeUnavailable
	}
	const maxScope = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScope+1))
	if err != nil || len(body) > maxScope {
		return ProblemHouseholdScopeUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return ProblemHouseholdScopeUnavailable
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ProblemHouseholdScopeUnavailable
	}
	return nil
}
