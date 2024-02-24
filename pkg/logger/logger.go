package base

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"runtime"
	"sort"

	"io"

	"github.com/natefinch/lumberjack"
	log "github.com/sirupsen/logrus"
)

type LoggerConfig struct {
	Level      string `json:"level" yaml:"level"`
	LogPath    string `json:"logPath" yaml:"log_path"`
	MaxSize    int    `json:"maxSize" yaml:"max_size"`
	MaxBackups int    `json:"maxBackups" yaml:"max_backups"`
	MaxAge     int    `json:"maxAge" yaml:"max_age"`
	Commpress  bool   `json:"commpress" yaml:"commpress"`
}

var (
	basicLogger = log.New()
)

type Logger log.Entry

func init() {
	//formatter := &log.TextFormatter{DisableColors:true, FullTimestamp:true, TimestampFormat:"01-02 15:04:05.000"}
	//formatter = &log.TextFormatter{}
	formatter := &log.JSONFormatter{TimestampFormat: "15:04:05.000"}
	//formatter := &MyFomatter{}
	formatter.TimestampFormat = "2006-01-02 15:04:05.000"
	basicLogger.Level = log.DebugLevel
	log.SetFormatter(formatter)
	basicLogger.Formatter = formatter
}

func NewConfig() *LoggerConfig {
	return &LoggerConfig{}
}

func SetLoggerConfig(config *LoggerConfig) {
	if config == nil {
		return
	}

	// 日志级别
	if len(config.Level) != 0 {
		level, err := log.ParseLevel(config.Level)
		if err == nil {
			SetLogLevel(level)
		}
	}

	// 日志文件路径
	if len(config.LogPath) == 0 {
		return
	}
	// 日志分割
	if config.MaxSize != 0 || config.MaxBackups != 0 || config.MaxAge != 0 || config.Commpress {
		SetLogPathWithRolling(config.LogPath, config.MaxSize, config.MaxBackups, config.MaxAge, config.Commpress)
	} else {
		SetLogPath(config.LogPath)
	}
}

func SetLogPathWithRolling(logPath string, maxSize, maxBackups, maxAge int, commpress bool) {
	if maxSize <= 0 {
		maxSize = 100 // 100M
	}
	if maxBackups <= 0 {
		maxBackups = 7
	}
	if maxAge <= 0 {
		maxAge = 28
	}

	if len(logPath) != 0 {
		//base.SetLogOutput(logFile)
		SetLogOutput(&lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    maxSize,    // megabytes
			MaxBackups: maxBackups, // files
			MaxAge:     maxAge,     // days
			Compress:   commpress,  // disabled by default
		})
		Info(log.Fields{
			"logPath":    logPath,
			"maxSize":    maxSize,
			"maxBackups": maxBackups,
			"maxAge":     maxAge,
			"commpress":  commpress,
		})
	} else {
		Info("Failed to log to file, using default stderr")
	}
}

func SetLogPath(logPath string) {
	logFile, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		SetLogOutput(logFile)
		Info(log.Fields{
			"logPath": logPath,
		})
	} else {
		Info("Failed to log to file, using default stderr")
	}
}

func SetLogOutput(o io.Writer) {
	basicLogger.Out = o
}

func SetLogLevel(l log.Level) {
	basicLogger.Level = l
}

type Debugger interface {
	Debug(v ...interface{})
	Debugf(format string, v ...interface{})
	Info(v ...interface{})
	Infof(format string, v ...interface{})
	Warn(v ...interface{})
	Warnf(format string, v ...interface{})
	Error(v ...interface{})
	Errorf(format string, v ...interface{})
}

type GameDebugger interface {
	Debugf(format string, v ...interface{})
	Debug(v ...interface{})
}

func NewLogger() *BasicLogger {
	return &BasicLogger{
		Logger: basicLogger,
	}
}

// BasicLogger 不需打印src
type BasicLogger struct {
	*log.Logger
}

func PrintRaw(content string) {
	basicLogger.Println(content)
}

func GetLogger() *log.Entry {
	_, file, line, _ := runtime.Caller(2)
	return basicLogger.WithField("__src", fmt.Sprintf("%s:%d", path.Base(file), line))
}

func Debug(v ...interface{}) {
	if basicLogger.Level < log.DebugLevel {
		return
	}
	GetLogger().Debug(fmt.Sprint(v...))
}

func Debugf(format string, v ...interface{}) {
	if basicLogger.Level < log.DebugLevel {
		return
	}
	GetLogger().Debugf(format, v...)
}

func Info(v ...interface{}) {
	if basicLogger.Level < log.InfoLevel {
		return
	}
	GetLogger().Info(v...)
}

func Infof(format string, v ...interface{}) {
	if basicLogger.Level < log.InfoLevel {
		return
	}
	GetLogger().Infof(format, v...)
}

func Warn(v ...interface{}) {
	if basicLogger.Level < log.WarnLevel {
		return
	}
	GetLogger().Warn(v...)
}

func Warnf(format string, v ...interface{}) {
	if basicLogger.Level < log.WarnLevel {
		return
	}
	GetLogger().Warnf(format, v...)
}

func Error(v ...interface{}) {
	if basicLogger.Level < log.ErrorLevel {
		return
	}
	GetLogger().Error(v...)
}

func Errorf(format string, v ...interface{}) {
	if basicLogger.Level < log.ErrorLevel {
		return
	}
	GetLogger().Errorf(format, v...)
}

func Fatal(v ...interface{}) {
	if basicLogger.Level < log.FatalLevel {
		return
	}
	GetLogger().Fatal(v...)
}

func Fatalf(format string, v ...interface{}) {
	if basicLogger.Level < log.FatalLevel {
		return
	}
	GetLogger().Fatalf(format, v...)
}

func Panic(v ...interface{}) {
	if basicLogger.Level < log.PanicLevel {
		return
	}
	GetLogger().Panic(v...)
}

func Panicf(format string, v ...interface{}) {
	if basicLogger.Level < log.PanicLevel {
		return
	}
	GetLogger().Panicf(format, v...)
}

func WithSrc(entry *log.Entry) *log.Entry {
	_, file, line, _ := runtime.Caller(2)
	return entry.WithField("src", fmt.Sprintf("%s:%d", path.Base(file), line))
}

type MyFomatter struct {
	log.TextFormatter
}

func (f *MyFomatter) Format(entry *log.Entry) ([]byte, error) {
	var src string
	if value, ok := entry.Data["__src"]; ok {
		src = value.(string)
		delete(entry.Data, "__src")
	}
	var ctx string
	if value, ok := entry.Data["__ctx"]; ok {
		ctx = value.(string)
		delete(entry.Data, "__ctx")
	}

	var b *bytes.Buffer
	var keys []string = make([]string, 0, len(entry.Data))
	for k := range entry.Data {
		keys = append(keys, k)
	}

	if !f.DisableSorting {
		sort.Strings(keys)
	}
	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	prefixFieldClashes(entry.Data)

	timestampFormat := f.TimestampFormat

	if !f.DisableTimestamp {
		f.appendValue(b, entry.Time.Format(timestampFormat))
	}

	f.appendValue(b, printLogLevel(entry.Level))

	if ctx != "" {
		f.appendValue(b, ctx)
	}
	if src != "" {
		f.appendValue(b, src)
	}

	if entry.Message != "" {
		f.appendValue(b, entry.Message)
	}

	for _, key := range keys {
		f.appendKeyValue(b, key, entry.Data[key])
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

func printLogLevel(level log.Level) string {
	switch level {
	case log.DebugLevel:
		return "DEBUG"
	case log.InfoLevel:
		return "INFO "
	case log.WarnLevel:
		return "WARN "
	case log.ErrorLevel:
		return "ERROR"
	case log.FatalLevel:
		return "FATAL"
	case log.PanicLevel:
		return "PANIC"
	}

	return "unknown"
}

func prefixFieldClashes(data log.Fields) {
	if t, ok := data["time"]; ok {
		data["fields.time"] = t
	}

	if m, ok := data["msg"]; ok {
		data["fields.msg"] = m
	}

	if l, ok := data["level"]; ok {
		data["fields.level"] = l
	}
}

func needsQuoting(text string) bool {
	return false
	// for _, ch := range text {
	// 	if !((ch >= 'a' && ch <= 'z') ||
	// 		(ch >= 'A' && ch <= 'Z') ||
	// 		(ch >= '0' && ch <= '9') ||
	// 		ch == '-' || ch == '.') {
	// 		return true
	// 	}
	// }
	// return false
}

func (f *MyFomatter) appendValue(b *bytes.Buffer, value interface{}) {
	switch value := value.(type) {
	case string:
		b.WriteString(value)
	case error:
		errmsg := value.Error()
		if !needsQuoting(errmsg) {
			b.WriteString(errmsg)
		} else {
			fmt.Fprintf(b, "%q", value)
		}
	default:
		fmt.Fprint(b, value)
	}

	b.WriteByte(' ')
}

func (f *MyFomatter) appendKeyValue(b *bytes.Buffer, key string, value interface{}) {

	b.WriteString(key)
	b.WriteByte('=')

	switch value := value.(type) {
	case string:
		if !needsQuoting(value) {
			b.WriteString(value)
		} else {
			fmt.Fprintf(b, "%q", value)
		}
	case error:
		errmsg := value.Error()
		if !needsQuoting(errmsg) {
			b.WriteString(errmsg)
		} else {
			fmt.Fprintf(b, "%q", value)
		}
	default:
		fmt.Fprint(b, value)
	}

	b.WriteByte(' ')
}
