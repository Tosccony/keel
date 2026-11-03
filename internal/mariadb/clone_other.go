//go:build !darwin && !linux

package mariadb

import "os"

func cloneTree(_, _ string) error {
	return errNoClone
}

func cloneFile(_, _ *os.File) error {
	return errNoClone
}
