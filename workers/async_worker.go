package workers

import (
	"log"
	"sync"

	"ai-generator/utils"
)

type Task func()

type AsyncWorkerPool struct {
	logger     *log.Logger
	workerSize int
	queueSize  int
	tasks      chan Task
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

func NewAsyncWorkerPool(workerSize, queueSize int, logger *log.Logger) *AsyncWorkerPool {
	if workerSize < 1 {
		workerSize = 1
	}
	if queueSize < workerSize {
		queueSize = workerSize * 2
	}

	return &AsyncWorkerPool{
		logger:     logger,
		workerSize: workerSize,
		queueSize:  queueSize,
		tasks:      make(chan Task, queueSize),
		stopCh:     make(chan struct{}),
	}
}

func (p *AsyncWorkerPool) Start() {
	utils.LogFlow(p.logger, "[WRK]", "POOL", "starting workers=%d queue=%d", p.workerSize, p.queueSize)
	for i := 0; i < p.workerSize; i++ {
		p.wg.Add(1)
		go func(workerID int) {
			defer p.wg.Done()
			utils.LogFlow(p.logger, "[WRK]", "WORKER", "worker=%d started", workerID)
			for {
				select {
				case <-p.stopCh:
					utils.LogFlow(p.logger, "[WRK]", "WORKER", "worker=%d stopped", workerID)
					return
				case task := <-p.tasks:
					if task == nil {
						continue
					}
					utils.LogFlow(p.logger, "[WRK]", "TASK", "worker=%d executing task", workerID)
					task()
					utils.LogSuccess(p.logger, "TASK", "worker=%d task finished", workerID)
				}
			}
		}(i + 1)
	}
}

func (p *AsyncWorkerPool) Submit(task Task) bool {
	select {
	case p.tasks <- task:
		return true
	default:
		utils.LogError(p.logger, "QUEUE", "task queue full capacity=%d", p.queueSize)
		return false
	}
}

func (p *AsyncWorkerPool) Stop() {
	utils.LogFlow(p.logger, "[WRK]", "POOL", "stopping worker pool")
	close(p.stopCh)
	p.wg.Wait()
	utils.LogSuccess(p.logger, "POOL", "worker pool stopped")
}
