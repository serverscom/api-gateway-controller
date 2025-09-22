package utils

import (
	"errors"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
)

func BoolPtr(v bool) *bool {
	return &v
}

func IgnoreNotFound(err error) error {
	var notFoundErr *serverscom.NotFoundError
	if errors.As(err, &notFoundErr) {
		return nil
	}
	return err
}
