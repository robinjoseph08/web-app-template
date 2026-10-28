package version

// Info is the body of GET /api/version.
type Info struct {
	Version string `json:"version"`
}
