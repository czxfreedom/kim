package kim

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gobwas/pool/pbufio"
	"github.com/gobwas/ws"
	"github.com/klintcheng/kim/logger"
	"github.com/panjf2000/ants/v2"
	"github.com/segmentio/ksuid"
)

type Upgrader interface {
	Name() string
	Upgrade(rawconn net.Conn, rd *bufio.Reader, wr *bufio.Writer) (Conn, error)
}

// ServerOptions ServerOptions
type ServerOptions struct {
	Loginwait       time.Duration //登录超时
	Readwait        time.Duration //读超时
	Writewait       time.Duration //写超时
	MessageGPool    int
	ConnectionGPool int
}

type ServerOption func(*ServerOptions)

func WithMessageGPool(val int) ServerOption {
	return func(opts *ServerOptions) {
		opts.MessageGPool = val
	}
}

func WithConnectionGPool(val int) ServerOption {
	return func(opts *ServerOptions) {
		opts.ConnectionGPool = val
	}
}

// DefaultServer is a websocket implement of the DefaultServer
type DefaultServer struct {
	Upgrader
	listen string
	ServiceRegistration
	ChannelMap
	Acceptor
	MessageListener
	StateListener
	once    sync.Once
	options *ServerOptions
	quit    int32
}

// NewServer NewServer
func NewServer(listen string, service ServiceRegistration, upgrader Upgrader, options ...ServerOption) *DefaultServer {
	defaultOpts := &ServerOptions{
		Loginwait:       DefaultLoginWait,
		Readwait:        DefaultReadWait,
		Writewait:       DefaultWriteWait,
		MessageGPool:    DefaultMessageReadPool,
		ConnectionGPool: DefaultConnectionPool,
	}
	for _, option := range options {
		option(defaultOpts)
	}
	return &DefaultServer{
		listen:              listen,
		ServiceRegistration: service,
		options:             defaultOpts,
		Upgrader:            upgrader,
		quit:                0,
	}
}

// Start server
func (s *DefaultServer) Start() error {
	log := logger.WithFields(logger.Fields{
		"module": s.Name(),
		"listen": s.listen,
		"id":     s.ServiceID(),
		"func":   "Start",
	})

	if s.Acceptor == nil {
		s.Acceptor = new(defaultAcceptor)
	}
	if s.StateListener == nil {
		return fmt.Errorf("StateListener is nil")
	}
	if s.ChannelMap == nil {
		s.ChannelMap = NewChannels(100)
	}
	lst, err := net.Listen("tcp", s.listen)
	if err != nil {
		return err
	}
	// 采用协程池来增加复用
	rpool, _ := ants.NewPool(s.options.MessageGPool)
	defer func() {
		rpool.Release()
	}()
	log.Info("started")

	for {
		rawconn, err := lst.Accept()
		if err != nil {
			if rawconn != nil {
				rawconn.Close()
			}
			log.Warn(err)
			continue
		}
		run := func(rawconn net.Conn) {
			if atomic.LoadInt32(&s.quit) == 1 {
				return
			}
			rd := pbufio.GetReader(rawconn, ws.DefaultServerReadBufferSize)
			wr := pbufio.GetWriter(rawconn, ws.DefaultServerWriteBufferSize)
			defer func() {
				pbufio.PutReader(rd)
				pbufio.PutWriter(wr)
			}()
			conn, err := s.Upgrade(rawconn, rd, wr)
			if err != nil {
				log.Info(err)
				conn.Close()
				return
			}
			id, err := s.Accept(conn, s.options.Loginwait)
			if err != nil {
				_ = conn.WriteFrame(OpClose, []byte(err.Error()))
				conn.Close()
				return
			}

			if _, ok := s.Get(id); ok {
				_ = conn.WriteFrame(OpClose, []byte("channelId is repeated"))
				conn.Close()
				return
			}

			channel := NewChannel(id, conn, rpool)
			channel.SetReadWait(s.options.Readwait)
			channel.SetWriteWait(s.options.Writewait)

			s.Add(channel)

			log.Infof("accept channel - ID: %s RemoteAddr: %s", channel.ID(), channel.RemoteAddr())
			err = channel.Readloop(s.MessageListener)
			if err != nil {
				log.Info(err)
			}
			s.Remove(channel.ID())
			_ = s.Disconnect(channel.ID())
			channel.Close()
		}

		go run(rawconn)

		if atomic.LoadInt32(&s.quit) == 1 {
			break
		}
	}
	log.Info("quit")
	return nil
}

// Shutdown Shutdown
func (s *DefaultServer) Shutdown(ctx context.Context) error {
	log := logger.WithFields(logger.Fields{
		"module": s.Name(),
		"id":     s.ServiceID(),
	})
	s.once.Do(func() {
		defer func() {
			log.Infoln("shutdown")
		}()
		if atomic.CompareAndSwapInt32(&s.quit, 0, 1) {
			return
		}

		// close channels
		chanels := s.ChannelMap.All()
		for _, ch := range chanels {
			ch.Close()

			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
	})
	return nil
}

// string channelID
// []byte data
func (s *DefaultServer) Push(id string, data []byte) error {
	ch, ok := s.ChannelMap.Get(id)
	if !ok {
		return errors.New("channel no found")
	}
	return ch.Push(data)
}

// SetAcceptor SetAcceptor
func (s *DefaultServer) SetAcceptor(acceptor Acceptor) {
	s.Acceptor = acceptor
}

// SetMessageListener SetMessageListener
func (s *DefaultServer) SetMessageListener(listener MessageListener) {
	s.MessageListener = listener
}

// SetStateListener SetStateListener
func (s *DefaultServer) SetStateListener(listener StateListener) {
	s.StateListener = listener
}

// SetChannels SetChannels
func (s *DefaultServer) SetChannelMap(channels ChannelMap) {
	s.ChannelMap = channels
}

// SetReadWait set read wait duration
func (s *DefaultServer) SetReadWait(Readwait time.Duration) {
	s.options.Readwait = Readwait
}

type defaultAcceptor struct {
}

// Accept defaultAcceptor
func (a *defaultAcceptor) Accept(conn Conn, timeout time.Duration) (string, error) {
	return ksuid.New().String(), nil
}