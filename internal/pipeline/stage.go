package pipeline

import (
	"fmt"
	"github.com/larkwiot/booker/internal/util"
	"sync"
)

type Stage struct {
	Name   string
	pool   util.ThreadPool
	worker func(any) (any, error)
	quit   chan struct{}
}

func NewStage(name string, poolSize int64, worker func(any) (any, error)) *Stage {
	s := &Stage{
		Name:   name,
		pool:   util.ThreadPool{Size: poolSize + 1},
		worker: worker,
		quit:   make(chan struct{}),
	}

	return s
}

func (s *Stage) Wait() {
	s.pool.Wait()
}

func (s *Stage) Close() {
	close(s.quit)
	s.pool.Wait()
}

func (s *Stage) Run(input chan any, output chan any, failHandler func(any, error)) {
	s.pool.StartThread()
	defer s.pool.StopThread()

	work := func(i any) {
		s.pool.StartThread()
		defer s.pool.StopThread()
		result, err := s.worker(i)
		if result == nil || err != nil {
			failHandler(i, err)
			return
		}
		output <- result
	}

	for {
		select {
		case i, isOpen := <-input:
			if !isOpen {
				return
			}

			go work(i)
		case <-s.quit:
			return
		}
	}
}

func (s *Stage) Status() string {
	return fmt.Sprintf("%s %d", s.Name, s.pool.Count.Load())
}

type CollectorStage struct {
	collector func(any)
	wait      sync.WaitGroup
	count     uint64
}

func NewCollectorStage(collector func(any)) *CollectorStage {
	return &CollectorStage{
		collector: collector,
	}
}

func (s *CollectorStage) Run(input chan any) {
	s.wait.Add(1)
	defer s.wait.Done()

	for {
		output, isOpen := <-input
		if !isOpen {
			return
		}
		s.count++
		s.collector(output)
	}
}

func (s *CollectorStage) Wait() {
	s.wait.Wait()
}

func (s *CollectorStage) Close() {
	s.count = 0
}

func (s *CollectorStage) Status() string {
	return fmt.Sprintf("collected %d", s.count)
}
