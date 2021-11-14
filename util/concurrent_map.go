package util

import "sync"

type ConcurrentUintMap struct {
	data  map[uint]interface{}
	mutex *sync.RWMutex
}

func NewUintMap() *ConcurrentUintMap {
	return &ConcurrentUintMap{
		data:  make(map[uint]interface{}),
		mutex: new(sync.RWMutex),
	}
}

func (m *ConcurrentUintMap) Put(key uint, value interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data[key] = value
}

func (m *ConcurrentUintMap) Get(key uint) (interface{}, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	val, present := m.data[key]
	return val, present
}

type ConcurrentStringMap struct {
	data  map[string]interface{}
	mutex *sync.RWMutex
}

func NewStringMap() *ConcurrentStringMap {
	return &ConcurrentStringMap{
		data:  make(map[string]interface{}),
		mutex: new(sync.RWMutex),
	}
}

func (m *ConcurrentStringMap) Put(key string, value interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data[key] = value
}

func (m *ConcurrentStringMap) Get(key string) (interface{}, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	val, present := m.data[key]
	return val, present
}
