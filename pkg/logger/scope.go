package logger

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
)

var tagCache sync.Map

type Scoped struct {
	tag string
}

func Scope(tag string) *Scoped {
	return &Scoped{tag: tag}
}

func Scopef(format string, args ...interface{}) *Scoped {
	return &Scoped{tag: fmt.Sprintf(format, args...)}
}

func (s *Scoped) Tag() string {
	return s.tag
}

func (s *Scoped) Info(message string) {
	emit(infoBadge, "", s.tag, message)
}

func (s *Scoped) Warn(message string) {
	emit(warnBadge, "WARN", s.tag, message)
}

func (s *Scoped) Error(message string) {
	emit(errorBadge, "ERRO", s.tag, message)
}

func (s *Scoped) Term(message string) {
	emit(termBadge, "", s.tag, message)
}

func (s *Scoped) Debug(message string) {
	if !debugMode {
		return
	}
	emit(debugBadge, "", s.tag, message)
}

func (s *Scoped) Infof(format string, args ...interface{}) {
	emit(infoBadge, "", s.tag, fmt.Sprintf(format, args...))
}

func (s *Scoped) Warnf(format string, args ...interface{}) {
	emit(warnBadge, "WARN", s.tag, fmt.Sprintf(format, args...))
}

func (s *Scoped) Errorf(format string, args ...interface{}) {
	emit(errorBadge, "ERRO", s.tag, fmt.Sprintf(format, args...))
}

func (s *Scoped) Debugf(format string, args ...interface{}) {
	if !debugMode {
		return
	}
	emit(debugBadge, "", s.tag, fmt.Sprintf(format, args...))
}

func callerTag(skipAboveSelf int) string {
	var pcs [1]uintptr
	if runtime.Callers(skipAboveSelf+2, pcs[:]) < 1 {
		return "?"
	}

	pc := pcs[0]
	if cached, ok := tagCache.Load(pc); ok {
		return cached.(string)
	}

	frame, _ := runtime.CallersFrames(pcs[:]).Next()
	tag := deriveTag(frame.Function)
	tagCache.Store(pc, tag)
	return tag
}

func deriveTag(fullName string) string {
	if fullName == "" {
		return "?"
	}

	if i := strings.LastIndex(fullName, "/"); i >= 0 {
		fullName = fullName[i+1:]
	}

	parts := strings.Split(fullName, ".")
	name := ""
	for _, part := range parts[1:] {
		if part == "" || strings.HasPrefix(part, "(") || isGeneratedPart(part) {
			continue
		}
		name = strings.TrimSuffix(part, "-fm")
	}

	if name == "" {
		return parts[0]
	}
	return name
}

func isGeneratedPart(part string) bool {
	for _, prefix := range []string{"func", "deferwrap", "gowrap"} {
		if rest, ok := strings.CutPrefix(part, prefix); ok && isDigits(rest) {
			return true
		}
	}
	return isDigits(part)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}
