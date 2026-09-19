//go:build !ai

package ai

// stubEngine 未编译 AI 后端时的占位实现。
// 目的：不带 -tags ai 构建的二进制仍能正常启动，AI 功能自动停用。
func init() { defaultEngine = nil }

type stubEngine struct{}

func (e *stubEngine) Name() string { return "stub" }
func (e *stubEngine) DetectFaces(path string) ([]FaceBox, error) {
	return nil, ErrDisabled
}
func (e *stubEngine) ClassifyScene(path string) ([]SceneLabel, error) {
	return nil, ErrDisabled
}
func (e *stubEngine) Close() {}
