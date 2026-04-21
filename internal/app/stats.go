package app

import "sync"

// QueryStats tracks LLM call latency and cache hit/miss counts for observability.
type QueryStats struct {
	mu           sync.Mutex
	LLMCallCount int64
	LLMTotalMs   int64
	CacheHits    int64
	CacheMisses  int64
}

func (s *QueryStats) recordLLMCall(ms int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LLMCallCount++
	s.LLMTotalMs += ms
}

func (s *QueryStats) recordCacheHit() {
	s.mu.Lock()
	s.CacheHits++
	s.mu.Unlock()
}

func (s *QueryStats) recordCacheMiss() {
	s.mu.Lock()
	s.CacheMisses++
	s.mu.Unlock()
}
