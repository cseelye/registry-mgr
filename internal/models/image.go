package models

import "time"

// Image represents a single tagged image in the registry.
type Image struct {
	Repository string
	Tag        string
	Digest     string
	Size       int64
	CreatedAt  time.Time
	OS         string
	Arch       string
	Labels     map[string]string
}
