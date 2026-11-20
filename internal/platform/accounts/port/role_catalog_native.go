package port

import "slices"

// HTXRoleCatalog is dedicated-realm FN-07 policy, including Admin-to-Admin management.
func HTXRoleCatalog() *RoleCatalog {
	roles := []string{"platform_admin", "coop_manager", "htx_member"}
	return &RoleCatalog{valid: true, managed: roles, mfa: []string{"platform_admin"}, orgKinds: map[string][]string{"coop_manager": {"cooperative"}, "htx_member": {"cooperative"}}, grants: map[string][]string{"platform_admin": slices.Clone(roles)}, adminRoles: []string{"platform_admin"}}
}

// NativeRoleCatalog snapshots account policy without application-owned roles.
// Applications may explicitly extend it with their trusted registrations. It is
// an opt-in base; omitted module configuration still uses LegacyRoleCatalog.
func NativeRoleCatalog() *RoleCatalog {
	managed := []string{
		RoleAccountAdmin, RoleAccountAdminProvince,
		"param_setter", "param_approver", "masterdata_admin", "registry_admin", "reports_admin",
		"consent_writer", "consent_reader", "coop_manager", "htx_member",
		"cgh_viewer", "cgh_admin",
		"plot_editor", "plot_viewer", "geo_admin",
		"content_editor_national", "content_editor_province", "content_reviewer",
		"mrv_collector", "mrv_reviewer", "mrv_viewer", "mrv_exporter",
		"device_admin", "sync_conflict_resolver",
		"vault_registrar", "vault_display", "vault_directory", "vault_evidence_viewer", "vault_eraser", "vault_pubkey_sync",
	}
	// Select native keys, retaining their existing organization-kind policy.
	// Copy values so extending a catalog cannot mutate the compatibility bridge.
	kinds := make(map[string][]string)
	for _, role := range []string{"coop_manager", "htx_member", RoleAccountAdminProvince, "cgh_viewer", "content_editor_province"} {
		kinds[role] = slices.Clone(RoleOrgKinds[role])
	}
	return &RoleCatalog{
		valid: true, managed: managed,
		// ADR-0061: vai đăng nhập bằng OTP email (Admin) không đăng ký TOTP.
		mfa: slices.Clone(MFARoles), loginTOTP: without(MFARoles, EmailOTPRoles...),
		orgKinds: kinds,
		grants: map[string][]string{
			"platform_admin":         slices.Clone(managed),
			RoleAccountAdmin:         without(managed, RoleAccountAdmin, "param_approver", "vault_eraser", "htx_member"),
			RoleAccountAdminProvince: {"coop_manager"},
		},
		adminRoles: slices.Clone(AdminRoles),
	}
}
