package cache

import (
	"container/list"
	"sync"
)

type LFU[K comparable, V any] struct {
	mu      sync.Mutex
	cap     int
	minFreq int
	keys    map[K]*list.Element
	freqs   map[int]*list.List
}

type lfuEntry[K comparable, V any] struct {
	key  K
	val  V
	freq int
}

func NewLFU[K comparable, V any](capacity int) *LFU[K, V] {
	return &LFU[K, V]{
		cap:   capacity,
		keys:  make(map[K]*list.Element),
		freqs: make(map[int]*list.List),
	}
}

func (c *LFU[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.keys[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.bump(el)
	return el.Value.(*lfuEntry[K, V]).val, true
}

func (c *LFU[K, V]) Put(key K, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cap <= 0 {
		return
	}
	if el, ok := c.keys[key]; ok {
		el.Value.(*lfuEntry[K, V]).val = val
		c.bump(el)
		return
	}
	if len(c.keys) >= c.cap {
		lst := c.freqs[c.minFreq]
		if lst != nil {
			if back := lst.Back(); back != nil {
				e := lst.Remove(back).(*lfuEntry[K, V])
				delete(c.keys, e.key)
				if lst.Len() == 0 {
					delete(c.freqs, c.minFreq)
				}
			}
		}
	}
	e := &lfuEntry[K, V]{key: key, val: val, freq: 1}
	lst := c.freqs[1]
	if lst == nil {
		lst = list.New()
		c.freqs[1] = lst
	}
	c.keys[key] = lst.PushFront(e)
	c.minFreq = 1
}

func (c *LFU[K, V]) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = make(map[K]*list.Element)
	c.freqs = make(map[int]*list.List)
	c.minFreq = 0
}

func (c *LFU[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.keys)
}

// bump moves el to the next frequency bucket.
func (c *LFU[K, V]) bump(el *list.Element) {
	e := el.Value.(*lfuEntry[K, V])
	old := e.freq
	lst := c.freqs[old]
	lst.Remove(el)
	if lst.Len() == 0 {
		delete(c.freqs, old)
		if c.minFreq == old {
			c.minFreq = old + 1
		}
	}
	e.freq++
	next := c.freqs[e.freq]
	if next == nil {
		next = list.New()
		c.freqs[e.freq] = next
	}
	c.keys[e.key] = next.PushFront(e)
}
