package server

import (
	"encoding/json"
	"go-p2p/storage"
	"go-p2p/utils"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/liushuochen/gotable"
)

var mapAddr = make(map[string]*net.UDPAddr)

var heartbeatChan = make(chan storage.UserMsg, 10)

func Run(port int) {
	// 开始监听UDP
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	})
	if err != nil {
		log.Fatalln(err)
	}

	// 释放资源
	defer conn.Close()

	log.Printf("listen: [%s]\n", conn.LocalAddr().String())

	// 开启心跳检测
	go CheckHeartbeat(conn)

	b := make([]byte, 1024)
	var usermsg storage.UserMsg
	for {
		// 等待客户端消息响应
		n, addr, err := conn.ReadFromUDP(b)
		if err != nil {
			log.Println(err)
			return
		}

		// 反序列化消息
		if err := json.Unmarshal(b[:n], &usermsg); err != nil {
			log.Println("unknown msg type:", err)
			continue
		}

		// 根据消息类型进行处理
		switch usermsg.MsgType {
		case storage.Heartbeat:
			heartbeatChan <- usermsg
		case storage.Connect:
			log.Printf("[%s] connect", addr.String())
			mapAddr[utils.RandStr(5)] = addr
		case storage.Rename:
			log.Printf("[%s] to rename: %s", addr.String(), usermsg.Msg)
			if a, ok := mapAddr[usermsg.Msg]; ok {
				if a.String() == addr.String() {
					SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "rename success"})
				} else {
					SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "rename failed, name already exists"})
				}
				continue
			} else {
				for k, v := range mapAddr {
					if v.String() == addr.String() {
						mapAddr[usermsg.Msg] = v
						delete(mapAddr, k)
						SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "rename success"})
						break
					}
				}
			}
		case storage.ConnectTo:
			log.Printf("[%s] want to connect with [%s]", addr.String(), usermsg.Msg)
			if a, ok := mapAddr[usermsg.Msg]; ok {
				SendMsg(conn, a, storage.UserMsg{MsgType: storage.ConnectTo, Msg: addr.String()})
				continue
			}
			SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "connect to failed, user not exist"})
		case storage.ConnectAllow:
			log.Printf("[%s] allowed to connect from [%s]", addr.String(), usermsg.Msg)
			raddr, _ := net.ResolveUDPAddr("udp4", usermsg.Msg)
			SendMsg(conn, raddr, storage.UserMsg{MsgType: storage.ConnectAllow, Msg: addr.String() + "," + raddr.String()})
		case storage.ConnectDeny:
			log.Printf("[%s] refused to connect from [%s]", addr.String(), usermsg.Msg)
			raddr, _ := net.ResolveUDPAddr("udp4", usermsg.Msg)
			SendMsg(conn, raddr, storage.UserMsg{MsgType: storage.Msg, Msg: addr.String()})
		case storage.Search:
			log.Printf("[%s] to search: %s", addr.String(), usermsg.Msg)
			_, ok := mapAddr[usermsg.Msg]
			SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: strconv.FormatBool(ok)})
		case storage.SearchAll:
			table, err := gotable.Create("Index", "Name", "Addr")
			if err != nil {
				log.Println(err.Error())
				continue
			}
			index := 0
			for k, v := range mapAddr {
				index++
				if v.String() == addr.String() {
					table.AddRow([]string{"*", k, v.String()})
					continue
				}
				table.AddRow([]string{strconv.Itoa(index), k, v.String()})
			}
			SendMsg(conn, addr, storage.UserMsg{MsgType: storage.Msg, Msg: "\n" + table.String()})
		case storage.Msg:
			log.Printf("[%s]:%s", addr.String(), string(b[:n]))
		default:
			log.Printf("unknown msg type: %d, from [%s]\n", usermsg.MsgType, addr.String())
		}
	}
}

func SendMsg(conn *net.UDPConn, addr *net.UDPAddr, msg storage.UserMsg) error {
	bs, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = conn.WriteToUDP(bs, addr)
	if err != nil {
		return err
	}
	return nil
}

func CheckHeartbeat(conn *net.UDPConn) {
	for {
		time.Sleep(5 * time.Second)
		for k, v := range mapAddr {
			if err := SendHeartbeat(conn, v); err != nil {
				log.Printf("sendHeartbeat:%s, [%s] failed to check heartbeat, disconnect\n", err.Error(), v.String())
				delete(mapAddr, k)
				continue
			}
			select {
			case <-time.After(3 * time.Second):
				log.Printf("[%s] failed to check heartbeat, disconnect\n", v.String())
				delete(mapAddr, k)
			case msg := <-heartbeatChan:
				if msg.Msg != v.String() {
					log.Printf("received [%s] not match with [%s], failed to check heartbeat, disconnect\n", msg.Msg, v.String())
					delete(mapAddr, k)
					continue
				}
			}
		}
	}
}

func SendHeartbeat(conn *net.UDPConn, addr *net.UDPAddr) error {
	hbmsg := storage.UserMsg{MsgType: storage.Heartbeat, Msg: addr.String()}
	bs, err := json.Marshal(hbmsg)
	if err != nil {
		return err
	}
	_, err = conn.WriteToUDP(bs, addr)
	if err != nil {
		return err
	}
	return nil
}
