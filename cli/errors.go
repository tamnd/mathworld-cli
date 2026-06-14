package cli

// isNotFound reports whether err indicates a resource was not found.
// MathWorld returns empty result sets rather than 404s, so this is
// mainly a hook for consistent error mapping across commands.
func isNotFound(err error) bool {
	return err != nil && err.Error() == "not found"
}
