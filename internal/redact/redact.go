// Package redact masks secret values in anything the tool prints.
package redact

import (
	"sort"
	"strings"
	"sync"
)

var (
	mu      sync.RWMutex
	secrets []string
)

// Add registers values that must never appear in output. Short values are ignored
// to avoid mangling ordinary text.
func Add(values ...string) {
	mu.Lock()
	defer mu.Unlock()
	for _, v := range values {
		if len(v) >= 8 {
			secrets = append(secrets, v)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
}

// String replaces every registered secret in s with a mask.
func String(s string) string {
	mu.RLock()
	defer mu.RUnlock()
	for _, v := range secrets {
		s = strings.ReplaceAll(s, v, "********")
	}
	return s
}

// Mask shows only the edges of a value, for display of stored credentials.
func Mask(v string) string {
	if len(v) <= 8 {
		return "********"
	}
	return v[:4] + "…" + v[len(v)-4:]
}
