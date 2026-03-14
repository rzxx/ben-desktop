package apitypes

import "errors"

var ErrNoActiveLibrary = errors.New("no active library selected")

func IsNoActiveLibrary(err error) bool {
	return errors.Is(err, ErrNoActiveLibrary)
}
