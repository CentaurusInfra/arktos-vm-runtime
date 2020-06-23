// Copyright 2016 Ayke van Laethem.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.txt file.

package osfs

import (
	"bytes"
	"testing"
)

func TestReadLiveLinux(t *testing.T) {
	return
	t.Parallel()
	mounts, err := Read()
	if err != nil {
		t.Error("could not read mount points:", err)
	}

	// This only works if /bin is not a separate filesystem
	mount, err := mounts.GetPath("/bin/sh")
	if err != nil {
		t.Fatal("could not stat /bin/sh:", err)
	}
	if mount == nil {
		t.Error("could not find mount point for /bin/sh")
	} else if mount.Root != "/" {
		if mount.Root == "/bin" {
			t.Error("unexpected: /bin is a separate filesystem?")
		} else {
			t.Error("expected /bin/sh to be mounted on /")
		}
	}

	fs := mount.Filesystem()
	if fs != defaultFilesystem() {
		t.Errorf("/bin/sh on a non-default filesystem (this may not be an issue if %#v is not a POSIX filesystem)", mount.Type)
	}
	if !fs.Hardlink {
		t.Errorf("/bin/sh is on a filesystem that doesn't support hardlinks?")
	}

	mount, err = mounts.GetPath("/run")
	if err != nil {
		t.Fatal("could not stat /run:", err)
	}
	if mount == nil {
		t.Error("could not find mount point for /run")
	} else if mount.Root != "/run" {
		t.Error("expected /run to be a mount point, got", mount.Root)
	}
	t.Log(mount)

	fs = mount.Filesystem()
	if !fs.Memory {
		t.Error("expected /run to be an in-memory filesystem")
	}
	if !fs.Hardlink {
		t.Errorf("/run does not support hardlinks?")
	}
}

func TestReadStaticLinux(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		success bool
		mount   MountPoint
		line    string
	}{
		{ // must fail
			false,
			MountPoint{},
			"",
		},
		{ // normal mount
			true,
			MountPoint{34, "/", "/home", "btrfs"},
			"42 19 0:34 / /home rw,noatime shared:30 - btrfs /dev/sdb1 rw,space_cache",
		},
		{ // subvolume
			true,
			MountPoint{34, "/subvol", "/mountpoint", "btrfs"},
			"42 19 0:34 /subvol /mountpoint rw,noatime shared:30 - btrfs /dev/sdb1 rw,space_cache",
		},
		{ // many optional fields (must be ignored)
			true,
			MountPoint{34, "/", "/home", "btrfs"},
			"42 19 0:34 / /home rw,noatime a b c - btrfs /dev/sdb1 rw,space_cache",
		},
		{ // no optional fields
			true,
			MountPoint{34, "/", "/home", "btrfs"},
			"42 19 0:34 / /home rw,noatime - btrfs /dev/sdb1 rw,space_cache",
		},
		{ // special characters
			true,
			MountPoint{34, "/", "/dir\\ \t@\nü€.*", "btrfs"},
			`42 19 0:34 / /dir\134\040\011@\012ü€.* rw,noatime - btrfs /dev/sdb1 rw,space_cache`,
		},
		// TODO: test major & minor number, and bigger major and minor numbers.
	} {
		r := bytes.NewBufferString(tc.line + "\n")
		info, err := read(r)
		if err != nil {
			t.Errorf("failed to parse line %#v: %s", tc.line, err)
			continue
		}
		if tc.success {
			if info.Len() != 1 {
				t.Errorf("expected to successfully parse 1 line, got %d\nline: %s", info.Len(), tc.line)
				continue
			}
			// get the first
			for _, mount := range info.mountPaths {
				if *mount != tc.mount {
					t.Errorf("line: %s\nexpected: %#v\nactual:   %#v", tc.line, *mount, tc.mount)
				}
			}
		} else {
			if info.Len() != 0 {
				t.Errorf("expected to successfully parse 0 lines, got %d", info.Len())
				continue
			}
		}
	}
}
