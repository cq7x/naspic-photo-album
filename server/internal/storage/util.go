package storage

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 支持的媒体扩展名（小写）
var (
	ImageExts = map[string]bool{
		"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true,
		"bmp": true, "heic": true, "heif": true, "avif": true, "tiff": true, "tif": true,
		"raw": true, "cr2": true, "nef": true, "arw": true, "dng": true, "orf": true, "rw2": true,
	}
	VideoExts = map[string]bool{
		"mp4": true, "mov": true, "m4v": true, "mkv": true, "avi": true,
		"webm": true, "flv": true, "3gp": true, "ts": true, "mpg": true, "mpeg": true,
	}
)

// ExtOf 取小写扩展名（不含点）
func ExtOf(name string) string {
	e := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	return e
}

// MediaTypeOf 由扩展名判定媒体类型：1=图片 2=视频 3=其他
func MediaTypeOf(ext string) int8 {
	switch {
	case ImageExts[ext]:
		return 1
	case VideoExts[ext]:
		return 2
	default:
		return 3
	}
}

// IsSupportedExt 是否受支持的媒体扩展名
func IsSupportedExt(ext string) bool {
	if ext == "" {
		return false
	}
	return ImageExts[ext] || VideoExts[ext]
}

// SafeJoin 将相对路径安全拼接到根目录下，阻断 ../ 穿越
func SafeJoin(root, rel string) (string, error) {
	clean := path.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))
	full := filepath.Join(root, filepath.FromSlash(clean))
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+string(os.PathSeparator)) {
		return "", ErrNotFound
	}
	return absFull, nil
}

// MatchIgnore 判断路径是否命中忽略规则（支持 glob：* ? [...] 与 **/ 前缀）
func MatchIgnore(rel string, rules []string) bool {
	if len(rules) == 0 {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, r := range rules {
		r = strings.TrimSpace(filepath.ToSlash(r))
		if r == "" {
			continue
		}
		// **/xxx/** 语义：只要路径中出现该目录名即忽略
		if strings.HasPrefix(r, "**/") && strings.HasSuffix(r, "/**") {
			mid := strings.TrimSuffix(strings.TrimPrefix(r, "**/"), "/**")
			for _, part := range strings.Split(rel, "/") {
				if ok, _ := path.Match(mid, part); ok {
					return true
				}
			}
			continue
		}
		if ok, _ := path.Match(r, rel); ok {
			return true
		}
		if ok, _ := path.Match(r, path.Base(rel)); ok {
			return true
		}
	}
	return false
}

// MatchInclude 扩展名白名单过滤，空列表表示不限制
func MatchInclude(ext string, include []string) bool {
	if len(include) == 0 {
		return true
	}
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	for _, e := range include {
		if ext == strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), ".")) {
			return true
		}
	}
	return false
}
