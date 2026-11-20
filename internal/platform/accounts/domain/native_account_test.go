package domain

import (
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/google/uuid"
)

func TestNativeAccountFN07(t *testing.T) {
	valid := func() NativeAccountInput {
		return NativeAccountInput{Username: " Dai.Dien ", FullName: " Nguyễn  Văn An ", Phone: "0912345678", CooperativeRef: uuid.New(), Roles: []string{htxroles.Member, htxroles.Representative}}
	}
	in, err := valid().Normalize()
	if err != nil || in.Username != "dai.dien" || in.FullName != "Nguyễn Văn An" || len(in.Roles) != 2 {
		t.Fatalf("normalize: %+v %v", in, err)
	}
	for name, tc := range map[string]struct {
		mutate func(*NativeAccountInput)
		field  string
	}{
		"missing cooperative":       {func(in *NativeAccountInput) { in.CooperativeRef = uuid.Nil }, "cooperative_ref"},
		"missing name":              {func(in *NativeAccountInput) { in.FullName = " " }, "full_name"},
		"missing phone":             {func(in *NativeAccountInput) { in.Phone = "" }, "phone"},
		"short phone":               {func(in *NativeAccountInput) { in.Phone = "091234567" }, "phone"},
		"international phone":       {func(in *NativeAccountInput) { in.Phone = "+84912345678" }, "phone"},
		"non ASCII phone":           {func(in *NativeAccountInput) { in.Phone = "０９１２３４５６７８" }, "phone"},
		"KN role":                   {func(in *NativeAccountInput) { in.Roles = []string{"kn_user"} }, "roles"},
		"admin plus representative": {func(in *NativeAccountInput) { in.Roles = []string{htxroles.Admin, htxroles.Representative} }, "roles"},
		"all roles": {func(in *NativeAccountInput) {
			in.Roles = []string{htxroles.Admin, htxroles.Representative, htxroles.Member}
		}, "roles"},
		"duplicate role":          {func(in *NativeAccountInput) { in.Roles = []string{htxroles.Member, htxroles.Member} }, "roles"},
		"admin cooperative":       {func(in *NativeAccountInput) { in.Roles = []string{htxroles.Admin}; in.Email = "admin@example.vn" }, "cooperative_ref"},
		"admin missing OTP email": {func(in *NativeAccountInput) { in.Roles = []string{htxroles.Admin}; in.CooperativeRef = uuid.Nil }, "email"},
	} {
		t.Run(name, func(t *testing.T) {
			in := valid()
			tc.mutate(&in)
			_, err := in.Normalize()
			var field FieldError
			if !errors.As(err, &field) || field.Field != tc.field {
				t.Fatalf("want %s, got %v", tc.field, err)
			}
		})
	}
	for _, role := range []string{htxroles.Admin, htxroles.Representative, htxroles.Member} {
		in := valid()
		in.Roles = []string{role}
		if role == htxroles.Admin {
			in.CooperativeRef = uuid.Nil
			in.Email = "admin@example.vn"
		}
		if _, err := in.Normalize(); err != nil {
			t.Fatalf("single %s: %v", role, err)
		}
	}
}
