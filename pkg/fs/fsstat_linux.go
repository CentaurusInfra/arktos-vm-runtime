// +build linux

/*
Copyright 2019 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fs

import (
	"syscall"
)

// GetFsStatsForPath returns the info about inode usage and space usage
// (in bytes) for the filesystem that contains the provided path.
func GetFsStatsForPath(path string) (uint64, uint64, error) {
	fs := syscall.Statfs_t{}
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, 0, err
	}
	return (fs.Blocks - fs.Bfree) * uint64(fs.Bsize), fs.Files - fs.Ffree, nil
}
