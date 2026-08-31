package unibox

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
)

// GetBody reads a message's full body blob. The key must match what the
// worker's StoreBody writes (users/<owner>/emails/<mailbox>/<message>.emsg),
// which is why the mailbox account id travels here alongside the owner:
// reading under any other key finds nothing and the message degrades to its
// one-line snippet.
func (s *uniboxService) GetBody(
	ctx context.Context,
	userID, emailID, id uuid.UUID,
) (*emsg.EmailBlob, error) {
	key := config.StorageEndpointEmailBody(userID, emailID, id)
	body, err := s.blob.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	obj, err := emsg.DecodeBinary(body)
	if err != nil {
		return nil, err
	}

	return obj, nil
}
