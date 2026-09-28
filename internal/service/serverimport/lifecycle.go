package serverimport

import "context"

func (service *Service) Start(parent context.Context) {
	service.lifecycleMu.Lock()
	defer service.lifecycleMu.Unlock()
	if service.started || service.closed {
		return
	}
	service.started = true
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	service.cancel = cancel
	service.wait.Add(1)
	go func() { defer service.wait.Done(); service.runLoop(ctx) }()
	service.signal()
}

func (service *Service) Close() {
	service.Stop()
	service.Wait()
}

func (service *Service) Stop() {
	service.lifecycleMu.Lock()
	if !service.closed {
		service.closed = true
		close(service.stop)
		if service.cancel != nil {
			service.cancel()
		}
	}
	service.lifecycleMu.Unlock()
}

func (service *Service) Wait() { service.wait.Wait() }
