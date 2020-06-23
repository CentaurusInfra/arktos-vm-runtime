// Copyright 2016 Ayke van Laethem.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.txt file.

package osfs

import (
	"errors"
	"os"
)

var errUnsupported = errors.New("osfs: Windows is not supported")

func defaultFilesystem() Filesystem {
	// Windows doesn't support much POSIX features (though there should be a way
	// to get unique inodes on NTFS).
	return Filesystem{}
}

func Read() (*Info, error) {
	return &Info{}, errUnsupported
}

func (info *Info) GetReal(path string, fi os.FileInfo) *MountPoint {
	return nil
}

func (p *MountPoint) DevNumber() (uint64, bool) {
	// Not yet implemented.
	return 0, false
}
