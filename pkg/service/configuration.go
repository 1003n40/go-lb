package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/1003n40/go-lb/pkg/global"
	"github.com/1003n40/go-lb/pkg/storage/model"
	"github.com/hashicorp/go-multierror"
	"github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type NodeConfiguration struct {
	*gorm.DB
	channel *amqp091.Channel
	logger  *logrus.Logger
}

func NewConfigurationService(pool *gorm.DB, channel *amqp091.Channel, logger *logrus.Logger) (*NodeConfiguration, error) {
	err := channel.ExchangeDeclare(global.NodeConfigUpdate, "fanout", true, false, false, false, nil)
	if err != nil {
		return nil, err
	}

	return &NodeConfiguration{
		DB:      pool,
		channel: channel,
		logger:  logger,
	}, nil
}

func (s *NodeConfiguration) GetConfigurations() ([]model.Node, error) {
	var nodes []model.Node
	err := s.DB.Model(&model.Node{}).Find(&nodes)
	if err.Error != nil {
		return nil, err.Error
	}

	return nodes, nil
}

func (s *NodeConfiguration) GetConfigurationById(id uint64) (*model.Node, error) {
	var node model.Node

	err := s.DB.Model(&model.Node{}).First(&node, id)
	if err.Error != nil {
		return nil, fmt.Errorf("could not find record with id: %d. Error: %w", id, err.Error)
	}

	return &node, nil
}

func (s *NodeConfiguration) DeleteConfigurationById(ctx context.Context, id uint64) error {
	var node model.Node
	err := s.DB.Model(&model.Node{}).First(&node, id).Error
	if err != nil {
		return fmt.Errorf("could not find node configuration to delete. Error: %w", err)
	}

	if err = s.DB.Delete(node).Error; err != nil {
		return fmt.Errorf("could not delete node configuration with id: %d. Error: %w", id, err)
	}

	objectJson, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("could not marshal object to json. Error: %w", err)
	}

	if err := s.sendEvent(ctx, objectJson, global.DeleteEvent); err != nil {
		return fmt.Errorf("could not send event to delete exchange. Error: %w", err)
	}

	return nil
}

func (s *NodeConfiguration) CreateConfiguration(ctx context.Context, node *model.Node) error {
	err := s.DB.Model(&model.Node{}).Omit("id").Create(node).Error
	if err != nil {
		return fmt.Errorf("could not create record for object: %+v. Error: %w", *node, err)
	}

	objectJson, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("could not marshal object to json. Error: %w", err)
	}

	if err := s.sendEvent(ctx, objectJson, global.CreateEvent); err != nil {
		return fmt.Errorf("could not send event to create exchange. Error: %w", err)
	}

	return nil
}

func (s *NodeConfiguration) UpdateConfiguration(ctx context.Context, id uint64, node *model.Node) error {
	err := s.DB.Model(&model.Node{ID: id}).Omit("id", "name").Updates(node).Error
	if err != nil {
		return fmt.Errorf("could not create record for object: %+v. Error: %w", *node, err)
	}

	var updatedNode model.Node
	err = s.DB.Model(&model.Node{}).First(&updatedNode, id).Error
	if err != nil {
		return fmt.Errorf("could not find updated configuration by its id. Error: %w", err)
	}

	*node = updatedNode
	objectJson, err := json.Marshal(updatedNode)
	if err != nil {
		return fmt.Errorf("could not marshal object to json. Error: %w", err)
	}

	if err := s.sendEvent(ctx, objectJson, global.UpdateEvent); err != nil {
		return fmt.Errorf("could not send event to update exchange. Error: %w", err)
	}

	return nil
}

func (s *NodeConfiguration) sendEvent(ctx context.Context, objectJsonBody []byte, eventType string) error {
	const retries = 2
	var merr error
	for i := 0; i < retries; i++ {
		s.logger.Infof("Sending event: %s, with body: %s", eventType, objectJsonBody)
		err := s.channel.PublishWithContext(
			ctx,
			global.NodeConfigUpdate, // exchange
			"",                      // routing key
			false,                   // mandatory
			false,                   // immediate
			amqp091.Publishing{
				ContentType: "application/json",
				Body:        objectJsonBody,
				Headers:     map[string]interface{}{"x-event-type": eventType},
			},
		)
		if err == nil {
			return nil
		}
		merr = multierror.Append(merr, err)
	}

	return merr
}
