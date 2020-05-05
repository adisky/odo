package data

// DevfileData is an interface that defines functions for Devfile data operations
type DevfileData interface {
	GetVersions() string
}

func (v V100) GetVersions() string {
	return "v1.0.0"
}

func (v V200) GetVersions() string {
	return "v2.0.0"
}
