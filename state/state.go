package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/waldman/anchor/config"
)

// Store records apply events.
type Store interface {
	Write(ctx context.Context, r Record) error
}

// Record holds the result of a single apply event.
type Record struct {
	Hostname      string
	Node          string
	SHA           string
	Status        string // "success" / "failed" / "skipped"
	ApplyTime     time.Time
	DurationS     int
	DaemonVersion string
	Error         string
}

// compositeStore writes to both local file and DynamoDB, treating both as best-effort.
type compositeStore struct {
	local  Store
	dynamo Store
}

func NewCompositeStore(awscfg aws.Config, cfg *config.Config, workingDir string) Store {
	return &compositeStore{
		local:  newLocalStore(workingDir),
		dynamo: newDynamoStore(awscfg, cfg),
	}
}

func (s *compositeStore) Write(ctx context.Context, r Record) error {
	if err := s.local.Write(ctx, r); err != nil {
		slog.Error("local state write failed", "error", err)
	}
	if err := s.dynamo.Write(ctx, r); err != nil {
		slog.Error("dynamodb state write failed", "error", err)
	}
	return nil
}

// localStore writes state.json.
type localStore struct {
	dir string
}

func newLocalStore(dir string) Store {
	return &localStore{dir: dir}
}

type jsonRecord struct {
	Hostname      string `json:"hostname"`
	Node          string `json:"node"`
	SHA           string `json:"sha"`
	Status        string `json:"status"`
	ApplyTime     string `json:"apply_time"`
	DurationS     int    `json:"duration_s"`
	DaemonVersion string `json:"daemon_version"`
	Error         string `json:"error"`
}

func (s *localStore) Write(_ context.Context, r Record) error {
	data, err := json.MarshalIndent(jsonRecord{
		Hostname:      r.Hostname,
		Node:          r.Node,
		SHA:           r.SHA,
		Status:        r.Status,
		ApplyTime:     r.ApplyTime.UTC().Format(time.RFC3339),
		DurationS:     r.DurationS,
		DaemonVersion: r.DaemonVersion,
		Error:         r.Error,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "state.json"), data, 0644); err != nil {
		return fmt.Errorf("write state.json: %w", err)
	}
	return nil
}

// dynamoStore writes to DynamoDB.
type dynamoStore struct {
	client  *dynamodb.Client
	table   string
	ttlDays int
}

func newDynamoStore(awscfg aws.Config, cfg *config.Config) Store {
	return &dynamoStore{
		client:  dynamodb.NewFromConfig(awscfg),
		table:   cfg.State.DynamoDBTable,
		ttlDays: cfg.State.TTLDays,
	}
}

func (s *dynamoStore) Write(ctx context.Context, r Record) error {
	ttl := r.ApplyTime.Add(time.Duration(s.ttlDays) * 24 * time.Hour).Unix()
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item: map[string]types.AttributeValue{
			"hostname":       &types.AttributeValueMemberS{Value: r.Hostname},
			"node":           &types.AttributeValueMemberS{Value: r.Node},
			"sha":            &types.AttributeValueMemberS{Value: r.SHA},
			"status":         &types.AttributeValueMemberS{Value: r.Status},
			"apply_time":     &types.AttributeValueMemberS{Value: r.ApplyTime.UTC().Format(time.RFC3339)},
			"duration_s":     &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", r.DurationS)},
			"daemon_version": &types.AttributeValueMemberS{Value: r.DaemonVersion},
			"error":          &types.AttributeValueMemberS{Value: r.Error},
			"ttl":            &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", ttl)},
		},
	})
	if err != nil {
		return fmt.Errorf("dynamodb put item: %w", err)
	}
	return nil
}
