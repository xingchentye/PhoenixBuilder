package chunk

import (
	"bytes"
	"fmt"
	"phoenixbuilder/mirror/define"
)

func NEMCNetworkDecode(data []byte, count int) (c *Chunk, nbtBlocks []map[string]interface{}, err error) {
	air, ok := StateToRuntimeID("minecraft:air", nil)
	if !ok {
		panic("cannot find air runtime ID")
	}
	var (
		buf     = bytes.NewBuffer(data)
		encoder = &nemcNetworkEncoding{}
	)
	c = New(air, define.Range{-64, 319})
	nbtBlocks = []map[string]interface{}{}
	defer func() {
		r := recover()
		if r != nil {
			c = nil
			err = fmt.Errorf("%v", r)
		}
	}()
	encoder.isChunkDecoding = true
	// offset := uint8((int(define.NEMCWorldStart) - define.WorldRange[0]) / 16)
	// fmt.Println(offset)
	for i := 0; i < count; i++ {
		index := uint8(i)
		// decodeSubChunk(buf, c, &index, NetworkEncoding)
		c.sub[index], err = decodeSubChunk(buf, c, &index, encoder)
		if err != nil {
			return nil, nil, err
		}
	}
	encoder.isChunkDecoding = false
	subChunkCount := 25
	for i := 0; i < subChunkCount; i++ {
		_, err := decodePalettedStorage(buf, encoder, biomePaletteEncoding{})
		if err != nil {
			fmt.Println(err)
			return nil, nil, err
		}
	}
	return c, nbtBlocks, nil
}

// DiskDecode decodes the data from a SerialisedData object into a chunk and returns it. If the data was
// invalid, an error is returned.
func DiskDecode(data SerialisedData, r define.Range) (*Chunk, error) {
	air, ok := StateToRuntimeID("minecraft:air", nil)
	if !ok {
		panic("cannot find air runtime ID")
	}

	c := New(air, r)

	var err error
	for i, sub := range data.SubChunks {
		if len(sub) == 0 {
			// No data for this sub chunk.
			continue
		}
		index := uint8(i)
		if c.sub[index], err = decodeSubChunk(bytes.NewBuffer(sub), c, &index, DiskEncoding); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// decodeSubChunk decodes a SubChunk from a bytes.Buffer. The Encoding passed defines how the block storages of the
// SubChunk are decoded.
func decodeSubChunk(buf *bytes.Buffer, c *Chunk, index *byte, e Encoding) (*SubChunk, error) {
	ver, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading version: %w", err)
	}
	sub := NewSubChunk(c.air)
	switch ver {
	default:
		return nil, fmt.Errorf("unknown sub chunk version %v: can't decode", ver)
	case 1:
		// Version 1 only has one layer for each sub chunk, but uses the format with palettes.
		storage, err := decodePalettedStorage(buf, e, BlockPaletteEncoding)
		if err != nil {
			return nil, err
		}
		sub.storages = append(sub.storages, storage)
	case 8, 9:
		// Version 8 allows up to 256 layers for one sub chunk.
		storageCount, err := buf.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("error reading storage count: %w", err)
		}
		if ver == 9 {
			uIndex, err := buf.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("error reading subchunk index: %w", err)
			}
			// The index as written here isn't the actual index of the subchunk within the chunk. Rather, it is the Y
			// value of the subchunk. This means that we need to translate it to an index.
			*index = uint8(int8(uIndex) - int8(c.r[0]>>4))
		}
		sub.storages = make([]*PalettedStorage, storageCount)

		for i := byte(0); i < storageCount; i++ {
			sub.storages[i], err = decodePalettedStorage(buf, e, BlockPaletteEncoding)
			if err != nil {
				return nil, err
			}
		}
	}
	return sub, nil
}

func decodeSubChunkNEMC(buf *bytes.Buffer, c *Chunk, index *byte, e Encoding) (*SubChunk, error) {
	ver, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading version: %w", err)
	}
	sub := NewSubChunk(c.air)
	switch ver {
	default:
		return nil, fmt.Errorf("unknown sub chunk version %v: can't decode", ver)
	case 8, 9:
		// Version 8 allows up to 256 layers for one sub chunk.
		storageCount, err := buf.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("error reading storage count: %w", err)
		}
		if ver == 9 {
			uIndex, err := buf.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("error reading subchunk index: %w", err)
			}
			// The index as written here isn't the actual index of the subchunk within the chunk. Rather, it is the Y
			// value of the subchunk. This means that we need to translate it to an index.
			*index = uint8(int8(uIndex) - int8(c.r[0]>>4))
		}
		sub.storages = make([]*PalettedStorage, storageCount)

		for i := byte(0); i < storageCount; i++ {
			sub.storages[i], err = decodePalettedStorageNEMC(buf, e, BlockPaletteEncoding)
			if err != nil {
				return nil, err
			}
		}
	}
	return sub, nil
}

// decodePalettedStorage decodes a PalettedStorage from a bytes.Buffer. The Encoding passed is used to read either a
// network or disk block storage.
func decodePalettedStorage(buf *bytes.Buffer, e Encoding, pe paletteEncoding) (*PalettedStorage, error) {
	blockSize, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading block size: %w", err)
	}
	blockSize >>= 1
	if blockSize == 0x7f {
		return nil, nil
	}

	size := paletteSize(blockSize)
	uint32Count := size.uint32s()

	uint32s := make([]uint32, uint32Count)
	byteCount := uint32Count * 4

	data := buf.Next(byteCount)
	if len(data) != byteCount {
		return nil, fmt.Errorf("cannot read paletted storage (size=%v) %T: not enough block data present: expected %v bytes, got %v", blockSize, pe, byteCount, len(data))
	}
	for i := 0; i < uint32Count; i++ {
		// Explicitly don't use the binary package to greatly improve performance of reading the uint32s.
		uint32s[i] = uint32(data[i*4]) | uint32(data[i*4+1])<<8 | uint32(data[i*4+2])<<16 | uint32(data[i*4+3])<<24
	}
	p, err := e.decodePalette(buf, paletteSize(blockSize), pe)
	return newPalettedStorage(uint32s, p), err
}

func decodePalettedStorageNEMC(buf *bytes.Buffer, e Encoding, pe paletteEncoding) (*PalettedStorage, error) {
	blockSize, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading block size: %w", err)
	}
	blockSize >>= 1
	if blockSize == 0x7f {
		return nil, nil
	}

	size := paletteSize(blockSize)
	uint32Count := size.uint32s()

	uint32s := make([]uint32, uint32Count)
	byteCount := uint32Count * 4

	data := buf.Next(byteCount)
	if len(data) != byteCount {
		return nil, fmt.Errorf("cannot read paletted storage (size=%v) %T: not enough block data present: expected %v bytes, got %v", blockSize, pe, byteCount, len(data))
	}
	for i := 0; i < uint32Count; i++ {
		// Explicitly don't use the binary package to greatly improve performance of reading the uint32s.
		uint32s[i] = uint32(data[i*4]) | uint32(data[i*4+1])<<8 | uint32(data[i*4+2])<<16 | uint32(data[i*4+3])<<24
	}
	p, err := e.decodePalette(buf, paletteSize(blockSize), pe)
	newPalette := newPalettedStorage(uint32s, p)
	newPalette.shrinkAir()
	return newPalette, err
}
