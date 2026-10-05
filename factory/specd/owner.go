package specd

import "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/procowner"

// claimOwner stamps this process as the driver of a status patch.
func claimOwner(status map[string]any, owner procowner.Owner) map[string]any {
	for field, value := range owner.Fields() {
		status[field] = value
	}
	return status
}

// clearOwner removes the driver from a status patch once the work settles.
func clearOwner(status map[string]any) map[string]any {
	for field, value := range procowner.Cleared() {
		status[field] = value
	}
	return status
}
