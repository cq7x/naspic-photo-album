// Package media 媒体元数据采集（EXIF / 基础信息）
package media

import (
	"image"
	_ "image/gif"  // 注册 gif 解码器，供 DecodeConfig 读尺寸
	_ "image/jpeg" // 注册 jpeg 解码器
	_ "image/png"  // 注册 png 解码器
	"os"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// TakenSource 拍摄时间来源
const (
	TakenSourceEXIF  int8 = 1 // 图片 EXIF
	TakenSourceMtime int8 = 2 // 文件修改时间（兜底）
	TakenSourceFix   int8 = 3 // 用户手工修正
	TakenSourceVideo int8 = 4 // 视频容器 creation_time
)

// Meta 媒体元数据
type Meta struct {
	TakenAt     *time.Time
	TakenSource int8 // 见 TakenSource*
	Make        string
	Model       string
	Lat         *float64
	Lon         *float64
	Orientation int16
	Width       int
	Height      int
	DurationMs  int
	ExifJSON    string
}

// Parse 解析图片 EXIF 与尺寸；失败时用文件 mtime 兜底，绝不抛错中断扫描。
//
// 尺寸用于前端两端对齐排版与详情页展示：走标准库 image.DecodeConfig，
// 只读文件头不解码整图，成本极低（jpeg/png/gif；webp/heic 暂返回 0）。
func Parse(path string) (*Meta, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	m := &Meta{TakenSource: 2}
	mt := st.ModTime()
	m.TakenAt = &mt

	// 只对图片尝试 EXIF；视频走后续 ffprobe（可选）
	ext := strings.ToLower(path[strings.LastIndex(path, ".")+1:])
	// 视频：容器里通常带着拍摄时间（iPhone/安卓/相机都写 creation_time），
	// 之前一律用文件 mtime，复制/导出过的文件时间全乱，排序自然不对。
	// 读到就用它，读不到（或没装 ffprobe）再退回 mtime。
	if isVideoExt(ext) {
		if vm := VideoInfo(path); vm != nil {
			if vm.TakenAt != nil {
				m.TakenAt = vm.TakenAt
				m.TakenSource = TakenSourceVideo
			}
			m.Width = vm.Width
			m.Height = vm.Height
			m.DurationMs = vm.DurationMs
		}
		return m, nil
	}

	if !isImageExt(ext) {
		return m, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return m, nil
	}
	defer f.Close()

	// 先取尺寸（即便没有 EXIF 也必须有，否则前端排版退化成等比方块）
	m.Width, m.Height = imageSize(f)

	// exif.Decode 会从当前 offset 读，先回到文件头
	_, _ = f.Seek(0, 0)

	x, err := exif.Decode(f)
	if err != nil {
		// 无 EXIF 或损坏：静默降级，用 mtime（尺寸已拿到，照常返回）
		return m, nil
	}

	m.TakenSource = 1
	if t, err := x.DateTime(); err == nil {
		m.TakenAt = &t
	}
	if v, err := x.Get(exif.Make); err == nil {
		m.Make, _ = v.StringVal()
	}
	if v, err := x.Get(exif.Model); err == nil {
		m.Model, _ = v.StringVal()
	}
	if lat, lon, err := x.LatLong(); err == nil {
		m.Lat, m.Lon = &lat, &lon
	}
	if v, err := x.Get(exif.Orientation); err == nil {
		if n, err := v.Int(0); err == nil {
			m.Orientation = int16(n)
		}
	}
	m.Make = strings.TrimSpace(m.Make)
	m.Model = strings.TrimSpace(m.Model)
	return m, nil
}

// imageSize 只读文件头拿宽高，失败返回 0,0（调用方按缺省比例处理）
func imageSize(f *os.File) (int, int) {
	if _, err := f.Seek(0, 0); err != nil {
		return 0, 0
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func isImageExt(ext string) bool {
	switch ext {
	case "jpg", "jpeg", "png", "webp", "tiff", "tif", "heic", "heif":
		return true
	}
	return false
}

func isVideoExt(ext string) bool {
	switch ext {
	case "mp4", "mov", "m4v", "mkv", "avi", "webm", "flv", "3gp", "ts", "mpg", "mpeg":
		return true
	}
	return false
}
