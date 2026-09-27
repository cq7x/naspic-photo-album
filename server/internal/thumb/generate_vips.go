package thumb

import (
	"os"

	"github.com/davidbyttow/govips/v2/vips"
)

// vipsGenerator 基于 libvips 的缩略图后端（推荐，ARM 友好，支持 HEIC 等）；
// 与 generate_go.go 同时编译，由 thumb.New 组装成「vips 优先、纯 Go 兜底」的回落后端。
// 编译：CGO_ENABLED=1（需要 libvips 开发头文件与运行时库）。
func init() {
	vips.LoggingSettings(nil, vips.LogLevelError)
	vips.Startup(&vips.Config{
		ConcurrencyLevel: 1, // ARM 低配：libvips 内部线程数也压到 1
		MaxCacheFiles:    0,
		MaxCacheMem:      32 << 20,
	})
	vipsGen = &vipsGenerator{}
}

type vipsGenerator struct{}

func (g *vipsGenerator) Name() string { return "vips" }

func (g *vipsGenerator) Generate(src, dst string, size, quality int, format string) error {
	img, err := vips.NewImageFromFile(src)
	if err != nil {
		return err // 损坏/不支持格式直接返回，由上层标记失败
	}
	defer img.Close()

	// Thumbnail 会利用 JPEG 的 DCT 缩放加载，比先全解再缩快数倍
	if err := img.Thumbnail(size, size, vips.InterestingNone); err != nil {
		return err
	}

	var out []byte
	switch format {
	case "webp":
		out, _, err = img.ExportWebp(&vips.WebpExportParams{Quality: quality, StripMetadata: true})
	default:
		out, _, err = img.ExportJpeg(&vips.JpegExportParams{Quality: quality, StripMetadata: true})
	}
	if err != nil {
		return err
	}
	return os.WriteFile(dst, out, 0o644)
}
