//go:build !vips

package thumb

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp" // 注册 WebP 解码器
	"golang.org/x/image/draw"
)

// goGenerator 纯 Go 降级后端（无 libvips 时使用）
// 限制：不支持 HEIC / RAW；WebP 只能解码不能编码（自动降级输出 JPEG）
func init() { defaultGenerator = &goGenerator{} }

type goGenerator struct{}

func (g *goGenerator) Name() string { return "go" }

func (g *goGenerator) Generate(src, dst string, size, quality int, format string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(src), "."))
	var img image.Image
	switch ext {
	case "jpg", "jpeg":
		img, err = jpeg.Decode(f)
	case "png":
		img, err = png.Decode(f)
	case "gif":
		img, err = gif.Decode(f)
	default:
		img, _, err = image.Decode(f) // webp 等由 x/image 注册的格式
	}
	if err != nil {
		return fmt.Errorf("解码失败(%s): %w", ext, err)
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("图片尺寸异常")
	}
	// 等比缩放，最长边 = size
	var nw, nh int
	if w >= h {
		if w <= size {
			nw, nh = w, h
		} else {
			nw, nh = size, h*size/w
		}
	} else {
		if h <= size {
			nw, nh = w, h
		} else {
			nh, nw = size, w*size/h
		}
	}
	if nh < 1 {
		nh = 1
	}
	if nw < 1 {
		nw = 1
	}

	dstImg := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dstImg, dstImg.Bounds(), img, b, draw.Src, nil)

	var buf bytes.Buffer
	// 纯 Go 无 WebP 编码器，统一输出 JPEG
	if err := jpeg.Encode(&buf, dstImg, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	return os.WriteFile(dst, buf.Bytes(), 0o644)
}
