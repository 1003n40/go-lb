package algorithm

import (
	"errors"
	"sync"
)

type RoundRobinParallel struct {
	i     int
	hosts []string

	sync.Mutex
}

func NewRoundRobinParallel(hosts ...string) *RoundRobinParallel {
	return &RoundRobinParallel{i: 0, hosts: hosts}
}

// Adds a host to the list of hosts, with the weight of the host being 1.
func (rb *RoundRobinParallel) Add(host string) {
	rb.Lock()
	defer rb.Unlock()

	for _, h := range rb.hosts {
		if h == host {
			return
		}
	}
	rb.hosts = append(rb.hosts, host)
}

// Weight increases the percentage of requests that get sent to the host
// Which can be calculated as `weight/(total_weights+weight)`.
func (rb *RoundRobinParallel) AddWeight(host string, weight int) {
	rb.Lock()
	defer rb.Unlock()

	for _, h := range rb.hosts {
		if h == host {
			return
		}
	}

	for i := 0; i < weight; i++ {
		rb.hosts = append(rb.hosts, host)
	}

}

// Check if host already exist
func (rb *RoundRobinParallel) Exists(host string) bool {
	rb.Lock()
	defer rb.Unlock()

	for _, h := range rb.hosts {
		if h == host {
			return true
		}
	}

	return false
}

// Remove all hosts that are matching
func (rb *RoundRobinParallel) Remove(host string) {
	rb.Lock()
	defer rb.Unlock()
	j := 0
	for i, v := range rb.hosts {
		if v != host {
			rb.hosts[j] = rb.hosts[i]
			j++
		}
	}
	rb.hosts = rb.hosts[:j]
}

// Get host based on round-robin balancing
func (rb *RoundRobinParallel) Balance() (string, error) {
	rb.Lock()
	defer rb.Unlock()

	if len(rb.hosts) == 0 {
		return "", errors.New("no hosts to balance")
	}

	host := rb.hosts[rb.i%len(rb.hosts)]
	rb.i++

	return host, nil
}
