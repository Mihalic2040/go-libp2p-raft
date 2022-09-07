package libp2praft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	logging "github.com/ipfs/go-log/v2"
	gostream "github.com/libp2p/go-libp2p-gostream"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const RaftProtocol protocol.ID = "/raft/1.0.0/rpc"

var raftLogger = logging.Logger("raftlib")

// this implements github.com/hashicorp/go-hclog
type hcLogToLogger struct {
	extraArgs []interface{}
	name      string
}

func (log *hcLogToLogger) formatArgs(args []interface{}) string {
	result := ""
	args = append(args, log.extraArgs)
	for i := 0; i < len(args); i = i + 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		val := args[i+1]
		result += fmt.Sprintf(" %s=%s.", key, val)
	}
	return result
}

func (log *hcLogToLogger) format(msg string, args []interface{}) string {
	argstr := log.formatArgs(args)
	if len(argstr) > 0 {
		argstr = ". Args: " + argstr
	}
	name := log.name
	if len(name) > 0 {
		name += ": "
	}
	return name + msg + argstr
}

func (log *hcLogToLogger) Log(level hclog.Level, msg string, args ...interface{}) {
	switch level {
	case hclog.Trace, hclog.Debug:
		log.Debug(msg, args)
	case hclog.NoLevel, hclog.Info:
		log.Info(msg, args)
	case hclog.Warn:
		log.Warn(msg, args)
	case hclog.Error:
		log.Error(msg, args)
	default:
		log.Warn(msg, args)
	}
}

func (log *hcLogToLogger) Trace(msg string, args ...interface{}) {
	raftLogger.Debug(log.format(msg, args))
}

func (log *hcLogToLogger) Debug(msg string, args ...interface{}) {
	raftLogger.Debug(log.format(msg, args))
}

func (log *hcLogToLogger) Info(msg string, args ...interface{}) {
	raftLogger.Info(log.format(msg, args))
}

func (log *hcLogToLogger) Warn(msg string, args ...interface{}) {
	raftLogger.Warn(log.format(msg, args))
}

func (log *hcLogToLogger) Error(msg string, args ...interface{}) {
	raftLogger.Error(log.format(msg, args))
}

func (log *hcLogToLogger) IsTrace() bool {
	return true
}

func (log *hcLogToLogger) IsDebug() bool {
	return true
}

func (log *hcLogToLogger) IsInfo() bool {
	return true
}

func (log *hcLogToLogger) IsWarn() bool {
	return true
}

func (log *hcLogToLogger) IsError() bool {
	return true
}

func (log *hcLogToLogger) Name() string {
	return log.name
}

func (log *hcLogToLogger) With(args ...interface{}) hclog.Logger {
	return &hcLogToLogger{extraArgs: args}
}

func (log *hcLogToLogger) Named(name string) hclog.Logger {
	return &hcLogToLogger{name: log.name + ": " + name}
}

func (log *hcLogToLogger) ResetNamed(name string) hclog.Logger {
	return &hcLogToLogger{name: name}
}

func (log *hcLogToLogger) SetLevel(level hclog.Level) {}

func (log *hcLogToLogger) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	return nil
}

func (log *hcLogToLogger) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	return nil
}

func (log *hcLogToLogger) ImpliedArgs() []interface{} {
	return nil
}

// streamLayer an implementation of raft.StreamLayer for use
// with raft.NetworkTransportConfig.
type streamLayer struct {
	host host.Host
	l    net.Listener
}

func newStreamLayer(h host.Host) (*streamLayer, error) {
	listener, err := gostream.Listen(h, RaftProtocol)
	if err != nil {
		return nil, err
	}

	return &streamLayer{
		host: h,
		l:    listener,
	}, nil
}

func (sl *streamLayer) Dial(address raft.ServerAddress, timeout time.Duration) (net.Conn, error) {
	if sl.host == nil {
		return nil, errors.New("streamLayer not initialized")
	}

	pid, err := peer.Decode(string(address))
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return gostream.Dial(ctx, sl.host, pid, RaftProtocol)
}

func (sl *streamLayer) Accept() (net.Conn, error) {
	return sl.l.Accept()
}

func (sl *streamLayer) Addr() net.Addr {
	return sl.l.Addr()
}

func (sl *streamLayer) Close() error {
	return sl.l.Close()
}

type addrProvider struct {
	h host.Host
}

// ServerAddr takes a raft.ServerID and returns it as a ServerAddress.  libp2p
// will either know how to contact that peer ID or try to find it using the
// configured routing mechanism.
func (ap *addrProvider) ServerAddr(id raft.ServerID) (raft.ServerAddress, error) {
	return raft.ServerAddress(id), nil

}

// type Libp2pTransport struct {
// 	raftTrans *raft.NetworkTransport
// }

func NewLibp2pTransport(h host.Host, timeout time.Duration) (*raft.NetworkTransport, error) {
	provider := &addrProvider{h}
	stream, err := newStreamLayer(h)
	if err != nil {
		return nil, err
	}

	// This is a configuration for raft.NetworkTransport
	// initialized with our own StreamLayer and Logger.
	// We set MaxPool to 0 so the NetworkTransport does not
	// pool connections. This allows re-using already stablished
	// TCP connections, for example, which are expensive to create.
	// We are, however, multiplexing streams over an already created
	// Libp2p connection, which is cheap. We don't need to re-use
	// streams.
	cfg := &raft.NetworkTransportConfig{
		ServerAddressProvider: provider,
		Logger:                &hcLogToLogger{},
		Stream:                stream,
		MaxPool:               0,
		Timeout:               timeout,
	}

	return raft.NewNetworkTransportWithConfig(cfg), nil
}
