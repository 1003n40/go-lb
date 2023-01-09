package cmd

import (
	"context"
	"errors"
	"github.com/1003n40/go-lb/pkg/command"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	stickyConnection   bool
	balancingAlgorithm string
)

// svcCmd represents the svc command
var svcCmd = &cobra.Command{
	Use:   "svc",
	Short: "Service for creating/deleting/updating/syncing service configuration",
	Long: `'svc' command starts REST API service which will update entries for different service configurations
in the storage, then it will send event to each instance of the load balancer (consumer), to read the entries from storage.`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		err := validateLBAlgorithm(balancingAlgorithm)
		if err != nil {
			return err
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := logrus.WithFields(logrus.Fields{"service": "test"}).Logger
		svc, err := command.NewServiceCommand(amqpHostURL, postgresHost, balancingAlgorithm, postgresPort, port, stickyConnection, logger)
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
	rootCmd.AddCommand(svcCmd)
	svcCmd.Flags().BoolVarP(&stickyConnection, "use-sticky-connections", "s", true, "Saves the session id in Redis cache in order to be consistent in sending to the same node")
	svcCmd.Flags().StringVarP(&balancingAlgorithm, "balancing-algo", "b", "Round-Robin", "Specifies the load balancing algorithm {Round-Robin, IPHash}")
}

func validateLBAlgorithm(lbAlgo string) error {
	supported := []string{"IPHash", "Round-Robin"}

	for _, supp := range supported {
		if supp == lbAlgo {
			return nil
		}
	}

	return errors.New("load balancing algorithm you have provided is not supported")
}
