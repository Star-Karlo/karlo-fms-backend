// Package grpcutil holds the gRPC server and client plumbing shared by every
// service: consistent interceptor ordering, health checking, reflection in
// non-production, and graceful shutdown.
package grpcutil

import (
	"fmt"
	"strings"
	"time"

	"github.com/karlo/fms-backend/internal/platform/authctx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

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
