# Go P2P (go-p2p)

## 描述 (Description)
`go-p2p` 是一个点对点(P2P)通信工具, 它允许在可能位于NAT设备后的两个客户端之间建立直接连接.
项目首先利用UDP进行NAT穿透 (NAT traversal), 成功建立P2P连接后, 可以选择切换到TCP以获得更可靠的数据传输.

`go-p2p` is a peer-to-peer (P2P) communication tool that allows establishing direct connections between two clients, potentially behind NAT devices.
The project initially uses UDP for NAT traversal. Once a P2P connection is established, there's an option to switch to TCP for more reliable data transfer.

## 特性 (Features)
*   **服务端协助的客户端发现**: 客户端在中央服务器注册并可以发现其他客户端. (Server-assisted client discovery)
*   **UDP NAT穿透**: 利用UDP打洞技术尝试建立直接的P2P连接. (UDP NAT traversal using hole punching)
*   **TCP连接切换**: UDP P2P连接建立后, 可切换到TCP以进行更稳定的通信. (Optional switch to TCP after UDP P2P establishment)
*   **客户端重命名**: 客户端可以在服务器上更改其名称. (Client renaming)
*   **简单的命令行界面**: 通过命令行进行交互操作. (Simple command-line interface)

## 工作原理 (How it Works)

1.  **服务器 (Server)**:
    *   监听UDP端口, 等待客户端连接.
    *   管理已连接客户端的列表, 包括它们的公网UDP地址和自定义名称.
    *   处理来自客户端的连接请求, 将一个客户端的地址信息转发给另一个客户端, 以便它们尝试直接连接.
    *   通过心跳机制检测客户端的在线状态.

2.  **客户端 (Client)**:
    *   启动时连接到服务器, 并被分配一个初始随机名称 (可以稍后更改).
    *   可以向服务器请求当前在线的其他客户端列表 (`all` 命令).
    *   可以请求连接到另一个客户端 (`connect <name>` 命令).
    *   **P2P连接流程**:
        1.  客户端S (Source) 希望连接到客户端T (Target). S向服务器发送连接请求, 指明T的名称.
        2.  服务器将S的名称和外部UDP地址信息发送给T. (实际上服务器仅将S的名称发送给T，T通过`allow`指令响应后，服务器再将T的地址发给S，S再将自己的地址发给T，或服务器中继双方地址) - *修正: 当前实现是服务器将S的名称发送给T. T响应allow后, 服务器将T的地址和S的地址分别发给S和T的对应方.*
        3.  T收到连接请求后, 可以选择允许 (`allow <S_name>`) 或拒绝 (`deny <S_name>`).
        4.  如果T允许, T会通知服务器.
        5.  服务器接着将T的外部UDP地址以及S自己的外部UDP地址(由服务器视角看到)回传给S. (服务器将T的地址发给S, S通过此信息与T联系. S的地址是服务器在S连接时记录的)
        6.  此时, S和T都拥有对方的公网UDP地址和端口. 它们都尝试从自己的外部地址/端口向对方的外部地址/端口发送UDP包 (UDP打洞).
        7.  如果NAT设备允许这种双向的UDP包通过, P2P UDP连接即建立成功.
        8.  连接成功后, 客户端可以发送消息 (`msg <text>`) 或尝试切换到TCP (`tcp` 命令).

3.  **UDP到TCP切换**:
    *   当一个客户端发起TCP切换请求后, 它会通知对端.
    *   两个客户端都会关闭当前的P2P UDP连接.
    *   它们会尝试使用之前用于UDP P2P的相同IP和端口来建立TCP连接. 通常一个客户端尝试监听 (listen), 另一个尝试拨号 (dial). 如果初始拨号失败, 发起方也会尝试监听.

## 安装与运行 (Setup and Usage)

### 前提 (Prerequisites)
*   Go (1.18或更高版本) (Go version 1.18 or newer)

### 编译 (Compilation)
```bash
go build -o go-p2p main.go
```
(或者 `go build` 将在当前目录下生成可执行文件 `go-p2p` 或 `main` 取决于操作系统和环境)
(Or `go build` will generate an executable named `go-p2p` or `main` in the current directory, depending on OS and environment)

### 运行服务器 (Running the Server)
服务器需要一个公网IP地址以便客户端可以从任何地方访问它.
The server needs a publicly accessible IP address for clients to connect from anywhere.

```bash
# 监听在UDP端口 9000 (默认)
# Listen on UDP port 9000 (default)
sudo ./go-p2p server
# 或者指定端口 (or specify a port)
sudo ./go-p2p server -p <port_number>
```
*注意: 可能需要 `sudo` 是因为通常监听低于1024的端口需要管理员权限. 如果您使用高端口号(>=1024)且没有权限问题, 可以不使用 `sudo`.*
*(Note: `sudo` might be needed for listening on ports below 1024. If using a high port number (>=1024) and no permission issues, `sudo` may not be required.)*

### 运行客户端 (Running the Client)
```bash
# 连接到位于 <server_ip>:9000 的服务器, 客户端使用本地UDP端口 9001 (默认)
# Connect to server at <server_ip>:9000, client uses local UDP port 9001 (default)
./go-p2p client -s <server_ip>:9000

# 指定客户端本地端口和服务器地址及端口
# Specify client's local port and server address with port
./go-p2p client -p <client_local_port> -s <server_ip>:<server_port>
```

### 客户端命令 (Client Commands)
连接成功后, 您可以在客户端输入以下命令:
Once connected, you can use the following commands in the client:

*   `all`
    *   描述: 列出所有在服务器上注册的其他客户端. (Lists all other clients registered with the server.)
*   `connect <client_name>`
    *   描述: 请求与名为 `<client_name>` 的客户端建立P2P连接. (Requests a P2P connection with the client named `<client_name>`.)
*   `allow <client_name>`
    *   描述: 允许来自名为 `<client_name>` 的客户端的连接请求. (Allows an incoming connection request from `<client_name>`.)
*   `deny <client_name>`
    *   描述: 拒绝来自名为 `<client_name>` 的客户端的连接请求. (Denies an incoming connection request from `<client_name>`.)
*   `msg <message_text>`
    *   描述: 通过已建立的P2P连接 (UDP或TCP) 发送消息. (Sends a message over the established P2P connection (UDP or TCP).)
*   `rename <new_name>`
    *   描述: 请求服务器将您的客户端名称更改为 `<new_name>`. (Requests the server to change your client name to `<new_name>`.)
*   `tcp`
    *   描述: 如果当前存在UDP P2P连接, 尝试将其切换到TCP连接以获得更稳定的通信. (If a UDP P2P connection is active, attempts to switch it to a TCP connection for more stable communication.)
*   `help`
    *   描述: 显示可用命令列表和用法. (Shows the list of available commands and their usage.)
*   `exit`
    *   描述: 关闭所有连接并退出客户端程序. (Closes all connections and exits the client program.)

## 提示 (Prompt)
客户端的提示符会显示您当前的名称和P2P连接状态:
The client prompt indicates your current name and P2P connection status:
*   `[YourName]> ` - 已连接到服务器, P2P未连接. (Connected to server, no P2P.)
*   `[YourName][UDP PeerAddr:Port]> ` - 已建立UDP P2P连接. (UDP P2P connection established.)
*   `[YourName][TCP PeerAddr:Port]> ` - 已建立TCP P2P连接. (TCP P2P connection established.)

## 注意事项 (Notes)
*   NAT穿透的成功率取决于NAT设备的类型和配置. 某些对称型NAT可能较难穿透. (NAT traversal success depends on the type and configuration of NAT devices. Symmetric NATs can be more challenging.)
*   由于UDP是无连接的, UDP P2P "连接" 的建立依赖于双方成功收到对方的消息. (Since UDP is connectionless, UDP P2P "connection" establishment relies on both parties successfully receiving messages from each other.)
*   切换到TCP通常会更稳定, 但也可能因为防火墙或NAT策略而失败. (Switching to TCP is generally more stable but might also fail due to firewalls or NAT policies.)
*   本项目主要用于学习和演示P2P基本原理, 未经严格安全审计. (This project is primarily for learning and demonstrating basic P2P principles and has not undergone rigorous security auditing.)
