package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// hashWriter 边写盘边算 SHA-256，避免二次读文件（ARM 设备 IO 很贵）
type hashWriter struct {
	w io.Writer
	h hash.Hash
	n int64
}

func (hw *hashWriter) Write(p []byte) (int, error) {
	if hw.h == nil {
		hw.h = sha256.New()
	}
	n, err := hw.w.Write(p)
	hw.n += int64(n)
	hw.h.Write(p[:n])
	return n, err
}

func (hw *hashWriter) hex() string {
	if hw.h == nil {
		hw.h = sha256.New()
	}
	return hex.EncodeToString(hw.h.Sum(nil))
}
