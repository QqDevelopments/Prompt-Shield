package engine

import (
	"context"
	"errors"
	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	health "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"time"
)

type Client struct {
	conn  *grpc.ClientConn
	rpc   pb.SecurityEngineClient
	token string
}

func New(address, token string) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 5 * time.Second, PermitWithoutStream: true}))
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, rpc: pb.NewSecurityEngineClient(conn), token: token}, nil
}
func (c *Client) Close() error { return c.conn.Close() }
func (c *Client) Inspect(ctx context.Context, in *pb.SanitizeRequest) (*pb.SanitizeResponse, error) {
	return c.rpc.InspectAndSanitize(metadata.AppendToOutgoingContext(ctx, "x-engine-token", c.token), in)
}
func (c *Client) Restore(ctx context.Context, in *pb.DeAnonymizeRequest) (*pb.DeAnonymizeResponse, error) {
	return c.rpc.DeAnonymize(metadata.AppendToOutgoingContext(ctx, "x-engine-token", c.token), in)
}
func (c *Client) Ready(ctx context.Context) error {
	response, err := health.NewHealthClient(c.conn).Check(ctx, &health.HealthCheckRequest{Service: "promptshield.v1.SecurityEngine"})
	if err == nil && response.Status != health.HealthCheckResponse_SERVING {
		return errors.New("engine_not_ready")
	}
	if err != nil {
		return err
	}
	_, err = c.Inspect(ctx, &pb.SanitizeRequest{RequestId: "readiness", OrganizationId: "readiness", Text: "health", Policy: &pb.SecurityPolicy{MaskPii: true, BlockSecrets: true}})
	return err
}
func (c *Client) ContextualReady(ctx context.Context) bool {
	response, err := health.NewHealthClient(c.conn).Check(ctx, &health.HealthCheckRequest{Service: "promptshield.v1.Ner"})
	return err == nil && response.Status == health.HealthCheckResponse_SERVING
}
