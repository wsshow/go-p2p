package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"
)

const (
	DefaultServerPort = 8000
	DefaultServerAddr = "127.0.0.1:8000"
)

func main() {
	// 创建应用程序
	app := &cli.App{
		Name:    "go-p2p",
		Usage:   "A peer-to-peer communication application",
		Version: "1.0.0",
		Authors: []*cli.Author{
			{
				Name: "Go-P2P Team",
			},
		},
		Commands: []*cli.Command{
			{
				Name:    "server",
				Aliases: []string{"s"},
				Usage:   "Start the P2P server",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "port",
						Aliases: []string{"p"},
						Value:   DefaultServerPort,
						Usage:   "Server port",
					},
					&cli.BoolFlag{
						Name:  "verbose",
						Usage: "Enable verbose logging",
					},
				},
				Action: func(c *cli.Context) error {
					return runServer(c.Int("port"), c.Bool("verbose"))
				},
			},
			{
				Name:    "client",
				Aliases: []string{"c"},
				Usage:   "Start the P2P client",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "name",
						Aliases:  []string{"n"},
						Usage:    "Client name (required)",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "server",
						Aliases: []string{"s"},
						Value:   DefaultServerAddr,
						Usage:   "Server address (host:port)",
					},
					&cli.BoolFlag{
						Name:  "verbose",
						Usage: "Enable verbose logging",
					},
				},
				Action: func(c *cli.Context) error {
					return runClient(c.String("name"), c.String("server"), c.Bool("verbose"))
				},
			},
			{
				Name:  "version",
				Usage: "Show version information",
				Action: func(c *cli.Context) error {
					fmt.Printf("go-p2p version %s\n", c.App.Version)
					return nil
				},
			},
		},
	}

	// 运行应用程序
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// ExecutableConfig 可执行文件配置
type ExecutableConfig struct {
	Name    string
	SubDir  string
	MainGo  string
	Verbose bool
}

// runServer 运行服务器
func runServer(port int, verbose bool) error {
	config := ExecutableConfig{
		Name:    "server",
		SubDir:  filepath.Join("cmd", "server"),
		MainGo:  filepath.Join("cmd", "server", "main.go"),
		Verbose: verbose,
	}

	args := []string{strconv.Itoa(port)}
	return runExecutable(config, args)
}

// runClient 运行客户端
func runClient(name, server string, verbose bool) error {
	// 验证服务器地址格式
	if err := validateServerAddress(server); err != nil {
		return fmt.Errorf("invalid server address: %v", err)
	}

	// 验证客户端名称
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("client name cannot be empty")
	}

	config := ExecutableConfig{
		Name:    "client",
		SubDir:  filepath.Join("cmd", "client"),
		MainGo:  filepath.Join("cmd", "client", "main.go"),
		Verbose: verbose,
	}

	args := []string{name, server}
	return runExecutable(config, args)
}

// runExecutable 运行可执行文件的通用函数
func runExecutable(config ExecutableConfig, args []string) error {
	// 尝试多种方式查找和运行可执行文件
	executablePaths := getExecutablePaths(config)

	for _, execPath := range executablePaths {
		if _, err := os.Stat(execPath); err == nil {
			if config.Verbose {
				fmt.Printf("Running executable: %s\n", execPath)
			}
			return runCommand(execPath, args, config.Verbose)
		}
	}

	// 如果找不到可执行文件，使用 go run
	if config.Verbose {
		fmt.Printf("Executable not found, using go run: %s\n", config.MainGo)
	}
	return runGoCommand(config.MainGo, args, config.Verbose)
}

// getExecutablePaths 获取可能的可执行文件路径
func getExecutablePaths(config ExecutableConfig) []string {
	execPath, err := os.Executable()
	if err != nil {
		return []string{
			filepath.Join(config.SubDir, config.Name),
			filepath.Join(config.SubDir, config.Name+".exe"),
		}
	}

	execDir := filepath.Dir(execPath)
	return []string{
		// 相对于当前可执行文件的路径
		filepath.Join(execDir, config.SubDir, config.Name),
		filepath.Join(execDir, config.SubDir, config.Name+".exe"),
		// 相对于当前工作目录的路径
		filepath.Join(config.SubDir, config.Name),
		filepath.Join(config.SubDir, config.Name+".exe"),
	}
}

// runCommand 运行命令
func runCommand(executable string, args []string, verbose bool) error {
	cmd := exec.Command(executable, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if verbose {
		fmt.Printf("Executing: %s %s\n", executable, strings.Join(args, " "))
	}

	return cmd.Run()
}

// runGoCommand 使用 go run 运行命令
func runGoCommand(mainFile string, args []string, verbose bool) error {
	goArgs := append([]string{"run", mainFile}, args...)
	cmd := exec.Command("go", goArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if verbose {
		fmt.Printf("Executing: go %s\n", strings.Join(goArgs, " "))
	}

	return cmd.Run()
}

// validateServerAddress 验证服务器地址格式
func validateServerAddress(address string) error {
	parts := strings.Split(address, ":")
	if len(parts) != 2 {
		return fmt.Errorf("address must be in format host:port")
	}

	host := strings.TrimSpace(parts[0])
	portStr := strings.TrimSpace(parts[1])

	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid port number: %v", err)
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	return nil
}
