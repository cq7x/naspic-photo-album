//go:build ai

package ai

// ONNX Runtime 推理后端（本地离线，ARM64 CPU）
// 依赖：
//   1. libonnxruntime.so（Microsoft 官方发布包，含 linux-aarch64 版本）
//   2. 模型文件放在 ${ai.model_dir}：
//        - face_detect.onnx   人脸检测（如 UltraFace / YuNet）
//        - face_embed.onnx    人脸特征（如 MobileFaceNet / ArcFace-MobileNetV2，输出 128/512 维）
//        - scene.onnx         场景分类（如 MobileNetV3-Small 微调）
//
// 编译：go build -tags ai ./cmd/server
// 运行：需设置 OrtLibPath（或把 so 放到 /usr/lib）

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"

	"github.com/naspic/naspic/internal/config"
)

func init() {
	// 允许通过环境变量指定 so 路径，找不到就交给库自己按默认名查找
	if p := os.Getenv("NASPIC_ORT_LIB"); p != "" {
		ort.SetSharedLibraryPath(p)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		log.Printf("[ai] ONNX Runtime 初始化失败，AI 停用: %v", err)
		return
	}
	defaultEngine = &onnxEngine{}
}

type onnxEngine struct {
	faceSession  *ort.Session[float32]
	embedSession *ort.Session[float32]
	sceneSession *ort.Session[float32]
	labels       []string
}

func (e *onnxEngine) Name() string { return "onnxruntime" }

// Init 由 main 调用，加载模型（失败则整个 AI 服务停用）
func (e *onnxEngine) Init(cfg config.AIConfig) error {
	dir := cfg.ModelDir
	if p := filepath.Join(dir, "face_detect.onnx"); fileExists(p) {
		s, err := ort.NewSession[float32](p, []string{"input"}, []string{"scores", "boxes"})
		if err != nil {
			return err
		}
		e.faceSession = s
	}
	if p := filepath.Join(dir, "face_embed.onnx"); fileExists(p) {
		s, err := ort.NewSession[float32](p, []string{"input"}, []string{"embedding"})
		if err != nil {
			return err
		}
		e.embedSession = s
	}
	if p := filepath.Join(dir, "scene.onnx"); fileExists(p) {
		s, err := ort.NewSession[float32](p, []string{"input"}, []string{"logits"})
		if err != nil {
			return err
		}
		e.sceneSession = s
		e.labels = loadLabels(filepath.Join(dir, "scene_labels.txt"))
	}
	return nil
}

func (e *onnxEngine) Close() {
	if e.faceSession != nil {
		e.faceSession.Destroy()
	}
	if e.embedSession != nil {
		e.embedSession.Destroy()
	}
	if e.sceneSession != nil {
		e.sceneSession.Destroy()
	}
	ort.DestroyEnvironment()
}

// DetectFaces 人脸检测 + 特征向量提取
func (e *onnxEngine) DetectFaces(path string) ([]FaceBox, error) {
	if e.faceSession == nil {
		return nil, ErrDisabled
	}
	// ① 预处理：缩放到 320x240（UltraFace 输入）
	vec, w, h, err := preprocess(path, 320, 240)
	if err != nil {
		return nil, err
	}
	input, err := ort.NewTensor(ort.NewShape(1, 3, 240, 320), vec)
	if err != nil {
		return nil, err
	}
	defer input.Destroy()

	scoresT, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 4420, 2))
	if err != nil {
		return nil, err
	}
	defer scoresT.Destroy()
	boxesT, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 4420, 4))
	if err != nil {
		return nil, err
	}
	defer boxesT.Destroy()

	if err := e.faceSession.Run([]ort.Value{input}, []ort.Value{scoresT, boxesT}); err != nil {
		return nil, err
	}

	scores := scoresT.GetData()
	boxes := boxesT.GetData()

	// ② 解析检测框（UltraFace 输出为归一化坐标）
	out := make([]FaceBox, 0, 4)
	for i := 0; i < 4420; i++ {
		score := scores[i*2+1]
		if score < 0.7 {
			continue
		}
		fb := FaceBox{
			X: clamp01(boxes[i*4]), Y: clamp01(boxes[i*4+1]),
			W: clamp01(boxes[i*4+2]) - clamp01(boxes[i*4]),
			H: clamp01(boxes[i*4+3]) - clamp01(boxes[i*4+1]),
			Score: score,
		}
		// ③ 裁剪并对齐后提取特征向量
		if e.embedSession != nil {
			if crop, ok := cropRGBA(path, fb); ok {
				ev, err := preprocessImage(crop, 112, 112)
				if err == nil {
					it, err := ort.NewTensor(ort.NewShape(1, 3, 112, 112), ev)
					if err == nil {
						ot, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 512))
						if err == nil {
							if err := e.embedSession.Run([]ort.Value{it}, []ort.Value{ot}); err == nil {
								emb := ot.GetData()
								fb.Embedding = normalize(emb)
							}
							ot.Destroy()
						}
						it.Destroy()
					}
				}
			}
		}
		out = append(out, fb)
		if len(out) >= 20 { // 单张最多 20 张脸，防止异常输出拖垮 ARM
			break
		}
	}
	_ = w
	_ = h
	return out, nil
}

// ClassifyScene 场景分类
func (e *onnxEngine) ClassifyScene(path string) ([]SceneLabel, error) {
	if e.sceneSession == nil {
		return nil, ErrDisabled
	}
	vec, _, _, err := preprocess(path, 224, 224)
	if err != nil {
		return nil, err
	}
	input, err := ort.NewTensor(ort.NewShape(1, 3, 224, 224), vec)
	if err != nil {
		return nil, err
	}
	defer input.Destroy()

	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(len(e.labels))))
	if err != nil {
		return nil, err
	}
	defer out.Destroy()

	if err := e.sceneSession.Run([]ort.Value{input}, []ort.Value{out}); err != nil {
		return nil, err
	}
	logits := out.GetData()
	probs := softmax(logits)

	res := make([]SceneLabel, 0, 3)
	for i, p := range probs {
		if p < 0.2 {
			continue
		}
		label := "unknown"
		if i < len(e.labels) {
			label = e.labels[i]
		}
		res = append(res, SceneLabel{Label: label, Score: p})
		if len(res) >= 3 {
			break
		}
	}
	return res, nil
}

// ---------- 图像预处理 ----------

// preprocess 读取图片并按 NCHW + 归一化输出
func preprocess(path string, w, h int) ([]float32, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, 0, 0, err
	}
	return preprocessImage(img, w, h), img.Bounds().Dx(), img.Bounds().Dy(), nil
}

func preprocessImage(img image.Image, w, h int) []float32 {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)

	// NCHW：mean=127, scale=1/128
	out := make([]float32, 3*w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := dst.At(x, y).RGBA()
			i := y*w + x
			out[i] = float32(r>>8-127) / 128
			out[w*h+i] = float32(g>>8-127) / 128
			out[2*w*h+i] = float32(b>>8-127) / 128
		}
	}
	return out
}

// cropRGBA 按归一化框裁剪人脸区域
func cropRGBA(path string, fb FaceBox) (image.Image, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, false
	}
	b := img.Bounds()
	x0, y0 := int(fb.X*float32(b.Dx())), int(fb.Y*float32(b.Dy()))
	x1, y1 := int((fb.X+fb.W)*float32(b.Dx())), int((fb.Y+fb.H)*float32(b.Dy()))
	if x1 <= x0 || y1 <= y0 || x0 < 0 || y0 < 0 {
		return nil, false
	}
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		return si.SubImage(image.Rect(x0, y0, x1, y1)), true
	}
	return nil, false
}

func normalize(v []float32) []float32 {
	var s float32
	for _, x := range v {
		s += x * x
	}
	if s == 0 {
		return v
	}
	n := float32(math.Sqrt(float64(s)))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

func softmax(v []float32) []float32 {
	max := float32(0)
	for _, x := range v {
		if x > max {
			max = x
		}
	}
	out := make([]float32, len(v))
	var sum float32
	for i, x := range v {
		out[i] = float32(math.Exp(float64(x - max)))
		sum += out[i]
	}
	if sum > 0 {
		for i := range out {
			out[i] /= sum
		}
	}
	return out
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func loadLabels(p string) []string {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			out = append(out, string(b[start:i]))
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, string(b[start:]))
	}
	return out
}
