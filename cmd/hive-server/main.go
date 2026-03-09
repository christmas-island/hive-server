package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/christmas-island/hive-server/internal/log"
	"github.com/spf13/cobra"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := App()
	cmd.SetContext(ctx)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// App is the main CLI entrypoint. It returns a [cobra.Command] instance.
func App() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hive-server",
		Short: "Cross-agent memory and task coordination API server.",
		Run: func(cmd *cobra.Command, args []string) {
			defer log.To(cmd.OutOrStdout())()
			log.Info("Use a subcommand. See --help for details.")
		},
	}

	cmd.AddCommand(Serve())
	return cmd
}
