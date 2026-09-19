// Package ai 本地离线 AI 模块（ONNX Runtime / ARM64 CPU 推理）
//
// 设计约束：
//  1. 完全离线，不联网、不上传任何图片；
//  2. 只保存特征向量（BLOB），不保存裁剪后的人脸图片；
//  3. 全局可关闭：ai.enabled=false 或编译未带 ai tag 时，模块自动停用且不阻塞启动；
//  4. 低优先级后台队列，ARM 设备限制 worker 数与内存。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/naspic/naspic/internal/config"
	"github.com/naspic/naspic/internal/model"
)

// ErrDisabled AI 模块未启用
var ErrDisabled = errors.New("ai: 模块未启用")

// FaceBox 人脸检测结果
type FaceBox struct {
	X         float32   `json:"x"`
	Y         float32   `json:"y"`
	W         float32   `json:"w"`
	H         float32   `json:"h"`
	Score     float32   `json:"score"`
	Embedding []float32 `json:"-"` // 128/512 维特征向量
}

// SceneLabel 场景分类结果
type SceneLabel struct {
	Label string  `json:"label"`
	Score float32 `json:"score"`
}

// Engine AI 推理引擎（由 engine_onnx.go / engine_stub.go 按 build tag 提供）
type Engine interface {
	Name() string
	DetectFaces(path string) ([]FaceBox, error)
	ClassifyScene(path string) ([]SceneLabel, error)
	Close()
}

// defaultEngine 由 build tag 文件注入
var defaultEngine Engine

// Service AI 服务
type Service struct {
	db     *gorm.DB
	cfg    *config.Config
	engine Engine
	queue  chan int64 // 待处理的 media_id
	quit   chan struct{}
}

// New 创建 AI 服务；未启用时返回 nil，调用方直接跳过
func New(db *gorm.DB, cfg *config.Config) *Service {
	if !cfg.AI.Enabled {
		log.Println("[ai] 模块已关闭（配置 ai.enabled=false）")
		return nil
	}
	if defaultEngine == nil {
		log.Println("[ai] 未编译 AI 后端（-tags ai），模块停用")
		return nil
	}
	if cfg.AI.Workers <= 0 {
		cfg.AI.Workers = 1
	}
	s := &Service{
		db: db, cfg: cfg, engine: defaultEngine,
		queue: make(chan int64, 1024),
		quit:  make(chan struct{}),
	}
	for i := 0; i < cfg.AI.Workers; i++ {
		go s.worker()
	}
	go s.feeder()
	log.Printf("[ai] 服务就绪 engine=%s workers=%d", s.engine.Name(), cfg.AI.Workers)
	return s
}

// Stop 停止 AI 服务
func (s *Service) Stop() {
	if s == nil {
		return
	}
	close(s.quit)
	if s.engine != nil {
		s.engine.Close()
	}
}

// feeder 定时把 ai_status=0 的媒体投入队列（低优先级：每 30s 补一批）
func (s *Service) feeder() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
			var ids []int64
			s.db.Model(&model.MediaFile{}).
				Where("ai_status = 0 AND deleted_at IS NULL AND media_type = 1").
				Order("id ASC").Limit(200).Pluck("id", &ids)
			for _, id := range ids {
				select {
				case s.queue <- id:
				default:
					return // 队列满，下一轮再补
				}
			}
		}
	}
}

// worker 消费队列
func (s *Service) worker() {
	for {
		select {
		case <-s.quit:
			return
		case id := <-s.queue:
			if err := s.process(id); err != nil {
				log.Printf("[ai] 处理 media %d 失败: %v", id, err)
				s.db.Model(&model.MediaFile{}).Where("id = ?", id).Update("ai_status", 3)
			}
		}
	}
}

// process 处理单张图片
func (s *Service) process(mediaID int64) error {
	var m model.MediaFile
	if err := s.db.First(&m, mediaID).Error; err != nil {
		return err
	}
	// 需要一个可读的绝对路径：由调用方在入队前写入 ExifJSON 旁的路径不可靠，
	// 因此这里通过 media_id 反查驱动路径的能力交给上层；此处仅处理已解析的场景。
	path, err := s.resolvePath(m)
	if err != nil || path == "" {
		s.db.Model(&model.MediaFile{}).Where("id = ?", m.ID).Update("ai_status", 2)
		return nil
	}

	now := time.Now()

	// ① 人脸检测 + 特征向量
	if s.cfg.AI.DetectFace {
		faces, err := s.engine.DetectFaces(path)
		if err == nil {
			for _, f := range faces {
				bbox, _ := json.Marshal([4]float32{f.X, f.Y, f.W, f.H})
				s.db.Create(&model.Face{
					MediaID: m.ID, BBoxJSON: string(bbox), Score: f.Score,
					Embedding: floatsToBytes(f.Embedding), CreatedAt: &now,
				})
			}
		}
	}

	// ② 场景分类
	if s.cfg.AI.SceneModel != "" {
		labels, err := s.engine.ClassifyScene(path)
		if err == nil {
			for _, l := range labels {
				if l.Score < 0.35 { // 低置信度不入库，避免标签污染
					continue
				}
				s.db.Create(&model.MediaScene{
					MediaID: m.ID, SceneLabel: l.Label, Score: l.Score, CreatedAt: &now,
				})
			}
		}
	}

	s.db.Model(&model.MediaFile{}).Where("id = ?", m.ID).Update("ai_status", 1)
	return nil
}

// resolvePath 由上层注入路径解析器（扫描/上传时已知绝对路径）
var pathResolver func(m model.MediaFile) (string, error)

// SetPathResolver 注册媒体绝对路径解析器
func SetPathResolver(fn func(m model.MediaFile) (string, error)) { pathResolver = fn }

func (s *Service) resolvePath(m model.MediaFile) (string, error) {
	if pathResolver == nil {
		return "", errors.New("未注册路径解析器")
	}
	return pathResolver(m)
}

// Cluster 人脸聚类（贪心 + 余弦相似度）
// 阈值 0.6：同一人的不同照片一般 > 0.6；不同人一般 < 0.5
func (s *Service) Cluster(userID int64, threshold float32) (int, error) {
	if s == nil {
		return 0, ErrDisabled
	}
	if threshold <= 0 {
		threshold = 0.6
	}
	var faces []model.Face
	if err := s.db.Where("cluster_id IS NULL").Order("id ASC").Find(&faces).Error; err != nil {
		return 0, err
	}
	clusters := 0
	for _, f := range faces {
		emb := bytesToFloats(f.Embedding)
		if len(emb) == 0 {
			continue
		}
		// 在已有簇里找最相似的
		var best struct {
			ID   int64
			Sim  float32
		}
		var cs []model.FaceCluster
		s.db.Where("user_id = ?", userID).Find(&cs)
		for _, c := range cs {
			var rep model.Face
			if s.db.Where("cluster_id = ?", c.ID).Order("score DESC").First(&rep).Error != nil {
				continue
			}
			sim := cosine(emb, bytesToFloats(rep.Embedding))
			if sim > best.Sim {
				best.Sim = sim
				best.ID = c.ID
			}
		}
		now := time.Now()
		if best.ID > 0 && best.Sim >= threshold {
			s.db.Model(&model.Face{}).Where("id = ?", f.ID).Update("cluster_id", best.ID)
			s.db.Model(&model.FaceCluster{}).Where("id = ?", best.ID).
				Updates(map[string]any{"face_count": gorm.Expr("face_count + 1"), "updated_at": now})
			continue
		}
		// 新建簇
		nc := model.FaceCluster{UserID: userID, Name: "", FaceCount: 1, Status: 0,
			CreatedAt: &now, UpdatedAt: &now}
		if err := s.db.Create(&nc).Error; err != nil {
			continue
		}
		s.db.Model(&model.Face{}).Where("id = ?", f.ID).Update("cluster_id", nc.ID)
		s.db.Model(&model.FaceCluster{}).Where("id = ?", nc.ID).Update("cover_face_id", f.ID)
		clusters++
	}
	return clusters, nil
}

// ---------- 向量工具 ----------

func cosine(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(float64(dot) / (math.Sqrt(float64(na)) * math.Sqrt(float64(nb))))
}

func floatsToBytes(f []float32) []byte {
	out := make([]byte, len(f)*4)
	for i, v := range f {
		bits := math.Float32bits(v)
		out[i*4] = byte(bits)
		out[i*4+1] = byte(bits >> 8)
		out[i*4+2] = byte(bits >> 16)
		out[i*4+3] = byte(bits >> 24)
	}
	return out
}

func bytesToFloats(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		bits := uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
		out[i] = math.Float32frombits(bits)
	}
	return out
}

// Status 供 Web 端展示
func (s *Service) Status() map[string]any {
	if s == nil {
		return map[string]any{"enabled": false, "reason": "未启用"}
	}
	return map[string]any{
		"enabled": true,
		"engine":  s.engine.Name(),
		"workers": s.cfg.AI.Workers,
		"pending": len(s.queue),
	}
}

var _ = context.Background
