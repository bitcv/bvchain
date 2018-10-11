package util

import "sync"

// StringSet is goroutine-safe
type StringSet struct {
	st map[string]struct{}
	mx sync.Mutex
}

func NewStringSet() *StringSet {
	return &StringSet{st: make(map[string]struct{})}
}

func (ss *StringSet) Has(key string) bool {
	ss.mx.Lock()
	defer ss.mx.Unlock()
	_, ok := ss.st[key]
	return ok
}

func (ss *StringSet) Add(key string) {
	ss.mx.Lock()
	defer ss.mx.Unlock()
	ss.st[key] = struct{}{}
}

func (ss *StringSet) Remove(key string) {
	ss.mx.Lock()
	defer ss.mx.Unlock()
	delete(ss.st, key)
}

func (ss *StringSet) Size() int {
	ss.mx.Lock()
	defer ss.mx.Unlock()
	return len(ss.st)
}

func (ss *StringSet) Clear() {
	ss.mx.Lock()
	defer ss.mx.Unlock()
	ss.st = make(map[string]struct{})
}

func (ss *StringSet) Items() []string {
	ss.mx.Lock()
	defer ss.mx.Unlock()

	keys := make([]string, 0, len(ss.st))
	for k := range ss.st {
		keys = append(keys, k)
	}
	return keys
}

