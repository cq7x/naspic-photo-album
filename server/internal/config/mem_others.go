//go:build !linux

package config

// totalMemoryGB 非 Linux 平台不做精确探测，返回 0（调用方回落到 mid 档）。
// 如需精确档位，显式配置 NASPIC_TIER=low|mid|high 即可。
func totalMemoryGB() int { return 0 }
