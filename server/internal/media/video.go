// Package media 视频元数据采集（ffprobe）
package media

import (
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ffprobeBin 视频元数据依赖 ffprobe（随 ffmpeg 一起装）。
// 只在第一次用到时探测一次，之后复用。
var ffprobeBin string

// FFProbeAvailable 对外暴露：没有 ffprobe 时视频时间补采不可用，前端要提示。
func FFProbeAvailable() bool {
	return haveFFProbe()
}

func haveFFProbe() bool {
	if ffprobeBin != "" {
		return true
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		ffprobeBin = p
		return true
	}
	return false
}

// VideoMeta 从视频容器里读到的信息
type VideoMeta struct {
	TakenAt    *time.Time // 容器里的 creation_time（拍摄时间）
	DurationMs int        // 时长（毫秒）
	Width      int
	Height     int
}

// ffprobeOut 只声明我们关心的字段，其余忽略
type ffprobeOut struct {
	Streams []struct {
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Tags   tags   `json:"tags"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		Tags     tags   `json:"tags"`
	} `json:"format"`
}

type tags struct {
	CreationTime string `json:"creation_time"`
}

// VideoInfo 读取视频容器的拍摄时间、时长与分辨率。
// 失败一律返回 nil（调用方用文件 mtime 兜底），绝不因为元数据拖慢/中断扫描。
func VideoInfo(path string) *VideoMeta {
	if !haveFFProbe() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobeBin,
		"-v", "quiet", "-print_format", "json",
		"-show_entries", "format=duration:format_tags=creation_time:stream=width,height:stream_tags=creation_time",
		"-select_streams", "v:0",
		path)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var v ffprobeOut
	if err := json.Unmarshal(out, &v); err != nil {
		return nil
	}

	vm := &VideoMeta{}
	// 分辨率
	if len(v.Streams) > 0 {
		vm.Width = v.Streams[0].Width
		vm.Height = v.Streams[0].Height
	}
	// 时长：format.duration 是字符串秒数
	if d, err := strconv.ParseFloat(strings.TrimSpace(v.Format.Duration), 64); err == nil && d > 0 {
		vm.DurationMs = int(d * 1000)
	}
	// 拍摄时间：格式标签优先，其次视频流标签
	raw := v.Format.Tags.CreationTime
	if raw == "" && len(v.Streams) > 0 {
		raw = v.Streams[0].Tags.CreationTime
	}
	if t := parseCreationTime(raw); t != nil {
		vm.TakenAt = t
	}
	if vm.TakenAt == nil && vm.DurationMs == 0 && vm.Width == 0 {
		return nil
	}
	return vm
}

// parseCreationTime 兼容 ffprobe 常见的几种时间写法：
//   2026-09-16T10:20:30.000000Z   （最常见，UTC）
//   2026-09-16T10:20:30+08:00
//   2026-09-16 10:20:30           （部分老设备，本地时间无时区）
func parseCreationTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" || s == "0000-00-00T00:00:00.000000Z" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000000Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			// 解析出的年份明显不对（1970 前 / 2038 后）视为无效
			if t.Year() < 1971 || t.Year() > time.Now().Year()+1 {
				return nil
			}
			return &t
		}
	}
	if ffprobeDebug {
		log.Printf("[media] 无法解析视频拍摄时间: %q", s)
	}
	return nil
}

// ffprobeDebug 打开后会打印解析失败的时间串（默认关，避免扫描刷屏）
const ffprobeDebug = false
