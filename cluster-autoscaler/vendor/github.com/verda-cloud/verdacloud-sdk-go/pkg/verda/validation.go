// validation.go contains shared validation helpers used across multiple service
// type files (e.g. container_deployments_types.go, serverless_jobs_types.go).
//
// Service-specific validation logic (Validate methods, validateCreate* functions)
// lives in the corresponding *_types.go file. Only put helpers here when they
// are reused by more than one service domain.

package verda

import "strings"

// IsLatestTag checks if a container image uses the "latest" tag.
// The API does not allow "latest" tag - a specific version must be used.
func IsLatestTag(image string) bool {
	if strings.HasSuffix(image, ":latest") {
		return true
	}
	if !strings.Contains(image, ":") && !strings.Contains(image, "@") {
		return true
	}
	return false
}
