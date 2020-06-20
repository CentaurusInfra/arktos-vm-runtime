// Copyright 2016 Ayke van Laethem.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.txt file.

// +build !linux,!windows

package osfs

import (
	"errors"
	"os"
)

var errUnsupported = errors.New("osfs: unsupported operating system")

func defaultFilesystem() Filesystem {
	// Assume a POSIX system.
	return Filesystem{
		Permissions: 0777,
		Symlink:     true,
		Hardlink:    true,
		Inode:       true,
	}
}

func Read() (*Info, error) {
	return &Info{}, errUnsupported
}

func (info *Info) GetReal(path string, fi os.FileInfo) *MountPoint {
	return nil
}

func (p *MountPoint) DevNumber() (uint64, bool) {
	return 0, false
}
