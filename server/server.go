package server

import (
	"encoding/json"
	"go-p2p/storage"
	"go-p2p/utils"
	"log"
	"net"
	"strconv"
	"strings"
	"sync" // Import sync package
	"time"

	"github.com/liushuochen/gotable"
)

// ClientInfo 存储客户端信息
type ClientInfo struct {
	Address *net.UDPAddr // 客户端地址
	Name    string       // 客户端名称
}

// mapClients 存储所有连接的客户端信息, 键为客户端名称
var mapClients = make(map[string]*ClientInfo)
var clientMutex = &sync.RWMutex{} // clientMutex 用于保护对mapClients的并发访问 (Mutex to protect concurrent access to mapClients)

var heartbeatChan = make(chan storage.UserMsg, 20) // 心跳消息通道, 增加容量以防突发 (Increased capacity for burst)

func Run(port int) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	})
	if err != nil {
		log.Printf("[ERROR] Failed to listen on UDP port %d: %v\n", port, err)
		return
	}
	defer conn.Close()
	log.Printf("[INFO] Server listening on: %s\n", conn.LocalAddr().String())

	go CheckHeartbeat(conn)

	b := make([]byte, 2048)
	var usermsg storage.UserMsg
	for {
		n, addr, err := conn.ReadFromUDP(b)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("[INFO] UDP listener closed, server loop exiting.\n")
				break
			}
			log.Printf("[WARN] Error reading from UDP: %v\n", err)
			continue
		}

		if err := json.Unmarshal(b[:n], &usermsg); err != nil {
			maxLength := 100
			if n < maxLength { maxLength = n }
			log.Printf("[WARN] Error unmarshalling message from %s: %v. Raw message (first %d bytes): %s\n", addr.String(), err, maxLength, string(b[:maxLength]))
			SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error: Malformed message format."})
			continue
		}

		// 根据消息类型进行处理 (Process based on message type)
		switch usermsg.MsgType {
		case storage.Heartbeat:
			// 心跳验证不直接修改mapClients, 但读取它. 实际的移除操作在CheckHeartbeat中处理.
			// (Heartbeat validation doesn't modify mapClients directly, but reads it. Actual removal is handled in CheckHeartbeat.)
			clientMutex.RLock() // 读取锁定 (Read lock)
			var clientNameForHeartbeat string
			clientFound := false
			// 根据心跳消息中的地址字符串(usermsg.Msg)来查找客户端
			// (Find client based on address string in heartbeat message (usermsg.Msg))
			for name, client := range mapClients {
				if client.Address.String() == usermsg.Msg {
					clientNameForHeartbeat = name
					clientFound = true
					break
				}
			}
			clientMutex.RUnlock() // 释放读取锁定 (Release read lock)

			if clientFound {
				// log.Printf("[DEBUG] Heartbeat validated for client: %s (%s)\n", clientNameForHeartbeat, usermsg.Msg)
				heartbeatChan <- usermsg
			} else {
				log.Printf("[WARN] Received heartbeat from actual address %s with payload address '%s'. Payload address does not match any known client's address string for validation.\n", addr.String(), usermsg.Msg)
			}

		case storage.Connect:
			log.Printf("[INFO] Client %s attempting to connect.\n", addr.String())
			clientMutex.Lock() // 写入锁定 (Write lock)
			clientName := utils.RandStr(5)
			// 循环直到生成一个当前未使用的客户端名称
			for {
				if _, exists := mapClients[clientName]; !exists {
					break
				}
				clientName = utils.RandStr(5)
			}
			mapClients[clientName] = &ClientInfo{Address: addr, Name: clientName}
			numClients := len(mapClients)
			clientMutex.Unlock() // 释放写入锁定 (Release write lock)
			SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Registered. Your name is: " + clientName})
			log.Printf("[INFO] Client %s registered with name '%s'. Total clients: %d\n", addr.String(), clientName, numClients)

		case storage.Rename:
			clientMutex.Lock() // 重命名操作全程写入锁定 (Write lock for the entire rename operation)
			var currentName string
			found := false
			for name, clientInfo := range mapClients {
				if clientInfo.Address.String() == addr.String() {
					currentName = name
					found = true
					break
				}
			}

			if !found {
				clientMutex.Unlock()
				log.Printf("[WARN] Rename attempt from unregistered address: %s\n", addr.String())
				SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Rename failed: You are not registered."})
				continue
			}

			newName := strings.TrimSpace(usermsg.Msg)
			log.Printf("[INFO] Client '%s' (%s) requests rename to: '%s'\n", currentName, addr.String(), newName)

			if newName == "" {
				clientMutex.Unlock()
				log.Printf("[WARN] Client '%s' (%s) attempted rename to empty string.\n", currentName, addr.String())
				SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Rename failed: New name cannot be empty."})
				continue
			}
			if newName == currentName {
				clientMutex.Unlock()
				SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Rename successful (name unchanged). Your name is still: " + currentName})
				continue
			}
			if _, exists := mapClients[newName]; exists { // 检查新名称是否已存在 (Check if new name already exists)
				clientMutex.Unlock()
				log.Printf("[WARN] Client '%s' (%s) attempted rename to existing name '%s'.\n", currentName, addr.String(), newName)
				SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Rename failed: Name '" + newName + "' already exists."})
				continue
			}

			clientInfo := mapClients[currentName]
			clientInfo.Name = newName
			mapClients[newName] = clientInfo // 使用新名称添加条目 (Add entry with new name)
			delete(mapClients, currentName)  // 删除旧名称的条目 (Delete entry with old name)
			clientMutex.Unlock() // 释放写入锁定 (Release write lock)
			SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Rename successful. Your new name is: " + newName})
			log.Printf("[INFO] Client %s renamed from '%s' to '%s'.\n", addr.String(), currentName, newName)

		case storage.ConnectTo:
			targetName := usermsg.Msg
			clientMutex.RLock() // 读取锁定 (Read lock)
			sourceClientName := "Unknown"
			var sourceClientAddr *net.UDPAddr // 用于记录源客户端地址 (To store source client address)
			foundSource := false
			for name, ci := range mapClients {
				if ci.Address.String() == addr.String() {
					sourceClientName = name
					sourceClientAddr = ci.Address
					foundSource = true
					break
				}
			}

			if !foundSource {
				clientMutex.RUnlock()
				log.Printf("[WARN] ConnectTo request from unregistered client: %s to target '%s'.\n", addr.String(), targetName)
				SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error: You are not registered. Cannot connect."})
				continue
			}

			log.Printf("[INFO] Client '%s' (%s) wants to connect with client '%s'.\n", sourceClientName, sourceClientAddr.String(), targetName)

			targetClientInfo, targetExists := mapClients[targetName] // 查找目标客户端 (Find target client)
			clientMutex.RUnlock() // 释放读取锁定 (Release read lock)

			if targetExists {
				if targetClientInfo.Address.String() == sourceClientAddr.String() { // 防止连接自身 (Prevent connecting to self)
					log.Printf("[WARN] Client '%s' (%s) attempted to connect to self ('%s').\n", sourceClientName, sourceClientAddr.String(), targetName)
					SendMsg(conn, sourceClientAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "You cannot connect to yourself."})
					continue
				}
				SendMsg(conn, targetClientInfo.Address, storage.UserMsg{MsgType: storage.ConnectTo, Msg: sourceClientName})
				log.Printf("[INFO] Sent connection request from '%s' to '%s' (%s).\n", sourceClientName, targetName, targetClientInfo.Address.String())
			} else {
				log.Printf("[WARN] Client '%s' (%s) tried to connect to non-existent user '%s'.\n", sourceClientName, sourceClientAddr.String(), targetName)
				SendMsg(conn, sourceClientAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "Connect to failed: User '" + targetName + "' does not exist."})
			}

		case storage.ConnectAllow:
			originalRequesterName := usermsg.Msg
			clientMutex.RLock() // 读取锁定 (Read lock)
			allowingClientName := "Unknown"
			var allowingClientAddr *net.UDPAddr
			foundAllowing := false
			for name, ci := range mapClients {
				if ci.Address.String() == addr.String() {
					allowingClientName = name
					allowingClientAddr = ci.Address
					foundAllowing = true
					break
				}
			}
			if !foundAllowing {
				clientMutex.RUnlock()
				log.Printf("[WARN] ConnectAllow from unregistered client: %s for '%s'.\n", addr.String(), originalRequesterName)
				continue
			}
			log.Printf("[INFO] Client '%s' (%s) allowed connection from '%s'.\n", allowingClientName, allowingClientAddr.String(), originalRequesterName)
			originalRequesterInfo, requesterExists := mapClients[originalRequesterName] // 检查请求方是否存在 (Check if requester exists)
			clientMutex.RUnlock() // 释放读取锁定 (Release read lock)

			if requesterExists {
				msgToS := storage.UserMsg{MsgType: storage.ConnectAllow, Msg: allowingClientAddr.String() + "," + originalRequesterInfo.Address.String()}
				SendMsg(conn, originalRequesterInfo.Address, msgToS)
				log.Printf("[INFO] Sent ConnectAllow confirmation to '%s' (%s) for connection with '%s' (%s).\n", originalRequesterName, originalRequesterInfo.Address.String(), allowingClientName, allowingClientAddr.String())
			} else {
				log.Printf("[WARN] Client '%s' (%s) allowed connection from '%s', but '%s' no longer exists.\n", allowingClientName, allowingClientAddr.String(), originalRequesterName, originalRequesterName)
				SendMsg(conn, allowingClientAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "User '" + originalRequesterName + "' (who you allowed) no longer exists."})
			}

		case storage.ConnectDeny:
			originalRequesterName := usermsg.Msg
			clientMutex.RLock() // 读取锁定 (Read lock)
			denyingClientName := "Unknown"
			var denyingClientAddr *net.UDPAddr
			foundDenying := false
			for name, ci := range mapClients {
				if ci.Address.String() == addr.String() {
					denyingClientName = name
					denyingClientAddr = ci.Address
					foundDenying = true
					break
				}
			}
			if !foundDenying {
				clientMutex.RUnlock()
				log.Printf("[WARN] ConnectDeny from unregistered client: %s for '%s'.\n", addr.String(), originalRequesterName)
				continue
			}
			log.Printf("[INFO] Client '%s' (%s) denied connection from '%s'.\n", denyingClientName, denyingClientAddr.String(), originalRequesterName)
			originalRequesterInfo, requesterExists := mapClients[originalRequesterName] // 检查请求方是否存在 (Check if requester exists)
			clientMutex.RUnlock() // 释放读取锁定 (Release read lock)

			if requesterExists {
				msgToS := storage.UserMsg{MsgType: storage.ConnectDeny, Msg: denyingClientName}
				SendMsg(conn, originalRequesterInfo.Address, msgToS)
				log.Printf("[INFO] Sent ConnectDeny notification to '%s' from '%s'.\n", originalRequesterName, denyingClientName)
			} else {
				log.Printf("[WARN] Client '%s' (%s) denied connection from '%s', but '%s' no longer exists.\n", denyingClientName, denyingClientAddr.String(), originalRequesterName, originalRequesterName)
				SendMsg(conn, denyingClientAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "User '" + originalRequesterName + "' (who you denied) no longer exists."})
			}

		case storage.Search:
			targetName := usermsg.Msg
			clientMutex.RLock() // 读取锁定 (Read lock)
			clientPerformingSearchName := "Unknown"
			var clientPerformingSearchAddr *net.UDPAddr
			foundSearcher := false
			for name, ci := range mapClients {
				if ci.Address.String() == addr.String() {
					clientPerformingSearchName = name
					clientPerformingSearchAddr = ci.Address
					foundSearcher = true
					break
				}
			}
			if !foundSearcher {
				clientMutex.RUnlock()
				log.Printf("[WARN] Search request for '%s' from unregistered client: %s.\n", targetName, addr.String());
				SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error: You are not registered. Cannot perform search."})
				continue
			}
			log.Printf("[INFO] Client '%s' (%s) is searching for: '%s'.\n", clientPerformingSearchName, clientPerformingSearchAddr.String(), targetName)
			_, ok := mapClients[targetName] // 检查目标是否存在 (Check if target exists)
			clientMutex.RUnlock() // 释放读取锁定 (Release read lock)
			if ok {
				SendMsg(conn, clientPerformingSearchAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "User '" + targetName + "' is online."})
			} else {
				SendMsg(conn, clientPerformingSearchAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "User '" + targetName + "' is not found."})
			}

		case storage.SearchAll:
			clientMutex.RLock() // 读取锁定 (Read lock)
			clientPerformingSearchAllName := "Unknown"
			var clientPerformingSearchAllAddr *net.UDPAddr
			foundSearchAll := false
			// 为安全迭代创建mapClients的快照 (Create snapshot of mapClients for safe iteration)
			var currentClientsSnapshot = make(map[string]*ClientInfo, len(mapClients))
            for name, client := range mapClients {
                currentClientsSnapshot[name] = client // 复制每个客户端信息 (Copy each client info)
				if client.Address.String() == addr.String() { // 同时查找请求客户端 (Also find the requesting client)
					clientPerformingSearchAllName = name
					clientPerformingSearchAllAddr = client.Address
					foundSearchAll = true
				}
            }
			numClients := len(currentClientsSnapshot)
			clientMutex.RUnlock() // 释放读取锁定 (Release read lock)

			if !foundSearchAll {
				log.Printf("[WARN] SearchAll request from client address %s not found in map.\n", addr.String());
				SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error: You are not registered. Cannot list users."})
				continue
			}

			log.Printf("[INFO] Client '%s' (%s) requested to list all users. Total clients: %d.\n", clientPerformingSearchAllName, clientPerformingSearchAllAddr.String(), numClients)
			table, err := gotable.Create("Name", "Address")
			if err != nil {
				log.Printf("[ERROR] Error creating table for SearchAll: %v\n", err)
				SendMsg(conn, clientPerformingSearchAllAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "Error generating user list on server."})
				continue
			}

			count := 0
			for name, clientInfo := range currentClientsSnapshot { // 迭代快照 (Iterate over snapshot)
				displayName := name
				if clientInfo.Address.String() == clientPerformingSearchAllAddr.String() {
					displayName = name + " (*)"
				}
				table.AddRow([]string{displayName, clientInfo.Address.String()})
				count++
			}
			if count == 0 {
				SendMsg(conn, clientPerformingSearchAllAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "No clients are currently registered."})
			} else {
				SendMsg(conn, clientPerformingSearchAllAddr, storage.UserMsg{MsgType: storage.Msg, Msg: "\nConnected Clients ("+strconv.Itoa(count)+"):\n" + table.String()})
			}

		case storage.Msg:
			clientMutex.RLock() // 读取锁定 (Read lock)
			senderName := "Unknown"
			for name, ci := range mapClients {
				if ci.Address.String() == addr.String() {
					senderName = name
					break
				}
			}
			clientMutex.RUnlock() // 释放读取锁定 (Release read lock)
			log.Printf("[INFO] General Msg from '%s' (%s): %s\n", senderName, addr.String(), usermsg.Msg)

		default: // 未知消息类型 (Unknown message type)
			clientMutex.RLock() // 读取锁定 (Read lock)
			sourceClientName := "Unknown"
			for name, client := range mapClients {
				if client.Address.String() == addr.String() {
					sourceClientName = name
					break
				}
			}
			clientMutex.RUnlock() // 释放读取锁定 (Release read lock)
			log.Printf("[WARN] Unknown message type: %d from client '%s' (%s). Message: %s\n", usermsg.MsgType, sourceClientName, addr.String(), usermsg.Msg)
		}
	}
}

// SendMsg 向指定地址发送UDP消息 (Sends UDP message to specified address)
func SendMsg(conn *net.UDPConn, addr *net.UDPAddr, msg storage.UserMsg) error {
	bs, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ERROR] Error marshalling message for %s: %v. Message: %+v\n", addr.String(), err, msg)
		return err
	}
	_, err = conn.WriteToUDP(bs, addr)
	if err != nil {
		// 避免在服务器关闭时记录连接已关闭的错误 (Avoid logging "use of closed network connection" when server is shutting down)
		if conn != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			log.Printf("[ERROR] Error sending message to %s: %v\n", addr.String(), err)
		}
		return err
	}
	return nil
}

// CheckHeartbeat 定期检查客户端心跳 (Periodically checks client heartbeats)
func CheckHeartbeat(conn *net.UDPConn) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			clientMutex.RLock() // 读取锁定mapClients以获取待ping客户端列表 (RLock mapClients to get list of clients to ping)
			clientsToPing := make(map[string]*ClientInfo)
			// 创建当前客户端列表的快照以进行ping操作 (Create snapshot of current client list for ping operation)
			for name, clientInfo := range mapClients {
				clientsToPing[name] = clientInfo
			}
			numCurrentClients := len(mapClients) // 获取当前客户端数量 (Get current number of clients)
			clientMutex.RUnlock() // 释放读取锁定 (Release RLock)

			if numCurrentClients == 0 { // 如果没有客户端, 则跳过 (If no clients, skip)
				continue
			}

			// log.Printf("[DEBUG] Pinging %d clients for heartbeat: %v\n", len(clientsToPing), func() []string { s := []string{}; for k := range clientsToPing { s = append(s, k) }; return s }())
			for name, clientInfo := range clientsToPing {
				err := SendMsg(conn, clientInfo.Address, storage.UserMsg{MsgType: storage.Heartbeat, Msg: clientInfo.Address.String()})
				if err != nil {
					// 此处记录错误, 但客户端的移除依赖于超时逻辑 (Log error here, but client removal relies on timeout logic)
					// log.Printf("[WARN] Error sending heartbeat to %s (%s): %v. Will be caught in timeout.\n", name, clientInfo.Address.String(), err)
				}
			}

			time.Sleep(5 * time.Second) // 等待响应的宽限期 (Grace period for responses)

			activeClientsInCycle := make(map[string]bool) // 存储本轮心跳周期中活跃的客户端 (Store active clients in this heartbeat cycle)
			for len(heartbeatChan) > 0 { // 处理所有在宽限期内收到的心跳响应 (Process all heartbeat responses received within grace period)
				hbMsg := <-heartbeatChan
				clientMutex.RLock() // 读取锁定以验证hbMsg (RLock to validate hbMsg)
				var clientNameResponding string
				foundClient := false
				// 验证hbMsg.Msg是否与本轮ping过的客户端地址匹配 (Validate if hbMsg.Msg matches address of a client pinged this cycle)
				for name, cInfo := range clientsToPing {
					if cInfo.Address.String() == hbMsg.Msg {
						// 再次确认该客户端仍然存在于全局map中 (Double check existence in global map)
						if _, stillExistsGlobally := mapClients[name]; stillExistsGlobally {
							clientNameResponding = name
							foundClient = true
							break
						}
					}
				}
				clientMutex.RUnlock() // 释放读取锁定 (Release RLock)

				if foundClient {
					activeClientsInCycle[clientNameResponding] = true
				} else {
					// log.Printf("[WARN] Heartbeat response with address %s in Msg field does not match any client pinged in this cycle or client no longer exists globally.\n", hbMsg.Msg)
				}
			}

			var disconnectedClientNamesThisCycle []string
			clientMutex.RLock() // 读取锁定以迭代clientsToPing并对照mapClients (RLock to iterate clientsToPing and check against mapClients)
			// 找出哪些在本轮ping过的客户端没有响应 (Find which clients pinged this round did not respond)
			for name, clientInfoToPing := range clientsToPing {
				if _, isActive := activeClientsInCycle[name]; !isActive {
					// 检查客户端是否仍然存在于全局map中 (Check if client still exists in global map)
					if currentClientInfo, stillExistsInGlobalMap := mapClients[name]; stillExistsInGlobalMap {
						// 并确保是我们ping的同一个客户端实例 (And ensure it's the same client instance we pinged)
						if currentClientInfo.Address.String() == clientInfoToPing.Address.String() {
							disconnectedClientNamesThisCycle = append(disconnectedClientNamesThisCycle, name)
						}
					}
				}
			}
			clientMutex.RUnlock() // 释放读取锁定 (Release RLock)

			if len(disconnectedClientNamesThisCycle) > 0 {
				clientMutex.Lock() // 写入锁定以修改mapClients (Lock for modifying mapClients)
				removedCount := 0
				for _, name := range disconnectedClientNamesThisCycle {
					// 在持有写入锁的情况下再次检查客户端是否存在, 避免竞态条件 (Recheck client existence under write lock to avoid race conditions)
					if clientInfo, ok := mapClients[name]; ok {
						log.Printf("[INFO] Client '%s' (%s) timed out. Disconnecting. Total clients before: %d\n", name, clientInfo.Address.String(), len(mapClients))
						delete(mapClients, name) // 从map中删除 (Delete from map)
						removedCount++
					}
				}
				currentTotal := len(mapClients)
				clientMutex.Unlock() // 释放写入锁定 (Release write lock)
				if removedCount > 0 {
					log.Printf("[INFO] Heartbeat: Removed %d clients. Total clients now: %d\n", removedCount, currentTotal)
				}
			}
		}
	}
}
