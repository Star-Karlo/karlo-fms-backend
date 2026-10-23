// Package iamclient is this service's gRPC connection to the shared
// authentication service.
//
// It exists for the two things fms-backend cannot answer on its own:
//
//   - LISTING COMPANIES for the tenant projection. That runs in a background
//     loop with no request behind it, so there is no caller's token to forward
//     and it needs a credential of its own. The platform's service-to-service
//     path is a shared service token on the gRPC metadata, not a user token —
//     a user token would tie a background sweep to one person's account and
//     stop working the day they leave.
//
//   - VALIDATING API KEYS. An integration presents a static key, not a JWT.
//     There is nothing to verify locally: the key's hash lives in
//     karlo_auth.api_keys along with the role it acts as. ValidateToken accepts
//     it — anything without two dots is treated as a static key — so this
//     service ends up with ONE way to establish a caller rather than two, and
//     the permission logic is not duplicated for machines.
package iamclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/karlo/fms-backend/internal/platform/authctx"
	authv1 "github.com/karlo/fms-backend/internal/platform/genproto/karlo/auth/v1"
	"github.com/karlo/fms-backend/internal/platform/grpcutil"
)

// Client talks to the authentication service.
type Client struct {
	conn   *grpc.ClientConn
	client authv1.AuthServiceClient
}

// New dials the authentication service, or returns nil when no target is
// configured — the state of a deployment that has not cut over.
func New(target, serviceName, serviceToken string) (*Client, error) {
	if target == "" {
		return nil, nil
	}
	conn, err := grpcutil.Dial(grpcutil.DialConfig{
		Service:      serviceName,
		Target:       target,
		ServiceToken: serviceToken,
	})
	if err != nil {
		return nil, fmt.Errorf("iamclient: %w", err)
	}
	return &Client{conn: conn, client: authv1.NewAuthServiceClient(conn)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// Company is the subset of a company this service uses.
type Company struct {
	ID          string
	Name        string
	LegalName   string
	FMSTenantID int64
	IsSuspended bool
	// APIEnabled is whether this company may be reached with an API key.
	//
	// Already resolved against the absent-means-ENABLED rule, so callers get a
	// plain answer and cannot apply the default the wrong way round. The wire
	// field has presence precisely so this decision happens once, here: key
	// authentication predates the switch, and reading "never decided" as off
	// would cut off every integration that was already running.
	APIEnabled bool
}

// ListFMSCompanies returns every company holding an FMS tenant id.
//
// Paged to exhaustion. The page size is capped server-side at 500, and reading
// only the first page would hand the background workers a truncated customer
// list — which looks correct until the deployment outgrows one page, and then
// silently stops delivering webhooks and running retention for everyone past
// it.
func (c *Client) ListFMSCompanies(ctx context.Context) ([]Company, error) {
	var out []Company
	const pageSize = 500

	for page := int32(0); ; page++ {
		resp, err := c.client.ListCompanies(ctx, &authv1.ListCompaniesRequest{
			Page:     page,
			PageSize: pageSize,
			OnlyFms:  true,
		})
		if err != nil {
			return nil, fmt.Errorf("iamclient: list companies: %w", err)
		}
		for _, co := range resp.GetCompanies() {
			// only_fms should make this impossible; checked anyway because a
			// zero here would have a worker set app.current_tenant to 0 and
			// sweep a tenant that does not exist.
			if co.GetFmsTenantId() == 0 {
				continue
			}
			// GetExternalApiEnabled() returns false for BOTH an explicit
			// false and an unset field, so the pointer is what distinguishes
			// them. nil is "never decided", which means enabled.
			apiEnabled := true
			if set := co.GetSettings(); set != nil && set.ExternalApiEnabled != nil {
				apiEnabled = *set.ExternalApiEnabled
			}
			out = append(out, Company{
				ID:          co.GetId(),
				Name:        co.GetName(),
				LegalName:   co.GetLegalName(),
				FMSTenantID: co.GetFmsTenantId(),
				IsSuspended: co.GetIsSuspended(),
				APIEnabled:  apiEnabled,
			})
		}
		if len(resp.GetCompanies()) < pageSize || int64(len(out)) >= resp.GetTotal() {
			return out, nil
		}
	}
}

// ValidateToken resolves a credential the caller could not verify locally —
// in practice an API key.
func (c *Client) ValidateToken(ctx context.Context, token string) (authctx.Principal, error) {
	resp, err := c.client.ValidateToken(ctx, &authv1.ValidateTokenRequest{Token: token})
	if err != nil {
		return authctx.Principal{}, fmt.Errorf("iamclient: validate: %w", err)
	}
	if !resp.GetValid() {
		return authctx.Principal{}, fmt.Errorf("iamclient: %s", resp.GetReason())
	}
	return principalFrom(resp.GetUser()), nil
}

// principalFrom maps the wire user onto a principal.
//
// For an API key the "user" is a stand-in: Id is the KEY's id and FullName its
// name, because there is no person behind a machine credential. That is why
// nothing here tries to resolve it as one — the id is carried so an audit trail
// can name the credential that acted.
func principalFrom(u *authv1.User) authctx.Principal {
	p := authctx.Principal{
		UserID:          u.GetId(),
		CompanyID:       u.GetCompanyId(),
		IsPlatformStaff: u.GetIsPlatformStaff(),
		FMSTenantID:     u.GetFmsTenantId(),
	}
	if access := u.GetAccess(); len(access) > 0 {
		p.Access = make(map[authctx.Product]authctx.ProductAccess, len(access))
		for product, a := range access {
			p.Access[authctx.Product(product)] = authctx.ProductAccess{
				Role:        a.GetRole(),
				Permissions: a.GetPermissions(),
				Features:    a.GetFeatures(),
				// Carried across the wire. Without it an administrator rebuilt
				// from a remote validation arrives with an empty permission
				// list and is refused everywhere.
				GrantsAll: a.GetGrantsAll(),
			}
		}
	}
	return p
}
