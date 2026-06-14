//go:build !windows

package main

func validateRuntimeLoadable(string) error {
	return nil
}
