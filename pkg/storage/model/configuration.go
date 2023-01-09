package model

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Node struct {
	ID             uint64 `json:"id,omitempty" gorm:"primaryKey;column:id;auto_increment;not null"`
	Name           string `json:"name,omitempty" gorm:"unique;not null;type:varchar(36);column:name"`
	Host           string `json:"host,omitempty" gorm:"type:varchar(100);unique;not null;column:host"`
	MaxConnections uint   `json:"maxConnections,omitempty" gorm:"column:max_connections;not null"`
}

func (n *Node) BeforeCreate(tx *gorm.DB) (err error) {
	uuid, err := uuid.NewUUID()
	if err != nil {
		return
	}

	n.Name = uuid.String()
	return
}
