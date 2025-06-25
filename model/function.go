package model

import (
	"time"
)

type Function struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	BinPath     string    `json:"binPath"`
	WasmPath    string    `json:"wasmPath"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
