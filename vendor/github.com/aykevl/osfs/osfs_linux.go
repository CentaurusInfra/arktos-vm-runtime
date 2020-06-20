// Copyright 2016 Ayke van Laethem.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.txt file.

// On Linux, it parses /proc/self/mountinfo. Then it can find the mount point
// based on the st_dev field of the stat result (os.FileInfo.Sys()), and
// determine filesystem capabilities from the filesystem type.
package osfs

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// MOUNTINFO_PATH is the common path for the mountinfo file on Linux.
const MOUNTINFO_PATH = "/proc/self/mountinfo"

// From the kernel docs at
// https://www.kernel.org/doc/Documentation/filesystems/proc.txt
//
//   This file contains lines of the form:
//
//   36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
//   (1)(2)(3)   (4)   (5)      (6)      (7)   (8) (9)   (10)         (11)
//
//   (1) mount ID:  unique identifier of the mount (may be reused after umount)
//   (2) parent ID:  ID of parent (or of self for the top of the mount tree)
//   (3) major:minor:  value of st_dev for files on filesystem
//   (4) root:  root of the mount within the filesystem
//   (5) mount point:  mount point relative to the process's root
//   (6) mount options:  per mount options
//   (7) optional fields:  zero or more fields of the form "tag[:value]"
//   (8) separator:  marks the end of the optional fields
//   (9) filesystem type:  name of filesystem of the form "type[.subtype]"
//   (10) mount source:  filesystem specific information or "none"
//   (11) super options:  per super block options

func defaultFilesystem() Filesystem {
	return Filesystem{
		Permissions: 0777,
		Symlink:     true,
		Hardlink:    true,
		Inode:       true,
	}
}

// Read retuns a list of all mountpoints and their filesystem types.
// It always returns a valid Info object, but may also return an error on
// failures. Errors are worked around as much as possible. Thus, you can safely
// ignore Read() errors while still having reasonable defaults.
func Read() (*Info, error) {
	f, err := os.Open(MOUNTINFO_PATH)
	if err != nil {
		// Maybe an old system that doesn't have the file, or /proc wasn't
		// mounted (yet).
		return &Info{}, err
	}
	return read(f)
}

func read(f io.Reader) (*Info, error) {
	info := &Info{
		mountPaths:   make(map[string]*MountPoint),
		mountNumbers: make(map[uint64]*MountPoint),
	}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// We haven't consumed the last 'line', but the file normally
				// ends in a newline anyway.
				return info, nil
			}
			return info, err
		}
		line = line[:len(line)-1]

		fields := strings.Split(line, " ")
		if len(fields) < 10 {
			// There are not enough fields (unexpected).
			continue
		}

		mount := &MountPoint{}

		// Extract major and minor device number.
		stdev := strings.Split(fields[2], ":")
		if len(stdev) != 2 {
			continue
		}
		devMajor, err1 := strconv.ParseUint(stdev[0], 10, 12)
		devMinor, err2 := strconv.ParseUint(stdev[1], 10, 20)
		if err1 != nil || err2 != nil {
			continue
		}

		// We want to know how the major and minor number are encoded in the
		// st_dev field of stat() results, as that's one of the ways we're going
		// to find the right filesystem to a file.
		// I have found two sources for this information: the Linux kernel
		// source and glibc.
		// Linux: include/linux/kdev_t.h in the Linux sources, e.g.
		//    http://lxr.free-electrons.com/source/include/linux/kdev_t.h
		// In particular, new_encode_dev() and new_decode_dev().
		// It looks like the st_dev field is encoded in the following format:
		//    bit 8-20 minor | bit 0-11 major | bit 0-7 minor
		// Additionally, it looks like the Linux kernel decided upon 32-bits
		// numbers (20 minor and 12 major).
		// The glibc source has 64 bits numbers, and takes a slightly different
		// approach (simply giving the last 32 bits to the major number):
		//    bit 12-43 major | bit 8-19 minor | bit 0-11 major | bit 0-7 minor
		// I'll keep it at the current Linux approach, and limit the minor and
		// major number to 20 and 12 bits (see above).
		mount.devNumber = devMinor&0xff | devMajor&0xfff<<8 | devMinor&^0xff<<12

		var ok bool
		mount.FSRoot, _ = mountParse(fields[3])
		mount.Root, ok = mountParse(fields[4])
		if !ok {
			// This is a critical part of the MountPoint struct.
			continue
		}

		pos := 6
		for pos < len(fields) && fields[pos] != "-" {
			pos++
		}
		pos++
		if pos >= len(fields) {
			// Type is another critical field.
			continue
		}
		mount.Type = fields[pos]

		// TODO check for duplicates?
		info.mountPaths[mount.Root] = mount
		info.mountNumbers[mount.devNumber] = mount
	}

	return info, nil
}

// mountParse parses paths like in /etc/fstab, /etc/mtab, and
// /proc/self/mountinfo.
func mountParse(in string) (string, bool) {
	out := make([]byte, len(in))
	i_out := 0
	for i_in := 0; i_in < len(in); i_in++ {
		c := in[i_in]
		if c == '\\' {
			// \xxx escape, where xxx is a three-digit octal number for the
			// character.
			if (i_in + 3) >= len(in) {
				// There aren't at least three more spaces left.
				return "", false
			}
			n, err := strconv.ParseUint(in[i_in+1:i_in+4], 8, 8)
			if err != nil {
				return "", false
			}
			c = byte(n)
			i_in += 3
		}
		out[i_out] = c
		i_out++
	}
	return string(out[:i_out]), true
}

// MountPoint returns the mountpoint associated with the FileInfo, or nil if the
// mount point wasn't found. When nil is returned, you can still safely call
// Filesystem() on the returned pointer (it will return the default filesystem).
// Calling GetReal() on a nil *Info is not allowed.
//
// The filePath must be absolute (pass filepath.IsAbs), otherwise this function
// will panic. If you aren't sure that the path is absolute, use filepath.Abs().
func (info *Info) GetReal(filePath string, fileInfo os.FileInfo) *MountPoint {
	if !filepath.IsAbs(filePath) {
		panic("path must be absolute")
	}

	// Find the st_dev value.
	var deviceId uint64
	switch sys := fileInfo.Sys().(type) {
	case *syscall.Stat_t:
		deviceId = sys.Dev
	case *unix.Stat_t:
		deviceId = sys.Dev
	default:
		return nil
	}

	// See:
	//  * https://criu.org/Filesystems_pecularities
	//  * https://bugzilla.redhat.com/show_bug.cgi?id=711881
	//  * https://www.mail-archive.com/linux-btrfs@vger.kernel.org/msg02875.html

	// First try finding by device number (works with most filesystems).
	if mount, ok := info.mountNumbers[deviceId]; ok {
		return mount
	}
	filePath = filepath.Clean(filePath)

	// Now find the path that is the closest to the requested path.
	for i := len(filePath) - 1; i >= 0; i-- {
		if filePath[i] != '/' {
			continue
		}
		// We've found a directory

		testPath := filePath[:i]
		if (testPath == "") {
			// Special-case the root directory
			testPath = "/"
		}

		if mount, ok := info.mountPaths[testPath]; ok {
			return mount
		}
	}

	// We can't find the root: the FileInfo is probably just empty.
	return nil
}

// Return the device number for this mount point (the st_dev field for files on
// it).
func (p *MountPoint) DevNumber() (uint64, bool) {
	if p == nil {
		return 0, false
	}
	return p.devNumber, true
}
