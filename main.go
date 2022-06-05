package main

import (
	"go-p2p/client"
	"go-p2p/server"
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

func initLogConfig() {
	log.SetPrefix("[go-p2p] ")
	log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)
}

func init() {
	initLogConfig()
}

func main() {
	app := &cli.App{
		Name:    "P2PTool",
		Usage:   "P2PTool is a tool for P2P communication.",
		Authors: []*cli.Author{{Name: "ws"}},
	}

	app.Commands = []*cli.Command{
		{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "Run server",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "port",
					Aliases: []string{"p"},
					Value:   9000,
					Usage:   "Server port",
				},
			},
			Action: func(c *cli.Context) error {
				server.Run(c.Int("port"))
				return nil
			},
		},
		{
			Name:    "client",
			Aliases: []string{"c"},
			Usage:   "Run client",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "port",
					Aliases: []string{"p"},
					Value:   9001,
					Usage:   "Client port",
				},
				&cli.StringFlag{
					Name:     "serverAddr",
					Aliases:  []string{"s"},
					Value:    "",
					Required: true,
					Usage:    "Server address",
				},
			},
			Action: func(c *cli.Context) error {
				client.Run(c.Int("port"), c.String("serverAddr"))
				return nil
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
