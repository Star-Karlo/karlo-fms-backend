// Package grpcutil holds the gRPC server and client plumbing shared by every
// service: consistent interceptor ordering, health checking, reflection in
// non-production, and graceful shutdown.
package grpcutil

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/karlo/fms-backend/internal/platform/authctx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// ServerConfig configures a service's gRPC listener.
type ServerConfig struct {
	Service string
	Addr    string
	// Verifier lets the server extract an end-user principal from forwarded
	// tokens. May be nil for services that never act on a user's behalf.
	Verifier *authctx.Verifier
	// AcceptedServiceTokens are the internal credentials this server honours.
	// Empty means service-token checking is disabled, which is only appropriate
	// in local development.
	AcceptedServiceTokens []string
	// EnableReflection should be false in production: reflection publishes the
	// full service schema to anyone who can reach the port.
	EnableReflection bool
}

// Server wraps a grpc.Server with its listener and health reporter.
type Server struct {
	grpc   *grpc.Server
	health *health.Server
	addr   string
}

// NewServer builds a gRPC server with the standard interceptor chain.
func NewServer(cfg ServerConfig) *Server {
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			RecoveryInterceptor(),
			LoggingInterceptor(),
			authctx.UnaryServerInterceptor(cfg.Verifier, cfg.AcceptedServiceTokens),
		),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              2 * time.Minute,
			Timeout:           20 * time.Second,
		}),
	)

	h := health.NewServer()
	healthpb.RegisterHealthServer(srv, h)

	if cfg.EnableReflection {
		reflection.Register(srv)
	}

	return &Server{grpc: srv, health: h, addr: cfg.Addr}
}

// Registrar exposes the underlying server so a service can register its own
// implementations.
func (s *Server) Registrar() grpc.ServiceRegistrar { return s.grpc }

// Serve starts listening. It blocks until the server stops.
//
// The listener is opened through a ListenConfig so that binding honours a
// context, which matters when a service is starting into a socket the previous
// instance has not yet released.
func (s *Server) Serve() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("grpcutil: listen %s: %w", s.addr, err)
	}
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	slog.Info("grpc server listening", "addr", s.addr)
	return s.grpc.Serve(lis)
}

// Shutdown stops the server, first reporting NOT_SERVING so load balancers can
// drain, then waiting for in-flight calls up to the context deadline.
func (s *Server) Shutdown(ctx context.Context) {
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)

	stopped := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-ctx.Done():
		slog.Warn("grpc graceful stop timed out, forcing")
		s.grpc.Stop()
	}
}

// RecoveryInterceptor turns a panic in a handler into an Internal error rather
// than taking the process down with it.
func RecoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("grpc handler panic", "method", info.FullMethod, "panic", r)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// LoggingInterceptor emits one structured line per call.
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		attrs := []any{
			"method", info.FullMethod,
			"duration_ms", time.Since(start).Milliseconds(),
			"code", status.Code(err).String(),
		}
		if err != nil {
			slog.Error("grpc call failed", append(attrs, "error", err.Error())...)
		} else {
			slog.Debug("grpc call", attrs...)
		}
		return resp, err
	}
}

// DialConfig configures an outbound connection to another service.
type DialConfig struct {
	// Service is this service's name, sent for observability.
	Service string
	Target  string
	// ServiceToken is this service's internal credential.
	ServiceToken string
}

// Dial opens a client connection with the standard interceptors.
//
// TLS IS CHOSEN BY THE TARGET, not by a separate flag.
//
// Insecure was right while both deployments kept the hop inside a trust
// boundary: locally the docker network, on AWS a single VPC. That stopped being
// true here. FMS runs in ap-southeast-1 and the platform services in
// ap-southeast-3, so Cloud Map's karlo.internal names do not resolve and the
// only route between them crosses the public internet — where a service token
// travelling in clear text is the whole credential.
//
// Port 443 means TLS. It is the one signal that cannot be set inconsistently
// with the address it applies to: a separate GRPC_TLS variable can be left
// false against a public hostname, and the failure is silent — the connection
// works and the token is readable in transit. An operator who writes :443 has
// already said what they mean.
//
// nil TLS config uses the host's root CAs and verifies the server name from the
// target, which is what an ALB with a real certificate needs and what a
// self-signed internal endpoint would (correctly) fail against.
func Dial(cfg DialConfig) (*grpc.ClientConn, error) {
	creds := insecure.NewCredentials()
	if strings.HasSuffix(cfg.Target, ":443") {
		creds = credentials.NewTLS(nil)
	}
	conn, err := grpc.NewClient(cfg.Target,
		grpc.WithTransportCredentials(creds),
		grpc.WithChainUnaryInterceptor(
			authctx.UnaryClientInterceptor(cfg.Service, cfg.ServiceToken),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                2 * time.Minute,
			Timeout:             20 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpcutil: dial %s: %w", cfg.Target, err)
	}
	return conn, nil
}
