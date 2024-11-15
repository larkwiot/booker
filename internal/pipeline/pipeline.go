package pipeline

import (
	"fmt"
	"github.com/larkwiot/booker/internal/util"
	"log"
	"strings"
	"sync/atomic"
	"time"
)

type stageDescription struct {
	Name   string
	Worker func(any) (any, error)
}

type Pipeline struct {
	Frontend          chan any
	stageDescriptions []stageDescription
	stages            []*Stage
	channels          []chan any
	Backend           chan any
	TotalThreadCount  int64
	collector         *CollectorStage
	status            <-chan time.Time
	failCount         atomic.Int64
}

func NewPipeline(totalThreadCount int64) *Pipeline {
	return &Pipeline{
		stageDescriptions: []stageDescription{},
		TotalThreadCount:  totalThreadCount,
		Frontend:          make(chan any),
		Backend:           make(chan any),
		status:            time.Tick(100 * time.Millisecond),
	}
}

func (p *Pipeline) AppendStage(name string, worker func(any) (any, error)) {
	p.stageDescriptions = append(p.stageDescriptions, stageDescription{Name: name, Worker: worker})
}

func (p *Pipeline) CollectorStage(collector func(any)) {
	p.collector = NewCollectorStage(collector)
}

func (p *Pipeline) Run(failHandler func(any, error)) {
	wrappedFailHandler := func(a any, err error) {
		p.failCount.Add(1)
		failHandler(a, err)
	}

	if len(p.stageDescriptions) == 0 {
		log.Println("warning: pipeline not running because no stageDescriptions were specified")
		return
	}

	if len(p.stageDescriptions) == 1 {
		stageDesc := p.stageDescriptions[0]
		stage := NewStage(stageDesc.Name, p.TotalThreadCount, stageDesc.Worker)
		go stage.Run(p.Frontend, p.Backend, wrappedFailHandler)
		return
	}

	perStageThreadCount := p.TotalThreadCount / int64(len(p.stageDescriptions))

	var lastOutput = p.Frontend
	for i, stageDesc := range p.stageDescriptions {
		var output chan any
		if i == len(p.stageDescriptions)-1 {
			output = p.Backend
		} else {
			output = make(chan any)
			p.channels = append(p.channels, output)
		}

		stage := NewStage(stageDesc.Name, perStageThreadCount, stageDesc.Worker)

		go stage.Run(lastOutput, output, wrappedFailHandler)

		p.stages = append(p.stages, stage)

		lastOutput = output
	}

	if p.collector != nil {
		go p.collector.Run(p.Backend)
	}

	go func() {
		for {
			select {
			case _, isOpen := <-p.status:
				if !isOpen {
					fmt.Printf(util.ClearTermLineString())
					return
				}

				statuses := make([]string, 0)
				for _, stage := range p.stages {
					statuses = append(statuses, (*stage).Status())
				}
				if p.collector != nil {
					statuses = append(statuses, p.collector.Status())
				}
				statuses = append(statuses, fmt.Sprintf("failed %d", p.failCount.Load()))

				fmt.Printf("%sprocessing: %s", util.ClearTermLineString(), strings.Join(statuses, " -> "))
			}
		}
	}()
}

func (p *Pipeline) Wait() {
	for _, stage := range p.stages {
		stage.Wait()
	}
	if p.collector != nil {
		p.collector.Wait()
	}
}

func (p *Pipeline) Close() {
	close(p.Frontend)
	for _, stage := range p.stages {
		stage.Close()
	}
	for _, channel := range p.channels {
		close(channel)
	}
	close(p.Backend)
	if p.collector != nil {
		p.collector.Close()
	}
	fmt.Printf(util.ClearTermLineString())
}
