package storage

import "sync"

// Registry 按相册库 ID 管理驱动实例
type Registry struct {
	mu sync.RWMutex
	m  map[int64]StorageDriver
}

func NewRegistry() *Registry { return &Registry{m: make(map[int64]StorageDriver)} }

func (r *Registry) Register(libraryID int64, d StorageDriver) {
	r.mu.Lock()
	r.m[libraryID] = d
	r.mu.Unlock()
}

func (r *Registry) Get(libraryID int64) (StorageDriver, bool) {
	r.mu.RLock()
	d, ok := r.m[libraryID]
	r.mu.RUnlock()
	return d, ok
}

func (r *Registry) Remove(libraryID int64) {
	r.mu.Lock()
	delete(r.m, libraryID)
	r.mu.Unlock()
}

func (r *Registry) All() map[int64]StorageDriver {
	r.mu.RLock()
	cp := make(map[int64]StorageDriver, len(r.m))
	for k, v := range r.m {
		cp[k] = v
	}
	r.mu.RUnlock()
	return cp
}
