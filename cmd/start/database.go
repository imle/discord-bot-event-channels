package start

import (
	"fmt"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/xo/dburl"
	"xorm.io/xorm"
	"xorm.io/xorm/log"
)

type EngineConfig struct {
	URI        *dburl.URL
	LogQueries bool
}

func NewEngine(cfg EngineConfig, logger *logrus.Logger) (xorm.EngineInterface, error) {
	engine, err := xorm.NewEngine(cfg.URI.Driver, cfg.URI.DSN)
	engine.SetLogger(&xormLogrus{logger: logger, level: log.LOG_DEBUG})
	engine.ShowSQL(cfg.LogQueries)
	return engine, err
}

// xormLogrus is the logrus implement of ILogger
type xormLogrus struct {
	logger  *logrus.Logger
	level   log.LogLevel
	showSQL bool
}

func (l *xormLogrus) BeforeSQL(ctx log.LogContext) {}

func (l *xormLogrus) AfterSQL(ctx log.LogContext) {
	var sessionPart string
	v := ctx.Ctx.Value(log.SessionIDKey)
	if key, ok := v.(string); ok {
		sessionPart = fmt.Sprintf(" [%s]", key)
	}

	fields := logrus.Fields{
		"xorm.session": sessionPart,
		"sql":          ctx.SQL,
		"args":         ctx.Args,
		"duration":     ctx.ExecuteTime,
	}

	l.logger.WithFields(fields).Debugf("sql statement")
}

func (l *xormLogrus) Debug(v ...interface{}) {
	l.logger.Debug(v...)
}

func (l *xormLogrus) Debugf(format string, v ...interface{}) {
	l.logger.Debugf(format, v...)
}

func (l *xormLogrus) Error(v ...interface{}) {
	l.logger.Error(v...)
}

func (l *xormLogrus) Errorf(format string, v ...interface{}) {
	l.logger.Errorf(format, v...)
}

func (l *xormLogrus) Info(v ...interface{}) {
	l.logger.Info(v...)
}

func (l *xormLogrus) Infof(format string, v ...interface{}) {
	l.logger.Infof(format, v...)
}

func (l *xormLogrus) Warn(v ...interface{}) {
	l.logger.Warn(v...)
}

func (l *xormLogrus) Warnf(format string, v ...interface{}) {
	l.logger.Warnf(format, v...)
}

func (l *xormLogrus) Level() log.LogLevel {
	return l.level
}

func (l *xormLogrus) SetLevel(lvl log.LogLevel) {
	l.level = lvl
}

func (l *xormLogrus) ShowSQL(show ...bool) {
	if len(show) == 0 {
		l.showSQL = true
		return
	}
	l.showSQL = show[0]
}

func (l *xormLogrus) IsShowSQL() bool {
	return l.showSQL
}
