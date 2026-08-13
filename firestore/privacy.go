package firestore

import (
	"context"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

type PrivacyService struct {
	client *firestore.Client
}

func NewPrivacyService(client *firestore.Client) *PrivacyService {
	return &PrivacyService{client: client}
}

func (s *PrivacyService) DeleteUserData(ctx context.Context, userID string) error {
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		query := s.client.Collection("players").Where("user_id", "==", userID)

		iter := tx.Documents(query)
		for {
			doc, err := iter.Next()
			if err != nil {
				if err == iterator.Done {
					break
				}
				return err
			}
			if err := tx.Delete(doc.Ref); err != nil {
				return err
			}
		}
		query = s.client.Collection("alliances").Where("code_channel.user_id", "==", userID)
		iter = tx.Documents(query)
		for {
			doc, err := iter.Next()
			if err != nil {
				if err == iterator.Done {
					break
				}
				return err
			}
			if err := tx.Update(doc.Ref, []firestore.Update{
				{Path: "code_channel.user_id", Value: "DELETED_USER"},
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
