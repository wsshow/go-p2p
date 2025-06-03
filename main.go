package main

import (
	"go-p2p/client" // This will refer to the package containing ClientApp
	"go-p2p/server"
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

// initLogConfig can be removed if client and server set their own prefixes,
// or kept if a global default is desired before specific prefixes are set.
// For this refactoring, assuming client/server handle their own prefixes.
// func initLogConfig() {
// 	log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)
// }

func main() {
	// initLogConfig() // Client and Server now set their own log prefixes.
	app := &cli.App{
		Name:    "go-p2p",
		Usage:   "P2P communication tool with NAT traversal.",
		Authors: []*cli.Author{{Name: "WS & AI Assistant"}},
	}

	app.Commands = []*cli.Command{
		{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "Run P2P server",
			Flags: []cli.Flag{
				&cli.IntFlag{Name: "port", Aliases: []string{"p"}, Value: 9000, Usage: "Server UDP port"},
			},
			Action: func(c *cli.Context) error {
				// Server sets its own prefix in its Run function
				server.Run(c.Int("port"))
				return nil
			},
		},
		{
			Name:    "client",
			Aliases: []string{"c"},
			Usage:   "Run P2P client",
			Flags: []cli.Flag{
				&cli.IntFlag{Name: "port", Aliases: []string{"p"}, Value: 9001, Usage: "Client local UDP port (0 for auto-selection)"},
				&cli.StringFlag{Name: "serverAddr", Aliases: []string{"s"}, Value: "", Required: true, Usage: "Server address (e.g., ip:port)"},
			},
			Action: func(c *cli.Context) error {
				// Client instantiation and Run call
				clientApp := client.NewClientApp(c.Int("port"), c.String("serverAddr"))
				clientApp.Run() // This will block until client exits
				return nil
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatalf("[FATAL] CLI app failed: %v", err) // Use FATAL prefix for consistency if log prefixing is active
	}
}
