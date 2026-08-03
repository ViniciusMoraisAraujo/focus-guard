//go:build windows

package tray

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/windows"
)

// embeddedIcon retorna o focusguard.ico embutido nos recursos do executável
// atual (RT_GROUP_ICON + imagens RT_ICON), remontado como um arquivo .ico
// completo. Retorna erro quando o executável não tem ícone embutido (ex.:
// builds de dev sem resource.syso).
func embeddedIcon() ([]byte, error) {
	var mod windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &mod); err != nil {
		return nil, fmt.Errorf("GetModuleHandleEx: %w", err)
	}
	return IconFromModule(mod)
}

// IconFromModule remonta o .ico do grupo de ícones embutido no módulo mod,
// combinando o RT_GROUP_ICON com os payloads RT_ICON referenciados. É a mesma
// estrutura do focusguard.ico original (Vista+ usa payloads PNG).
func IconFromModule(mod windows.Handle) ([]byte, error) {
	// O goversioninfo grava o grupo de ícones no ID 1 (verificado empiricamente);
	// o loop 1..32 cobre esse caso e variações de ferramentas.
	for id := uint16(1); id <= 32; id++ {
		res, err := windows.FindResource(mod, windows.ResourceID(id), windows.RT_GROUP_ICON)
		if err != nil {
			continue
		}
		group, err := windows.LoadResourceData(mod, res)
		if err != nil {
			return nil, err
		}
		return icoFromGroupIcon(mod, group)
	}
	return nil, fmt.Errorf("nenhum grupo de ícones (RT_GROUP_ICON) encontrado nos IDs 1..32")
}

// icoFromGroupIcon monta o arquivo .ico a partir do blob do grupo de ícones e
// dos recursos RT_ICON do módulo.
func icoFromGroupIcon(mod windows.Handle, group []byte) ([]byte, error) {
	if len(group) < 6 {
		return nil, fmt.Errorf("grupo de ícones pequeno demais: %d bytes", len(group))
	}
	count := int(binary.LittleEndian.Uint16(group[4:6]))
	if count == 0 {
		return nil, fmt.Errorf("grupo de ícones sem imagens")
	}

	type entry struct {
		width, height, colorCount, reserved byte
		planes, bitCount                    uint16
		bytesInRes                          uint32
		iconID                              uint16
	}
	entries := make([]entry, 0, count)
	for i := 0; i < count; i++ {
		off := 6 + i*14
		if off+14 > len(group) {
			return nil, fmt.Errorf("grupo de ícones truncado na entrada %d", i)
		}
		entries = append(entries, entry{
			width:      group[off],
			height:     group[off+1],
			colorCount: group[off+2],
			reserved:   group[off+3],
			planes:     binary.LittleEndian.Uint16(group[off+4:]),
			bitCount:   binary.LittleEndian.Uint16(group[off+6:]),
			bytesInRes: binary.LittleEndian.Uint32(group[off+8:]),
			iconID:     binary.LittleEndian.Uint16(group[off+12:]),
		})
	}

	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, uint16(0)) // reserved
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // type: icon
	_ = binary.Write(buf, binary.LittleEndian, uint16(count))

	offset := uint32(6 + 16*count)
	var blobs [][]byte
	for _, e := range entries {
		res, err := windows.FindResource(mod, windows.ResourceID(e.iconID), windows.RT_ICON)
		if err != nil {
			return nil, fmt.Errorf("RT_ICON %d não encontrado: %w", e.iconID, err)
		}
		data, err := windows.LoadResourceData(mod, res)
		if err != nil {
			return nil, err
		}
		blobs = append(blobs, data)
		buf.WriteByte(e.width)
		buf.WriteByte(e.height)
		buf.WriteByte(e.colorCount)
		buf.WriteByte(e.reserved)
		_ = binary.Write(buf, binary.LittleEndian, e.planes)
		_ = binary.Write(buf, binary.LittleEndian, e.bitCount)
		_ = binary.Write(buf, binary.LittleEndian, e.bytesInRes)
		_ = binary.Write(buf, binary.LittleEndian, offset)
		offset += e.bytesInRes
	}
	for _, b := range blobs {
		buf.Write(b)
	}
	return buf.Bytes(), nil
}
