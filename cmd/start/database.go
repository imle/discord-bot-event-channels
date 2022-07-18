package start

import (
	"github.com/xo/dburl"
	"xorm.io/xorm"

	_ "github.com/lib/pq"
)

type EngineConfig struct {
	URI *dburl.URL
}

func NewEngine(cfg EngineConfig) (xorm.EngineInterface, error) {
	return xorm.NewEngine(cfg.URI.Driver, cfg.URI.DSN)
}
