package client

import (
	"encoding/json"
	"fmt"
	"go-p2p/storage"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

var lrconn *net.UDPConn
var tcpconn *net.TCPConn
var raddr, laddr *net.UDPAddr
var signalMsg = make(chan storage.UserMsg)

func Run(port int, serverAddr string) {

	laddr = &net.UDPAddr{IP: net.IPv4zero, Port: port}
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
			log.Printf("[%s]:退出\n", conn.LocalAddr().String())
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
			conn.Write(b[:n])
		case storage.ConnectTo:
			log.Printf("收到[%s]连接消息，是否同意(allow>addr/deny>addr):\n", usermsg.Msg)
			raddr, err = net.ResolveUDPAddr("udp4", usermsg.Msg)
			if err != nil {
				log.Println(err)
				continue
			}
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
			lrconn, err = ConnectWithUDP(laddr, raddr)
			if err != nil {
				log.Printf("连接[%s]失败:%s\n", raddr.String(), err)
				return
			}
			go RecvMsgWithUDP()
		case storage.ConnectDeny:
			log.Printf("[%s]拒绝连接\n", usermsg.Msg)
		case storage.Msg:
			fmt.Printf("[%s]:%s\n", caddr.String(), usermsg.Msg)
			signalMsg <- usermsg
		default:
			log.Printf("未知的消息类型:%d, 来自[%s]\n", usermsg.MsgType, caddr.String())
		}
	}
}

// 用户指令
func UserCommand(conn *net.UDPConn) {
	// 显示问题暂不启用
	// go PromptRun()
	go func() {
		var msg string
		var err error
		// 显示问题暂不启用
		// for msg = range signalInput {
		for {
			msg = ReadInput()
			index := strings.Index(msg, ">")
			if index == -1 {
				log.Println("指令格式错误")
				continue
			}
			switch msg[:index] {
			case "all":
				err = SendUDPMsg(conn, storage.UserMsg{MsgType: storage.SearchAll})
				<-signalMsg
			case "connectto":
				err = SendUDPMsg(conn, storage.UserMsg{MsgType: storage.ConnectTo, Msg: msg[index+1:]})
			case "allow":
				_ = SendUDPMsg(conn, storage.UserMsg{MsgType: storage.ConnectAllow, Msg: msg[index+1:]})
				raddr.Port = raddr.Port + 100
				laddr.Port = laddr.Port + 100
				lrconn, err = ConnectWithUDP(laddr, raddr)
				if err != nil {
					log.Printf("连接[%s]失败:%s\n", raddr.String(), err)
					return
				}
				go RecvMsgWithUDP()
			case "deny":
				err = SendUDPMsg(conn, storage.UserMsg{MsgType: storage.ConnectDeny, Msg: msg[index+1:]})
			case "msg":
				if lrconn != nil {
					err = SendUDPMsg(lrconn, storage.UserMsg{MsgType: storage.Msg, Msg: msg[index+1:]})
				}
				if tcpconn != nil {
					err = SendTCPMsg(tcpconn, storage.UserMsg{MsgType: storage.Msg, Msg: msg[index+1:]})
				}
			case "rename":
				err = SendUDPMsg(conn, storage.UserMsg{MsgType: storage.Rename, Msg: msg[index+1:]})
			case "changetotcp":
				_ = SendUDPMsg(lrconn, storage.UserMsg{MsgType: storage.ChangeToTCP})
				laddr = lrconn.LocalAddr().(*net.UDPAddr)
				lrconn.Close()
				time.Sleep(3 * time.Second)
				tcpconn, err = ConnectWithTCP(&net.TCPAddr{IP: laddr.IP, Port: laddr.Port}, &net.TCPAddr{IP: raddr.IP, Port: raddr.Port})
				if err != nil {
					return
				}
				go RecvMsgWithTCP()
			case "exit":
				if conn != nil {
					conn.Close()
				}
				if lrconn != nil {
					lrconn.Close()
				}
				return
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

func SendUDPMsg(conn *net.UDPConn, msg storage.UserMsg) error {
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

func ConnectWithUDP(tladdr, traddr *net.UDPAddr) (*net.UDPConn, error) {
	rlconn, err := net.DialUDP("udp4", tladdr, traddr)
	if err != nil {
		return nil, err
	}
	bs, _ := json.Marshal(storage.UserMsg{MsgType: storage.Msg})
	_, err = rlconn.Write(bs)
	if err != nil {
		return nil, err
	}
	livePrefix = fmt.Sprintf("[UDP %s]", laddr.String())
	return rlconn, nil
}

func RecvMsgWithUDP() {
	bs := make([]byte, 1024)
	var usermsg storage.UserMsg
	failCount := 0
	for {
		n, caddr, err := lrconn.ReadFromUDP(bs)
		if err != nil {
			if failCount > 3 {
				log.Printf("[%s]:UDP连接退出\n", lrconn.LocalAddr().String())
				return
			}
			failCount++
			continue
		}
		err = json.Unmarshal(bs[:n], &usermsg)
		if err != nil {
			log.Printf("反序列化失败:%s\n", err)
			continue
		}
		switch usermsg.MsgType {
		case storage.Msg:
			fmt.Printf("[%s]:%s\n", caddr.String(), usermsg.Msg)
		case storage.ChangeToTCP:
			lrconn.Close()
			time.Sleep(time.Second)
			tcpconn, err = ConnectWithTCP(&net.TCPAddr{IP: laddr.IP, Port: laddr.Port}, &net.TCPAddr{IP: raddr.IP, Port: raddr.Port})
			if err != nil {
				return
			}
			go RecvMsgWithTCP()
		default:
			log.Printf("未知的消息类型:%d, 来自[%s]\n", usermsg.MsgType, caddr.String())
		}
	}
}

func SendTCPMsg(conn *net.TCPConn, msg storage.UserMsg) error {
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

func ConnectWithTCP(laddr, raddr *net.TCPAddr) (*net.TCPConn, error) {
	tcpConn, err := net.DialTCP("tcp4", laddr, raddr)
	if err != nil {
		log.Printf("与客户端[%s]建立TCP失败:%s\n", raddr, err)
		return nil, err
	}
	livePrefix = fmt.Sprintf("[TCP %s]", laddr.String())
	return tcpConn, nil
}

func RecvMsgWithTCP() {
	bs := make([]byte, 1024)
	var usermsg storage.UserMsg
	for {
		n, err := tcpconn.Read(bs)
		if n == 0 {
			log.Printf("[%s]:TCP连接退出\n", raddr.String())
			return
		}
		if err != nil && err != io.EOF {
			log.Printf("接收信息失败：%s\n", err)
			return
		}
		err = json.Unmarshal(bs[:n], &usermsg)
		if err != nil {
			log.Printf("反序列化失败:%s\n", err)
			continue
		}
		switch usermsg.MsgType {
		case storage.Msg:
			fmt.Printf("[%s]:%s\n", raddr.String(), usermsg.Msg)
		default:
			log.Printf("未知的消息类型:%d, 来自[%s]\n", usermsg.MsgType, raddr.String())
		}
	}
}
