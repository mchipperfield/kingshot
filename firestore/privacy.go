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
		playerQuery := s.client.Collection("players").Where("user_id", "==", userID)
		playerIter := tx.Documents(playerQuery)
		var playerRefs []*firestore.DocumentRef
		for {
			doc, err := playerIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}
			playerRefs = append(playerRefs, doc.Ref)
		}

		allianceQuery := s.client.Collection("alliances").Where("code_channel.user_id", "==", userID)
		allianceIter := tx.Documents(allianceQuery)
		var allianceRefs []*firestore.DocumentRef
		for {
			doc, err := allianceIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}
			allianceRefs = append(allianceRefs, doc.Ref)
		}

		for _, ref := range playerRefs {
			if err := tx.Delete(ref); err != nil {
				return err
			}
		}
		for _, ref := range allianceRefs {
			if err := tx.Update(ref, []firestore.Update{
				{Path: "code_channel.user_id", Value: "DELETED_USER"},
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
