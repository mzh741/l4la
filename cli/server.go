package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/hzyitc/l4la"
	"github.com/hzyitc/mnh/log"
	"golang.org/x/net/netutil"
)

type Server struct {
	service string

	conns sync.Map
}

func NewServer(port int, server string) {
	local := "[::]:" + strconv.Itoa(port)
	listener, err := net.Listen("tcp", local)
	if err != nil {
		log.Error(err.Error())
		return
	}
	log.Info("Listening at " + listener.Addr().String())

	s := &Server{
		service: server,

		conns: sync.Map{},
	}

	// Set socket options
	listener = netutil.LimitListener(listener, 1024)             // Limit the number of open connections
	listener = tcpKeepAliveListener{listener.(*net.TCPListener)} // Set TCP keepalive

	s.main(listener)
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)

	// Set socket options
	fd, err := tc.File()
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	// Set SO_KEEPALIVE
	err = syscall.SetsockoptInt(int(fd.Fd()), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1)
	if err != nil {
		fmt.Printf("SetsockoptInt SO_KEEPALIVE error: %v\n", err)
	}

	// Set TCP_NODELAY
	err = syscall.SetsockoptInt(int(fd.Fd()), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
	if err != nil {
		fmt.Printf("SetsockoptInt TCP_NODELAY error: %v\n", err)
	}

	// Set TCP_QUICKACK
	err = syscall.SetsockoptInt(int(fd.Fd()), syscall.IPPROTO_TCP, 12, 1) // 12 is the constant for TCP_QUICKACK in Linux
	if err != nil {
		fmt.Printf("SetsockoptInt TCP_QUICKACK error: %v\n", err)
	}

	// Set TCP_USER_TIMEOUT
	err = syscall.SetsockoptInt(int(fd.Fd()), syscall.IPPROTO_TCP, 37, 60000) // 37 is the constant for TCP_USER_TIMEOUT in Linux
	if err != nil {
		fmt.Printf("SetsockoptInt TCP_USER_TIMEOUT error: %v\n", err)
	}

	// Set TCP_CONGESTION
	err = syscall.SetsockoptString(int(fd.Fd()), syscall.IPPROTO_TCP, 13, "bbr") // 13 is the constant for TCP_CONGESTION in Linux
	if err != nil {
		fmt.Printf("SetsockoptString TCP_CONGESTION error: %v\n", err)
	}

	// Set TCP_NOTSENT_LOWAT
	err = syscall.SetsockoptInt(int(fd.Fd()), syscall.IPPROTO_TCP, 23, 128000) // 23 is the constant for TCP_NOTSENT_LOWAT in Linux
	if err != nil {
		fmt.Printf("SetsockoptInt TCP_NOTSENT_LOWAT error: %v\n", err)
	}

	// Set SO_MARK
	err = syscall.SetsockoptInt(int(fd.Fd()), syscall.SOL_SOCKET, 36, 42069) // 36 is the constant for SO_MARK in Linux
	if err != nil {
		fmt.Printf("SetsockoptInt SO_MARK error: %v\n", err)
	}

	return tc, nil
}

func (s *Server) main(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Error("server_main error", err.Error())
			return
		}

		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	log.Info("New connection from " + conn.RemoteAddr().String())

	buf := make([]byte, 16)
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		log.Error("server_handle read error:", err.Error())
		conn.Close()
		return
	}

	id, err := uuid.FromBytes(buf)
	if err != nil {
		log.Error("server_handle read uuid error:", err.Error())
		conn.Close()
		return
	}

	if id == uuid.Nil {
		service, err := net.Dial("tcp", s.service)
		if err != nil {
			log.Error("server_handle dial error:", err.Error())
			conn.Close()
			return
		}

		id = uuid.New()

		c, err := l4la.NewConn(context.TODO())
		if err != nil {
			log.Error("server_handle newRemoteConn error:", err.Error())
			conn.Close()
			service.Close()
			return
		}
		log.Info("Created new connection", id.String())
		s.conns.Store(id, c)

		go func() {
			io.Copy(c, service)
			c.Close()
		}()

		go func() {
			io.Copy(service, c)
			service.Close()
		}()

		go func() {
			<-c.WaitClose()
			s.conns.Delete(id)
			log.Info("Closed connection", id.String())
		}()

		buf, err := id.MarshalBinary()
		if err != nil {
			log.Error("server_handle id.MarshalBinary error:", err.Error())
			conn.Close()
			c.Close()
			return
		}

		n, err := conn.Write(buf)
		if err != nil {
			log.Error("server_handle write error:", err.Error())
			conn.Close()
			c.Close()
			return
		}
		if n != len(buf) {
			log.Error("server_handle write error:", fmt.Errorf("sent %d bytes instand of %d bytes", n, len(buf)))
			conn.Close()
			c.Close()
			return
		}

		log.Info("New connection to ", id.String())
		c.AddRemoteConn(conn)
	} else {
		v, ok := s.conns.Load(id)
		if !ok {
			log.Error("Unknown conn id: ", id.String())
			conn.Close()
			return
		}

		c := v.(*l4la.Conn)

		log.Info("New connection to ", id.String())
		c.AddRemoteConn(conn)
	}

}
