//go:build linux

package config

import (
	"bufio"
	"os"
	"strings"
)

// totalMemoryGB 从 /proc/meminfo 读取总内存（GB），失败返回 0
func totalMemoryGB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		var kb int64
		for _, r := range fields[1] {
			if r < '0' || r > '9' {
				break
			}
			kb = kb*10 + int64(r-'0')
		}
		return int(kb / 1024 / 1024)
	}
	return 0
}
