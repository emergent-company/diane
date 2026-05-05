// Package ptr provides pointer and default-value helpers.
// Eliminates duplicate definitions across agent.go and registry.go.
package ptr

// Str returns a pointer to s, or nil if s is empty.
func Str(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Int returns a pointer to v.
func Int(v int) *int {
	return &v
}

// F32 returns a pointer to v.
func F32(v float32) *float32 {
	return &v
}

// OrDefault returns s if non-empty, otherwise def.
func OrDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// OrDefaultInt returns v if non-zero, otherwise def.
func OrDefaultInt(v, def int) int {
	if v != 0 {
		return v
	}
	return def
}

// SafeDeref dereferences p, returning zero-value on nil.
func SafeDeref[T ~int | ~int64](p *T) T {
	if p == nil {
		return 0
	}
	return *p
}
