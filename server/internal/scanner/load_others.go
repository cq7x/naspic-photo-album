//go:build !linux

package scanner

// loadAvg1 非 Linux 平台无法读取 loadavg，返回 false 表示不做 CPU 退避
func loadAvg1() (float64, bool) { return 0, false }
