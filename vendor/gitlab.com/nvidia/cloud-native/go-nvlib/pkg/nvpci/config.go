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

	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvpci/bytes"
)

const (
	pciCfgSpaceStandardSize  = 256
	pciCfgSpaceExtendedSize  = 4096
	pciCapabilityListPointer = 0x34
)

type ConfigSpace struct {
	Path string
}

type ConfigSpaceIO interface {
	bytes.Bytes
	GetVendorID() uint16
	GetDeviceID() uint16
	GetPCICapabilities() (*PCICapabilities, error)
}

type configSpaceIO struct {
	bytes.Bytes
}

type PCIStandardCapability struct {
	bytes.Bytes
}

type PCIExtendedCapability struct {
	bytes.Bytes
	Version uint8
}

type PCICapabilities struct {
	Standard map[uint8]*PCIStandardCapability
	Extended map[uint16]*PCIExtendedCapability
}

func (cs *ConfigSpace) Read() (ConfigSpaceIO, error) {
	config, err := ioutil.ReadFile(cs.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	return &configSpaceIO{bytes.New(&config)}, nil
}

func (cs *configSpaceIO) GetVendorID() uint16 {
	return cs.Read16(0)
}

func (cs *configSpaceIO) GetDeviceID() uint16 {
	return cs.Read16(2)
}

func (cs *configSpaceIO) GetPCICapabilities() (*PCICapabilities, error) {
	caps := &PCICapabilities{
		make(map[uint8]*PCIStandardCapability),
		make(map[uint16]*PCIExtendedCapability),
	}

	soffset := cs.Read8(pciCapabilityListPointer)
	if int(soffset) >= cs.Len() {
		return nil, fmt.Errorf("capability list pointer out of bounds")
	}

	for soffset != 0 {
		if soffset == 0xff {
			return nil, fmt.Errorf("config space broken")
		}
		if int(soffset) >= pciCfgSpaceStandardSize {
			return nil, fmt.Errorf("standard capability list pointer out of bounds")
		}
		data := cs.Read32(int(soffset))
		id := uint8(data & 0xff)
		caps.Standard[id] = &PCIStandardCapability{
			cs.Slice(int(soffset), cs.Len()-int(soffset)),
		}
		soffset = uint8((data >> 8) & 0xff)
	}

	if cs.Len() <= pciCfgSpaceStandardSize {
		return caps, nil
	}

	eoffset := uint16(pciCfgSpaceStandardSize)
	for eoffset != 0 {
		if eoffset == 0xffff {
			return nil, fmt.Errorf("config space broken")
		}
		if int(eoffset) >= pciCfgSpaceExtendedSize {
			return nil, fmt.Errorf("extended capability list pointer out of bounds")
		}
		data := cs.Read32(int(eoffset))
		id := uint16(data & 0xffff)
		version := uint8((data >> 16) & 0xf)
		caps.Extended[id] = &PCIExtendedCapability{
			cs.Slice(int(eoffset), cs.Len()-int(eoffset)),
			version,
		}
		eoffset = uint16((data >> 4) & 0xffc)
	}

	return caps, nil
}
