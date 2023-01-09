package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/1003n40/go-lb/pkg/service"
	"github.com/1003n40/go-lb/pkg/storage"
	"github.com/1003n40/go-lb/pkg/storage/model"
	"github.com/gorilla/mux"
	"github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"net/http"
	"strconv"
)

type Service struct {
	postgresHost string
	postgresPort uint
	rabbitMQHost string
	servicePort  uint

	useStickyConnection bool
	balancingAlgo       string

	logger *logrus.Logger
}

func NewServiceCommand(rabbitMQHost, postgresHost, balancingAlgo string, postgresPort, servicePort uint, useStickConnections bool, logger *logrus.Logger) (*Service, error) {
	return &Service{
		postgresHost:        postgresHost,
		postgresPort:        postgresPort,
		rabbitMQHost:        rabbitMQHost,
		servicePort:         servicePort,
		useStickyConnection: useStickConnections,
		balancingAlgo:       balancingAlgo,
		logger:              logger,
	}, nil
}

func (s *Service) Execute(ctx context.Context) error {
	dbConfig, err := storage.NewPostgresConfig(s.postgresHost, s.postgresPort)
	if err != nil {
		return err
	}
	// Load service configuration
	// Db initialization
	// Do optional migration on known tables, we should ignore the errors
	s.logger.Info("Creating database connection pool...")
	dbConnPool, err := storage.GetConnectionPool(*dbConfig)
	if err != nil {
		return err
	}
	defer dbConnPool.Close()

	s.logger.Info("Migrating database tables configuration to pg")
	_ = dbConnPool.AutoMigrate(&model.Node{})

	// Message queue configuration
	rConnection, err := amqp091.Dial(s.rabbitMQHost)
	if err != nil {
		s.logger.Error(err, "Could not create connection to rabbitmq cluster")
		return errors.New("could not create connection to rabbitmq instance")
	}
	defer rConnection.Close()

	channel, err := rConnection.Channel()
	if err != nil {
		s.logger.Error(err)
		return errors.New("could not create channel for current connection")
	}

	configurationService, err := service.NewConfigurationService(dbConnPool.DB, channel, s.logger)
	if err != nil {
		s.logger.Error(err)
		return errors.New("could not create configuration service instance, possibly could not create needed exchanges")
	}

	// Web server initialization
	router := s.initializeServiceRouter(*configurationService)
	serviceAddr := fmt.Sprintf(":%d", s.servicePort)
	s.logger.Infof("Starting to listern for HTTP requests on: %s", serviceAddr)
	return http.ListenAndServe(serviceAddr, router)
}

// initializeServiceRouter initializes the router
func (s *Service) initializeServiceRouter(svc service.NodeConfiguration) *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/configuration", s.getConfigurations()).Methods(http.MethodGet)

	r.HandleFunc("/configurations", s.getNodeConfiguration(svc)).Methods(http.MethodGet)
	r.HandleFunc("/configurations/{id}", s.getNodeConfigurationById(svc)).Methods(http.MethodGet)
	r.HandleFunc("/configurations", s.createNodeConfiguration(context.TODO(), svc)).Methods(http.MethodPost)
	r.HandleFunc("/configurations/{id}", s.updateNodeConfiguration(context.TODO(), svc)).Methods(http.MethodPut)
	r.HandleFunc("/configurations/{id}", s.deleteNodeConfiguration(context.TODO(), svc)).Methods(http.MethodDelete)

	return r
}

func (s *Service) getConfigurations() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{}
		defer json.NewEncoder(w).Encode(response)

		response["balancingAlgorithm"] = s.balancingAlgo
		response["useStickyConnection"] = s.useStickyConnection

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Service) getNodeConfiguration(svc service.NodeConfiguration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{}
		defer json.NewEncoder(w).Encode(response)

		nodes, err := svc.GetConfigurations()
		if err != nil {
			s.logger.Error(err)
			response["error"] = err.Error()
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		response["nodes"] = nodes
		w.WriteHeader(http.StatusOK)
	}
}

func (s *Service) getNodeConfigurationById(svc service.NodeConfiguration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{}
		defer json.NewEncoder(w).Encode(response)

		params := mux.Vars(r)
		id, err := strconv.ParseUint(params["id"], 10, 32)
		if err != nil {
			s.logger.Error(err)
			response["error"] = fmt.Errorf("could not parse the id you have provided: %s", params["id"])
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		node, err := svc.GetConfigurationById(id)
		if err != nil {
			s.logger.Error(err)
			response["error"] = err.Error()
			w.WriteHeader(http.StatusNotFound)
			return
		}

		response["node"] = node
		w.WriteHeader(http.StatusOK)
	}
}

func (s *Service) createNodeConfiguration(ctx context.Context, svc service.NodeConfiguration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{}
		defer json.NewEncoder(w).Encode(response)

		node := &model.Node{}
		err := json.NewDecoder(r.Body).Decode(node)
		if err != nil {
			s.logger.Error(err)
			response["error"] = fmt.Sprintf("Could not parse request body. Error: %w", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := svc.CreateConfiguration(ctx, node); err != nil {
			s.logger.Error(err)
			response["error"] = err.Error()
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		response["createdNode"] = node
		w.WriteHeader(http.StatusOK)
	}
}

func (s *Service) updateNodeConfiguration(ctx context.Context, svc service.NodeConfiguration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{}
		defer json.NewEncoder(w).Encode(response)

		params := mux.Vars(r)
		id, err := strconv.ParseUint(params["id"], 10, 32)
		if err != nil {
			s.logger.Error(err)
			response["error"] = fmt.Errorf("could not parse the id you have provided: %s", params["id"])
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		node := &model.Node{}
		err = json.NewDecoder(r.Body).Decode(node)
		if err != nil {
			s.logger.Error(err)
			response["error"] = fmt.Sprintf("Could not parse request body. Error: %w", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := svc.UpdateConfiguration(ctx, id, node); err != nil {
			s.logger.Error(err)
			response["error"] = err.Error()
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		response["updatedNode"] = node
		w.WriteHeader(http.StatusOK)
	}
}

func (s *Service) deleteNodeConfiguration(ctx context.Context, svc service.NodeConfiguration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{}
		defer json.NewEncoder(w).Encode(response)

		params := mux.Vars(r)
		id, err := strconv.ParseUint(params["id"], 10, 32)
		if err != nil {
			s.logger.Error(err)
			response["error"] = fmt.Errorf("could not parse the id you have provided: %s", params["id"])
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err = svc.DeleteConfigurationById(ctx, id); err != nil {
			s.logger.Error(err)
			response["error"] = err.Error()
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
