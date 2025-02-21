// Code generated by internal/rbac/yamldata. DO NOT EDIT.
package types

// A PermissionNamespace represents a distinct context within which permission policies
// are defined and enforced.
type PermissionNamespace string

func (n PermissionNamespace) String() string {
	return string(n)
}

const BatchChangesNamespace PermissionNamespace = "BATCH_CHANGES"

// Valid checks if a namespace is valid and supported by Sourcegraph's RBAC system.
func (n PermissionNamespace) Valid() bool {
	switch n {
	case BatchChangesNamespace:
		return true
	default:
		return false
	}
}
