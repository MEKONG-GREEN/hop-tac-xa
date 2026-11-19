package store

const unscopedDelete = "DELETE FROM pf_party.org WHERE ref=$1 AND kind='cooperative'" // want "ADR-0011"

func RemoveCooperativeProjection() string {
	_ = "DELETE FROM pf_accounts.account" // want "ADR-0011"
	return "DELETE FROM pf_party.org WHERE ref=$1 AND kind='cooperative'"
}

func DeleteUsedOrganization() string {
	return "DELETE FROM pf_party.org WHERE ref=$1" // want "ADR-0011"
}
