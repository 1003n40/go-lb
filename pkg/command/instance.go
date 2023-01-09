package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/1003n40/go-lb/pkg/algorithm"
	"github.com/1003n40/go-lb/pkg/global"
	"github.com/1003n40/go-lb/pkg/storage/model"
	"github.com/gomodule/redigo/redis"
	_ "github.com/gomodule/redigo/redis"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	"github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

const (
	SESSION_COOKIE_NAME = "session-cookie-name"
)

type Instance struct {
	rabbitMQHost         string
	redisHost            string
	redisPass            string
	backendServiceAddr   string
	servicePort          uint
	passiveCheckInterval uint

	logger *logrus.Logger
}

type ServerPool map[string]*Backend

var (
	nodes = ServerPool{}
	mtx   sync.Mutex
	wg    sync.WaitGroup

	rr *algorithm.RoundRobinParallel
)

type Backend struct {
	node    model.Node
	isAlive bool
}

type InstanceConfiguration struct {
	BalancingAlgorithm string `json:"balancingAlgorithm"`
	StickyConnection   bool   `json:"useStickyConnection"`
}

type NodeConfigurations struct {
	Nodes []model.Node `json:"nodes"`
}

func NewInstanceCommand(rabbitMQHost, redisHost, redisPass, backendServiceAddr string, servicePort, checkInterval uint, logger *logrus.Logger) (*Instance, error) {
	return &Instance{
		rabbitMQHost:         rabbitMQHost,
		redisHost:            redisHost,
		redisPass:            redisPass,
		servicePort:          servicePort,
		backendServiceAddr:   backendServiceAddr,
		passiveCheckInterval: checkInterval,
		logger:               logger,
	}, nil
}

func (s *Instance) Execute(ctx context.Context) error {
	// Initialize the configuration from backend (REST API Call)
	instanceConf, err := s.retrieveServiceConfiguration()
	if err != nil {
		return err
	}
	s.logger.Infof("Starting service with load balancing algorithm: %s, and use of sticky connections: %t", instanceConf.BalancingAlgorithm, instanceConf.StickyConnection)

	// Retrieve initial node configurations
	initialNodes, err := s.retrieveNodeConfigurations()
	if err != nil {
		return err
	}

	// Registering nodes
	for _, node := range initialNodes.Nodes {
		s.logger.Infof("Registering node: %+v, to pool of nodes", node)
		nodes[node.Name] = &Backend{node: node}
	}

	/*
			Initialize round-robin algorithm, which is going to be our primary host picker.
		We need to have at least 1 host available, before trying to balance, or else we should return error.
		That being said, before starting instance, we should guarantee that we have some nodes available.
	*/
	// Initialize round-robin
	rr = algorithm.NewRoundRobinParallel(make([]string, 0)...)

	// We want to monitor all the servers we get (passive check), this check will make changes to available
	// hosts in round-robin setup, by adding removing them. For the rest of the handling implementation will
	// reside on even driven design, by registering, removing or updating nodes via event handlers. We want
	// this handler to be async in order to start processing events ASAP and not loose node that was registered
	// because of a ping wait to specific host.
	s.healthMonitor()

	/*
		Initialize RabbitMQ connection for retrieving messages on CRUD operations from the service
	*/
	connection, err := amqp091.Dial(s.rabbitMQHost)
	if err != nil {
		return errors.New("could not create rabbitmq connection")
	}
	defer connection.Close()

	channel, err := connection.Channel()
	if err != nil {
		return errors.New("could not create channel")
	}
	defer channel.Close()

	queue, err := channel.QueueDeclare("", false, false, true, false, nil)
	if err != nil {
		return errors.New("could not create basic queue to consume events")
	}

	err = channel.QueueBind(queue.Name, "", global.NodeConfigUpdate, false, nil)
	if err != nil {
		return errors.New("could not bind queue to consume fanout exchange events")
	}

	msgs, err := channel.Consume(queue.Name, "", true, false, false, false, nil)
	if err != nil {
		return err
	}

	// We want to handle messages async, as we should do also the processing
	s.handleEvents(msgs)

	//// We want to monitor all the servers we get (passive check)
	//s.healthMonitor()

	// Handle server requests (in parallel)
	err = s.handleLBRequests(instanceConf)
	if err != nil {
		return err
	}

	wg.Wait()
	return nil
}

/*
Balancing algorithm is the side that determines how the load balancing would be done.
In our case we have two options:
  - Round Robin - it would do round-robin based on max connections for node, or when we have sticky connections enabled,

it would do balancing on the same node

  - IPHash - will do hashing based on the ip address of the retrieved request, save it to cache (if not present), if present,

it will try to redirect request to the same host
*/
func (s *Instance) handleLBRequests(instanceConfig *InstanceConfiguration) error {
	switch instanceConfig.BalancingAlgorithm {
	case "Round-Robin":
		err := s.handleRRRequest(instanceConfig.StickyConnection, s.retrieveRedisConnPool())
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("load balancing algorithm: %s, not supported", instanceConfig.BalancingAlgorithm)
	}

	return nil
}

func (s *Instance) handleRRRequest(sticky bool, redispool *redis.Pool) error {
	conn := redispool.Get()
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			host, err := s.rrRequestHandler(sticky, conn, req)
			if err != nil {
				s.logger.Error(err, "could not fetch host")
				return
			}
			url, _ := url.Parse(host)
			// Choose the next backend server to send the request to
			schema := url.Scheme
			if schema == "" {
				schema = "http"
			}
			req.URL.Scheme = schema
			req.URL.Host = url.Host
		},
	}

	// Start the server
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", s.servicePort), proxy))
	return nil
}

func (s *Instance) rrRequestHandler(sticky bool, conn redis.Conn, r *http.Request) (string, error) {
	if !sticky {
		s.logger.Info("Non sticky")
		host, _ := rr.Balance()
		return host, nil
	}

	// Check for the cookie
	// If cookie is not present create it
	cookie, err := r.Cookie(SESSION_COOKIE_NAME)
	if err != nil {
		return s.handleNoSession(r, conn)
	}

	// If cookie is present
	hostname, err := redis.String(conn.Do("GET", cookie.Value))
	if err != nil {
		// Session was expired
		return s.handleNoSession(r, conn)
	}

	return s.handleWithSession(hostname)
}

func (s *Instance) handleWithSession(hostname string) (string, error) {
	s.logger.Info("Handling with session...")
	backend, ok := nodes[hostname]
	if !ok {
		return "", fmt.Errorf("hostname: %s not found", hostname)
	}

	return backend.node.Host, nil
}

func (s *Instance) handleNoSession(r *http.Request, conn redis.Conn) (string, error) {
	s.logger.Info("Handling with no session...")

	host, err := rr.Balance()
	if err != nil {
		s.logger.Error(err, "could not serve rr request")
		return "", err
	}
	// Generate cookie value
	sessionToken := uuid.NewString()
	expiredAt := time.Now().Add(24 * time.Hour)
	ttl := expiredAt.Sub(time.Now())
	hostname, err := findHostnameByHost(host)
	if err != nil {
		return "", err
	}
	// Write to redis that this host is the chosen one with the respective session
	_, err = conn.Do("SETEX", sessionToken, 1000*(ttl.Milliseconds()), hostname)
	if err != nil {
		s.logger.Error(err, "could not create sessionToken for current redis cache")
		return "", err
	}
	// Set cookie to the request
	r.AddCookie(&http.Cookie{Name: SESSION_COOKIE_NAME, Value: sessionToken, Expires: expiredAt})
	return host, err
}

func findHostnameByHost(host string) (string, error) {
	mtx.Lock()
	defer mtx.Unlock()
	for k, v := range nodes {
		if host == v.node.Host {
			return k, nil
		}
	}
	return "", fmt.Errorf("could not find hostname for host: %s", host)
}

// TODO implement this
func (s *Instance) handleIPHashRequest() error {
	return nil
}

func (s *Instance) retrieveRedisConnPool() *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			dialOptions := redis.DialPassword(s.redisPass)
			return redis.Dial("tcp", s.redisHost, dialOptions)
		},
	}
}

func (s *Instance) handleEvents(msgs <-chan amqp091.Delivery) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for msg := range msgs {
			eventType, ok := msg.Headers["x-event-type"].(string)
			if !ok {
				continue
			}

			switch eventType {
			case global.CreateEvent:
				err := s.handleCreateEvent(msg)
				if err != nil {
					s.logger.Error(err)
				}
			case global.DeleteEvent:
				err := s.handleDeleteEvent(msg)
				if err != nil {
					s.logger.Error(err)
				}
			case global.UpdateEvent:
				err := s.handleUpdateEvent(msg)
				if err != nil {
					s.logger.Error(err)
				}
			default:
				continue
			}
		}
	}()
}

func (s *Instance) healthMonitor() {
	wg.Add(1)
	go func() {
		t := time.NewTicker(time.Duration(s.passiveCheckInterval) * time.Second)
		for {
			select {
			// TODO add channel for handling os signals in order to free the wait-group
			case <-t.C:
				s.logger.Info("Starting health check...")
				s.healthCheck()
				s.logger.Info("Health check completed")
			}
		}
	}()
}

func (s *Instance) healthCheck() {
	var merr error
	mtx.Lock()
	defer mtx.Unlock()
	for _, backend := range nodes {
		url, err := url.Parse(backend.node.Host)
		if err != nil {
			merr = multierror.Append(merr, err)
		}

		alive := isBackendAlive(url)
		if !alive {
			exist := rr.Exists(backend.node.Host)
			if exist {
				rr.Remove(backend.node.Host)
			}
		} else {
			exists := rr.Exists(backend.node.Host)
			if !exists {
				rr.AddWeight(backend.node.Host, int(backend.node.MaxConnections))
			}
		}
		backend.isAlive = alive
	}

	if merr != nil {
		s.logger.Error(merr)
	}
}

func isBackendAlive(u *url.URL) bool {
	_, err := http.Get(u.String())
	if err != nil {
		log.Println("Site unreachable, error: ", err)
		return false
	}
	return true
}

// Create event will create entry to the map with available hosts which we define here
func (s *Instance) handleCreateEvent(delivery amqp091.Delivery) error {
	var node model.Node

	if err := json.Unmarshal(delivery.Body, &node); err != nil {
		return err
	}

	u, _ := url.Parse(node.Host)
	alive := isBackendAlive(u)

	mtx.Lock()
	defer mtx.Unlock()
	nodes[node.Name] = &Backend{node: node, isAlive: alive}
	if alive {
		s.logger.Info("Adding node after creation")
		rr.AddWeight(node.Host, int(node.MaxConnections))
	}
	s.logger.Infof("Registered new node: %s, available nodes in pool: %d", node.Name, len(nodes))

	return nil
}

// Update event will update event if it is present on the map, if not we will skip creating
// also it will have to update redis cache too
func (s *Instance) handleUpdateEvent(delivery amqp091.Delivery) error {
	var node model.Node

	if err := json.Unmarshal(delivery.Body, &node); err != nil {
		return err
	}

	u, _ := url.Parse(node.Host)
	alive := isBackendAlive(u)

	mtx.Lock()
	defer mtx.Unlock()
	currNode, ok := nodes[node.Name]
	if !ok {
		s.logger.Infof("Node with name: %s, was deleted, no update will be done", node.Name)
		return nil
	}

	exits := rr.Exists(currNode.node.Host)
	if exits {
		rr.Remove(currNode.node.Host)
	}
	if alive {
		rr.AddWeight(node.Host, int(node.MaxConnections))
	}
	nodes[node.Name] = &Backend{node: node, isAlive: alive}
	s.logger.Infof("Updated node: %s, updated node: %+v", node.Name, node)

	return nil
}

// Delete event deletes event from node map, and removes entry from redis cache
func (s *Instance) handleDeleteEvent(delivery amqp091.Delivery) error {
	var node model.Node

	if err := json.Unmarshal(delivery.Body, &node); err != nil {
		return err
	}

	mtx.Lock()
	defer mtx.Unlock()

	exist := rr.Exists(node.Host)
	if exist {
		rr.Remove(node.Host)
	}
	delete(nodes, node.Name)
	s.logger.Infof("Deleted node: %s", node.Name)

	return nil
}

func (s *Instance) retrieveNodeConfigurations() (*NodeConfigurations, error) {
	url, err := url.JoinPath(s.backendServiceAddr, "configurations")
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	var nodeConfigs NodeConfigurations
	err = json.NewDecoder(resp.Body).Decode(&nodeConfigs)
	if err != nil {
		return nil, err
	}
	return &nodeConfigs, nil
}

func (s *Instance) retrieveServiceConfiguration() (*InstanceConfiguration, error) {
	url, err := url.JoinPath(s.backendServiceAddr, "configuration")
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	var conf InstanceConfiguration
	err = json.NewDecoder(resp.Body).Decode(&conf)
	if err != nil {
		return nil, err
	}
	return &conf, nil
}
