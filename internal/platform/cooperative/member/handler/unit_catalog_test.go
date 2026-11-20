package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
)

type fakeUnitCatalog struct{ party *fakeParty }

func (u fakeUnitCatalog) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	u.party.mu.Lock()
	defer u.party.mu.Unlock()
	unit, ok := u.party.units[code]
	if !ok {
		return partyport.AdminUnit{}, partyport.ErrUnitNotFound
	}
	return unit, nil
}
func (u fakeUnitCatalog) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	u.party.mu.Lock()
	defer u.party.mu.Unlock()
	codes := []string{}
	for _, unit := range u.party.units {
		if unit.Code == code || unit.ParentCode == code {
			codes = append(codes, unit.Code)
		}
	}
	return codes, nil
}
func (u fakeUnitCatalog) Current(ctx context.Context, code string, at time.Time) ([]string, error) {
	if _, err := u.Get(ctx, code, at); err != nil {
		return nil, err
	}
	return []string{code}, nil
}

func TestAH271_HouseholdCannotChooseDisabledCommune(t *testing.T) {
	f := newFixture(t)
	tree := f.seedTree(f.root())
	manager := f.as(f.tenant, tree.c1, partyport.RoleCoopManager)
	f.party.mu.Lock()
	unit := f.party.units["30001"]
	unit.Disabled = true
	f.party.units["30001"] = unit
	f.party.mu.Unlock()
	result := manager.appDo(http.MethodPost, "/v1/households", farmer(tree.c1, "0907000099"))
	if result.status != http.StatusUnprocessableEntity {
		t.Fatalf("disabled commune: %d %v", result.status, result.body)
	}
	f.svc.Units = nil
	result = manager.appDo(http.MethodPost, "/v1/households", farmer(tree.c1, "0907000098"))
	if result.status != http.StatusServiceUnavailable {
		t.Fatalf("missing catalog must fail closed: %d %v", result.status, result.body)
	}
}
