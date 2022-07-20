package start

import (
	"github.com/xo/dburl"
	"xorm.io/xorm"

	_ "github.com/lib/pq"
)

type EngineConfig struct {
	URI        *dburl.URL
	LogQueries bool
}

func NewEngine(cfg EngineConfig) (xorm.EngineInterface, error) {
	engine, err := xorm.NewEngine(cfg.URI.Driver, cfg.URI.DSN)
	engine.ShowSQL(cfg.LogQueries)
	return engine, err
}
