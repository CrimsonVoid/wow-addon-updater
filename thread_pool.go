package main

import (
	"sync"
)

// spawnTaskPool creates a pool of `threads` goroutines to process tasks supporting `taskCapacity`
// number of pending tasks. Tasks can be queued by sending functions over `tasks`
//
// close `tasks` to finish processing all remaining tasks and exit
// call `cancel` to stop processing tasks immediately
func spawnTaskPool(threads int, taskCapacity int) (tasks chan<- func(), cancel func()) {
	tasksCh := make(chan func(), taskCapacity)
	cancelCh := make(chan struct{})
	cancelFn := func() { close(cancelCh) }

	for range threads {
		go func() {
			for {
				select {
				case <-cancelCh:
					return
				case task, ok := <-tasksCh:
					if !ok {
						return
					}
					if task != nil {
						task()
					}
				}
			}
		}()
	}

	return tasksCh, cancelFn
}

// spawnTaskPool creates a pool of goroutines to process tasks concurrently. Tasks can be queued by
// sending functions over `tasks` with task output sent back over `results`.
//
// spawn `threads` goroutines to execute incoming tasks with a queue size of `taskCapacity`
// close `tasks` to finish processing all remaining tasks and exit
// call `cancel` to stop processing tasks immediately
// task function output is sent over `results` (results may be out of order)
func spawnTaskResPool[R any](threads int, taskCapacity int) (tasks chan<- func() R, results <-chan R, cancel func()) {
	tasksCh := make(chan func() R, taskCapacity)
	resultsCh := make(chan R, taskCapacity)
	cancelCh := make(chan struct{})
	cancelFn := func() { close(cancelCh) }

	doneWg := &sync.WaitGroup{}
	doneWg.Add(threads)

	for range threads {
		go func() {
			defer doneWg.Done()

			for {
				select {
				case <-cancelCh:
					return
				case task, ok := <-tasksCh:
					// tasks closed
					if !ok {
						return
					}
					if task == nil {
						continue
					}

					res := task()
					resultsCh <- res
				}
			}
		}()
	}

	go func() {
		doneWg.Wait()
		close(resultsCh)
	}()

	return tasksCh, resultsCh, cancelFn
}
