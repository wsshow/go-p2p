package server

import (
	"encoding/json"
	"fmt"
	"go-p2p/storage"
	"go-p2p/utils"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liushuochen/gotable"
)

// ClientInfo 存储客户端信息
type ClientInfo struct {
	Address *net.UDPAddr // 客户端地址
	Name    string       // 客户端名称
}

// --- ClientRegistry ---

// ClientRegistry 负责管理所有连接的客户端信息 (ClientRegistry is responsible for managing all connected client information)
// 它包含一个客户端映射和一个读写互斥锁以保证并发安全 (It contains a map of clients and a RWMutex for concurrent access safety)
type ClientRegistry struct {
	clients map[string]*ClientInfo // 以客户端名称为键的客户端信息映射 (Map of client info, keyed by client name)
	mutex   sync.RWMutex           // 用于保护对clients map的并发访问 (Mutex for protecting concurrent access to the clients map)
}

// NewClientRegistry 创建并初始化一个新的ClientRegistry实例
// (NewClientRegistry creates and initializes a new ClientRegistry instance)
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		clients: make(map[string]*ClientInfo), // 初始化clients map (Initialize the clients map)
	}
}

// AddClient 将一个新的客户端信息添加到注册表中
// 此方法假定clientInfo.Name已经是唯一的 (This method assumes clientInfo.Name is already unique)
// (Adds a new client's information to the registry)
func (r *ClientRegistry) AddClient(clientInfo *ClientInfo) {
	r.mutex.Lock() // 获取写锁以修改map (Acquire write lock to modify the map)
	defer r.mutex.Unlock() // 确保函数退出时释放锁 (Ensure lock is released on function exit)
	r.clients[clientInfo.Name] = clientInfo
	log.Printf("[DEBUG] ClientRegistry: Added client '%s' (%s). Total: %d\n", clientInfo.Name, clientInfo.Address.String(), len(r.clients))
}

// RemoveClient 通过名称从注册表中移除一个客户端
// (Removes a client from the registry by its name)
func (r *ClientRegistry) RemoveClient(name string) {
	r.mutex.Lock() // 获取写锁以修改map (Acquire write lock to modify the map)
	defer r.mutex.Unlock()
	if client, ok := r.clients[name]; ok {
		delete(r.clients, name) // 从map中删除 (Delete from map)
		log.Printf("[INFO] ClientRegistry: Removed client '%s' (%s). Total: %d\n", name, client.Address.String(), len(r.clients))
	} else {
		log.Printf("[WARN] ClientRegistry: Attempted to remove non-existent client '%s'\n", name)
	}
}

// GetClientByName 通过名称查找客户端, 返回客户端信息和是否存在
// (Finds a client by its name, returns client info and whether it exists)
func (r *ClientRegistry) GetClientByName(name string) (*ClientInfo, bool) {
	r.mutex.RLock() // 获取读锁以安全读取map (Acquire read lock for safe map read)
	defer r.mutex.RUnlock()
	client, ok := r.clients[name]
	return client, ok
}

// GetClientByAddr 通过UDP地址查找客户端信息及其名称
// (Finds a client's info and its name by its UDP address)
func (r *ClientRegistry) GetClientByAddr(addr *net.UDPAddr) (info *ClientInfo, name string, exists bool) {
	r.mutex.RLock() // 获取读锁 (Acquire read lock)
	defer r.mutex.RUnlock()
	addrStr := addr.String()
	// 遍历map查找匹配的地址 (Iterate through map to find matching address)
	for n, ci := range r.clients {
		if ci.Address.String() == addrStr {
			return ci, n, true // 找到匹配项 (Match found)
		}
	}
	return nil, "", false // 未找到 (Not found)
}

// RenameClient 为指定地址的客户端重命名
// 如果成功, 返回更新后的ClientInfo; 否则返回错误
// (Renames the client at the specified address. Returns updated ClientInfo on success, or an error.)
func (r *ClientRegistry) RenameClient(currentAddr *net.UDPAddr, newName string) (*ClientInfo, error) {
	r.mutex.Lock() // 获取写锁, 因为涉及查找和可能的修改 (Acquire write lock as it involves find and potential modification)
	defer r.mutex.Unlock()

	var currentName string
	var clientToRename *ClientInfo
	foundByAddr := false

	// 首先通过地址找到客户端及其当前名称 (First, find the client and its current name by address)
	for name, ci := range r.clients {
		if ci.Address.String() == currentAddr.String() {
			currentName = name
			clientToRename = ci
			foundByAddr = true
			break
		}
	}

	if !foundByAddr {
		return nil, fmt.Errorf("client with address %s not found (未找到地址为 %s 的客户端)", currentAddr.String(), currentAddr.String())
	}
	trimmedNewName := strings.TrimSpace(newName)
	if trimmedNewName == "" {
		return nil, fmt.Errorf("new name cannot be empty (新名称不能为空)")
	}
	if trimmedNewName == currentName { // 新旧名称相同, 无需操作 (New and old names are the same, no operation needed)
		return clientToRename, nil
	}
	if _, exists := r.clients[trimmedNewName]; exists { // 检查新名称是否已被占用 (Check if the new name is already taken)
		return nil, fmt.Errorf("name '%s' already exists (名称 '%s' 已存在)", trimmedNewName, trimmedNewName)
	}

	// 执行重命名: 更新ClientInfo中的Name字段, 并更新map中的键
	// (Perform rename: update Name field in ClientInfo, and update key in the map)
	clientToRename.Name = trimmedNewName
	r.clients[trimmedNewName] = clientToRename // 添加新名称的条目 (Add entry for new name)
	delete(r.clients, currentName)             // 删除旧名称的条目 (Delete entry for old name)

	log.Printf("[DEBUG] ClientRegistry: Renamed client from '%s' to '%s' (%s)\n", currentName, trimmedNewName, currentAddr.String())
	return clientToRename, nil
}

// GetAllClients 返回当前所有客户端信息的快照 (浅拷贝)
// 此方法用于需要迭代客户端列表的场景, 如心跳或列出所有客户端, 同时避免长时间锁定注册表
// (Returns a snapshot (shallow copy) of all current client information.)
// (This is used for scenarios needing to iterate the client list, like heartbeats or listing all clients, while avoiding long locks on the registry.)
func (r *ClientRegistry) GetAllClients() map[string]*ClientInfo {
	r.mutex.RLock() // 获取读锁 (Acquire read lock)
	defer r.mutex.RUnlock()
	snapshot := make(map[string]*ClientInfo, len(r.clients))
	for name, client := range r.clients {
		// 复制ClientInfo以防止外部修改影响原始数据 (Copy ClientInfo to prevent external modifications from affecting original data)
		infoCopy := *client
		snapshot[name] = &infoCopy
	}
	return snapshot
}

// GenerateUniqueClientName 生成一个在当前注册表中唯一的客户端名称
// (Generates a client name that is unique within the current registry)
func (r *ClientRegistry) GenerateUniqueClientName() string {
	// 此方法在调用时需要持有写锁,因为它确保生成的名称在添加前是唯一的
	// (This method needs a write lock if called externally to ensure name uniqueness before adding,
	// but here it's used by AddClient which already holds the lock)
	// The RLock here is for the check loop; the actual AddClient operation will use a Write Lock.
	// For standalone generation, a Write Lock would be safer if it immediately reserved the name.
	// However, the current usage pattern in handleConnectMsg is: GenerateUniqueClientName (needs RLock for check) then AddClient (needs WriteLock).
	// To avoid lock upgrade issues, GenerateUniqueClientName itself doesn't modify r.clients.
	// The calling function (handleConnectMsg via registry.AddClient) ensures atomicity.

	// Simplified: GenerateUniqueClientName is called from within AddClient which holds a Write Lock,
	// so direct access is fine here without its own lock. Assuming AddClient handles locking.
	// Let's stick to the provided structure where AddClient calls this.
	// If GenerateUniqueClientName were public and could be called before AddClient, it would need its own write lock or a reservation mechanism.
	// For now, assuming it's an internal helper or called under appropriate lock.
	// The current implementation in the prompt has GenerateUniqueClientName in handleConnectMsg *before* AddClient.
	// This means GenerateUniqueClientName needs to be correct and thread-safe on its own if called directly.
	// Let's assume GenerateUniqueClientName is called internally by a method that already holds the necessary write lock.

	// Corrected approach for GenerateUniqueClientName if it's meant to be callable and safe:
	// It should be called when the registry is already write-locked if it's part of an atomic "add" operation.
	// If it's just a utility to get a potential name, RLock is fine for the check.
	// The provided code calls it from handleConnectMsg, which then calls AddClient. This is slightly off.
	// Let's refine: handleConnectMsg should call a registry method like `registry.RegisterNewClient(addr)`
	// which internally generates name and adds.

	// Sticking to the current prompt's structure for GenerateUniqueClientName method:
	r.mutex.RLock() // RLock for reading client names to check for uniqueness
	defer r.mutex.RUnlock()
	var clientName string
	for {
		clientName = utils.RandStr(5)
		if _, exists := r.clients[clientName]; !exists {
			break // 名称可用 (Name is available)
		}
	}
	return clientName
}


// --- MessageHandler ---

// MessageHandlerFunc 定义消息处理函数的类型签名
// (Defines the type signature for message handler functions)
// 每个处理函数负责处理一种特定类型的消息 (Each handler function is responsible for processing a specific type of message)
type MessageHandlerFunc func(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg)

// ServerMessageHandler 负责根据消息类型分发消息到具体的处理函数
// (ServerMessageHandler is responsible for dispatching messages to specific handler functions based on message type)
type ServerMessageHandler struct {
	registry        *ClientRegistry                                // 客户端注册表实例 (Instance of ClientRegistry)
	handlers        map[storage.MsgType]MessageHandlerFunc         // 消息类型到处理函数的映射 (Map of message types to handler functions)
	heartbeatChan   chan<- storage.UserMsg                         // 用于传递心跳消息的通道 (Channel for passing heartbeat messages)
}

// NewServerMessageHandler 创建并初始化一个新的ServerMessageHandler
// (NewServerMessageHandler creates and initializes a new ServerMessageHandler)
func NewServerMessageHandler(registry *ClientRegistry, hbChan chan<- storage.UserMsg) *ServerMessageHandler {
	mh := &ServerMessageHandler{
		registry:      registry,
		handlers:      make(map[storage.MsgType]MessageHandlerFunc),
		heartbeatChan: hbChan,
	}
	mh.registerHandlers() // 注册所有消息处理逻辑 (Register all message handling logic)
	return mh
}

// registerHandlers 将消息类型映射到其对应的处理函数
// (registerHandlers maps message types to their corresponding handler functions)
func (mh *ServerMessageHandler) registerHandlers() {
	mh.handlers[storage.Heartbeat] = handleHeartbeatMsg     // 心跳消息处理 (Heartbeat message handling)
	mh.handlers[storage.Connect] = handleConnectMsg         // 连接请求处理 (Connection request handling)
	mh.handlers[storage.Rename] = handleRenameMsg           // 重命名请求处理 (Rename request handling)
	mh.handlers[storage.ConnectTo] = handleConnectToMsg     // 请求P2P连接处理 (P2P connection request handling)
	mh.handlers[storage.ConnectAllow] = handleConnectAllowMsg // 允许P2P连接处理 (Allow P2P connection handling)
	mh.handlers[storage.ConnectDeny] = handleConnectDenyMsg   // 拒绝P2P连接处理 (Deny P2P connection handling)
	mh.handlers[storage.Search] = handleSearchMsg           // 搜索客户端处理 (Search client handling)
	mh.handlers[storage.SearchAll] = handleSearchAllMsg     // 搜索所有客户端处理 (Search all clients handling)
	mh.handlers[storage.Msg] = handleGenericMsg           // 通用消息处理 (Generic message handling)
}

// ProcessMessage 解析收到的UDP消息,并调用相应的处理函数
// (ProcessMessage parses the received UDP message and calls the corresponding handler function)
func (mh *ServerMessageHandler) ProcessMessage(conn *net.UDPConn, addr *net.UDPAddr, rawMsg []byte, n int) {
	var usermsg storage.UserMsg
	// 反序列化JSON消息 (Unmarshal JSON message)
	if err := json.Unmarshal(rawMsg[:n], &usermsg); err != nil {
		maxLength := 100
		if n < maxLength { maxLength = n }
		log.Printf("[WARN] Error unmarshalling message from %s: %v. Raw message (first %d bytes): %s\n", addr.String(), err, maxLength, string(rawMsg[:maxLength]))
		SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error: Malformed message format."}) // 通知客户端消息格式错误 (Notify client of malformed message)
		return
	}

	// log.Printf("[DEBUG] MH: Processing MsgType: %d from %s\n", usermsg.MsgType, addr.String())
	// 根据消息类型查找并执行处理函数 (Find and execute handler function based on message type)
	if handler, ok := mh.handlers[usermsg.MsgType]; ok {
		handler(conn, addr, &usermsg, mh.registry, mh.heartbeatChan)
	} else {
		// 未找到对应类型的处理器 (No handler found for this message type)
		log.Printf("[WARN] MH: No handler registered for message type %d from %s\n", usermsg.MsgType, addr.String())
	}
}

// --- Individual Message Handlers ---
// (各个消息类型的具体处理函数)

// handleHeartbeatMsg 处理心跳消息: 验证来源并将有效心跳放入通道
// (Handles heartbeat messages: validates source and puts valid heartbeats into the channel)
func handleHeartbeatMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	// 验证心跳消息中的地址字符串 (msg.Msg) 是否与实际发送地址 (addr) 对应的已注册客户端匹配
	// (Validate if the address string in the heartbeat message (msg.Msg) matches a registered client associated with the actual sender address (addr))
	clientInfo, _, exists := registry.GetClientByAddr(addr)

	if exists && clientInfo.Address.String() == msg.Msg { // msg.Msg 是客户端回显的其地址字符串 (msg.Msg is the client's echoed address string)
		// log.Printf("[DEBUG] MH: Heartbeat validated for client: %s (%s)\n", clientInfo.Name, msg.Msg)
		hbChan <- *msg // 将验证过的心跳消息传递给心跳检测逻辑 (Pass validated heartbeat message to heartbeat checking logic)
	} else if exists {
		// 心跳包载荷中的地址与记录的客户端地址不符 (Address in heartbeat payload does not match recorded client address)
        log.Printf("[WARN] MH: Received heartbeat from %s (Name: %s) but payload '%s' does not match its address string '%s'.\n", addr.String(), clientInfo.Name, msg.Msg, clientInfo.Address.String())
    } else {
		// 从一个未注册的地址收到心跳, 或者载荷地址不匹配 (Received heartbeat from an unregistered address, or payload address mismatch)
		log.Printf("[WARN] MH: Received heartbeat from %s with payload '%s', but this address is not registered or payload mismatch.\n", addr.String(), msg.Msg)
	}
}

// handleConnectMsg 处理新的客户端连接请求: 注册客户端并分配名称
// (Handles new client connection requests: registers the client and assigns a name)
func handleConnectMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	log.Printf("[INFO] MH: Client %s attempting to connect.\n", addr.String())

	// 检查此地址的客户端是否已存在 (Check if a client with this address already exists)
	_, _, existingClient := registry.GetClientByAddr(addr)
	if existingClient {
		log.Printf("[WARN] MH: Client %s is already connected. Ignoring new connect attempt.\n", addr.String())
		return
	}

	// 为新客户端生成唯一名称并注册 (Generate a unique name for the new client and register it)
	clientName := registry.GenerateUniqueClientName()
	newClient := &ClientInfo{Address: addr, Name: clientName}
	registry.AddClient(newClient)

	SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Registered. Your name is: " + clientName}) // 通知客户端其名称 (Inform client of its name)
	log.Printf("[INFO] MH: Client %s registered with name '%s'.\n", addr.String(), clientName)
}

// handleRenameMsg 处理客户端重命名请求
// (Handles client rename requests)
func handleRenameMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	newName := strings.TrimSpace(msg.Msg) // 获取并清理新名称 (Get and trim the new name)

	updatedClientInfo, err := registry.RenameClient(addr, newName) // 调用注册表方法执行重命名 (Call registry method to perform rename)
	if err != nil {
		log.Printf("[WARN] MH: Rename failed for %s to '%s': %v\n", addr.String(), newName, err)
		SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Rename failed: " + err.Error()}) // 将错误信息发送给客户端 (Send error message to client)
		return
	}

	// 根据重命名是否实际发生 (名称有无变化) 发送不同确认消息
	// (Send different confirmation messages based on whether the name actually changed)
	if updatedClientInfo.Name == newName {
		log.Printf("[INFO] MH: Client %s renamed to '%s'.\n", addr.String(), newName)
		SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Rename successful. Your new name is: " + newName})
	} else {
		SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Rename successful (name unchanged). Your name is still: " + updatedClientInfo.Name})
	}
}

// handleConnectToMsg 处理一个客户端请求连接到另一个客户端的逻辑
// (Handles the logic for one client requesting to connect to another)
func handleConnectToMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	targetName := msg.Msg // 目标客户端名称 (Target client name)
	// 查找发起请求的源客户端 (Find the source client making the request)
	sourceClientInfo, sourceClientName, foundSource := registry.GetClientByAddr(addr)

	if !foundSource { // 源客户端未注册 (Source client not registered)
		log.Printf("[WARN] MH: ConnectTo request from unregistered client: %s to target '%s'.\n", addr.String(), targetName)
		SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error: You are not registered. Cannot connect."})
		return
	}
	log.Printf("[INFO] MH: Client '%s' (%s) wants to connect with client '%s'.\n", sourceClientName, sourceClientInfo.Address.String(), targetName)

	// 查找目标客户端 (Find the target client)
	targetClientInfo, foundTarget := registry.GetClientByName(targetName)
	if foundTarget {
		if targetClientInfo.Address.String() == sourceClientInfo.Address.String() { // 检查是否尝试连接自身 (Check if trying to connect to self)
			log.Printf("[WARN] MH: Client '%s' (%s) attempted to connect to self ('%s').\n", sourceClientName, sourceClientInfo.Address.String(), targetName)
			SendMsg(conn, sourceClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "You cannot connect to yourself."})
			return
		}
		// 向目标客户端转发连接请求, 消息体为源客户端的名称
		// (Forward connection request to target client, message body is source client's name)
		SendMsg(conn, targetClientInfo.Address, storage.UserMsg{MsgType: storage.ConnectTo, Msg: sourceClientName})
		log.Printf("[INFO] MH: Sent connection request from '%s' to '%s' (%s).\n", sourceClientName, targetName, targetClientInfo.Address.String())
	} else { // 目标客户端不存在 (Target client does not exist)
		log.Printf("[WARN] MH: Client '%s' (%s) tried to connect to non-existent user '%s'.\n", sourceClientName, sourceClientInfo.Address.String(), targetName)
		SendMsg(conn, sourceClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "Connect to failed: User '" + targetName + "' does not exist."})
	}
}

// handleConnectAllowMsg 处理客户端允许P2P连接的响应
// (Handles client's response to allow a P2P connection)
func handleConnectAllowMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	originalRequesterName := msg.Msg // 这是最初发起连接请求的客户端的名称 (Name of the client who initially requested connection)
	// 查找批准连接的客户端 (当前消息的发送方) (Find the client who is allowing the connection - sender of current message)
	allowingClientInfo, allowingClientName, foundAllowing := registry.GetClientByAddr(addr)

	if !foundAllowing { // 批准方未注册 (Allowing client not registered)
		log.Printf("[WARN] MH: ConnectAllow from unregistered client: %s for '%s'.\n", addr.String(), originalRequesterName)
		return
	}
	log.Printf("[INFO] MH: Client '%s' (%s) allowed connection from '%s'.\n", allowingClientName, allowingClientInfo.Address.String(), originalRequesterName)

	// 查找最初的请求方 (Find the original requester)
	originalRequesterInfo, foundRequester := registry.GetClientByName(originalRequesterName)
	if foundRequester {
		// 构建包含双方地址的消息, 用于NAT穿透 (Construct message with both addresses for NAT traversal)
		// 格式: "批准方公网地址,请求方公网地址" (Format: "approver_public_address,requester_public_address")
		msgToRequester := storage.UserMsg{
			MsgType: storage.ConnectAllow,
			Msg:     allowingClientInfo.Address.String() + "," + originalRequesterInfo.Address.String(),
		}
		SendMsg(conn, originalRequesterInfo.Address, msgToRequester) // 将此信息发送给最初的请求方 (Send this info to original requester)
		log.Printf("[INFO] MH: Sent ConnectAllow confirmation to '%s' (%s) for connection with '%s' (%s).\n", originalRequesterName, originalRequesterInfo.Address.String(), allowingClientName, allowingClientInfo.Address.String())
	} else { // 最初的请求方已下线 (Original requester is now offline)
		log.Printf("[WARN] MH: Client '%s' (%s) allowed connection from '%s', but '%s' (original requester) no longer exists.\n", allowingClientName, allowingClientInfo.Address.String(), originalRequesterName, originalRequesterName)
		SendMsg(conn, allowingClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "User '" + originalRequesterName + "' (who you allowed) no longer exists."})
	}
}

// handleConnectDenyMsg 处理客户端拒绝P2P连接的响应
// (Handles client's response to deny a P2P connection)
func handleConnectDenyMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	originalRequesterName := msg.Msg // 最初请求连接的客户端名称 (Name of client who initially requested connection)
	// 查找拒绝连接的客户端 (当前消息发送方) (Find client denying connection - sender of current message)
	denyingClientInfo, denyingClientName, foundDenying := registry.GetClientByAddr(addr)

	if !foundDenying { // 拒绝方未注册 (Denying client not registered)
		log.Printf("[WARN] MH: ConnectDeny from unregistered client: %s for '%s'.\n", addr.String(), originalRequesterName)
		return
	}
	log.Printf("[INFO] MH: Client '%s' (%s) denied connection from '%s'.\n", denyingClientName, denyingClientInfo.Address.String(), originalRequesterName)

	// 查找最初的请求方 (Find original requester)
	originalRequesterInfo, foundRequester := registry.GetClientByName(originalRequesterName)
	if foundRequester {
		// 通知最初的请求方连接被拒绝, 消息内容为拒绝方的名称
		// (Notify original requester that connection was denied, message content is denier's name)
		msgToRequester := storage.UserMsg{MsgType: storage.ConnectDeny, Msg: denyingClientName}
		SendMsg(conn, originalRequesterInfo.Address, msgToRequester)
		log.Printf("[INFO] MH: Sent ConnectDeny notification to '%s' from '%s'.\n", originalRequesterName, denyingClientName)
	} else { // 最初的请求方已下线 (Original requester is offline)
		log.Printf("[WARN] MH: Client '%s' (%s) denied connection from '%s', but '%s' (original requester) no longer exists.\n", denyingClientName, denyingClientInfo.Address.String(), originalRequesterName, originalRequesterName)
		SendMsg(conn, denyingClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "User '" + originalRequesterName + "' (who you denied) no longer exists."})
	}
}

// handleSearchMsg 处理客户端搜索特定用户是否在线的请求
// (Handles client's request to search if a specific user is online)
func handleSearchMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	targetName := msg.Msg // 要搜索的目标客户端名称 (Target client name to search for)
	// 查找发起搜索的客户端 (Find client performing the search)
	requestingClientInfo, requestingClientName, foundRequester := registry.GetClientByAddr(addr)

	if !foundRequester { // 搜索方未注册 (Searching client not registered)
		log.Printf("[WARN] MH: Search request for '%s' from unregistered client: %s.\n", targetName, addr.String());
		SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error: You are not registered. Cannot perform search."})
		return
	}
	log.Printf("[INFO] MH: Client '%s' (%s) is searching for: '%s'.\n", requestingClientName, requestingClientInfo.Address.String(), targetName)

	// 在注册表中查找目标客户端 (Search for target client in registry)
	_, foundTarget := registry.GetClientByName(targetName)
	if foundTarget {
		SendMsg(conn, requestingClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "User '" + targetName + "' is online."})
	} else {
		SendMsg(conn, requestingClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "User '" + targetName + "' is not found."})
	}
}

// handleSearchAllMsg 处理客户端请求列出所有在线用户的逻辑
// (Handles client's request to list all online users)
func handleSearchAllMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	// 查找请求此列表的客户端 (Find client requesting this list)
	requestingClientInfo, requestingClientName, foundRequester := registry.GetClientByAddr(addr)
	if !foundRequester { // 请求方未注册 (Requester not registered)
		log.Printf("[WARN] MH: SearchAll request from unregistered client: %s.\n", addr.String());
		SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error: You are not registered. Cannot list users."})
		return
	}

	allClientsSnapshot := registry.GetAllClients() // 获取所有客户端信息的快照以安全迭代 (Get snapshot of all client info for safe iteration)
	log.Printf("[INFO] MH: Client '%s' (%s) requested to list all users. Total clients in snapshot: %d.\n", requestingClientName, requestingClientInfo.Address.String(), len(allClientsSnapshot))

	table, err := gotable.Create("Name", "Address") // 创建表格用于显示 (Create table for display)
	if err != nil {
		log.Printf("[ERROR] MH: Error creating table for SearchAll: %v\n", err)
		SendMsg(conn, requestingClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "Error generating user list on server."})
		return
	}

	count := 0
	// 遍历快照填充表格 (Iterate snapshot to populate table)
	for name, clientInfo := range allClientsSnapshot {
		displayName := name
		if clientInfo.Address.String() == requestingClientInfo.Address.String() { // 标记当前请求列表的客户端 (Mark the client who requested the list)
			displayName = name + " (*)"
		}
		table.AddRow([]string{displayName, clientInfo.Address.String()})
		count++
	}

	if count == 0 { // 理论上至少应有请求者本身 (Theoretically, at least the requester should be present)
		SendMsg(conn, requestingClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "No clients are currently registered."})
	} else {
		SendMsg(conn, requestingClientInfo.Address, storage.UserMsg{MsgType: storage.Msg, Msg: "\nConnected Clients ("+strconv.Itoa(count)+"):\n" + table.String()})
	}
}

// handleGenericMsg 处理未被其他特定处理器捕获的通用消息
// (Handles generic messages not caught by other specific handlers)
func handleGenericMsg(conn *net.UDPConn, addr *net.UDPAddr, msg *storage.UserMsg, registry *ClientRegistry, hbChan chan<- storage.UserMsg) {
	_, senderName, found := registry.GetClientByAddr(addr)
	if !found {
		senderName = "Unknown/Unregistered" // 消息来自未注册的客户端 (Message from an unregistered client)
	}
	log.Printf("[INFO] MH: General Msg from '%s' (%s): %s\n", senderName, addr.String(), msg.Msg)
	// 服务器通常不转发通用消息, 除非是聊天服务器等特定设计
	// (Server typically does not relay generic messages unless specifically designed, e.g., a chat server)
}


// --- Main Server Logic ---

var globalHeartbeatChan = make(chan storage.UserMsg, 20) // 全局心跳通道, 传递给消息处理器
                                                        // (Global heartbeat channel, passed to MessageHandler)
// Run 启动服务器的主逻辑
// (Run starts the main server logic)
func Run(port int) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	})
	if err != nil {
		log.Fatalf("[FATAL] Failed to listen on UDP port %d: %v\n", port, err) // 使用FATAL级别日志, 表示严重错误导致无法启动 (Use FATAL log level for critical startup errors)
		return
	}
	defer udpConn.Close() // 确保服务器退出时关闭连接 (Ensure connection is closed on server exit)
	log.Printf("[INFO] Server listening on: %s\n", udpConn.LocalAddr().String())

	clientRegistry := NewClientRegistry() // 创建客户端注册表实例 (Create ClientRegistry instance)
	messageHandler := NewServerMessageHandler(clientRegistry, globalHeartbeatChan) // 创建消息处理器实例 (Create MessageHandler instance)

	go CheckHeartbeat(udpConn, clientRegistry, globalHeartbeatChan) // 启动心跳检测协程 (Start heartbeat goroutine)

	buffer := make([]byte, 2048) // UDP消息读取缓冲区 (Buffer for reading UDP messages)
	for { // 服务器主循环: 持续读取和处理消息 (Main server loop: continuously read and process messages)
		n, addr, err := udpConn.ReadFromUDP(buffer) // 读取UDP数据 (Read UDP data)
		if err != nil {
			// 检查错误是否因为监听器关闭 (Check if error is due to listener being closed)
			if strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("[INFO] UDP listener closed, server loop exiting.\n")
				break // 监听器关闭, 退出循环 (Listener closed, exit loop)
			}
			log.Printf("[WARN] Error reading from UDP: %v\n", err)
			continue // 继续处理其他消息 (Continue processing other messages)
		}
		messageHandler.ProcessMessage(udpConn, addr, buffer, n) // 将消息交由处理器处理 (Pass message to handler)
	}
}

// SendMsg 是一个工具函数, 用于向指定的UDP地址发送UserMsg类型的消息
// (SendMsg is a utility function to send UserMsg type messages to a specified UDP address)
func SendMsg(conn *net.UDPConn, addr *net.UDPAddr, msg storage.UserMsg) error {
	bs, err := json.Marshal(msg) // 将消息序列化为JSON (Serialize message to JSON)
	if err != nil {
		log.Printf("[ERROR] SendMsg: Error marshalling message for %s: %v. Message: %+v\n", addr.String(), err, msg)
		return err
	}
	_, err = conn.WriteToUDP(bs, addr) // 通过UDP连接发送数据 (Send data via UDP connection)
	if err != nil {
		// 避免在连接已关闭时记录错误 (Avoid logging error if connection is already closed)
		if conn != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			log.Printf("[ERROR] SendMsg: Error sending message to %s: %v\n", addr.String(), err)
		}
		return err
	}
	return nil
}

// CheckHeartbeat 定期检查客户端的心跳, 并移除无响应的客户端
// (CheckHeartbeat periodically checks client heartbeats and removes unresponsive clients)
func CheckHeartbeat(conn *net.UDPConn, registry *ClientRegistry, hbChan <-chan storage.UserMsg) { // hbChan is read-only here
	ticker := time.NewTicker(15 * time.Second) // 心跳检查周期: 每15秒一次 (Heartbeat check interval: every 15 seconds)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C: // 定时器触发 (Timer triggered)
			clientsToPing := registry.GetAllClients() // 获取当前所有客户端的快照 (Get snapshot of all current clients)

			if len(clientsToPing) == 0 { // 如果没有客户端, 则跳过本轮检查 (If no clients, skip this check round)
				continue
			}
			// log.Printf("[DEBUG] CHB: Pinging %d clients for heartbeat.\n", len(clientsToPing))
			// 向所有客户端发送心跳请求 (Send heartbeat requests to all clients)
			for _, clientInfo := range clientsToPing {
				// 服务器发送客户端的地址字符串; 客户端必须在Msg字段中回显它以供验证
				// (Server sends client's address string; client must echo it back in Msg field for validation)
				err := SendMsg(conn, clientInfo.Address, storage.UserMsg{MsgType: storage.Heartbeat, Msg: clientInfo.Address.String()})
				if err != nil {
					// log.Printf("[WARN] CHB: Error sending heartbeat to %s (%s): %v.\n", clientInfo.Name, clientInfo.Address.String(), err)
				}
			}

			time.Sleep(5 * time.Second) // 等待心跳响应的宽限期 (Grace period for heartbeat responses)

			activeClientsInCycle := make(map[string]bool) // 存储本轮周期内响应了心跳的客户端 (Store clients that responded in this cycle)

			// 处理在宽限期内收到的所有心跳响应 (Process all heartbeat responses received within the grace period)
			for len(hbChan) > 0 {
				hbMsg := <-hbChan
				// 心跳消息的Msg字段包含客户端回显的其自身地址字符串
				// (The Msg field of heartbeat message contains the client's echoed own address string)
				// 我们需要找到这个地址对应的客户端名称, 以便标记为活跃
				// (We need to find the client name corresponding to this address to mark it as active)
				found := false
				// 检查响应的地址是否属于本轮ping的客户端 (Check if responding address belongs to a client pinged this cycle)
				for name, client := range clientsToPing { // 仅对照本轮ping过的客户端列表 (Only check against clients pinged this cycle)
					if client.Address.String() == hbMsg.Msg {
						activeClientsInCycle[name] = true
						found = true
						break
					}
				}
				if !found {
					// log.Printf("[DEBUG] CHB: Received heartbeat on channel for address %s not in current ping cycle map or mismatch.\n", hbMsg.Msg)
				}
			}

			var disconnectedClientNamesThisCycle []string
			// 识别哪些在本轮ping过的客户端没有响应 (Identify which clients pinged this cycle did not respond)
			for name := range clientsToPing {
				if _, isActive := activeClientsInCycle[name]; !isActive {
					// 为确保安全, 再次确认该客户端是否仍存在于注册表中 (For safety, reconfirm if client still exists in registry)
					// 因为它可能在GetAllClients之后, 到此检查之前, 因其他原因被移除 (e.g., client explicitly disconnected)
					// (Because it might have been removed for other reasons between GetAllClients and this check)
					if _, currentExists := registry.GetClientByName(name); currentExists {
						disconnectedClientNamesThisCycle = append(disconnectedClientNamesThisCycle, name)
					}
				}
			}

			// 移除所有超时的客户端 (Remove all timed-out clients)
			if len(disconnectedClientNamesThisCycle) > 0 {
				log.Printf("[INFO] CHB: Heartbeat timeout for clients: %v. Removing.\n", disconnectedClientNamesThisCycle)
				for _, name := range disconnectedClientNamesThisCycle {
					registry.RemoveClient(name) // RemoveClient方法内部会处理日志记录 (RemoveClient method handles logging internally)
				}
			}
		}
	}
}
