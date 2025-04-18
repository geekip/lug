package util

import (
	"errors"
	"html/template"
	"strings"
	"sync"
)

type templateEntry struct {
	once sync.Once
	tmpl *template.Template
	err  error
}

var templateCache sync.Map

func ParseTemplateFiles(paths ...string) (*template.Template, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one template file path is required")
	}
	key := strings.Join(paths, "\x00")
	entry := getTemplateEntry(key)
	entry.once.Do(func() {
		entry.tmpl, entry.err = template.ParseFiles(paths...)
	})
	return entry.tmpl, entry.err
}

func ParseTemplateString(str, cacheKey string) (*template.Template, error) {
	if cacheKey != "" {
		entry := getTemplateEntry(cacheKey)
		entry.once.Do(func() {
			entry.tmpl, entry.err = template.New(cacheKey).Parse(str)
		})
		return entry.tmpl, entry.err
	}
	return template.New("").Parse(str)
}

func getTemplateEntry(key string) *templateEntry {
	entryInterface, _ := templateCache.LoadOrStore(key, &templateEntry{})
	return entryInterface.(*templateEntry)
}
