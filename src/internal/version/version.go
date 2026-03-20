package version

import "fmt"

var (
	version   = "n/a"
	buildDate = "n/a"
	commit    = "n/a"
)

// Version returns the semantic version of the build.
func Version() string {
	return version
}

// BuildDate returns the UTC timestamp when the build was created.
func BuildDate() string {
	return buildDate
}

// Commit returns the short git commit hash of the build.
func Commit() string {
	return commit
}

// Info returns all version information as a struct for structured responses.
type Info struct {
	Version   string `json:"version"`
	BuildDate string `json:"build_date"`
	Commit    string `json:"commit"`
}

// GetInfo returns all version information in a structured format.
func GetInfo() Info {
	return Info{
		Version:   version,
		BuildDate: buildDate,
		Commit:    commit,
	}
}

// Print returns a formatted ASCII art banner with version information
// suitable for CLI display.
func Print() string {
	const mnemonic = `
  __  __                                  _
 |  \/  |                                (_)
 | \  / |_ __   ___ _ __ ___   ___  _ __  _  ___
 | |\/| | '_ \ / _ \ '_ ' _ \ / _ \| '_ \| |/ __|
 | |  | | | | |  __/ | | | | | (_) | | | | | (__
 |_|  |_|_| |_|\___|_| |_| |_|\___/|_| |_|_|\___|`

	return fmt.Sprintf("%s\n                                   version %s\n", mnemonic, Version())
}
