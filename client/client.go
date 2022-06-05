package client

import (
	"encoding/json"
	"fmt"
	"go-p2p/storage"
	"log"
	"net"
	"strings"
)

var lrconn *net.UDPConn

func Run(port int, serverAddr string) {
	raddr := &net.UDPAddr{IP: net.IPv4zero, Port: port}
	laddr := &net.UDPAddr{IP: net.IPv4zero, Port: port}
	saddr, _ := net.ResolveUDPAddr("udp4", serverAddr)

	log.Printf("本机地址[%s]\n", laddr)

	conn, err := net.DialUDP("udp4", laddr, saddr)
	if err != nil {
		log.Printf("连接服务器失败:%s\n", err)
		return
	}

	defer conn.Close()

	// 发送连接消息
	bs, err := json.Marshal(storage.UserMsg{MsgType: storage.Connect})
	if err != nil {
		log.Printf("序列化失败:%s\n", err)
		return
	}
	_, err = conn.Write(bs)
	if err != nil {
		log.Printf("发送连接消息失败:%s\n", err)
		return
	}

	log.Println("连接消息发送成功")

	UserCommand(conn)

	b := make([]byte, 1024)
	var usermsg storage.UserMsg
	for {
		n, caddr, err := conn.ReadFromUDP(b)
		if err != nil {
			log.Printf("接收数据失败:%s\n", err)
			return
		}
		err = json.Unmarshal(b[:n], &usermsg)
		if err != nil {
			log.Printf("反序列化失败:%s\n", err)
			continue
		}
		// 根据消息类型进行处理
		switch usermsg.MsgType {
		case storage.Heartbeat:
			// log.Printf("收到[%s]心跳消息:%s", raddr.String(), string(b[:n]))
			conn.Write(b[:n])
		case storage.ConnectTo:
			log.Printf("收到[%s]连接消息，是否同意(allow>addr/deny>addr):\n", usermsg.Msg)
		case storage.ConnectAllow:
			ss := strings.Split(usermsg.Msg, ",")
			if len(ss) != 2 {
				log.Println("连接消息格式错误")
				return
			}
			log.Printf("[%s]同意连接\n", ss[0])
			raddr, _ = net.ResolveUDPAddr("udp4", ss[0])
			laddr, _ = net.ResolveUDPAddr("udp4", ss[1])
			raddr.Port = raddr.Port + 100
			laddr.Port = laddr.Port + 100
			lrconn, err = Connect(laddr, raddr)
			if err != nil {
				log.Printf("连接[%s]失败:%s\n", raddr.String(), err)
				return
			}
			go RecvMsg()
		case storage.ConnectDeny:
			log.Printf("[%s]拒绝连接\n", usermsg.Msg)
		case storage.Msg:
			log.Printf("收到[%s]消息:%s\n", caddr.String(), usermsg.Msg)
		default:
			log.Printf("未知的消息类型:%d, 来自[%s]\n", usermsg.MsgType, caddr.String())
		}
	}
}

// 用户指令
func UserCommand(conn *net.UDPConn) {
	go func() {
		var msg string
		var err error
		for {
			fmt.Scanln(&msg)
			index := strings.Index(msg, ">")
			if index == -1 {
				log.Println("指令格式错误")
				continue
			}
			switch msg[:index] {
			case "all":
				err = SendMsg(conn, storage.UserMsg{MsgType: storage.SearchAll})
			case "connectto":
				err = SendMsg(conn, storage.UserMsg{MsgType: storage.ConnectTo, Msg: msg[index+1:]})
			case "msg":
				err = SendMsg(conn, storage.UserMsg{MsgType: storage.Msg, Msg: msg[index+1:]})
			case "allow":
				_ = SendMsg(conn, storage.UserMsg{MsgType: storage.ConnectAllow, Msg: msg[index+1:]})
				raddr, _ := net.ResolveUDPAddr("udp4", msg[index+1:])
				laddr := conn.LocalAddr().(*net.UDPAddr)
				raddr.Port = raddr.Port + 100
				laddr.Port = laddr.Port + 100
				lrconn, err = Connect(laddr, raddr)
				if err != nil {
					log.Printf("连接[%s]失败:%s\n", raddr.String(), err)
					return
				}
				go RecvMsg()
			case "deny":
				err = SendMsg(conn, storage.UserMsg{MsgType: storage.ConnectDeny, Msg: msg[index+1:]})
			case "send":
				err = SendMsg(lrconn, storage.UserMsg{MsgType: storage.Msg, Msg: msg[index+1:]})
			default:
				log.Println("未知的指令")
				continue
			}
			if err != nil {
				log.Printf("发送指令失败:%s\n", err)
				continue
			}
		}
	}()
}

func SendMsg(conn *net.UDPConn, msg storage.UserMsg) error {
	bs, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = conn.Write(bs)
	if err != nil {
		return err
	}
	return nil
}

func Connect(laddr, raddr *net.UDPAddr) (*net.UDPConn, error) {
	rlconn, err := net.DialUDP("udp4", laddr, raddr)
	if err != nil {
		return nil, err
	}
	bs, _ := json.Marshal(storage.UserMsg{MsgType: storage.Msg, Msg: "Connect"})
	_, err = rlconn.Write(bs)
	if err != nil {
		return nil, err
	}
	return rlconn, nil
}

func RecvMsg() {
	bs := make([]byte, 1024)
	var usermsg storage.UserMsg
	for {
		n, caddr, err := lrconn.ReadFromUDP(bs)
		if err != nil {
			log.Printf("接收消息失败:%s\n", err)
			continue
		}
		err = json.Unmarshal(bs[:n], &usermsg)
		if err != nil {
			log.Printf("反序列化失败:%s\n", err)
			continue
		}
		switch usermsg.MsgType {
		case storage.Msg:
			log.Printf("收到[%s]消息:%s\n", caddr.String(), usermsg.Msg)
		default:
			log.Printf("未知的消息类型:%d, 来自[%s]\n", usermsg.MsgType, caddr.String())
		}
	}
}
