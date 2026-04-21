package config

import (
	_ "embed"

	"github.com/BurntSushi/toml"
)

//go:embed defaults.toml
var defaultsTOML string

// DefaultTOML returns the embedded default TOML document as a string.
func DefaultTOML() string {
	return defaultsTOML
}

// DefaultConfig returns a fully populated Config decoded from the embedded
// defaults.toml. It panics if the embedded file is malformed, which would
// indicate a build-time bug.
func DefaultConfig() *Config {
	var c Config
	if _, err := toml.Decode(defaultsTOML, &c); err != nil {
		panic("config: embedded defaults.toml is invalid: " + err.Error())
	}
	return &c
}
