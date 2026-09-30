//go:build unit

package service

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

// This fixture has a valid PNG configuration but no pixel stream. The security
// gate must reject its dimensions before the full decoder can allocate pixels.
func oversizedAvatarPNG(width, height uint32) []byte {
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(kind string, data []byte) {
		_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))
		out.WriteString(kind)
		out.Write(data)
		hash := crc32.NewIEEE()
		_, _ = hash.Write([]byte(kind))
		_, _ = hash.Write(data)
		_ = binary.Write(&out, binary.BigEndian, hash.Sum32())
	}
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[0:4], width)
	binary.BigEndian.PutUint32(header[4:8], height)
	header[8] = 8 // grayscale, eight bits per pixel
	chunk("IHDR", header)
	chunk("tEXt", bytes.Repeat([]byte{'x'}, targetAvatarBytes+1))
	chunk("IDAT", nil)
	chunk("IEND", nil)
	return out.Bytes()
}

func TestInlineAvatarRejectsPixelBudgetBeforeDecoding(t *testing.T) {
	for _, dimensions := range [][2]uint32{{12000, 12000}, {2048, 2049}, {4097, 1}} {
		data := oversizedAvatarPNG(dimensions[0], dimensions[1])
		require.Greater(t, len(data), targetAvatarBytes)
		require.Less(t, len(data), maxInlineAvatarBytes)
		_, err := normalizeInlineUserAvatarInput("data:image/png;base64," + base64.StdEncoding.EncodeToString(data))
		require.ErrorIs(t, err, ErrAvatarTooLarge)
	}
}

func TestAvatarCompressionCapsOutputDimensions(t *testing.T) {
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1200, 600))))
	compressed, mime, err := compressInlineAvatar(data.Bytes())
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", mime)
	config, _, err := image.DecodeConfig(bytes.NewReader(compressed))
	require.NoError(t, err)
	require.Equal(t, maxAvatarOutputSize, config.Width)
	require.Equal(t, maxAvatarOutputSize/2, config.Height)
}
