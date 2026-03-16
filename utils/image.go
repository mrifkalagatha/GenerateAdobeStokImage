package utils

import (
	"fmt"
	"image"
	"image/jpeg"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
)

func NormalizeToStockJPEG(path string, targetWidth, targetHeight, minPixels int) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(src)
	src.Close()
	if err != nil {
		return fmt.Errorf("decode image failed: %w", err)
	}

	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("invalid image dimensions %dx%d", w, h)
	}

	if targetWidth <= 0 {
		targetWidth = w
	}
	if targetHeight <= 0 {
		targetHeight = h
	}

	needResize := w*h < minPixels || w != targetWidth || h != targetHeight
	var out image.Image = img

	if needResize {
		dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
		out = dst
	}

	tmpPath := path + ".tmp.jpg"
	tmp, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	if err := jpeg.Encode(tmp, out, &jpeg.Options{Quality: 95}); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode jpeg failed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func EnsureJPGPath(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return filename + ".jpg"
	}
	return filename[:len(filename)-len(ext)] + ".jpg"
}

