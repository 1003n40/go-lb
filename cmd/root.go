package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	port         uint
	redisHost    string
	redisPass    string
	postgresHost string
	postgresPort uint
	amqpHostURL  string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "lb",
	Short: "Simple lightweight implementation of load balancer in Golang",
	Long:  `High availability tiny implementation of load balancer using Golang, Redis, Postgres, RabbitMQ`,
	// Run: func(cmd *cobra.Command, args []string) { },
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().UintVarP(&port, "port", "p", 8080, "Port for running instances svc/inst (MUST BE UNIQUE)")
	rootCmd.Flags().StringVarP(&redisHost, "redis-host", "r", "localhost:6379", "Redis host including port, example 127.0.0.1:6379")
	rootCmd.Flags().StringVarP(&redisPass, "redis-pass", "z", "eYVX7EwVmmxKPCDmwMtyKVge8oLd2t81", "Redis host password")
	rootCmd.Flags().StringVarP(&postgresHost, "postgres-host", "s", "localhost", "Postgres host, example 127.0.0.1")
	rootCmd.Flags().UintVarP(&postgresPort, "postgres-port", "v", 5432, "Postgres port, example 5432")
	rootCmd.Flags().StringVarP(&amqpHostURL, "amqp_server_url", "m", "amqp://guest:guest@localhost:5672/", "RabbitMQ AMQP server url")

}
