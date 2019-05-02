package cloudmap

import (
	"errors"
	"sync"
	"time"

	log "github.com/micro/go-log"
	"github.com/micro/go-micro/registry"
	hash "github.com/mitchellh/hashstructure"
)

type watcher struct {
	r    *cregistry
	wo   registry.WatchOptions
	next chan *registry.Result
	exit chan struct{}

	sync.RWMutex
	services  map[string][]*registry.Service
	instances map[string]map[string]*registry.Service
}

func (w *watcher) Next() (*registry.Result, error) {
	select {
	case <-w.exit:
		return nil, errors.New("result chan closed")
	case r, ok := <-w.next:
		if !ok {
			return nil, errors.New("result chan closed")
		}
		return r, nil
	}
}

func (w *watcher) Stop() {
	select {
	case <-w.exit:
		return
	default:
		close(w.exit)
	}
}

func newWatcher(cr *cregistry, opts ...registry.WatchOption) (registry.Watcher, error) {
	var wo registry.WatchOptions
	for _, o := range opts {
		o(&wo)
	}

	log.Log("consul-watcher: creating for " + wo.Service)

	w := &watcher{
		r:         cr,
		wo:        wo,
		exit:      make(chan struct{}),
		next:      make(chan *registry.Result, 10),
		services:  make(map[string][]*registry.Service),
		instances: make(map[string]map[string]*registry.Service),
	}

	// right now we aren't supporting watching "all" services. tbd later
	if len(w.wo.Service) > 0 {
		w.wo.Service = sanitizeServiceName(w.wo.Service)
		go w.watch()
	}

	return w, nil
}

func (w *watcher) watch() {
	// Not supporting "all" services at the moment
	if len(w.wo.Service) == 0 {
		return
	}

	// start a ticker
	t := time.NewTicker(getPollInterval(w.wo.Context))
	done := make(chan struct{})
	for {
		// select on tick or exit
		select {
		case <-w.exit:
			close(done)
			return
		case <-t.C:
			log.Log("cloudmap-watcher: tick")
			// on tick, DiscoverInstances for service
			serviceName := w.wo.Service
			services, err := w.r.GetService(serviceName)
			if err != nil {
				log.Log("cloudmap-watcher failed getting service: " + serviceName)
				continue
			}
			newInstances := toInstanceMap(services)
			current := w.cloneInstances(serviceName)
			if current == nil {
				// TODO decide how to handle getting new instance map, but not being able to get
				// existing one. all creates? something else?
				continue
			}

			// evaluate new instances for creates or updates
			for id, newSvc := range newInstances {
				old, ok := current[id]
				if !ok {
					// new
					w.next <- &registry.Result{Action: "create", Service: newSvc}
					continue
				}
				newHash, err := hash.Hash(newSvc, nil)
				if err != nil {
					continue
				}
				oldHash, err := hash.Hash(old, nil)
				if err != nil {
					continue
				}

				if newHash != oldHash {
					log.Log("consul-watcher: oldHash does not equal newHash, update")
					w.next <- &registry.Result{Action: "update", Service: newSvc}
					continue
				}
			}

			// check for removed old instances
			for id, old := range current {
				if _, ok := newInstances[id]; !ok {
					w.next <- &registry.Result{Action: "delete", Service: old}
				}
			}

			w.Lock()
			w.instances[serviceName] = newInstances
			w.Unlock()
		}
	}

	// convert to registry.Service
	// check instances for node.Id
	// - if found, hash compare and ignore or update
	// - if not found, announce add
	// check new instances for old ids
	// - if not found, delete and announce
	// - if found, ignore because it was already handled during the earlier compare
}

func toInstanceMap(services []*registry.Service) map[string]*registry.Service {
	rsp := make(map[string]*registry.Service)
	for _, svc := range services {
		rsp[svc.Nodes[0].Id] = svc
	}

	return rsp
}

func (w *watcher) cloneInstances(service string) map[string]*registry.Service {
	w.RLock()
	defer w.RUnlock()

	current, ok := w.instances[service]
	if !ok {
		return nil
	}

	clones := make(map[string]*registry.Service)
	for k, v := range current {
		clones[k] = v
	}
	return clones
}
