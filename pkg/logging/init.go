package logging

import (
	"context"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Config struct {
	Logger LoggerConfig
}

type LoggerConfig struct {
	Debug      bool
	Level      string // debug/info/warn/error/dpanic/panic/fatal
	CallerSkip int
	File       struct {
		Enable     bool
		Path       string
		MaxSize    int
		MaxBackups int
	}
	Hooks []*HookConfig
}

type HookConfig struct {
	Enable    bool
	Level     string
	Type      string // gorm
	MaxBuffer int
	MaxThread int
	Options   map[string]string
	Extra     map[string]string
}

type HookHandlerFunc func(ctx context.Context, hookCfg *HookConfig) (*Hook, error)

func LoadConfigFromToml(filename string) (*LoggerConfig, error) {
	cfg := &Config{}
	buf, err := os.ReadFile(filename) //nolint:gosec // Path from the configuration.
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(buf, cfg); err != nil {
		return nil, err
	}
	return &cfg.Logger, nil
}

func InitWithConfig(ctx context.Context, cfg *LoggerConfig, hookHandle ...HookHandlerFunc) (func(), error) {
	var zconfig zap.Config
	if cfg.Debug {
		cfg.Level = "debug"
		zconfig = zap.NewDevelopmentConfig()
	} else {
		zconfig = zap.NewProductionConfig()
	}

	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	zconfig.Level.SetLevel(level)

	var (
		logger   *zap.Logger
		cleanFns []func()
	)

	if cfg.File.Enable {
		filename := cfg.File.Path
		if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
			return nil, err
		}
		fileWriter := &lumberjack.Logger{
			Filename:   filename,
			MaxSize:    cfg.File.MaxSize,
			MaxBackups: cfg.File.MaxBackups,
			Compress:   false,
			LocalTime:  true,
		}

		cleanFns = append(cleanFns, func() {
			_ = fileWriter.Close()
		})

		zc := zapcore.NewCore(
			zapcore.NewJSONEncoder(zconfig.EncoderConfig),
			zapcore.AddSync(fileWriter),
			zconfig.Level,
		)
		logger = zap.New(zc)
	} else {
		ilogger, err := zconfig.Build()
		if err != nil {
			return nil, err
		}
		logger = ilogger
	}

	// Callers log through the *zap.Logger returned by Context, so 0 reports
	// the calling line. Wrappers that log on behalf of their caller should
	// add their own AddCallerSkip.
	skip := max(cfg.CallerSkip, 0)

	logger = logger.WithOptions(
		zap.WithCaller(true),
		zap.AddStacktrace(zap.ErrorLevel),
		zap.AddCallerSkip(skip),
	)

	for _, h := range cfg.Hooks {
		if !h.Enable || len(hookHandle) == 0 {
			continue
		}

		writer, err := hookHandle[0](ctx, h)
		if err != nil {
			return nil, err
		} else if writer == nil {
			continue
		}

		cleanFns = append(cleanFns, func() {
			writer.Flush()
		})

		hookLevel := zap.NewAtomicLevel()
		if level, err := zapcore.ParseLevel(h.Level); err == nil {
			hookLevel.SetLevel(level)
		} else {
			hookLevel.SetLevel(zap.InfoLevel)
		}

		hookEncoder := zap.NewProductionEncoderConfig()
		hookEncoder.EncodeTime = zapcore.EpochMillisTimeEncoder
		hookEncoder.EncodeDuration = zapcore.MillisDurationEncoder
		hookCore := zapcore.NewCore(
			zapcore.NewJSONEncoder(hookEncoder),
			zapcore.AddSync(writer),
			hookLevel,
		)

		logger = logger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, hookCore)
		}))
	}

	zap.ReplaceGlobals(logger)
	return func() {
		for _, fn := range cleanFns {
			fn()
		}
	}, nil
}
