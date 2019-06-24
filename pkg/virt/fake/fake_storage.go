/*
Copyright 2017 Mirantis

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

package fake

import (
	"fmt"
	"path"
	"sort"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/pkg/virt"
)

var capacityUnits = map[string]uint64{
	"b":     1,
	"bytes": 1,
	"KB":    1000,
	"k":     1024,
	"KiB":   1024,
	"":      1024, // libvirt defaults to KiB
	"MB":    1000000,
	"M":     1048576,
	"MiB":   1048576,
	"GB":    1000000000,
	"G":     1073741824,
	"GiB":   1073741824,
	"TB":    1000000000000,
	"T":     1099511627776,
	"TiB":   1099511627776,
}

// FakeStorageConnection is a fake implementation of StorageConnection.
type FakeStorageConnection struct {
	rec   testutils.Recorder
	pools map[string]*FakeStoragePool
}

// NewFakeStorageConnection creates a new FakeStorageConnection using
// the specified Recorder to record any changes.
func NewFakeStorageConnection(rec testutils.Recorder) *FakeStorageConnection {
	return &FakeStorageConnection{
		rec:   rec,
		pools: make(map[string]*FakeStoragePool),
	}
}

// CreateStoragePool implements CreateStoragePool method of StorageConnection interface.
func (sc *FakeStorageConnection) CreateStoragePool(def *libvirtxml.StoragePool) (virt.StoragePool, error) {
	sc.rec.Rec("CreateStoragePool", mustMarshal(def))
	if _, found := sc.pools[def.Name]; found {
		return nil, fmt.Errorf("storage pool already exists: %v", def.Name)
	}
	p := NewFakeStoragePool(testutils.NewChildRecorder(sc.rec, def.Name), def)
	sc.pools[def.Name] = p
	return p, nil
}

// LookupStoragePoolByName implements LookupStoragePoolByName method of StorageConnection interface.
func (sc *FakeStorageConnection) LookupStoragePoolByName(name string) (virt.StoragePool, error) {
	if p, found := sc.pools[name]; found {
		return p, nil
	}
	return nil, virt.ErrStoragePoolNotFound
}

// ListPools implements ListPools method of StorageConnection interface.
func (sc *FakeStorageConnection) ListPools() ([]virt.StoragePool, error) {
	r := make([]virt.StoragePool, len(sc.pools))
	names := make([]string, 0, len(sc.pools))
	for name := range sc.pools {
		names = append(names, name)
	}
	sort.Strings(names)
	for n, name := range names {
		r[n] = sc.pools[name]
	}
	return r, nil
}

// PutFiles implements PutFiles method of StorageConnection interface.
func (sc *FakeStorageConnection) PutFiles(imagePath string, files map[string][]byte) error {
	fileStrs := make(map[string]string)
	for filename, content := range files {
		fileStrs[filename] = string(content)
	}
	sc.rec.Rec("PutFiles", map[string]interface{}{
		"imagePath": fixPath(imagePath),
		"files":     fileStrs,
	})
	return nil
}

// FakeStoragePool is a fake implementation of StoragePool interface.
type FakeStoragePool struct {
	rec     testutils.Recorder
	name    string
	path    string
	volumes map[string]*FakeStorageVolume
	def     *libvirtxml.StoragePool
}

// NewFakeStoragePool creates a new StoragePool using the specified
// recorder, name and pool path.
func NewFakeStoragePool(rec testutils.Recorder, def *libvirtxml.StoragePool) *FakeStoragePool {
	poolPath := "/"
	if def.Target != nil {
		poolPath = def.Target.Path
	}
	return &FakeStoragePool{
		rec:     rec,
		name:    def.Name,
		path:    poolPath,
		volumes: make(map[string]*FakeStorageVolume),
		def:     def,
	}
}

func (p *FakeStoragePool) createStorageVol(def *libvirtxml.StorageVolume) (virt.StorageVolume, error) {
	if _, found := p.volumes[def.Name]; found {
		return nil, fmt.Errorf("storage volume already exists: %v", def.Name)
	}
	v, err := newFakeStorageVolume(testutils.NewChildRecorder(p.rec, def.Name), p, def)
	if err != nil {
		return nil, err
	}
	p.volumes[def.Name] = v
	return v, nil
}

// CreateStorageVol implements CreateStorageVol method of StoragePool interface.
func (p *FakeStoragePool) CreateStorageVol(def *libvirtxml.StorageVolume) (virt.StorageVolume, error) {
	p.rec.Rec("CreateStorageVol", mustMarshal(def))
	return p.createStorageVol(def)
}

// ListVolumes implements ListVolumes method of StoragePool interface.
func (p *FakeStoragePool) ListVolumes() ([]virt.StorageVolume, error) {
	r := make([]virt.StorageVolume, len(p.volumes))
	names := make([]string, 0, len(p.volumes))
	for name := range p.volumes {
		names = append(names, name)
	}
	sort.Strings(names)
	for n, name := range names {
		r[n] = p.volumes[name]
	}
	return r, nil
}

// LookupVolumeByName implements LookupVolumeByName method of StoragePool interface.
func (p *FakeStoragePool) LookupVolumeByName(name string) (virt.StorageVolume, error) {
	if v, found := p.volumes[name]; found {
		return v, nil
	}
	return nil, virt.ErrStorageVolumeNotFound
}

func (p *FakeStoragePool) removeVolumeByName(name string) error {
	if _, found := p.volumes[name]; !found {
		return nil
	}
	delete(p.volumes, name)
	return nil
}

// RemoveVolumeByName implements RemoveVolumeByName method of StoragePool interface.
func (p *FakeStoragePool) RemoveVolumeByName(name string) error {
	p.rec.Rec("RemoveVolumeByName", name)
	return p.removeVolumeByName(name)
}

// XML implements XML method of StoragePool interface.
func (p *FakeStoragePool) XML() (*libvirtxml.StoragePool, error) {
	return p.def, nil
}

// FakeStorageVolume is a fake implementation of StorageVolume interface.
type FakeStorageVolume struct {
	rec  testutils.Recorder
	pool *FakeStoragePool
	name string
	path string
	size uint64
	def  *libvirtxml.StorageVolume
}

func newFakeStorageVolume(rec testutils.Recorder, pool *FakeStoragePool, def *libvirtxml.StorageVolume) (*FakeStorageVolume, error) {
	volPath := ""
	if def.Target != nil {
		volPath = def.Target.Path
	}
	if volPath == "" {
		volPath = path.Join(pool.path, def.Name)
	}

	v := &FakeStorageVolume{
		rec:  rec,
		pool: pool,
		name: def.Name,
		path: volPath,
		def:  def,
	}
	if def.Capacity != nil {
		coef, found := capacityUnits[def.Capacity.Unit]
		if !found {
			return nil, fmt.Errorf("bad capacity units: %q", def.Capacity.Unit)
		}
		v.size = def.Capacity.Value * coef
	}

	return v, nil
}

func (v *FakeStorageVolume) descriptiveName() string {
	return v.pool.name + "." + v.name
}

// Name implements Name method of StorageVolume interface.
func (v *FakeStorageVolume) Name() string {
	return v.name
}

// Size implements Size method of StorageVolume interface.
func (v *FakeStorageVolume) Size() (uint64, error) {
	return v.size, nil
}

// Path implements Path method of StorageVolume interface.
func (v *FakeStorageVolume) Path() (string, error) {
	return v.path, nil
}

// Remove implements Remove method of StorageVolume interface.
func (v *FakeStorageVolume) Remove() error {
	v.rec.Rec("Remove", nil)
	return v.pool.removeVolumeByName(v.name)
}

// Format implements Format method of StorageVolume interface.
func (v *FakeStorageVolume) Format() error {
	v.rec.Rec("Format", nil)
	return nil
}

// XML implements XML method of StorageVolume interface.
func (v *FakeStorageVolume) XML() (*libvirtxml.StorageVolume, error) {
	return v.def, nil
}
