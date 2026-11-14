package backend

import (
	"onbo/internal/config"
	"time"
)

// Config contains HTTP-layer settings; persistence and sessions are injected separately.
type Config struct {
	DemoMode   bool
	StaticDir  string
	LLMMode    string
	API        config.API
	SessionTTL time.Duration
}
