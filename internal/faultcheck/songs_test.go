//go:build faults

package faultcheck

func OwnerRotation(count int, ownerIDs ...string) []string {
	rotation := make([]string, count)
	if len(ownerIDs) == 0 {
		return rotation
	}
	for index := range rotation {
		rotation[index] = ownerIDs[index%len(ownerIDs)]
	}
	return rotation
}
