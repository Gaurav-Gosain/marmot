//go:build !darwin

package driver

import "errors"

func cloneFile(src, dst string) error { return errors.ErrUnsupported }
