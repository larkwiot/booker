package util

import (
	"sync"
	"sync/atomic"
	"time"
)

type ThreadPool struct {
	Size  int64
	Count atomic.Int64
	wait  sync.WaitGroup
}

func (pool *ThreadPool) StartThread() {
	for pool.Count.Load() >= pool.Size {
		time.Sleep(500 * time.Millisecond)
	}
	pool.wait.Add(1)
	pool.Count.Add(1)
}

func (pool *ThreadPool) StopThread() {
	pool.Count.Add(-1)
	pool.wait.Done()
}

func (pool *ThreadPool) Wait() {
	pool.wait.Wait()
}
