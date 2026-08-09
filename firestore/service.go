package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

func NewClient(ctx context.Context, projectId string) (*firestore.Client, error) {
	client, err := firestore.NewClient(ctx, projectId)
	if err != nil {
		return nil, fmt.Errorf("firestore: new client: %w", err)
	}
	return client, nil
}
