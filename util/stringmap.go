package util

import "sync"

// StringMap is a goroutine-safe map
type StringMap struct {
	m map[string]interface{}
	l sync.Mutex
}

func NewStringMap() *StringMap {
	return &StringMap{
		m: make(map[string]interface{}),
	}
}

func (sm *StringMap) Set(key string, value interface{}) {
	sm.l.Lock()
	defer sm.l.Unlock()
	sm.m[key] = value
}

func (sm *StringMap) Get(key string) interface{} {
	sm.l.Lock()
	defer sm.l.Unlock()
	return sm.m[key]
}

func (sm *StringMap) Has(key string) bool {
	sm.l.Lock()
	defer sm.l.Unlock()
	_, ok := sm.m[key]
	return ok
}

func (sm *StringMap) Remove(key string) {
	sm.l.Lock()
	defer sm.l.Unlock()
	delete(sm.m, key)
}

func (sm *StringMap) Size() int {
	sm.l.Lock()
	defer sm.l.Unlock()
	return len(sm.m)
}

func (sm *StringMap) Clear() {
	sm.l.Lock()
	defer sm.l.Unlock()
	sm.m = make(map[string]interface{})
}

func (sm *StringMap) Keys() []string {
	sm.l.Lock()
	defer sm.l.Unlock()

	keys := []string{}
	for k := range sm.m {
		keys = append(keys, k)
	}
	return keys
}

func (sm *StringMap) Values() []interface{} {
	sm.l.Lock()
	defer sm.l.Unlock()
	items := []interface{}{}
	for _, v := range sm.m {
		items = append(items, v)
	}
	return items
}

