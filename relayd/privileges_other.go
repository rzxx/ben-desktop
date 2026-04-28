//go:build !linux

package main

func prepareProcessPrivileges(relaydOptions) error {
	return nil
}
