package cmd

import (
	"context"
	"github.com/1003n40/go-lb/pkg/command"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	passiveCheckInterval  uint
	backendServiceAddress string
)

// instCmd represents the inst command
var instCmd = &cobra.Command{
	Use:   "inst",
	Short: "Deploy instance of the load-balancer",
	Long:  `Deploy instance of the load-balancer which is going to be used`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := logrus.WithFields(logrus.Fields{"service": "test"}).Logger
		svc, err := command.NewInstanceCommand(amqpHostURL, redisHost, redisPass, backendServiceAddress, port, passiveCheckInterval, logger)
		if err != nil {
			logger.Error(err)
			return err
		}

		err = svc.Execute(context.Background())
		if err != nil {
			logger.Error(err)
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(instCmd)
	instCmd.Flags().UintVarP(&passiveCheckInterval, "passive-check-interval", "i", 20, "Passive check for the connection health")
	instCmd.Flags().StringVarP(&backendServiceAddress, "backend-service", "b", "http://localhost:8080/", "Address for the backend configuration management service")
}
