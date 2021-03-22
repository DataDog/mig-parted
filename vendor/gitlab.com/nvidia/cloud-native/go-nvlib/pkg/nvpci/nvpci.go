/*
 * Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package nvpci

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	// pciDevicesRoot represents base path for all pci devices under sysfs
	pciDevicesRoot = "/sys/bus/pci/devices"
	// pciNvidiaVendorID represents PCI vendor id for NVIDIA
	pciNvidiaVendorID uint16 = 0x10de
	// pciVgaControllerClass represents the PCI class for VGA Controllers
	pciVgaControllerClass uint32 = 0x030000
	// pci3dControllerClass represents the PCI class for 3D Graphics accellerators
	pci3dControllerClass uint32 = 0x030200
	// pciNvSwitchClass represents the PCI class for NVSwitches
	pciNvSwitchClass uint32 = 0x068000
)

// Interface allows us to get a list of all NVIDIA PCI devices
type Interface interface {
	GetAllDevices() ([]*NvidiaPCIDevice, error)
	Get3DControllers() ([]*NvidiaPCIDevice, error)
	GetVGAControllers() ([]*NvidiaPCIDevice, error)
	GetNVSwitches() ([]*NvidiaPCIDevice, error)
	GetGPUs() ([]*NvidiaPCIDevice, error)
}

type nvpci struct {
	pciDevicesRoot string
}

var _ Interface = (*nvpci)(nil)

// NvidiaPCIDevice represents a PCI device for an NVIDIA product
type NvidiaPCIDevice struct {
	Path      string
	Address   string
	Vendor    uint16
	Class     uint32
	Device    uint16
	Config    *ConfigSpace
	Resources map[int]*MemoryResource
}

func (d *NvidiaPCIDevice) IsVGAController() bool {
	return d.Class == pciVgaControllerClass
}

func (d *NvidiaPCIDevice) Is3DController() bool {
	return d.Class == pci3dControllerClass
}

func (d *NvidiaPCIDevice) IsNVSwitch() bool {
	return d.Class == pciNvSwitchClass
}

func (d *NvidiaPCIDevice) IsGPU() bool {
	return d.IsVGAController() || d.Is3DController()
}

func (d *NvidiaPCIDevice) IsResetAvailable() bool {
	_, err := os.Stat(path.Join(d.Path, "reset"))
	if err != nil {
		return false
	}
	return true
}

func (d *NvidiaPCIDevice) Reset() error {
	err := ioutil.WriteFile(path.Join(d.Path, "reset"), []byte("1"), 0)
	if err != nil {
		return fmt.Errorf("unable to write to reset file: %v", err)
	}
	return nil
}

func New() Interface {
	return &nvpci{pciDevicesRoot}
}

// GetAllDevices returns all Nvidia PCI devices on the system
func (p *nvpci) GetAllDevices() ([]*NvidiaPCIDevice, error) {
	deviceDirs, err := ioutil.ReadDir(p.pciDevicesRoot)
	if err != nil {
		return nil, fmt.Errorf("unable to read PCI bus devices: %v", err)
	}

	var nvdevices []*NvidiaPCIDevice
	for _, deviceDir := range deviceDirs {
		devicePath := path.Join(p.pciDevicesRoot, deviceDir.Name())
		address := deviceDir.Name()

		vendor, err := ioutil.ReadFile(path.Join(devicePath, "vendor"))
		if err != nil {
			return nil, fmt.Errorf("unable to read PCI device vendor id for %s: %v", address, err)
		}
		vendorStr := strings.TrimSpace(string(vendor))
		vendorID, err := strconv.ParseUint(vendorStr, 0, 16)
		if err != nil {
			return nil, fmt.Errorf("unable to convert vendor string to uint16: %v", vendorStr)
		}

		if uint16(vendorID) != pciNvidiaVendorID {
			continue
		}

		class, err := ioutil.ReadFile(path.Join(devicePath, "class"))
		if err != nil {
			return nil, fmt.Errorf("unable to read PCI device class for %s: %v", address, err)
		}
		classStr := strings.TrimSpace(string(class))
		classID, err := strconv.ParseUint(classStr, 0, 32)
		if err != nil {
			return nil, fmt.Errorf("unable to convert class string to uint32: %v", classStr)
		}

		device, err := ioutil.ReadFile(path.Join(devicePath, "device"))
		if err != nil {
			return nil, fmt.Errorf("unable to read PCI device id for %s: %v", address, err)
		}
		deviceStr := strings.TrimSpace(string(device))
		deviceID, err := strconv.ParseUint(deviceStr, 0, 16)
		if err != nil {
			return nil, fmt.Errorf("unable to convert device string to uint16: %v", deviceStr)
		}

		config := &ConfigSpace{
			Path: path.Join(devicePath, "config"),
		}

		resource, err := ioutil.ReadFile(path.Join(devicePath, "resource"))
		if err != nil {
			return nil, fmt.Errorf("unable to read PCI resource file for %s: %v", address, err)
		}

		resources := make(map[int]*MemoryResource)
		for i, line := range strings.Split(strings.TrimSpace(string(resource)), "\n") {
			values := strings.Split(line, " ")
			if len(values) != 3 {
				return nil, fmt.Errorf("more than 3 entries in line '%d' of resource file", i)
			}

			start, _ := strconv.ParseUint(values[0], 0, 64)
			end, _ := strconv.ParseUint(values[1], 0, 64)
			flags, _ := strconv.ParseUint(values[2], 0, 64)

			if (end - start) != 0 {
				resources[i] = &MemoryResource{
					uintptr(start),
					uintptr(end),
					flags,
					fmt.Sprintf("%s/resource%d", devicePath, i),
				}
			}
		}

		nvdevice := &NvidiaPCIDevice{
			Path:      devicePath,
			Address:   address,
			Vendor:    uint16(vendorID),
			Class:     uint32(classID),
			Device:    uint16(deviceID),
			Config:    config,
			Resources: resources,
		}

		nvdevices = append(nvdevices, nvdevice)
	}

	addressToId := func(address string) uint64 {
		address = strings.ReplaceAll(address, ":", "")
		address = strings.ReplaceAll(address, ".", "")
		id, _ := strconv.ParseUint(address, 16, 64)
		return id
	}

	sort.Slice(nvdevices, func(i, j int) bool {
		return addressToId(nvdevices[i].Address) < addressToId(nvdevices[j].Address)
	})

	return nvdevices, nil
}

// Get3DControllers returns all NVIDIA 3D Controller PCI devices on the system
func (p *nvpci) Get3DControllers() ([]*NvidiaPCIDevice, error) {
	devices, err := p.GetAllDevices()
	if err != nil {
		return nil, fmt.Errorf("error getting all NVIDIA devices: %v", err)
	}

	var filtered []*NvidiaPCIDevice
	for _, d := range devices {
		if d.Is3DController() {
			filtered = append(filtered, d)
		}
	}

	return filtered, nil
}

// GetVGAControllers returns all NVIDIA VGA Controller PCI devices on the system
func (p *nvpci) GetVGAControllers() ([]*NvidiaPCIDevice, error) {
	devices, err := p.GetAllDevices()
	if err != nil {
		return nil, fmt.Errorf("error getting all NVIDIA devices: %v", err)
	}

	var filtered []*NvidiaPCIDevice
	for _, d := range devices {
		if d.IsVGAController() {
			filtered = append(filtered, d)
		}
	}

	return filtered, nil
}

// GetNVSwitches returns all NVIDIA NVSwitch PCI devices on the system
func (p *nvpci) GetNVSwitches() ([]*NvidiaPCIDevice, error) {
	devices, err := p.GetAllDevices()
	if err != nil {
		return nil, fmt.Errorf("error getting all NVIDIA devices: %v", err)
	}

	var filtered []*NvidiaPCIDevice
	for _, d := range devices {
		if d.IsNVSwitch() {
			filtered = append(filtered, d)
		}
	}

	return filtered, nil
}

// GetGPUs returns all NVIDIA GPU devices on the system
func (p *nvpci) GetGPUs() ([]*NvidiaPCIDevice, error) {
	devices, err := p.GetAllDevices()
	if err != nil {
		return nil, fmt.Errorf("error getting all NVIDIA devices: %v", err)
	}

	var filtered []*NvidiaPCIDevice
	for _, d := range devices {
		if d.IsGPU() {
			filtered = append(filtered, d)
		}
	}

	return filtered, nil
}
