//go:build !windows

package netmap

import "os"

func readShared(name string) ([]byte, error) { return os.ReadFile(name) }
