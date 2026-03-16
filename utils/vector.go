package utils

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type vecPixel struct {
	color string
}

// ConvertImageToSVG converts raster image into a true SVG (vector rectangles) using color quantization + row run-length encoding.
func ConvertImageToSVG(srcPath, dstPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	b := img.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", srcW, srcH)
	}

	// Keep vector file compact: downsample to workable grid.
	targetW := clampInt(srcW/8, 120, 320)
	targetH := int(math.Round(float64(srcH) * float64(targetW) / float64(srcW)))
	targetH = clampInt(targetH, 80, 320)

	grid := make([][]vecPixel, targetH)
	for y := 0; y < targetH; y++ {
		row := make([]vecPixel, targetW)
		sy := b.Min.Y + (y*srcH)/targetH
		for x := 0; x < targetW; x++ {
			sx := b.Min.X + (x*srcW)/targetW
			r, g, bl, a := img.At(sx, sy).RGBA()
			c := quantizedHex(r, g, bl, a)
			row[x] = vecPixel{color: c}
		}
		grid[y] = row
	}

	scaleX := float64(srcW) / float64(targetW)
	scaleY := float64(srcH) / float64(targetH)

	var sb strings.Builder
	sb.Grow(targetW * targetH * 16)
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+"\n", srcW, srcH, srcW, srcH))
	sb.WriteString(`<rect width="100%" height="100%" fill="#000000"/>` + "\n")

	for y := 0; y < targetH; y++ {
		row := grid[y]
		runStart := 0
		runColor := row[0].color
		for x := 1; x <= targetW; x++ {
			flush := x == targetW || row[x].color != runColor
			if !flush {
				continue
			}
			runLen := x - runStart
			if runLen > 0 {
				rx := float64(runStart) * scaleX
				ry := float64(y) * scaleY
				rw := float64(runLen) * scaleX
				rh := scaleY
				sb.WriteString(fmt.Sprintf(`<rect x="%.3f" y="%.3f" width="%.3f" height="%.3f" fill="%s"/>`+"\n", rx, ry, rw, rh, runColor))
			}
			if x < targetW {
				runStart = x
				runColor = row[x].color
			}
		}
	}
	sb.WriteString(`</svg>` + "\n")

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dstPath, []byte(sb.String()), 0o644)
}

func quantizedHex(r, g, b, a uint32) string {
	// Convert 16-bit to 8-bit
	rr := int((r + 128) / 257)
	gg := int((g + 128) / 257)
	bb := int((b + 128) / 257)
	aa := int((a + 128) / 257)

	if aa < 10 {
		return "#000000"
	}

	// Un-premultiply alpha approximation
	if aa < 255 {
		rr = (rr * 255) / aa
		gg = (gg * 255) / aa
		bb = (bb * 255) / aa
		rr = clampInt(rr, 0, 255)
		gg = clampInt(gg, 0, 255)
		bb = clampInt(bb, 0, 255)
	}

	rr = (rr / 32) * 32
	gg = (gg / 32) * 32
	bb = (bb / 32) * 32
	rr = clampInt(rr, 0, 255)
	gg = clampInt(gg, 0, 255)
	bb = clampInt(bb, 0, 255)

	return fmt.Sprintf("#%02x%02x%02x", rr, gg, bb)
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
