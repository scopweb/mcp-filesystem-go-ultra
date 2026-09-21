//go:build !unix && !windows

package cache

import "os"

func identityFromInfo(os.FileInfo) (FileIdentity, bool) {
	return FileIdentity{}, false
}

func identityFromFile(*os.File) (FileIdentity, bool) {
	return FileIdentity{}, false
}
