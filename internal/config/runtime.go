package config

import (
	"encoding/json"
	"sync/atomic"
	"time"
)

type Snapshot struct {
	Config   *Config
	Source   string
	SHA256   string
	LoadedAt time.Time
	Raw      []byte
}

type Runtime struct {
	current atomic.Pointer[Snapshot]
}

func NewRuntime(snapshot *Snapshot) *Runtime {
	r := &Runtime{}
	if snapshot != nil {
		r.current.Store(snapshot)
	}
	return r
}

func (r *Runtime) Current() *Snapshot {
	return r.current.Load()
}

func (r *Runtime) Config() *Config {
	current := r.current.Load()
	if current == nil {
		return nil
	}
	return current.Config
}

func (r *Runtime) Update(snapshot *Snapshot) {
	r.current.Store(snapshot)
}

func (r *Runtime) MarshalCurrent() ([]byte, error) {
	current := r.current.Load()
	if current == nil {
		return nil, nil
	}
	return json.Marshal(current.Config)
}
