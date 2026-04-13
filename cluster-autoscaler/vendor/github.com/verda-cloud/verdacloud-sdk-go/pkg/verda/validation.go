package verda

import "strings"

// isLatestTag checks if a container image uses the "latest" tag
// The API does not allow "latest" tag - a specific version must be used
func isLatestTag(image string) bool {
	// Check for explicit :latest tag
	if strings.HasSuffix(image, ":latest") {
		return true
	}

	// Check for image without any tag (defaults to latest)
	// e.g., "nginx" without ":tag" would default to "nginx:latest"
	if !strings.Contains(image, ":") && !strings.Contains(image, "@") {
		// Image has no tag and no digest - would default to latest
		return true
	}

	return false
}
