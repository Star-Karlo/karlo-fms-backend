package authctx

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	// authorizationMD carries the end user's token on service-to-service calls,
	// so the callee can apply the same authorisation the caller would.
	authorizationMD = "authorization"
	// serviceTokenMD carries the calling service's own credential. This is what
	// distinguishes "the business service is asking" from "someone reached the
	// internal port". It is checked independently of the user token.
	serviceTokenMD = "x-karlo-service-token"
	// serviceNameMD is informational, for logs and metrics.
	serviceNameMD = "x-karlo-service"
)

// UnaryClientInterceptor attaches this service's credential, and forwards the
// end user's token when the call is made on behalf of a request.
func UnaryClientInterceptor(serviceName, serviceToken string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx,
			serviceTokenMD, serviceToken,
			serviceNameMD, serviceName,
		)
		if raw, ok := OutgoingToken(ctx); ok {
			ctx = metadata.AppendToOutgoingContext(ctx, authorizationMD, "Bearer "+raw)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

type outgoingTokenKey struct{}

// OutgoingToken returns the end-user token to forward, if any.
func OutgoingToken(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(outgoingTokenKey{}).(string)
	return t, ok && t != ""
}
