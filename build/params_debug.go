//go:build debug
// +build debug

package build

func init() {
	InsecurePoStValidation = false
	BuildType |= BuildDebug
}

// NOTE: Also includes settings from params_2k
