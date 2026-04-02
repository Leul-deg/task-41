package ux

import "fmt"

func ConflictResolveURL(ticketID string) string {
	return fmt.Sprintf("/rpc/api/support/tickets/%s/conflict-resolve", ticketID)
}

func CanAccess(perms map[string]map[string]bool, module, action string) bool {
	if perms == nil {
		return false
	}
	if _, ok := perms[module]; !ok {
		return false
	}
	return perms[module][action]
}

func ModuleVisible(perms map[string]map[string]bool, module string) bool {
	return CanAccess(perms, module, "view") || CanAccess(perms, module, "create")
}
