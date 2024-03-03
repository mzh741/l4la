package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"syscall"

	"github.com/google/uuid"
	"github.com/hzyitc/l4la"
	"github.com/hzyitc/mnh/log"
)

type Client struct {
	server      string
	conn_number int
}

func NewClient(port int, server string, conn_number int) {
	local := "[::]:" + strconv.Itoa(port)
	listener, err := net.Listen("tcp", local)
	if err != nil {
		log.Error(err.Error())
		return
	}
	log.Info("Listening at " + listener.Addr().String())

	c := &Client{
		server:      server,
		conn_number: conn_number,
	}
	c.main(listener)
}

func (c *Client) main(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Error("client_main error", err.Error())
			return
		}

		go c.handle(conn)
	}
}

func (c *Client) handle(local net.Conn) {
	log.Info("New connection from " + local.RemoteAddr().String())

	cc, err := l4la.NewConn(context.TODO())
	if err != nil {
		log.Error("client_handle NewConn error:", err.Error())
		local.Close()
		return
	}

	go func() {
		io.Copy(cc, local)
		cc.Close()
	}()

	go func() {
		io.Copy(local, cc)
		local.Close()
	}()

	conn, err := newRemoteConn(c.server, uuid.Nil)
	if err != nil {
		log.Error("client_handle newRemoteConn error:", err.Error())
		cc.Close()
		return
	}

	buf := make([]byte, 16)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		log.Error("client_handle read error:", err.Error())
		cc.Close()
		conn.Close()
		return
	}

	id, err := uuid.FromBytes(buf)
	if err != nil {
		log.Error("client_handle read uuid error:", err.Error())
		cc.Close()
		conn.Close()
		return
	}

	cc.AddRemoteConn(conn)

	for i := 1; i < c.conn_number; i++ {
		conn, err := newRemoteConn(c.server, id)
		if err != nil {
			log.Error("client_handle error:", err.Error())
			cc.Close()
			break
		}
		cc.AddRemoteConn(conn)
	}
}

func newRemoteConn(server string, id uuid.UUID) (net.Conn, error) {
	data, err := id.MarshalBinary()
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				// Set SO_KEEPALIVE
				err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1)
				if err != nil {
					fmt.Printf("SetsockoptInt SO_KEEPALIVE error: %v\n", err)
				}

				// Set TCP_NODELAY
				err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
				if err != nil {
					fmt.Printf("SetsockoptInt TCP_NODELAY error: %v\n", err)
				}

				// Set TCP_QUICKACK
				err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, 12, 1) // 12 is the constant for TCP_QUICKACK in Linux
				if err != nil {
					fmt.Printf("SetsockoptInt TCP_QUICKACK error: %v\n", err)
				}

				// Set TCP_USER_TIMEOUT
				err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, 37, 60000) // 37 is the constant for TCP_USER_TIMEOUT in Linux
				if err != nil {
					fmt.Printf("SetsockoptInt TCP_USER_TIMEOUT error: %v\n", err)
				}

				// Set TCP_CONGESTION
				err = syscall.SetsockoptString(int(fd), syscall.IPPROTO_TCP, 13, "bbr") // 13 is the constant for TCP_CONGESTION in Linux
				if err != nil {
					fmt.Printf("SetsockoptString TCP_CONGESTION error: %v\n", err)
				}

				// Set TCP_NOTSENT_LOWAT
				err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, 23, 128000) // 23 is the constant for TCP_NOTSENT_LOWAT in Linux
				if err != nil {
					fmt.Printf("SetsockoptInt TCP_NOTSENT_LOWAT error: %v\n", err)
				}

				// Set SO_MARK
				err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 36, 42069) // 36 is the constant for SO_MARK in Linux
				if err != nil {
					fmt.Printf("SetsockoptInt SO_MARK error: %v\n", err)
				}

			})
		},
	}

	conn, err := dialer.Dial("tcp", server)
	if err != nil {
		return nil, err
	}

	n, err := conn.Write(data)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if n != len(data) {
		conn.Close()
		return nil, fmt.Errorf("sent %d bytes instand of %d bytes", n, len(data))
	}

	return conn, nil
}
