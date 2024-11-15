package service

import (
	"log"
	"sync"
	"time"
)

type Service interface {
	Name() string
	SelfCheck() (bool, string)
	HealthCheck() (bool, string)
}

type ServiceManager struct {
	services            []Service
	servicesLock        sync.RWMutex
	liveServices        map[string]Service
	liveServicesLock    sync.RWMutex
	healthCheckInterval time.Duration
	quit                chan struct{}
}

func NewServiceManager(healthCheckInterval time.Duration) *ServiceManager {
	svcmgr := &ServiceManager{
		services:            make([]Service, 0),
		servicesLock:        sync.RWMutex{},
		liveServices:        make(map[string]Service),
		liveServicesLock:    sync.RWMutex{},
		healthCheckInterval: healthCheckInterval,
		quit:                make(chan struct{}),
	}

	go svcmgr.watch()

	return svcmgr
}

func (dd *ServiceManager) Manage(service Service) {
	dd.servicesLock.Lock()
	defer dd.servicesLock.Unlock()
	dd.liveServicesLock.Lock()
	defer dd.liveServicesLock.Unlock()

	dd.services = append(dd.services, service)
	dd.liveServices[service.Name()] = service
}

func (dd *ServiceManager) Close() {
	close(dd.quit)
}

func (dd *ServiceManager) watch() {
	for {
		select {
		case <-dd.quit:
			return
		default:
		}

		time.Sleep(dd.healthCheckInterval)

		for _, service := range dd.services {
			up, reason := service.SelfCheck()
			if up {
				up, reason = service.HealthCheck()
			}

			if !up {
				log.Printf("warning: %s is down because: %s\n", service.Name(), reason)

				dd.liveServicesLock.Lock()
				delete(dd.liveServices, service.Name())
				dd.liveServicesLock.Unlock()
			}
		}
	}
}

func (dd *ServiceManager) GetLiveServices() []Service {
	dd.liveServicesLock.RLock()
	defer dd.liveServicesLock.RUnlock()
	services := make([]Service, 0)
	for _, service := range dd.liveServices {
		services = append(services, service)
	}
	return services
}
