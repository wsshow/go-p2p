# go-p2p

## 描述
使用UDP进行内网穿透，穿透完成后可切换TCP进行连接

## 基本功能
1. UDP内网穿透
2. TCP数据传输

## 使用
```bash
server: sudo go-p2p s -p 9000
client: sudo go-p2p c -p 9001 -s [ip]:9000
```
