package api

import (
	"context"
	"fmt"
	"time"

	"github.com/gigvault/shared/api/proto/crl"
	"github.com/gigvault/shared/pkg/logger"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CRLGRPCServer implements the CRL gRPC service
type CRLGRPCServer struct {
	crl.UnimplementedCRLServiceServer
	db     *pgxpool.Pool
	logger *logger.Logger
}

// NewCRLGRPCServer creates a new CRL gRPC server
func NewCRLGRPCServer(db *pgxpool.Pool) *CRLGRPCServer {
	return &CRLGRPCServer{
		db:     db,
		logger: logger.Global(),
	}
}

// AddRevocation adds a certificate revocation to the CRL
func (s *CRLGRPCServer) AddRevocation(ctx context.Context, req *crl.AddRevocationRequest) (*crl.AddRevocationResponse, error) {
	s.logger.Info("Received AddRevocation request",
		zap.String("serial", req.SerialNumber),
		zap.String("reason", req.Reason),
	)

	// Validate input
	if req.SerialNumber == "" {
		return nil, status.Error(codes.InvalidArgument, "serial number is required")
	}

	// Insert revocation into database
	query := `
		INSERT INTO crl_entries (serial, revoked_at, reason)
		VALUES ($1, $2, $3)
		ON CONFLICT (serial) DO UPDATE SET
			revoked_at = EXCLUDED.revoked_at,
			reason = EXCLUDED.reason
	`

	revokedAt := time.Unix(req.RevokedAt.Seconds, 0)
	if req.RevokedAt == nil || req.RevokedAt.Seconds == 0 {
		revokedAt = time.Now()
	}

	_, err := s.db.Exec(ctx, query, req.SerialNumber, revokedAt, req.Reason)
	if err != nil {
		s.logger.Error("Failed to add revocation", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to add revocation")
	}

	s.logger.Info("Revocation added successfully", zap.String("serial", req.SerialNumber))

	return &crl.AddRevocationResponse{
		Success: true,
		Message: "revocation added successfully",
	}, nil
}

// GetCRL returns the current Certificate Revocation List
func (s *CRLGRPCServer) GetCRL(ctx context.Context, req *crl.GetCRLRequest) (*crl.GetCRLResponse, error) {
	s.logger.Info("Received GetCRL request")

	// Query all revoked certificates
	query := `
		SELECT serial, revoked_at, reason
		FROM crl_entries
		ORDER BY revoked_at DESC
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		s.logger.Error("Failed to query CRL entries", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to query CRL")
	}
	defer rows.Close()

	// Build CRL PEM (simplified - in production use x509.CreateRevocationList)
	var crlPEM string
	var entries []string

	for rows.Next() {
		var serial, reason string
		var revokedAt time.Time
		if err := rows.Scan(&serial, &revokedAt, &reason); err != nil {
			s.logger.Error("Failed to scan CRL entry", zap.Error(err))
			continue
		}
		entries = append(entries, fmt.Sprintf("%s,%s,%s", serial, revokedAt.Format(time.RFC3339), reason))
	}

	// In production, this should be a proper X.509 CRL
	// For now, return a simplified format
	crlPEM = "-----BEGIN X509 CRL-----\n"
	for _, entry := range entries {
		crlPEM += entry + "\n"
	}
	crlPEM += "-----END X509 CRL-----\n"

	s.logger.Info("CRL retrieved", zap.Int("entries", len(entries)))

	return &crl.GetCRLResponse{
		CrlPem: crlPEM,
	}, nil
}

// PublishCRL publishes the CRL to distribution points
func (s *CRLGRPCServer) PublishCRL(ctx context.Context, req *crl.PublishCRLRequest) (*crl.PublishCRLResponse, error) {
	s.logger.Info("Received PublishCRL request")

	// Get current CRL
	crlResp, err := s.GetCRL(ctx, &crl.GetCRLRequest{})
	if err != nil {
		return nil, err
	}

	// Update publication timestamp
	query := `
		INSERT INTO crl_metadata (id, last_published, next_update)
		VALUES (1, NOW(), NOW() + INTERVAL '24 hours')
		ON CONFLICT (id) DO UPDATE SET
			last_published = NOW(),
			next_update = NOW() + INTERVAL '24 hours'
	`

	_, err = s.db.Exec(ctx, query)
	if err != nil {
		s.logger.Error("Failed to update CRL metadata", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to publish CRL")
	}

	s.logger.Info("CRL published successfully")

	return &crl.PublishCRLResponse{
		Success: true,
		Message: fmt.Sprintf("CRL published successfully (%d bytes)", len(crlResp.CrlPem)),
	}, nil
}
