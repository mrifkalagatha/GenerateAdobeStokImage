package utils

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"
	"os"
)

// AverageHash64 computes an 8x8 average hash for similarity checks.
func AverageHash64(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= 0 || h <= 0 {
		return 0, fmt.Errorf("invalid image dimensions %dx%d", w, h)
	}

	var gray [64]uint8
	var sum int
	i := 0
	for y := 0; y < 8; y++ {
		sy := b.Min.Y + (y*h)/8
		for x := 0; x < 8; x++ {
			sx := b.Min.X + (x*w)/8
			r, g, bl, _ := img.At(sx, sy).RGBA()
			v := uint8((((r>>8)*299 + (g>>8)*587 + (bl>>8)*114) / 1000))
			gray[i] = v
			sum += int(v)
			i++
		}
	}
	avg := uint8(sum / 64)
	var h64 uint64
	for j := 0; j < 64; j++ {
		if gray[j] >= avg {
			h64 |= 1 << uint(j)
		}
	}
	return h64, nil
}

func HammingDistance64(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}
