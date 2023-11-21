package grace

import "sync"

type WorkItem func() error

type Workgroup struct {
	wg *sync.WaitGroup

	workstream chan WorkItem
}

func NewWorkgroup(cap int) Workgroup {
	wg := &sync.WaitGroup{}
	result := Workgroup{
		workstream: make(chan WorkItem, cap),
		wg:         wg,
	}

	wg.Add(cap)
	for i := 0; i < cap; i++ {
		go result.run()
	}

	return result
}

func (w *Workgroup) Go(work WorkItem) {
	w.workstream <- work
}

func (w *Workgroup) Wait() {
	close(w.workstream)

	w.wg.Wait()
}

func (w *Workgroup) run() {
	for {
		work, ok := <-w.workstream
		if !ok {
			w.wg.Done()
			return
		}

		// TODO: Get error and handle it :)
		work()
	}
}
