package cli

import (
	"errors"

	"github.com/tamnd/proofwiki-cli/proofwiki"
)

func isNotFound(err error) bool {
	return errors.Is(err, proofwiki.ErrNotFound)
}
